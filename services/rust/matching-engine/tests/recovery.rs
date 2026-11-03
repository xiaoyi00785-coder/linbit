//! Crash-recovery consistency: a fresh engine rebuilt from snapshot + WAL must
//! reproduce the exact state of the engine that wrote them.

use std::sync::Arc;

use matching_engine::book::{BookState, SymbolEngine};
use matching_engine::bus::InMemorySink;
use matching_engine::types::*;

fn place(id: &str, user: &str, side: Side, tif: TimeInForce, price: &str, qty: &str) -> Command {
    Command::Place(PlaceCommand {
        symbol: "BTCUSDT".into(),
        order_id: id.into(),
        user_id: user.into(),
        side,
        order_type: OrderType::Limit,
        time_in_force: tif,
        price: price.into(),
        quantity: qty.into(),
        quote_order_qty: String::new(),
    })
}

fn digest(state: &BookState) -> String {
    // Compare everything except seq bookkeeping that legitimately differs only
    // when the engine was snapshotted — here we compare the book itself plus the
    // trade counter (which must match for deterministic trade ids).
    serde_json::to_string(&(
        &state.bids,
        &state.asks,
        state.trade_counter,
    ))
    .unwrap()
}

fn tmpdir(tag: &str) -> std::path::PathBuf {
    let base = std::env::temp_dir().join(format!(
        "linbit-wal-{tag}-{}-{:?}",
        std::process::id(),
        std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH).unwrap().as_nanos()
    ));
    std::fs::create_dir_all(&base).unwrap();
    base
}

fn run_commands(engine: &mut SymbolEngine) {
    // Mixed workload: resting orders, a partial fill, and a cancel.
    engine.process(&place("s1", "a", Side::Sell, TimeInForce::GTC, "70010", "1.5"));
    engine.process(&place("s2", "a", Side::Sell, TimeInForce::GTC, "70000", "2.0"));
    engine.process(&place("b1", "b", Side::Buy, TimeInForce::GTC, "69990", "1.0"));
    // Crosses s2 partially (buys 0.5 of the 2.0 at 70000).
    engine.process(&place("b2", "c", Side::Buy, TimeInForce::GTC, "70000", "0.5"));
    engine.process(&Command::Cancel(CancelCommand { symbol: "BTCUSDT".into(), order_id: "b1".into() }));
    engine.process(&place("s3", "a", Side::Sell, TimeInForce::GTC, "70020", "3.0"));
}

#[test]
fn recovers_from_wal_without_snapshot() {
    let dir = tmpdir("nosnap");
    let sink = Arc::new(InMemorySink::default());

    // snapshot_every = 0 -> never snapshots, so recovery is pure WAL replay.
    let mut e1 = SymbolEngine::open(dir.clone(), "BTCUSDT", 1, 6, 0, sink.clone()).unwrap();
    run_commands(&mut e1);
    let before = digest(&e1.snapshot_state());
    drop(e1);

    let e2 = SymbolEngine::open(dir.clone(), "BTCUSDT", 1, 6, 0, sink).unwrap();
    let after = digest(&e2.snapshot_state());
    assert_eq!(before, after, "WAL replay must reproduce the book exactly");

    std::fs::remove_dir_all(&dir).ok();
}

#[test]
fn recovers_from_snapshot_plus_wal_tail() {
    let dir = tmpdir("snap");
    let sink = Arc::new(InMemorySink::default());

    let mut e1 = SymbolEngine::open(dir.clone(), "BTCUSDT", 1, 6, 1000, sink.clone()).unwrap();
    // First batch, then force a snapshot (truncates WAL)...
    e1.process(&place("s1", "a", Side::Sell, TimeInForce::GTC, "70010", "1.5"));
    e1.process(&place("s2", "a", Side::Sell, TimeInForce::GTC, "70000", "2.0"));
    e1.maybe_snapshot();
    // ...then a second batch that lands only in the post-snapshot WAL tail.
    e1.process(&place("b2", "c", Side::Buy, TimeInForce::GTC, "70000", "0.5"));
    e1.process(&place("s3", "a", Side::Sell, TimeInForce::GTC, "70020", "3.0"));
    let before = digest(&e1.snapshot_state());
    drop(e1);

    let e2 = SymbolEngine::open(dir.clone(), "BTCUSDT", 1, 6, 1000, sink).unwrap();
    let after = digest(&e2.snapshot_state());
    assert_eq!(before, after, "snapshot + WAL-tail replay must reproduce the book");

    std::fs::remove_dir_all(&dir).ok();
}
