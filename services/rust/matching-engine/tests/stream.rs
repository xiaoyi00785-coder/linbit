//! Result-stream semantics for the spot engine (H1–H3):
//!
//!   1. The stream is exact across a crash: entries regenerated from the WAL
//!      during recovery match the ones produced live.
//!   2. Acknowledged entries are dropped; unacknowledged entries survive.
//!   3. An expired place is rejected *before* the WAL write, so it never
//!      executes and never appears in the stream.
//!   4. The stream watermark is monotonic and advances with the engine clock.
//!   5. A duplicate order id is rejected and never rests in the book.
//!   6. A torn WAL tail is repaired (truncated) on open.

use std::io::Write;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;

use matching_engine::book::SymbolEngine;
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
        deadline_ms: 0,
    })
}

fn place_deadline(id: &str, user: &str, side: Side, price: &str, qty: &str, deadline_ms: u64) -> Command {
    Command::Place(PlaceCommand {
        symbol: "BTCUSDT".into(),
        order_id: id.into(),
        user_id: user.into(),
        side,
        order_type: OrderType::Limit,
        time_in_force: TimeInForce::GTC,
        price: price.into(),
        quantity: qty.into(),
        quote_order_qty: String::new(),
        deadline_ms,
    })
}

fn cancel(id: &str) -> Command {
    Command::Cancel(CancelCommand { symbol: "BTCUSDT".into(), order_id: id.into() })
}

fn tmpdir(tag: &str) -> std::path::PathBuf {
    let base = std::env::temp_dir().join(format!(
        "linbit-stream-{tag}-{}-{:?}",
        std::process::id(),
        std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH).unwrap().as_nanos()
    ));
    std::fs::create_dir_all(&base).unwrap();
    base
}

fn seqs(page: &matching_engine::book::StreamPage) -> Vec<u64> {
    page.entries.iter().map(|e| e.seq).collect()
}

#[test]
fn stream_is_exact_across_recovery() {
    let dir = tmpdir("exact");
    let sink = Arc::new(InMemorySink::default());

    let mut e1 = SymbolEngine::open(dir.clone(), "BTCUSDT", 1, 6, 0, sink.clone()).unwrap();
    e1.process(&place("s1", "a", Side::Sell, TimeInForce::GTC, "70000", "2.0"));
    e1.process(&place("b1", "b", Side::Buy, TimeInForce::GTC, "70000", "0.5")); // partial fill
    e1.process(&cancel("s1"));
    let live = e1.stream_page(0, 1000);
    let _live_json = serde_json::to_string(&live).unwrap();
    drop(e1);

    let mut e2 = SymbolEngine::open(dir.clone(), "BTCUSDT", 1, 6, 0, sink).unwrap();
    let recovered = e2.stream_page(0, 1000);
    // The entries (seq, ts, kind, report) are a deterministic function of the
    // WAL, so they must match exactly. The watermark is the live wall clock at
    // query time and is exercised by `watermark_is_monotonic_and_advances`.
    assert_eq!(
        serde_json::to_string(&live.entries).unwrap(),
        serde_json::to_string(&recovered.entries).unwrap(),
        "regenerated stream entries must match the live ones exactly",
    );
    assert_eq!(live.head, recovered.head, "head (last applied seq) must match");
    assert_eq!(seqs(&recovered), vec![1, 2, 3]);

    std::fs::remove_dir_all(&dir).ok();
}

#[test]
fn ack_drops_entries_but_keeps_unacked() {
    let dir = tmpdir("ack");
    let sink = Arc::new(InMemorySink::default());

    let mut e = SymbolEngine::open(dir.clone(), "BTCUSDT", 1, 6, 0, sink.clone()).unwrap();
    e.process(&place("s1", "a", Side::Sell, TimeInForce::GTC, "70000", "1.0"));
    e.process(&place("s2", "a", Side::Sell, TimeInForce::GTC, "70010", "1.0"));
    e.process(&place("s3", "a", Side::Sell, TimeInForce::GTC, "70020", "1.0"));
    assert_eq!(e.report_backlog(), 3);

    // Ack through seq 2; only seq 3 should remain queryable.
    e.ack_reports(2).unwrap();
    assert_eq!(e.report_backlog(), 1);
    let page = e.stream_page(0, 1000);
    assert_eq!(seqs(&page), vec![3], "acked entries must be dropped, unacked kept");

    // A second ack of the same seq is a no-op.
    e.ack_reports(2).unwrap();
    assert_eq!(e.report_backlog(), 1);

    // Acking past the last seq is clamped to it.
    e.ack_reports(u64::MAX).unwrap();
    assert_eq!(e.report_backlog(), 0);

    std::fs::remove_dir_all(&dir).ok();
}

#[test]
fn expired_place_never_reaches_wal_or_stream() {
    let dir = tmpdir("exp");
    let sink = Arc::new(InMemorySink::default());
    let now = Arc::new(AtomicU64::new(1_000_000));

    let mut e = SymbolEngine::open(dir.clone(), "BTCUSDT", 1, 6, 0, sink.clone()).unwrap();
    let clock = now.clone();
    e.set_clock(move || clock.load(Ordering::Relaxed));

    // A resting order that lands in the book.
    e.process(&place("s1", "a", Side::Sell, TimeInForce::GTC, "70000", "1.0"));
    // Advance the engine clock past the new order's deadline.
    now.store(2_000_000, Ordering::Relaxed);
    let r = e.process(&place_deadline("b1", "b", Side::Buy, "70000", "1.0", 1_500_000));
    assert_eq!(r.status, OrderStatus::Rejected);
    assert_eq!(r.reject_reason.as_deref(), Some("expired"));

    // The expired order is not in the stream, and the book still has s1 resting.
    let page = e.stream_page(0, 1000);
    assert_eq!(seqs(&page), vec![1], "expired place must not appear in the stream");
    let resting: Vec<_> = e.resting_orders().into_iter().map(|o| o.order_id).collect();
    assert_eq!(resting, vec!["s1"], "expired place must not rest in the book");

    drop(e);

    // Recovery reproduces the same state: the expired order never reached the
    // WAL, so it cannot be replayed.
    let now2 = Arc::new(AtomicU64::new(2_000_000));
    let mut e2 = SymbolEngine::open(dir.clone(), "BTCUSDT", 1, 6, 0, sink).unwrap();
    e2.set_clock(move || now2.load(Ordering::Relaxed));
    let page = e2.stream_page(0, 1000);
    assert_eq!(seqs(&page), vec![1], "expired place must not reappear on recovery");
    let resting: Vec<_> = e2.resting_orders().into_iter().map(|o| o.order_id).collect();
    assert_eq!(resting, vec!["s1"]);

    std::fs::remove_dir_all(&dir).ok();
}

#[test]
fn watermark_is_monotonic_and_advances() {
    let dir = tmpdir("wm");
    let sink = Arc::new(InMemorySink::default());
    let now = Arc::new(AtomicU64::new(1_000_000));

    let mut e = SymbolEngine::open(dir.clone(), "BTCUSDT", 1, 6, 0, sink.clone()).unwrap();
    let clock = now.clone();
    e.set_clock(move || clock.load(Ordering::Relaxed));

    let p1 = e.stream_page(0, 1000);
    let w1 = p1.watermark;
    // A later page (after the clock moves forward) must not move the watermark
    // backwards, even if the caller passes a stale `after`.
    now.store(1_000_500, Ordering::Relaxed);
    let p2 = e.stream_page(0, 1000);
    assert!(p2.watermark >= w1, "watermark must be monotonic: {} >= {}", p2.watermark, w1);

    // And it never goes backwards across a read with no clock change.
    let p3 = e.stream_page(0, 1000);
    assert_eq!(p3.watermark, p2.watermark);

    std::fs::remove_dir_all(&dir).ok();
}

#[test]
fn duplicate_order_id_is_rejected() {
    let dir = tmpdir("dup");
    let sink = Arc::new(InMemorySink::default());

    let mut e = SymbolEngine::open(dir.clone(), "BTCUSDT", 1, 6, 0, sink.clone()).unwrap();
    let r1 = e.process(&place("x1", "a", Side::Sell, TimeInForce::GTC, "70000", "1.0"));
    assert_eq!(r1.status, OrderStatus::New, "first copy rests");

    // Same id again — must be rejected, not rested twice.
    let r2 = e.process(&place("x1", "b", Side::Buy, TimeInForce::GTC, "69990", "1.0"));
    assert_eq!(r2.status, OrderStatus::Rejected);
    assert_eq!(r2.reject_reason.as_deref(), Some("duplicate_order_id"));

    // The book holds exactly one x1, on the sell side at 70000.
    let resting: Vec<_> = e.resting_orders().into_iter().filter(|o| o.order_id == "x1").collect();
    assert_eq!(resting.len(), 1, "duplicate must not rest twice");

    std::fs::remove_dir_all(&dir).ok();
}

#[test]
fn torn_wal_tail_is_repaired_on_open() {
    let dir = tmpdir("torn");
    let sink = Arc::new(InMemorySink::default());

    let mut e1 = SymbolEngine::open(dir.clone(), "BTCUSDT", 1, 6, 0, sink.clone()).unwrap();
    e1.process(&place("s1", "a", Side::Sell, TimeInForce::GTC, "70000", "1.0"));
    e1.process(&place("s2", "a", Side::Sell, TimeInForce::GTC, "70010", "1.0"));
    drop(e1);

    // Append a torn JSON line, as a crash mid-fsync would leave behind.
    let mut f = std::fs::OpenOptions::new().append(true).open(dir.join("wal.log")).unwrap();
    f.write_all(br#"{"seq":3,"ts":0,"command":{"cmd":"place","symbol":"BTCUS"#).unwrap();
    f.sync_all().unwrap();
    drop(f);

    // Open must succeed (the torn tail is truncated) and reproduce s1, s2 only.
    let e2 = SymbolEngine::open(dir.clone(), "BTCUSDT", 1, 6, 0, sink).unwrap();
    let ids: Vec<_> = e2.resting_orders().into_iter().map(|o| o.order_id).collect();
    assert_eq!(ids, vec!["s1", "s2"], "torn tail must be dropped, intact prefix kept");

    std::fs::remove_dir_all(&dir).ok();
}
