//! 组提交的持久化：把 fsync 合成一批不能改变引擎的行为。这些测试钉住 `process_batch` 的三点：
//!
//!   1. 组提交下的崩溃恢复能还原崩溃前的订单簿，纯 WAL 重放和「快照 + WAL 尾部」两条路都要能还原。
//!   2. 组提交和逐条 fsync 从外面看一样：同一串命令得到同样的报告和同样的最终订单簿，
//!      不管怎么切批（包括批边界正好跨过快照阈值）。
//!   3. 没有完整 fsync 的一批（写到一半的尾部，也就是提交过程中崩溃）在恢复时整批丢掉，
//!      尚未确认的批不会应用一半。

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

/// 一串比较密的混合指令：双边挂单、部分成交的吃单，以及一次撤单。
/// 用来覆盖成交号区间、部分成交和订单簿改动。
fn workload() -> Vec<Command> {
    vec![
        place("s1", "a", Side::Sell, TimeInForce::GTC, "70010", "1.5"),
        place("s2", "a", Side::Sell, TimeInForce::GTC, "70000", "2.0"),
        place("b1", "b", Side::Buy, TimeInForce::GTC, "69990", "1.0"),
        place("b2", "c", Side::Buy, TimeInForce::GTC, "70000", "0.5"), // 部分成交 s2
        cancel("b1"),
        place("s3", "a", Side::Sell, TimeInForce::GTC, "70020", "3.0"),
        place("b3", "d", Side::Buy, TimeInForce::GTC, "70010", "2.0"), // 先吃 70000，再吃 70010
        place("s4", "e", Side::Sell, TimeInForce::IOC, "69995", "10.0"), // IOC 打买盘
        cancel("nope"), // 拒绝 not_found，仍然记入 WAL
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

/// 把 `cmds` 按固定大小切成块交给 `process_batch`。最后一块可以不满，这就是真实的批边界。
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

    // snapshot_every = 0：恢复时只重放 WAL，不读快照。
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
    // 第一批之后强制打快照（WAL 被截断），第二批只落在快照之后的 WAL 尾部。
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

    // 对照：逐条 fsync，一次一条。
    let dir_ref = tmpdir("ref");
    let mut reference = SymbolEngine::open(dir_ref.clone(), "BTCUSDT", 1, 6, 0, Arc::new(InMemorySink::default())).unwrap();
    let ref_reports: Vec<ExecReport> = cmds.iter().map(|c| reference.process(c)).collect();
    let ref_digest = digest(&reference.snapshot_state());

    // 同一串命令无论怎么切批，报告和最终订单簿都必须相同。
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
    // snapshot_every = 3，但批大小是 4，所以一批会跨过阈值。
    // 每批只检查一次快照仍然要触发，留下一份能精确恢复的快照（尾部可以是空的）。
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
    // 模拟 fsync 写到一半：前面的命令已经落盘，最后一条记录只写了一半。
    // 恢复必须保留已确认的前缀，丢掉残缺的、从未确认的尾部，不能应用一半。
    let dir = tmpdir("torn");
    let sink = Arc::new(InMemorySink::default());

    let cmds = workload();
    let mut e1 = SymbolEngine::open(dir.clone(), "BTCUSDT", 1, 6, 0, sink.clone()).unwrap();
    e1.process_batch(&cmds[..5]); // durable, acknowledged prefix
    let before = digest(&e1.snapshot_state());
    drop(e1);

    // 往 WAL 末尾追加一行写了一半的 JSON，相当于下一批 fsync 过程中崩溃留下的尾巴。
    let mut f = std::fs::OpenOptions::new().append(true).open(dir.join("wal.log")).unwrap();
    f.write_all(br#"{"seq":6,"command":{"cmd":"place","symbol":"BTCUS"#).unwrap();
    f.sync_all().unwrap();
    drop(f);

    let e2 = SymbolEngine::open(dir.clone(), "BTCUSDT", 1, 6, 0, sink).unwrap();
    assert_eq!(before, digest(&e2.snapshot_state()), "torn tail must be dropped, prefix preserved");

    std::fs::remove_dir_all(&dir).ok();
}
