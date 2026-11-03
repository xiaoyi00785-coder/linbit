//! Throughput + latency benchmark for the matching core and its WAL.
//!
//! Two kinds of runs are reported side by side:
//!
//!   * **memory** — the engine with no WAL at all, measuring pure matching cost
//!     (the historical baseline).
//!   * **disk** — the engine writing a real WAL to a temp dir with fsync enabled,
//!     under both durability policies: per-command fsync and group commit at a
//!     range of batch sizes. This isolates the fsync-amortization win.
//!
//!   cargo run --release --bin bench                 # 1,000,000 mem / 30,000 disk
//!   cargo run --release --bin bench -- 2000000      # custom memory order count
//!   cargo run --release --bin bench -- 2000000 50000 # custom memory + disk counts
//!
//! For each run it prints throughput (orders/sec) and p50/p99/p999 latency, plus
//! the host's core count and the temp filesystem + a measured raw-fsync latency so
//! the absolute numbers are interpretable.
//!
//! Latency model: a command's latency is the time until it is durable and
//! applied. Under per-command fsync that is its own fsync. Under group commit a
//! command only acks once its *batch* has fsync'd, so each command in a batch is
//! charged the whole batch's service time — the latency a client that joined at
//! batch open would see. Larger batches therefore trade per-command latency for
//! throughput, which is exactly what the table shows.

use std::fs::OpenOptions;
use std::io::Write;
use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::time::Instant;

use matching_engine::book::SymbolEngine;
use matching_engine::bus::EventSink;
use matching_engine::fixed;
use matching_engine::types::*;

/// A sink that discards events — we are measuring matching + WAL, not the bus.
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

const LEVELS: u64 = 1000;

/// Build the stream of crossing orders used by every run: alternating a buy that
/// lifts the best ask and a replenishing sell so the book never empties.
fn workload(n: usize, start_oid: u64) -> Vec<Command> {
    let mut cmds = Vec::with_capacity(n);
    let mut oid = start_oid;
    for i in 0..n {
        oid += 1;
        let cmd = if i % 2 == 0 {
            place(oid, "taker", Side::Buy, "71000", "1")
        } else {
            place(oid, "maker", Side::Sell, &(70000 + (i as u64 % LEVELS)).to_string(), "1")
        };
        cmds.push(cmd);
    }
    cmds
}

/// Seed a warm, deep book (1000 resting sell levels). Returns the next order id.
fn seed(engine: &mut SymbolEngine) -> u64 {
    let mut oid = 0u64;
    for i in 0..LEVELS {
        oid += 1;
        engine.process(&place(oid, "maker", Side::Sell, &(70000 + i).to_string(), "1000"));
    }
    oid
}

struct Stats {
    label: String,
    orders: usize,
    trades: u64,
    secs: f64,
    ops: f64,
    p50: u64,
    p99: u64,
    p999: u64,
}

fn summarize(label: &str, orders: usize, trades: u64, secs: f64, mut lat: Vec<u64>) -> Stats {
    lat.sort_unstable();
    let pct = |p: f64| lat[((lat.len() as f64 * p) as usize).min(lat.len() - 1)];
    Stats {
        label: label.to_string(),
        orders,
        trades,
        secs,
        ops: orders as f64 / secs,
        p50: pct(0.50),
        p99: pct(0.99),
        p999: pct(0.999),
    }
}

/// Memory baseline: no WAL, one command at a time.
fn run_memory(n: usize) -> Stats {
    let mut engine = SymbolEngine::in_memory("BTCUSDT", 1, 6, Arc::new(NullSink));
    let oid = seed(&mut engine);
    let cmds = workload(n, oid);
    let mut lat = Vec::with_capacity(n);
    let mut trades = 0u64;
    let start = Instant::now();
    for cmd in &cmds {
        let t = Instant::now();
        let r = engine.process(cmd);
        lat.push(t.elapsed().as_nanos() as u64);
        trades += r.trades.len() as u64;
    }
    summarize("memory (no WAL)", n, trades, start.elapsed().as_secs_f64(), lat)
}

/// Disk WAL run at a given batch size. `batch == 1` is the per-command policy
/// (one fsync per command); larger batches group-commit with a single fsync.
/// Each command in a committed batch is charged the batch's full service time.
fn run_disk(dir: &Path, n: usize, batch: usize) -> Stats {
    // snapshot_every = 0: never snapshot, so the measurement is pure WAL fsync
    // cost (no periodic snapshot/truncate noise).
    let mut engine = SymbolEngine::open(dir.to_path_buf(), "BTCUSDT", 1, 6, 0, Arc::new(NullSink)).unwrap();
    let oid = seed(&mut engine);
    let cmds = workload(n, oid);
    let mut lat = Vec::with_capacity(n);
    let mut trades = 0u64;
    let start = Instant::now();
    for group in cmds.chunks(batch) {
        let t = Instant::now();
        let reports = engine.process_batch(group);
        let dt = t.elapsed().as_nanos() as u64;
        for r in &reports {
            trades += r.trades.len() as u64;
            lat.push(dt); // whole-batch service time, per command
        }
    }
    let secs = start.elapsed().as_secs_f64();
    let label = if batch == 1 {
        "disk per-command fsync".to_string()
    } else {
        format!("disk group commit (batch {batch})")
    };
    summarize(&label, n, trades, secs, lat)
}

fn main() {
    let mem_n: usize = std::env::args().nth(1).and_then(|s| s.parse().ok()).unwrap_or(1_000_000);
    let disk_n: usize = std::env::args().nth(2).and_then(|s| s.parse().ok()).unwrap_or(30_000);

    let cores = std::thread::available_parallelism().map(|n| n.get()).unwrap_or(0);
    let tmp = std::env::temp_dir();
    let (fs_dev, fs_type, fs_mount) = temp_fs_info(&tmp);
    let fsync_us = raw_fsync_latency_us(&tmp);

    println!("=== matching-engine benchmark ===");
    println!("host cores (available_parallelism): {cores}");
    println!("temp dir   : {}", tmp.display());
    println!("temp fs    : {fs_dev} ({fs_type}) at {fs_mount}");
    println!("raw fsync  : {fsync_us:.1} µs/fsync (128B append + fdatasync, measured)");
    println!("memory orders: {mem_n}   disk orders: {disk_n}   (book: {LEVELS} resting levels)");
    println!();

    let mut rows = vec![run_memory(mem_n)];

    // Disk runs: per-command, then group commit at increasing batch sizes.
    for batch in [1usize, 8, 32, 128, 1024] {
        let dir = PathBuf::from(&tmp).join(format!("linbit-bench-b{batch}-{}", std::process::id()));
        std::fs::create_dir_all(&dir).ok();
        rows.push(run_disk(&dir, disk_n, batch));
        std::fs::remove_dir_all(&dir).ok();
    }

    print_table(&rows);

    let _ = fixed::ONE; // keep the import meaningful if matching changes
}

fn print_table(rows: &[Stats]) {
    println!(
        "{:<30} {:>8} {:>8} {:>8} {:>14} {:>12} {:>12} {:>12}",
        "run", "orders", "trades", "secs", "orders/sec", "p50", "p99", "p999"
    );
    println!("{}", "-".repeat(30 + 9 * 3 + 15 + 13 * 3));
    for s in rows {
        println!(
            "{:<30} {:>8} {:>8} {:>8.3} {:>14.0} {:>12} {:>12} {:>12}",
            s.label,
            s.orders,
            s.trades,
            s.secs,
            s.ops,
            fmt_ns(s.p50),
            fmt_ns(s.p99),
            fmt_ns(s.p999),
        );
    }
    println!("(latency columns are per-command; group-commit rows charge each command its full batch service time)");
}

/// Format a nanosecond latency compactly as ns / µs / ms.
fn fmt_ns(ns: u64) -> String {
    if ns < 1_000 {
        format!("{ns}ns")
    } else if ns < 1_000_000 {
        format!("{:.1}µs", ns as f64 / 1_000.0)
    } else {
        format!("{:.2}ms", ns as f64 / 1_000_000.0)
    }
}

/// Best-effort lookup of the device/fs-type/mount backing `path` via /proc/mounts
/// (longest matching mount point wins). Returns "unknown" fields off Linux.
fn temp_fs_info(path: &Path) -> (String, String, String) {
    let target = path.to_string_lossy().to_string();
    let mounts = match std::fs::read_to_string("/proc/mounts") {
        Ok(m) => m,
        Err(_) => return ("unknown".into(), "unknown".into(), "unknown".into()),
    };
    let mut best: Option<(String, String, String)> = None;
    let mut best_len = 0usize;
    for line in mounts.lines() {
        let mut f = line.split_whitespace();
        let (dev, mount, fstype) = match (f.next(), f.next(), f.next()) {
            (Some(d), Some(m), Some(t)) => (d, m, t),
            _ => continue,
        };
        if target.starts_with(mount) && mount.len() >= best_len {
            best_len = mount.len();
            best = Some((dev.to_string(), fstype.to_string(), mount.to_string()));
        }
    }
    best.unwrap_or_else(|| ("unknown".into(), "unknown".into(), "unknown".into()))
}

/// Measure mean single-fsync latency on the benchmark's filesystem so the table's
/// absolute numbers can be read against the raw disk cost.
fn raw_fsync_latency_us(dir: &Path) -> f64 {
    let path = dir.join(format!("linbit-fsync-probe-{}", std::process::id()));
    let mut f = match OpenOptions::new().create(true).write(true).truncate(true).open(&path) {
        Ok(f) => f,
        Err(_) => return f64::NAN,
    };
    let n = 200u32;
    let buf = [b'x'; 128];
    let start = Instant::now();
    for _ in 0..n {
        let _ = f.write_all(&buf);
        let _ = f.sync_data();
    }
    let us = start.elapsed().as_micros() as f64 / n as f64;
    drop(f);
    std::fs::remove_file(&path).ok();
    us
}
