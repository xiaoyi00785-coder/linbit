//! 单个交易对的订单簿和撮合循环。
//!
//! 一个 [`SymbolEngine`] 持有该交易对的全部内存订单簿，并且只由它自己的撮合线程驱动，
//! 所以撮合路径不加锁。挂单按价格优先、同价时间优先（FIFO）：买单从最高价吃起，
//! 卖单从最低价吃起，同一价位上更早挂上的单先成交。

use std::collections::{BTreeMap, HashMap, VecDeque};
use std::path::PathBuf;
use std::sync::Arc;

use serde::{Deserialize, Serialize};

use crate::bus::EventSink;
use crate::fixed;
use crate::types::*;
use crate::wal::{self, Wal, WalRecord};

/// 订单簿上的一笔挂单（maker）。
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RestingOrder {
    pub order_id: String,
    pub user_id: String,
    pub price: i128,
    /// 原始基础资产数量。
    pub quantity: i128,
    /// 尚未成交的基础资产数量。
    pub remaining: i128,
}

/// 订单簿里需要写入快照的部分。运行时才有的字段（事件出口、WAL 句柄）留在 [`SymbolEngine`] 上。
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct BookState {
    pub symbol: String,
    pub symbol_index: u64,
    pub qty_precision: u32,
    pub seq: u64,
    pub trade_counter: u64,
    pub depth_seq: u64,
    /// 价位 -> 该价位上按时间排队的挂单。
    pub bids: Vec<(i128, VecDeque<RestingOrder>)>,
    pub asks: Vec<(i128, VecDeque<RestingOrder>)>,
}

/// 单个交易对的撮合引擎。
pub struct SymbolEngine {
    pub symbol: String,
    symbol_index: u64,
    qty_precision: u32,
    seq: u64,
    trade_counter: u64,
    depth_seq: u64,
    bids: BTreeMap<i128, VecDeque<RestingOrder>>,
    asks: BTreeMap<i128, VecDeque<RestingOrder>>,
    /// order_id -> (是否买单, 价格)，撤单时 O(1) 定位。
    index: HashMap<String, (bool, i128)>,

    sink: Arc<dyn EventSink>,
    wal: Option<Wal>,
    dir: Option<PathBuf>,
    snapshot_every: u64,
    since_snapshot: u64,
}

impl SymbolEngine {
    /// 纯内存引擎，不落盘。测试和压测用，避免 WAL 的 fsync 盖过撮合本身的耗时。
    pub fn in_memory(symbol: &str, symbol_index: u64, qty_precision: u32, sink: Arc<dyn EventSink>) -> Self {
        Self {
            symbol: symbol.to_string(),
            symbol_index,
            qty_precision,
            seq: 0,
            trade_counter: 0,
            depth_seq: 0,
            bids: BTreeMap::new(),
            asks: BTreeMap::new(),
            index: HashMap::new(),
            sink,
            wal: None,
            dir: None,
            snapshot_every: 0,
            since_snapshot: 0,
        }
    }

    /// 打开以 `dir` 为根的持久化引擎：有快照就加载，再把快照之后的 WAL 尾部重放一遍。
    pub fn open(
        dir: PathBuf,
        symbol: &str,
        symbol_index: u64,
        qty_precision: u32,
        snapshot_every: u64,
        sink: Arc<dyn EventSink>,
    ) -> std::io::Result<Self> {
        let mut engine = Self::in_memory(symbol, symbol_index, qty_precision, sink);
        engine.dir = Some(dir.clone());
        engine.snapshot_every = snapshot_every;

        if let Some(state) = wal::read_snapshot::<BookState>(&dir)? {
            engine.load_state(state);
        }
        // 重放比快照新的 WAL。直接走 `apply`：不再追加 WAL，也不向外发事件。
        let snapshot_seq = engine.seq;
        for rec in Wal::read_all(&dir)? {
            if rec.seq > snapshot_seq {
                engine.seq = rec.seq;
                let mut touched = Vec::new();
                let _ = engine.apply(&rec.command, &mut touched);
            }
        }
        engine.wal = Some(Wal::open(&dir)?);
        Ok(engine)
    }

    fn load_state(&mut self, s: BookState) {
        self.symbol = s.symbol;
        self.symbol_index = s.symbol_index;
        self.qty_precision = s.qty_precision;
        self.seq = s.seq;
        self.trade_counter = s.trade_counter;
        self.depth_seq = s.depth_seq;
        self.bids = s.bids.into_iter().collect();
        self.asks = s.asks.into_iter().collect();
        self.index.clear();
        for (price, q) in &self.bids {
            for o in q {
                self.index.insert(o.order_id.clone(), (true, *price));
            }
        }
        for (price, q) in &self.asks {
            for o in q {
                self.index.insert(o.order_id.clone(), (false, *price));
            }
        }
    }

    /// 当前状态的快照视图。
    pub fn snapshot_state(&self) -> BookState {
        BookState {
            symbol: self.symbol.clone(),
            symbol_index: self.symbol_index,
            qty_precision: self.qty_precision,
            seq: self.seq,
            trade_counter: self.trade_counter,
            depth_seq: self.depth_seq,
            bids: self.bids.iter().map(|(p, q)| (*p, q.clone())).collect(),
            asks: self.asks.iter().map(|(p, q)| (*p, q.clone())).collect(),
        }
    }

    /// 单条命令的持久化路径：先写入 WAL 并 fsync，再改订单簿、发事件，到期则打快照。
    /// 返回的执行报告是结算依据。
    ///
    /// 批量组提交走 [`process_batch`](Self::process_batch)。这条单命令路径留给
    /// 逐条 fsync 策略、测试，以及恢复重放。
    pub fn process(&mut self, cmd: &Command) -> ExecReport {
        self.seq += 1;
        if let Some(wal) = self.wal.as_mut() {
            let rec = WalRecord { seq: self.seq, command: cmd.clone() };
            if let Err(e) = wal.append(&rec) {
                // WAL 写失败就不能确认这笔单，序号回退，订单簿不动。
                self.seq -= 1;
                return reject(cmd, format!("wal append failed: {e}"));
            }
        }

        let report = self.apply_and_emit(cmd);

        self.since_snapshot += 1;
        if self.snapshot_every > 0 && self.since_snapshot >= self.snapshot_every {
            self.maybe_snapshot();
        }
        report
    }

    /// 组提交：先把本批每条命令的 WAL 记录写进文件，整批只 fsync 一次，成功之后才改订单簿、
    /// 发事件并生成报告。报告顺序与 `cmds` 一致。
    ///
    /// 持久化语义和 [`process`](Self::process) 相同：这次 fsync 返回之前，批内任何命令都不会被应用、
    /// 发出或由调用方确认。fsync 之前崩溃，整批（尚未确认）丢失，恢复时丢掉残缺的尾部；
    /// fsync 之后崩溃，每条命令都能原样恢复。
    ///
    /// WAL 写入或 fsync 失败则整批拒绝：序号不前进，每条命令一份拒绝报告，订单簿保持原样。
    pub fn process_batch(&mut self, cmds: &[Command]) -> Vec<ExecReport> {
        if cmds.is_empty() {
            return Vec::new();
        }

        // 第一阶段：全部记录先落文件，再一次 fsync。任何 WAL 错误都不会碰到订单簿，seq 保持不动。
        if let Some(wal) = self.wal.as_mut() {
            let base = self.seq;
            let mut err: Option<String> = None;
            for (i, cmd) in cmds.iter().enumerate() {
                let rec = WalRecord { seq: base + i as u64 + 1, command: cmd.clone() };
                if let Err(e) = wal.append_buffered(&rec) {
                    err = Some(format!("wal append failed: {e}"));
                    break;
                }
            }
            if err.is_none() {
                if let Err(e) = wal.sync() {
                    err = Some(format!("wal sync failed: {e}"));
                }
            }
            if let Some(msg) = err {
                return cmds.iter().map(|c| reject(c, msg.clone())).collect();
            }
        }

        // 第二阶段：整批已经落盘，逐条应用、发事件、出报告。
        let mut reports = Vec::with_capacity(cmds.len());
        for cmd in cmds {
            self.seq += 1;
            reports.push(self.apply_and_emit(cmd));
        }

        // 每批只检查一次快照。刚应用的命令都已经 fsync，之后截断 WAL 是安全的。
        // 超出 `snapshot_every` 的部分不会超过本批的条数。
        self.since_snapshot += cmds.len() as u64;
        if self.snapshot_every > 0 && self.since_snapshot >= self.snapshot_every {
            self.maybe_snapshot();
        }
        reports
    }

    /// 把一条已经落盘的命令应用到订单簿并发布事件。
    /// [`process`](Self::process) 和 [`process_batch`](Self::process_batch) 共用。
    /// 调用前 `self.seq` 已经为这条命令前进过，这里不再写 WAL，也不打快照。
    fn apply_and_emit(&mut self, cmd: &Command) -> ExecReport {
        let mut touched = Vec::new();
        let report = self.apply(cmd, &mut touched);
        for t in &report.trades {
            self.sink.publish(&EngineEvent::Trade(t.clone()));
        }
        self.emit_depth(&touched);
        report
    }

    /// 立刻写快照并截断 WAL。快照写失败时不截断，避免只剩一段对不上的日志。
    pub fn maybe_snapshot(&mut self) {
        let Some(dir) = self.dir.clone() else { return };
        let state = self.snapshot_state();
        if let Err(e) = wal::write_snapshot(&dir, &state) {
            eprintln!("[{}] snapshot failed: {e}", self.symbol);
            return;
        }
        if let Some(wal) = self.wal.as_mut() {
            if let Err(e) = wal.truncate() {
                eprintln!("[{}] wal truncate failed: {e}", self.symbol);
            }
        }
        self.since_snapshot = 0;
    }

    // --- 撮合本身：不写 WAL，也不发事件 ------------------------------------

    fn apply(&mut self, cmd: &Command, touched: &mut Vec<(bool, i128)>) -> ExecReport {
        match cmd {
            Command::Place(p) => self.apply_place(p, touched),
            Command::Cancel(c) => self.apply_cancel(c, touched),
        }
    }

    fn apply_cancel(&mut self, c: &CancelCommand, touched: &mut Vec<(bool, i128)>) -> ExecReport {
        let Some((is_bid, price)) = self.index.remove(&c.order_id) else {
            return ExecReport {
                symbol: self.symbol.clone(),
                order_id: c.order_id.clone(),
                status: OrderStatus::Rejected,
                filled_quantity: fixed::format(0),
                trades: vec![],
                maker_updates: vec![],
                reject_reason: Some("not_found".into()),
            };
        };
        let book = if is_bid { &mut self.bids } else { &mut self.asks };
        let mut filled = 0;
        if let Some(level) = book.get_mut(&price) {
            if let Some(pos) = level.iter().position(|o| o.order_id == c.order_id) {
                let o = level.remove(pos).unwrap();
                filled = o.quantity - o.remaining;
            }
            if level.is_empty() {
                book.remove(&price);
            }
        }
        touched.push((is_bid, price));
        ExecReport {
            symbol: self.symbol.clone(),
            order_id: c.order_id.clone(),
            status: OrderStatus::Canceled,
            filled_quantity: fixed::format(filled),
            trades: vec![],
            maker_updates: vec![],
            reject_reason: None,
        }
    }

    fn apply_place(&mut self, p: &PlaceCommand, touched: &mut Vec<(bool, i128)>) -> ExecReport {
        // 解析价格和数量。精度和最小名义金额在 Go 订单服务校验；这里解析失败就是畸形命令，直接拒绝。
        let price = if p.order_type == OrderType::Market || p.price.is_empty() {
            0
        } else {
            match fixed::parse(&p.price) {
                Ok(v) if v > 0 => v,
                _ => return reject_place(p, &self.symbol, "bad price"),
            }
        };
        let is_market = p.order_type == OrderType::Market;
        let is_buy = p.side == Side::Buy;
        // 市价买单按报价预算成交，其余订单按基础资产数量成交。
        let market_by_quote = is_market && is_buy;

        let (mut remaining, mut budget) = if market_by_quote {
            match fixed::parse(&p.quote_order_qty) {
                Ok(v) if v > 0 => (i128::MAX, v),
                _ => return reject_place(p, &self.symbol, "bad quoteOrderQty"),
            }
        } else {
            match fixed::parse(&p.quantity) {
                Ok(v) if v > 0 => (v, 0),
                _ => return reject_place(p, &self.symbol, "bad quantity"),
            }
        };
        let order_qty = remaining; // 原始目标数量；按报价的市价买单这里是 i128::MAX

        // 市价单一律按 IOC 处理，限价单尊重请求里的有效期。
        let tif = if is_market { TimeInForce::IOC } else { p.time_in_force };

        // 只做 maker：会立刻成交就整单拒绝，不部分成交。
        if tif == TimeInForce::PostOnly && self.crosses(is_buy, price) {
            return reject_place(p, &self.symbol, "post_only_would_cross");
        }
        // FOK：不能一次全部成交就拒绝，订单簿不动。
        if tif == TimeInForce::FOK && !self.fully_fillable(is_buy, price, remaining) {
            return reject_place(p, &self.symbol, "fok_unfillable");
        }

        let mut trades = Vec::new();
        let mut maker_updates = Vec::new();
        let mut taker_filled = 0;

        loop {
            // 对手盘里仍满足吃单方价格限制的最优价。市价单不设价格上限。
            let best = self.best_opposite(is_buy, price, is_market);
            let Some(level_price) = best else { break };

            // 与该价位队首挂单计算本次成交量。
            let opp = if is_buy { &mut self.asks } else { &mut self.bids };
            let level = opp.get_mut(&level_price).expect("level exists");
            let maker = level.front_mut().expect("non-empty level");

            let fill = if market_by_quote {
                let by_budget = fixed::floor_to(fixed::qty_for_budget(budget, level_price), self.qty_precision);
                maker.remaining.min(by_budget)
            } else {
                remaining.min(maker.remaining)
            };
            if fill <= 0 {
                break; // 剩余预算已经买不满一个数量步长
            }

            maker.remaining -= fill;
            taker_filled += fill;
            if market_by_quote {
                budget -= fixed::notional(level_price, fill);
            } else {
                remaining -= fill;
            }

            self.trade_counter += 1;
            // 成交号 = 交易对在 SYMBOLS 里的下标 * 10^12 + 该交易对自己的递增计数。
            // 只能在列表末尾追加交易对，否则历史成交号会落到别的交易对上。
            let trade_id = self.symbol_index * 1_000_000_000_000 + self.trade_counter;
            trades.push(Trade {
                trade_id: trade_id.to_string(),
                symbol: self.symbol.clone(),
                price: fixed::format(level_price),
                quantity: fixed::format(fill),
                taker_order_id: p.order_id.clone(),
                maker_order_id: maker.order_id.clone(),
                taker_user_id: p.user_id.clone(),
                maker_user_id: maker.user_id.clone(),
                taker_side: p.side,
            });
            let maker_status = if maker.remaining == 0 {
                OrderStatus::Filled
            } else {
                OrderStatus::PartiallyFilled
            };
            maker_updates.push(OrderUpdate {
                order_id: maker.order_id.clone(),
                user_id: maker.user_id.clone(),
                status: maker_status,
                filled_quantity: fixed::format(maker.quantity - maker.remaining),
            });

            if maker.remaining == 0 {
                let done = level.pop_front().unwrap();
                self.index.remove(&done.order_id);
                if level.is_empty() {
                    opp.remove(&level_price);
                }
            }
            touched.push((!is_buy, level_price));

            // 按数量下的单：剩余数量吃完就停。按报价的市价买单停在预算买不满一步，或对手盘耗尽。
            if !market_by_quote && remaining == 0 {
                break;
            }
        }

        // 决定吃单方的最终状态。
        let status = if market_by_quote {
            // 市价买单：预算已经买不满下一步视为全部成交；对手盘耗尽且预算还有剩余则取消，
            // 剩余冻结由下游解冻。
            if self.best_opposite(true, 0, true).is_none() && budget > 0 {
                OrderStatus::Canceled
            } else {
                OrderStatus::Filled
            }
        } else if remaining == 0 {
            OrderStatus::Filled
        } else {
            // 还有未成交数量。
            match tif {
                TimeInForce::GTC | TimeInForce::PostOnly => {
                    // 剩余部分挂成 maker。
                    self.rest(p, price, order_qty, remaining, is_buy, touched);
                    if taker_filled > 0 {
                        OrderStatus::PartiallyFilled
                    } else {
                        OrderStatus::New
                    }
                }
                // IOC、市价，以及上面已经保证能全部成交的 FOK：剩余部分丢掉，不挂单。
                _ => OrderStatus::Canceled,
            }
        };

        let filled_report = if market_by_quote {
            fixed::format(taker_filled)
        } else {
            fixed::format(order_qty - remaining)
        };
        ExecReport {
            symbol: self.symbol.clone(),
            order_id: p.order_id.clone(),
            status,
            filled_quantity: filled_report,
            trades,
            maker_updates,
            reject_reason: None,
        }
    }

    fn rest(&mut self, p: &PlaceCommand, price: i128, quantity: i128, remaining: i128, is_buy: bool, touched: &mut Vec<(bool, i128)>) {
        let book = if is_buy { &mut self.bids } else { &mut self.asks };
        book.entry(price).or_default().push_back(RestingOrder {
            order_id: p.order_id.clone(),
            user_id: p.user_id.clone(),
            price,
            quantity,
            remaining,
        });
        self.index.insert(p.order_id.clone(), (is_buy, price));
        touched.push((is_buy, price));
    }

    /// 吃单方能碰到的最优对手价。没有可成交档位时返回 None。
    /// 买单看最低卖价（BTreeMap 的首档），卖单看最高买价（末档）。
    fn best_opposite(&self, is_buy: bool, price: i128, is_market: bool) -> Option<i128> {
        if is_buy {
            let (&ask, _) = self.asks.iter().next()?; // 最低卖价
            if is_market || ask <= price {
                Some(ask)
            } else {
                None
            }
        } else {
            let (&bid, _) = self.bids.iter().next_back()?; // 最高买价
            if is_market || bid >= price {
                Some(bid)
            } else {
                None
            }
        }
    }

    /// 在 `is_buy` 这一侧、价格为 `price` 的挂单现在会不会立刻碰到对手盘。
    fn crosses(&self, is_buy: bool, price: i128) -> bool {
        self.best_opposite(is_buy, price, false).is_some()
    }

    /// 在价格限制内，`qty` 能不能立刻全部成交。FOK 在改订单簿之前用它做预检。
    fn fully_fillable(&self, is_buy: bool, price: i128, qty: i128) -> bool {
        let mut need = qty;
        if is_buy {
            for (&ask, level) in self.asks.iter() {
                if ask > price {
                    break;
                }
                for o in level {
                    need -= o.remaining;
                    if need <= 0 {
                        return true;
                    }
                }
            }
        } else {
            for (&bid, level) in self.bids.iter().rev() {
                if bid < price {
                    break;
                }
                for o in level {
                    need -= o.remaining;
                    if need <= 0 {
                        return true;
                    }
                }
            }
        }
        need <= 0
    }

    fn emit_depth(&mut self, touched: &[(bool, i128)]) {
        if touched.is_empty() {
            return;
        }
        let mut seen = std::collections::HashSet::new();
        let mut bids = Vec::new();
        let mut asks = Vec::new();
        for &(is_bid, price) in touched {
            if !seen.insert((is_bid, price)) {
                continue;
            }
            let book = if is_bid { &self.bids } else { &self.asks };
            // 价位被吃空时数量记 0，下游用 0 删除这一档。
            let qty: i128 = book.get(&price).map(|q| q.iter().map(|o| o.remaining).sum()).unwrap_or(0);
            let level = Level { price: fixed::format(price), quantity: fixed::format(qty) };
            if is_bid {
                bids.push(level);
            } else {
                asks.push(level);
            }
        }
        self.depth_seq += 1;
        self.sink.publish(&EngineEvent::Depth(DepthEvent {
            symbol: self.symbol.clone(),
            sequence: self.depth_seq,
            snapshot: false,
            bids,
            asks,
        }));
    }

    /// 当前订单簿的全量深度。买盘从高价到低价，卖盘从低价到高价。
    pub fn depth_snapshot(&self) -> DepthEvent {
        let bids = self
            .bids
            .iter()
            .rev()
            .map(|(p, q)| Level {
                price: fixed::format(*p),
                quantity: fixed::format(q.iter().map(|o| o.remaining).sum()),
            })
            .collect();
        let asks = self
            .asks
            .iter()
            .map(|(p, q)| Level {
                price: fixed::format(*p),
                quantity: fixed::format(q.iter().map(|o| o.remaining).sum()),
            })
            .collect();
        DepthEvent { symbol: self.symbol.clone(), sequence: self.depth_seq, snapshot: true, bids, asks }
    }
}

fn reject(cmd: &Command, reason: String) -> ExecReport {
    ExecReport {
        symbol: cmd.symbol().to_string(),
        order_id: match cmd {
            Command::Place(p) => p.order_id.clone(),
            Command::Cancel(c) => c.order_id.clone(),
        },
        status: OrderStatus::Rejected,
        filled_quantity: "0".into(),
        trades: vec![],
        maker_updates: vec![],
        reject_reason: Some(reason),
    }
}

fn reject_place(p: &PlaceCommand, symbol: &str, reason: &str) -> ExecReport {
    ExecReport {
        symbol: symbol.to_string(),
        order_id: p.order_id.clone(),
        status: OrderStatus::Rejected,
        filled_quantity: "0".into(),
        trades: vec![],
        maker_updates: vec![],
        reject_reason: Some(reason.into()),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::bus::InMemorySink;

    fn eng() -> SymbolEngine {
        SymbolEngine::in_memory("BTCUSDT", 1, 6, Arc::new(InMemorySink::default()))
    }

    fn place(id: &str, user: &str, side: Side, ty: OrderType, tif: TimeInForce, price: &str, qty: &str) -> Command {
        Command::Place(PlaceCommand {
            symbol: "BTCUSDT".into(),
            order_id: id.into(),
            user_id: user.into(),
            side,
            order_type: ty,
            time_in_force: tif,
            price: price.into(),
            quantity: qty.into(),
            quote_order_qty: String::new(),
        })
    }

    fn market_buy_quote(id: &str, user: &str, quote: &str) -> Command {
        Command::Place(PlaceCommand {
            symbol: "BTCUSDT".into(),
            order_id: id.into(),
            user_id: user.into(),
            side: Side::Buy,
            order_type: OrderType::Market,
            time_in_force: TimeInForce::IOC,
            price: String::new(),
            quantity: String::new(),
            quote_order_qty: quote.into(),
        })
    }

    #[test]
    fn resting_limit_then_cross_fills() {
        let mut e = eng();
        let r = e.process(&place("s1", "alice", Side::Sell, OrderType::Limit, TimeInForce::GTC, "70000", "0.01"));
        assert_eq!(r.status, OrderStatus::New);
        assert!(r.trades.is_empty());

        let r = e.process(&place("b1", "bob", Side::Buy, OrderType::Limit, TimeInForce::GTC, "70000", "0.01"));
        assert_eq!(r.status, OrderStatus::Filled);
        assert_eq!(r.trades.len(), 1);
        let t = &r.trades[0];
        assert_eq!(t.price, "70000");
        assert_eq!(t.quantity, "0.01");
        assert_eq!(t.maker_order_id, "s1");
        assert_eq!(t.taker_order_id, "b1");
        assert_eq!(t.taker_side, Side::Buy);
        assert_eq!(r.maker_updates[0].status, OrderStatus::Filled);
    }

    #[test]
    fn price_priority_best_price_first() {
        let mut e = eng();
        e.process(&place("s_high", "a", Side::Sell, OrderType::Limit, TimeInForce::GTC, "70010", "1"));
        e.process(&place("s_low", "b", Side::Sell, OrderType::Limit, TimeInForce::GTC, "70000", "1"));
        // 买单应先打到最低卖价 70000。
        let r = e.process(&place("b1", "c", Side::Buy, OrderType::Limit, TimeInForce::GTC, "70010", "1"));
        assert_eq!(r.trades[0].maker_order_id, "s_low");
        assert_eq!(r.trades[0].price, "70000");
    }

    #[test]
    fn time_priority_fifo_within_level() {
        let mut e = eng();
        e.process(&place("s1", "a", Side::Sell, OrderType::Limit, TimeInForce::GTC, "70000", "1"));
        e.process(&place("s2", "b", Side::Sell, OrderType::Limit, TimeInForce::GTC, "70000", "1"));
        let r = e.process(&place("b1", "c", Side::Buy, OrderType::Limit, TimeInForce::GTC, "70000", "1"));
        // 同一价位上更早的 s1 先成交。
        assert_eq!(r.trades.len(), 1);
        assert_eq!(r.trades[0].maker_order_id, "s1");
    }

    #[test]
    fn partial_fill_rests_remainder() {
        let mut e = eng();
        e.process(&place("s1", "a", Side::Sell, OrderType::Limit, TimeInForce::GTC, "70000", "0.004"));
        let r = e.process(&place("b1", "b", Side::Buy, OrderType::Limit, TimeInForce::GTC, "70000", "0.01"));
        assert_eq!(r.status, OrderStatus::PartiallyFilled);
        assert_eq!(r.filled_quantity, "0.004");
        // 剩余 0.006 挂在买盘。
        let d = e.depth_snapshot();
        assert_eq!(d.bids.len(), 1);
        assert_eq!(d.bids[0].quantity, "0.006");
    }

    #[test]
    fn ioc_partial_then_cancels_remainder() {
        let mut e = eng();
        e.process(&place("s1", "a", Side::Sell, OrderType::Limit, TimeInForce::GTC, "70000", "0.004"));
        let r = e.process(&place("b1", "b", Side::Buy, OrderType::Limit, TimeInForce::IOC, "70000", "0.01"));
        assert_eq!(r.status, OrderStatus::Canceled);
        assert_eq!(r.filled_quantity, "0.004");
        assert!(e.bids.is_empty(), "IOC remainder must not rest");
    }

    #[test]
    fn fok_all_or_nothing() {
        let mut e = eng();
        e.process(&place("s1", "a", Side::Sell, OrderType::Limit, TimeInForce::GTC, "70000", "0.004"));
        // 流动性不够：拒绝，没有成交，订单簿不动。
        let r = e.process(&place("b1", "b", Side::Buy, OrderType::Limit, TimeInForce::FOK, "70000", "0.01"));
        assert_eq!(r.status, OrderStatus::Rejected);
        assert!(r.trades.is_empty());
        assert_eq!(e.depth_snapshot().asks[0].quantity, "0.004");
        // 流动性够了就全部成交。
        e.process(&place("s2", "a", Side::Sell, OrderType::Limit, TimeInForce::GTC, "70000", "0.006"));
        let r = e.process(&place("b2", "b", Side::Buy, OrderType::Limit, TimeInForce::FOK, "70000", "0.01"));
        assert_eq!(r.status, OrderStatus::Filled);
        assert_eq!(r.trades.len(), 2);
    }

    #[test]
    fn post_only_rejects_when_crossing() {
        let mut e = eng();
        e.process(&place("s1", "a", Side::Sell, OrderType::Limit, TimeInForce::GTC, "70000", "1"));
        // 会吃单，拒绝。
        let r = e.process(&place("b1", "b", Side::Buy, OrderType::Limit, TimeInForce::PostOnly, "70000", "1"));
        assert_eq!(r.status, OrderStatus::Rejected);
        // 不会吃单，挂上。
        let r = e.process(&place("b2", "b", Side::Buy, OrderType::Limit, TimeInForce::PostOnly, "69990", "1"));
        assert_eq!(r.status, OrderStatus::New);
    }

    #[test]
    fn cancel_removes_resting_order() {
        let mut e = eng();
        e.process(&place("s1", "a", Side::Sell, OrderType::Limit, TimeInForce::GTC, "70000", "1"));
        let r = e.process(&Command::Cancel(CancelCommand { symbol: "BTCUSDT".into(), order_id: "s1".into() }));
        assert_eq!(r.status, OrderStatus::Canceled);
        assert!(e.asks.is_empty());
        // 再撤一次报 not_found。
        let r = e.process(&Command::Cancel(CancelCommand { symbol: "BTCUSDT".into(), order_id: "s1".into() }));
        assert_eq!(r.status, OrderStatus::Rejected);
    }

    #[test]
    fn market_buy_by_quote_budget() {
        let mut e = eng();
        e.process(&place("s1", "a", Side::Sell, OrderType::Limit, TimeInForce::GTC, "70000", "0.01"));
        e.process(&place("s2", "a", Side::Sell, OrderType::Limit, TimeInForce::GTC, "71000", "0.01"));
        // 700 报价在 70000 正好买到 0.01。
        let r = e.process(&market_buy_quote("mb", "b", "700"));
        assert_eq!(r.status, OrderStatus::Filled);
        assert_eq!(r.trades.len(), 1);
        assert_eq!(r.trades[0].quantity, "0.01");
        assert_eq!(r.filled_quantity, "0.01");
    }

    #[test]
    fn trade_ids_are_namespaced_and_monotonic() {
        let mut e = eng();
        e.process(&place("s1", "a", Side::Sell, OrderType::Limit, TimeInForce::GTC, "70000", "0.02"));
        let r = e.process(&place("b1", "b", Side::Buy, OrderType::Limit, TimeInForce::GTC, "70000", "0.01"));
        assert_eq!(r.trades[0].trade_id, "1000000000001");
        let r = e.process(&place("b2", "b", Side::Buy, OrderType::Limit, TimeInForce::GTC, "70000", "0.01"));
        assert_eq!(r.trades[0].trade_id, "1000000000002");
    }
}
