//! 崩溃恢复的一致性：用快照加 WAL 重建出来的引擎，必须和写出它们的那份引擎状态一致。

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
    // 比较订单簿本身和成交计数。成交计数必须一致，成交号才是确定的。
    // 序号一类的簿记在打过快照之后可以不同，不放进摘要。
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
    // 混合负载：挂单、一笔部分成交、一次撤单。
    engine.process(&place("s1", "a", Side::Sell, TimeInForce::GTC, "70010", "1.5"));
    engine.process(&place("s2", "a", Side::Sell, TimeInForce::GTC, "70000", "2.0"));
    engine.process(&place("b1", "b", Side::Buy, TimeInForce::GTC, "69990", "1.0"));
    // 部分吃掉 s2：70000 上的 2.0 里买走 0.5。
    engine.process(&place("b2", "c", Side::Buy, TimeInForce::GTC, "70000", "0.5"));
    engine.process(&Command::Cancel(CancelCommand { symbol: "BTCUSDT".into(), order_id: "b1".into() }));
    engine.process(&place("s3", "a", Side::Sell, TimeInForce::GTC, "70020", "3.0"));
}

#[test]
fn recovers_from_wal_without_snapshot() {
    let dir = tmpdir("nosnap");
    let sink = Arc::new(InMemorySink::default());

    // snapshot_every = 0：从不打快照，恢复就是纯 WAL 重放。
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
    // 第一批之后强制打快照（WAL 被截断）……
    e1.process(&place("s1", "a", Side::Sell, TimeInForce::GTC, "70010", "1.5"));
    e1.process(&place("s2", "a", Side::Sell, TimeInForce::GTC, "70000", "2.0"));
    e1.maybe_snapshot();
    // ……然后第二批只落在快照之后的 WAL 尾部。
    e1.process(&place("b2", "c", Side::Buy, TimeInForce::GTC, "70000", "0.5"));
    e1.process(&place("s3", "a", Side::Sell, TimeInForce::GTC, "70020", "3.0"));
    let before = digest(&e1.snapshot_state());
    drop(e1);

    let e2 = SymbolEngine::open(dir.clone(), "BTCUSDT", 1, 6, 1000, sink).unwrap();
    let after = digest(&e2.snapshot_state());
    assert_eq!(before, after, "snapshot + WAL-tail replay must reproduce the book");

    std::fs::remove_dir_all(&dir).ok();
}
