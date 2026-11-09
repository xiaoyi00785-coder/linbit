//! 引擎内外共用的传输类型和领域类型。
//!
//! 命令来自 Go 的 `order` 服务。线上的价格和数量是十进制字符串，
//! 进入某个交易对的订单簿时才解析成定点整数（见 [`crate::fixed`]）。

use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Side {
    Buy,
    Sell,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum OrderType {
    Limit,
    Market,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum TimeInForce {
    GTC,
    IOC,
    FOK,
    PostOnly,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum OrderStatus {
    New,
    PartiallyFilled,
    Filled,
    Canceled,
    Rejected,
}

/// 提交给引擎的一条命令。按原样写入 WAL，重放时走出同一串成交。
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "cmd", rename_all = "lowercase")]
pub enum Command {
    Place(PlaceCommand),
    Cancel(CancelCommand),
}

impl Command {
    pub fn symbol(&self) -> &str {
        match self {
            Command::Place(p) => &p.symbol,
            Command::Cancel(c) => &c.symbol,
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PlaceCommand {
    pub symbol: String,
    #[serde(rename = "orderId")]
    pub order_id: String,
    #[serde(rename = "userId")]
    pub user_id: String,
    pub side: Side,
    #[serde(rename = "type")]
    pub order_type: OrderType,
    #[serde(rename = "timeInForce")]
    pub time_in_force: TimeInForce,
    /// 限价，十进制字符串。市价单为空或 "0"。
    #[serde(default)]
    pub price: String,
    /// 基础资产数量，十进制字符串。按报价金额的市价买单可以不填。
    #[serde(default)]
    pub quantity: String,
    /// 市价买单的报价预算，十进制字符串。其他订单留空。
    #[serde(default, rename = "quoteOrderQty")]
    pub quote_order_qty: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CancelCommand {
    pub symbol: String,
    #[serde(rename = "orderId")]
    pub order_id: String,
}

/// 吃单（taker）和挂单（maker）的一笔成交。名义金额和手续费在下游 Go 里算；
/// 引擎只给出这笔成交的价格和数量，结算以这里为准。
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Trade {
    #[serde(rename = "tradeId")]
    pub trade_id: String,
    pub symbol: String,
    pub price: String,
    pub quantity: String,
    #[serde(rename = "takerOrderId")]
    pub taker_order_id: String,
    #[serde(rename = "makerOrderId")]
    pub maker_order_id: String,
    #[serde(rename = "takerUserId")]
    pub taker_user_id: String,
    #[serde(rename = "makerUserId")]
    pub maker_user_id: String,
    #[serde(rename = "takerSide")]
    pub taker_side: Side,
}

/// 一笔成交碰到的挂单（maker）的新状态。
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct OrderUpdate {
    #[serde(rename = "orderId")]
    pub order_id: String,
    #[serde(rename = "userId")]
    pub user_id: String,
    pub status: OrderStatus,
    #[serde(rename = "filledQuantity")]
    pub filled_quantity: String,
}

/// 处理一条命令的同步结果。作为该命令的 HTTP 响应交回订单服务，并驱动结算。
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ExecReport {
    pub symbol: String,
    #[serde(rename = "orderId")]
    pub order_id: String,
    pub status: OrderStatus,
    /// 被提交或被撤销的那张订单已成交的基础资产数量（累计）。
    #[serde(rename = "filledQuantity")]
    pub filled_quantity: String,
    pub trades: Vec<Trade>,
    /// 成交状态发生了变化的对手挂单（其他用户）。
    #[serde(rename = "makerUpdates")]
    pub maker_updates: Vec<OrderUpdate>,
    #[serde(rename = "rejectReason", skip_serializing_if = "Option::is_none")]
    pub reject_reason: Option<String>,
}

/// 深度快照或增量里的一个价位。
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Level {
    pub price: String,
    pub quantity: String,
}

/// 发到事件总线上的深度。`snapshot` 为真时是全量订单簿；否则是增量，数量 `"0"` 表示该价位已删除。
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DepthEvent {
    pub symbol: String,
    #[serde(rename = "sequence")]
    pub sequence: u64,
    pub snapshot: bool,
    pub bids: Vec<Level>,
    pub asks: Vec<Level>,
}

/// 发给下游（例如行情服务）的事件。结算以同步的 [`ExecReport`] 为准；
/// 这些事件只是推送视图，丢了也不会改余额。
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "event", rename_all = "snake_case")]
pub enum EngineEvent {
    Trade(Trade),
    Depth(DepthEvent),
}
