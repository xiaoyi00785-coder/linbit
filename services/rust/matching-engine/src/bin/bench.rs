//! Throughput + latency benchmark for the matching core.
//!
//! Runs the engine fully in memory (no WAL fsync, no HTTP) so the numbers
//! reflect pure matching cost. It seeds a deep resting book and then fires a
//! stream of crossing orders, recording per-command latency.
//!
//!   cargo run --release --bin bench            # default 1,000,000 orders
//!   cargo run --release --bin bench -- 2000000 # custom count
//!
//! Reports orders/sec and p50/p99/p999 latency.

use std::sync::Arc;
use std::time::Instant;

use matching_engine::book::SymbolEngine;
use matching_engine::bus::EventSink;
use matching_engine::fixed;
use matching_engine::types::*;

/// A sink that discards events — we are measuring matching, not I/O.
struct NullSink;
impl EventSink for NullSink {
    fn publish(&self, _event: &EngineEvent) {}
}

fn place(id: u64, user: &str, side: Side, price: &str, qty: &str) -> Command {
    Command::Place(PlaceCommand {
        symbol: "BTCUSDT".into(),
        order_id: id.to_string(),
        user_id: user.into(),
        side,
        order_type: OrderType::Limit,
        time_in_force: TimeInForce::GTC,
        price: price.into(),
        quantity: qty.into(),
        quote_order_qty: String::new(),
    })
}

fn main() {
    let n: usize = std::env::args().nth(1).and_then(|s| s.parse().ok()).unwrap_or(1_000_000);
    let mut engine = SymbolEngine::in_memory("BTCUSDT", 1, 6, Arc::new(NullSink));

    // Warm, deep book: resting sells spread across 1000 price levels so takers
    // keep finding liquidity throughout the run.
    let levels = 1000u64;
    let mut oid = 0u64;
    for i in 0..levels {
        let price = 70000 + i;
        oid += 1;
        engine.process(&place(oid, "maker", Side::Sell, &price.to_string(), "1000"));
    }

    println!("benchmark: {n} crossing orders against a {levels}-level book");
    let mut latencies: Vec<u64> = Vec::with_capacity(n);
    let start = Instant::now();
    let mut matched = 0u64;

    for i in 0..n {
        // Alternate: a buy that lifts the best ask, then a replenishing sell so
        // the book never fully empties. Both exercise the matching path.
        oid += 1;
        let cmd = if i % 2 == 0 {
            place(oid, "taker", Side::Buy, "71000", "1")
        } else {
            place(oid, "maker", Side::Sell, &(70000 + (i as u64 % levels)).to_string(), "1")
        };
        let t = Instant::now();
        let r = engine.process(&cmd);
        latencies.push(t.elapsed().as_nanos() as u64);
        matched += r.trades.len() as u64;
    }
    let elapsed = start.elapsed();

    latencies.sort_unstable();
    let pct = |p: f64| latencies[((latencies.len() as f64 * p) as usize).min(latencies.len() - 1)];
    let total = elapsed.as_secs_f64();
    let ops = n as f64 / total;

    println!("----------------------------------------");
    println!("orders processed : {n}");
    println!("trades generated : {matched}");
    println!("wall time        : {total:.3}s");
    println!("throughput       : {ops:.0} orders/sec");
    println!("latency p50      : {} ns ({:.3} µs)", pct(0.50), pct(0.50) as f64 / 1000.0);
    println!("latency p99      : {} ns ({:.3} µs)", pct(0.99), pct(0.99) as f64 / 1000.0);
    println!("latency p999     : {} ns ({:.3} µs)", pct(0.999), pct(0.999) as f64 / 1000.0);
    // Touch fixed to keep the import meaningful if matching changes.
    let _ = fixed::ONE;
}
