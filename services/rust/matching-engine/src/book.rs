//! Per-symbol order book and matching loop.
//!
//! One [`SymbolEngine`] owns the full in-memory book for a single symbol and is
//! driven by exactly one thread (the symbol's matching loop), so matching needs
//! no locks. Orders rest in price→time (FIFO) priority: bids are matched from
//! the highest price, asks from the lowest, and within a price level the oldest
//! resting order fills first.

use std::collections::{BTreeMap, HashMap, VecDeque};
use std::path::PathBuf;
use std::sync::Arc;

use serde::{Deserialize, Serialize};

use crate::bus::EventSink;
use crate::fixed;
use crate::types::*;
use crate::wal::{self, Wal, WalRecord};

/// A resting (maker) order sitting in the book.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RestingOrder {
    pub order_id: String,
    pub user_id: String,
    pub price: i128,
    /// Original base quantity.
    pub quantity: i128,
    /// Unfilled base quantity.
    pub remaining: i128,
}

/// The serializable portion of a book — everything a snapshot needs. Runtime-
/// only fields (sink, WAL handle) live on [`SymbolEngine`].
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct BookState {
    pub symbol: String,
    pub symbol_index: u64,
    pub qty_precision: u32,
    pub seq: u64,
    pub trade_counter: u64,
    pub depth_seq: u64,
    /// Price level -> FIFO queue of resting orders.
    pub bids: Vec<(i128, VecDeque<RestingOrder>)>,
    pub asks: Vec<(i128, VecDeque<RestingOrder>)>,
}

/// Matching engine for one symbol.
pub struct SymbolEngine {
    pub symbol: String,
    symbol_index: u64,
    qty_precision: u32,
    seq: u64,
    trade_counter: u64,
    depth_seq: u64,
    bids: BTreeMap<i128, VecDeque<RestingOrder>>,
    asks: BTreeMap<i128, VecDeque<RestingOrder>>,
    /// order_id -> (is_bid, price) for O(1) cancel lookups.
    index: HashMap<String, (bool, i128)>,

    sink: Arc<dyn EventSink>,
    wal: Option<Wal>,
    dir: Option<PathBuf>,
    snapshot_every: u64,
    since_snapshot: u64,
}

impl SymbolEngine {
    /// Build an in-memory engine with no persistence (used by tests and the
    /// benchmark, where WAL fsync would dominate the measurement).
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

    /// Open a persistent engine rooted at `dir`, recovering from a snapshot and
    /// replaying the WAL tail if present.
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
        // Replay any WAL records newer than the snapshot. Replay uses `apply`
        // directly: no WAL re-append, no event emission.
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

    /// Snapshot-able view of the current state.
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

    /// Process a command: write-ahead to the WAL, apply it, emit events, and
    /// snapshot if due. Returns the authoritative execution report.
    pub fn process(&mut self, cmd: &Command) -> ExecReport {
        self.seq += 1;
        if let Some(wal) = self.wal.as_mut() {
            let rec = WalRecord { seq: self.seq, command: cmd.clone() };
            if let Err(e) = wal.append(&rec) {
                // A durable WAL is a hard requirement; refuse the command.
                return reject(cmd, format!("wal append failed: {e}"));
            }
        }

        let mut touched = Vec::new();
        let report = self.apply(cmd, &mut touched);

        for t in &report.trades {
            self.sink.publish(&EngineEvent::Trade(t.clone()));
        }
        self.emit_depth(&touched);

        self.since_snapshot += 1;
        if self.snapshot_every > 0 && self.since_snapshot >= self.snapshot_every {
            self.maybe_snapshot();
        }
        report
    }

    /// Force a snapshot now and truncate the WAL.
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

    // --- core matching (no WAL, no event emission) --------------------------

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
        // Parse amounts. Validation of precision/min-notional happens in Go; a
        // parse error here means a malformed command and is rejected.
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
        // Market buys are driven by a quote budget; everything else by base qty.
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
        let order_qty = remaining; // original target (i128::MAX for market buy)

        // Market orders behave as IOC; otherwise honour the requested TIF.
        let tif = if is_market { TimeInForce::IOC } else { p.time_in_force };

        // PostOnly must not cross: reject outright if it would match.
        if tif == TimeInForce::PostOnly && self.crosses(is_buy, price) {
            return reject_place(p, &self.symbol, "post_only_would_cross");
        }
        // FOK must fill in full or not at all.
        if tif == TimeInForce::FOK && !self.fully_fillable(is_buy, price, remaining) {
            return reject_place(p, &self.symbol, "fok_unfillable");
        }

        let mut trades = Vec::new();
        let mut maker_updates = Vec::new();
        let mut taker_filled = 0;

        loop {
            // Best opposite price that still satisfies the taker's price bound.
            let best = self.best_opposite(is_buy, price, is_market);
            let Some(level_price) = best else { break };

            // Determine fill quantity against the front resting order.
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
                break; // budget can no longer buy a whole step
            }

            maker.remaining -= fill;
            taker_filled += fill;
            if market_by_quote {
                budget -= fixed::notional(level_price, fill);
            } else {
                remaining -= fill;
            }

            self.trade_counter += 1;
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

            // Stop conditions for base-quantity orders.
            if !market_by_quote && remaining == 0 {
                break;
            }
        }

        // Decide the final disposition of the taker order.
        let status = if market_by_quote {
            // Market buy: filled if the budget was effectively consumed (no more
            // can be bought), otherwise the book ran dry with budget left over.
            if self.best_opposite(true, 0, true).is_none() && budget > 0 {
                // Book ran dry with budget left; leftover is unfrozen downstream.
                OrderStatus::Canceled
            } else {
                OrderStatus::Filled
            }
        } else if remaining == 0 {
            OrderStatus::Filled
        } else {
            // Unfilled remainder.
            match tif {
                TimeInForce::GTC | TimeInForce::PostOnly => {
                    // Rest the remainder as a maker.
                    self.rest(p, price, order_qty, remaining, is_buy, touched);
                    if taker_filled > 0 {
                        OrderStatus::PartiallyFilled
                    } else {
                        OrderStatus::New
                    }
                }
                // IOC / market / FOK (FOK is guaranteed full above): discard rest.
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

    /// Best opposite price for a taker, or None if nothing is matchable.
    fn best_opposite(&self, is_buy: bool, price: i128, is_market: bool) -> Option<i128> {
        if is_buy {
            let (&ask, _) = self.asks.iter().next()?; // lowest ask
            if is_market || ask <= price {
                Some(ask)
            } else {
                None
            }
        } else {
            let (&bid, _) = self.bids.iter().next_back()?; // highest bid
            if is_market || bid >= price {
                Some(bid)
            } else {
                None
            }
        }
    }

    /// Would a resting order at `price` on `is_buy`'s side cross the book now?
    fn crosses(&self, is_buy: bool, price: i128) -> bool {
        self.best_opposite(is_buy, price, false).is_some()
    }

    /// Can `qty` be fully filled immediately within the price bound?
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

    /// Full depth snapshot of the current book (best prices first).
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
        // A buy should hit the lowest ask (70000) first.
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
        // Oldest resting order (s1) fills first.
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
        // Remainder 0.006 now rests on the bid side.
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
        // Not enough liquidity -> rejected, no trades, book untouched.
        let r = e.process(&place("b1", "b", Side::Buy, OrderType::Limit, TimeInForce::FOK, "70000", "0.01"));
        assert_eq!(r.status, OrderStatus::Rejected);
        assert!(r.trades.is_empty());
        assert_eq!(e.depth_snapshot().asks[0].quantity, "0.004");
        // Enough liquidity -> fully fills.
        e.process(&place("s2", "a", Side::Sell, OrderType::Limit, TimeInForce::GTC, "70000", "0.006"));
        let r = e.process(&place("b2", "b", Side::Buy, OrderType::Limit, TimeInForce::FOK, "70000", "0.01"));
        assert_eq!(r.status, OrderStatus::Filled);
        assert_eq!(r.trades.len(), 2);
    }

    #[test]
    fn post_only_rejects_when_crossing() {
        let mut e = eng();
        e.process(&place("s1", "a", Side::Sell, OrderType::Limit, TimeInForce::GTC, "70000", "1"));
        // Crosses -> rejected.
        let r = e.process(&place("b1", "b", Side::Buy, OrderType::Limit, TimeInForce::PostOnly, "70000", "1"));
        assert_eq!(r.status, OrderStatus::Rejected);
        // Does not cross -> rests.
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
        // Cancelling again reports not found.
        let r = e.process(&Command::Cancel(CancelCommand { symbol: "BTCUSDT".into(), order_id: "s1".into() }));
        assert_eq!(r.status, OrderStatus::Rejected);
    }

    #[test]
    fn market_buy_by_quote_budget() {
        let mut e = eng();
        e.process(&place("s1", "a", Side::Sell, OrderType::Limit, TimeInForce::GTC, "70000", "0.01"));
        e.process(&place("s2", "a", Side::Sell, OrderType::Limit, TimeInForce::GTC, "71000", "0.01"));
        // 700 quote buys exactly 0.01 at 70000.
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
