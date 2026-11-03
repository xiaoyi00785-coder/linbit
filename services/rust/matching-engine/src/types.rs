//! Wire and domain types shared across the engine.
//!
//! Commands arrive from the Go `order` service; prices and quantities are
//! decimal strings on the wire and parsed into scaled integers (see
//! [`crate::fixed`]) when a command is admitted to a symbol's book.

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

/// A command submitted to the engine. Serialized to the WAL verbatim so that
/// replay reproduces the exact same sequence of matches.
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
    /// Limit price (decimal string). Empty / "0" for market orders.
    #[serde(default)]
    pub price: String,
    /// Base quantity (decimal string). Required except for market-buy-by-quote.
    #[serde(default)]
    pub quantity: String,
    /// Quote budget for a market buy (decimal string); empty otherwise.
    #[serde(default, rename = "quoteOrderQty")]
    pub quote_order_qty: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CancelCommand {
    pub symbol: String,
    #[serde(rename = "orderId")]
    pub order_id: String,
}

/// A single match between an aggressing (taker) order and a resting (maker)
/// order. Money math (notional/fees) is done downstream in Go; the engine only
/// reports the authoritative price and quantity of each fill.
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

/// Updated state of a resting (maker) order touched by a match.
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

/// The synchronous result of processing one command — returned to the order
/// service as the command's HTTP response and used to drive settlement.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ExecReport {
    pub symbol: String,
    #[serde(rename = "orderId")]
    pub order_id: String,
    pub status: OrderStatus,
    /// Cumulative filled base quantity of the submitted/canceled order.
    #[serde(rename = "filledQuantity")]
    pub filled_quantity: String,
    pub trades: Vec<Trade>,
    /// Resting orders (other users) whose fill state changed.
    #[serde(rename = "makerUpdates")]
    pub maker_updates: Vec<OrderUpdate>,
    #[serde(rename = "rejectReason", skip_serializing_if = "Option::is_none")]
    pub reject_reason: Option<String>,
}

/// One price level in a depth snapshot/delta.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Level {
    pub price: String,
    pub quantity: String,
}

/// A depth event emitted on the event bus. `snapshot` carries the full book;
/// otherwise the entry is a delta (quantity "0" means the level was removed).
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DepthEvent {
    pub symbol: String,
    #[serde(rename = "sequence")]
    pub sequence: u64,
    pub snapshot: bool,
    pub bids: Vec<Level>,
    pub asks: Vec<Level>,
}

/// Events published on the (replaceable) event bus for downstream consumers
/// such as market-data. The authoritative settlement path uses the synchronous
/// [`ExecReport`]; these events are the streaming/pub-sub view.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "event", rename_all = "snake_case")]
pub enum EngineEvent {
    Trade(Trade),
    Depth(DepthEvent),
}
