//! Group-commit durability: batching the fsync must not change what the engine
//! does. These tests pin three properties of `process_batch`:
//!
//!   1. Crash recovery under group commit reproduces the exact pre-crash book,
//!      both from a pure WAL replay and from a snapshot + WAL tail.
//!   2. Group commit is observationally identical to per-command fsync — the same
//!      command stream yields the same reports and the same final book, however
//!      it is chopped into batches (including batch boundaries that straddle the
//!      snapshot threshold).
//!   3. A batch that was not fully fsynced (a torn tail, i.e. a crash mid-commit)
//!      is dropped wholesale on recovery, so an unacknowledged batch never
//!      half-applies.

use std::io::Write;
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

fn cancel(id: &str) -> Command {
    Command::Cancel(CancelCommand { symbol: "BTCUSDT".into(), order_id: id.into() })
}

/// A reasonably busy mixed workload: resting orders on both sides, crossing
/// takers that partially fill makers, and a cancel. Enough to exercise trade-id
/// namespacing, partial fills, and book mutations.
fn workload() -> Vec<Command> {
    vec![
        place("s1", "a", Side::Sell, TimeInForce::GTC, "70010", "1.5"),
        place("s2", "a", Side::Sell, TimeInForce::GTC, "70000", "2.0"),
        place("b1", "b", Side::Buy, TimeInForce::GTC, "69990", "1.0"),
        place("b2", "c", Side::Buy, TimeInForce::GTC, "70000", "0.5"), // partial-fills s2
        cancel("b1"),
        place("s3", "a", Side::Sell, TimeInForce::GTC, "70020", "3.0"),
        place("b3", "d", Side::Buy, TimeInForce::GTC, "70010", "2.0"), // sweeps 70000 then 70010
        place("s4", "e", Side::Sell, TimeInForce::IOC, "69995", "10.0"), // IOC against bids
        cancel("nope"), // rejected-not-found, still recorded in the WAL
        place("b4", "f", Side::Buy, TimeInForce::GTC, "70005", "0.25"),
    ]
}

fn digest(state: &BookState) -> String {
    serde_json::to_string(&(&state.bids, &state.asks, state.trade_counter, state.seq)).unwrap()
}

fn tmpdir(tag: &str) -> std::path::PathBuf {
    let base = std::env::temp_dir().join(format!(
        "linbit-gc-{tag}-{}-{:?}",
        std::process::id(),
        std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH).unwrap().as_nanos()
    ));
    std::fs::create_dir_all(&base).unwrap();
    base
}

/// Feed `cmds` through `process_batch` in fixed-size chunks (the last chunk may
/// be short — a real batch boundary).
fn run_in_chunks(engine: &mut SymbolEngine, cmds: &[Command], chunk: usize) -> Vec<ExecReport> {
    let mut reports = Vec::new();
    for group in cmds.chunks(chunk.max(1)) {
        reports.extend(engine.process_batch(group));
    }
    reports
}

#[test]
fn group_commit_recovers_from_wal_without_snapshot() {
    let dir = tmpdir("nosnap");
    let sink = Arc::new(InMemorySink::default());

    // snapshot_every = 0 -> pure WAL replay on recovery.
    let mut e1 = SymbolEngine::open(dir.clone(), "BTCUSDT", 1, 6, 0, sink.clone()).unwrap();
    run_in_chunks(&mut e1, &workload(), 4);
    let before = digest(&e1.snapshot_state());
    drop(e1);

    let e2 = SymbolEngine::open(dir.clone(), "BTCUSDT", 1, 6, 0, sink).unwrap();
    assert_eq!(before, digest(&e2.snapshot_state()), "group-commit WAL replay must reproduce the book");

    std::fs::remove_dir_all(&dir).ok();
}

#[test]
fn group_commit_recovers_from_snapshot_plus_wal_tail() {
    let dir = tmpdir("snap");
    let sink = Arc::new(InMemorySink::default());

    let mut e1 = SymbolEngine::open(dir.clone(), "BTCUSDT", 1, 6, 1000, sink.clone()).unwrap();
    let cmds = workload();
    // First batch, forced snapshot (truncates WAL), then a second batch landing
    // only in the post-snapshot WAL tail.
    e1.process_batch(&cmds[..4]);
    e1.maybe_snapshot();
    run_in_chunks(&mut e1, &cmds[4..], 3);
    let before = digest(&e1.snapshot_state());
    drop(e1);

    let e2 = SymbolEngine::open(dir.clone(), "BTCUSDT", 1, 6, 1000, sink).unwrap();
    assert_eq!(before, digest(&e2.snapshot_state()), "snapshot + group-commit tail must reproduce the book");

    std::fs::remove_dir_all(&dir).ok();
}

#[test]
fn group_commit_is_identical_to_per_command() {
    let cmds = workload();

    // Reference: per-command fsync, one command at a time.
    let dir_ref = tmpdir("ref");
    let mut reference = SymbolEngine::open(dir_ref.clone(), "BTCUSDT", 1, 6, 0, Arc::new(InMemorySink::default())).unwrap();
    let ref_reports: Vec<ExecReport> = cmds.iter().map(|c| reference.process(c)).collect();
    let ref_digest = digest(&reference.snapshot_state());

    // Every chunking of the same stream must yield identical reports and book.
    for chunk in [1usize, 2, 3, 5, cmds.len(), cmds.len() + 7] {
        let dir = tmpdir(&format!("gc{chunk}"));
        let mut e = SymbolEngine::open(dir.clone(), "BTCUSDT", 1, 6, 0, Arc::new(InMemorySink::default())).unwrap();
        let reports = run_in_chunks(&mut e, &cmds, chunk);
        assert_eq!(
            serde_json::to_string(&ref_reports).unwrap(),
            serde_json::to_string(&reports).unwrap(),
            "group commit (chunk={chunk}) must produce the same reports as per-command"
        );
        assert_eq!(ref_digest, digest(&e.snapshot_state()), "group commit (chunk={chunk}) must produce the same book");
        std::fs::remove_dir_all(&dir).ok();
    }
    std::fs::remove_dir_all(&dir_ref).ok();
}

#[test]
fn group_commit_batch_straddles_snapshot_boundary() {
    // snapshot_every = 3, but batches are size 4, so a single batch crosses the
    // threshold. The one-snapshot-per-batch check must still fire and leave a
    // consistent snapshot (+ possibly-empty tail) that recovers exactly.
    let dir = tmpdir("straddle");
    let sink = Arc::new(InMemorySink::default());
    let mut e1 = SymbolEngine::open(dir.clone(), "BTCUSDT", 1, 6, 3, sink.clone()).unwrap();
    run_in_chunks(&mut e1, &workload(), 4);
    let before = digest(&e1.snapshot_state());
    drop(e1);

    let e2 = SymbolEngine::open(dir.clone(), "BTCUSDT", 1, 6, 3, sink).unwrap();
    assert_eq!(before, digest(&e2.snapshot_state()), "snapshot straddled by a batch must still recover exactly");

    std::fs::remove_dir_all(&dir).ok();
}

#[test]
fn torn_final_batch_is_dropped_on_recovery() {
    // Model a crash mid-fsync: a durable prefix of commands, then a half-written
    // trailing record. Recovery must keep the acknowledged prefix and discard the
    // torn (never-acknowledged) tail — no partial application.
    let dir = tmpdir("torn");
    let sink = Arc::new(InMemorySink::default());

    let cmds = workload();
    let mut e1 = SymbolEngine::open(dir.clone(), "BTCUSDT", 1, 6, 0, sink.clone()).unwrap();
    e1.process_batch(&cmds[..5]); // durable, acknowledged prefix
    let before = digest(&e1.snapshot_state());
    drop(e1);

    // Append a torn JSON line to the WAL, as a crash during the next batch's
    // fsync would leave behind.
    let mut f = std::fs::OpenOptions::new().append(true).open(dir.join("wal.log")).unwrap();
    f.write_all(br#"{"seq":6,"command":{"cmd":"place","symbol":"BTCUS"#).unwrap();
    f.sync_all().unwrap();
    drop(f);

    let e2 = SymbolEngine::open(dir.clone(), "BTCUSDT", 1, 6, 0, sink).unwrap();
    assert_eq!(before, digest(&e2.snapshot_state()), "torn tail must be dropped, prefix preserved");

    std::fs::remove_dir_all(&dir).ok();
}
