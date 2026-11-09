//! 撮合核心和 WAL 的吞吐、延迟压测。
//!
//! 两种跑法并排列出：
//!
//!   * memory — 完全不写 WAL，只量撮合本身的成本（历史基线）。
//!   * disk — 往临时目录写真实 WAL 并 fsync。逐条 fsync，以及若干批大小的组提交都跑，
//!     用来把「fsync 被分摊掉」的收益单独看出来。
//!
//!   cargo run --release --bin bench                 # 内存 1,000,000 / 磁盘 30,000
//!   cargo run --release --bin bench -- 2000000      # 自定义内存订单数
//!   cargo run --release --bin bench -- 2000000 50000 # 自定义内存和磁盘订单数
//!
//! 每次打印吞吐（订单/秒）和 p50/p99/p999 延迟，并附上机器核数、临时目录所在文件系统，
//! 以及测得的单次 fsync 延迟，方便对照绝对数字。
//!
//! 延迟定义：从命令开始到它落盘并被应用。逐条 fsync 时就是它自己那次 fsync。
//! 组提交时，一条命令要等自己所在的那一批 fsync 完才确认，所以批内每条命令都记整批的服务时间，
//! 也就是批一开始就加入的客户端能看到的延迟。批越大，单条延迟换吞吐，表里就是这个关系。

use std::fs::OpenOptions;
use std::io::Write;
use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::time::Instant;

use matching_engine::book::SymbolEngine;
use matching_engine::bus::EventSink;
use matching_engine::fixed;
use matching_engine::types::*;

/// 丢掉事件的出口。这里量的是撮合和 WAL，不是事件总线。
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

/// 构造每次压测用的成交流：一笔打掉最优卖价的买单，接着一笔补回卖单，订单簿不会被吃空。
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

/// 预先挂上 1000 档卖单。返回下一次可用的订单号。
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

/// 内存基线：没有 WAL，一次一条命令。
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

/// 按给定批大小跑磁盘 WAL。`batch == 1` 就是逐条策略（每条一次 fsync）；
/// 更大的批用一次 fsync 做组提交。已提交的一批里，每条命令都记整批的服务时间。
fn run_disk(dir: &Path, n: usize, batch: usize) -> Stats {
    // snapshot_every = 0：从不打快照，测到的就是 WAL fsync 本身，没有周期性快照和截断的噪声。
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

    // 磁盘：先逐条，再按递增的批大小做组提交。
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

/// 把纳秒延迟收成 ns / µs / ms。
fn fmt_ns(ns: u64) -> String {
    if ns < 1_000 {
        format!("{ns}ns")
    } else if ns < 1_000_000 {
        format!("{:.1}µs", ns as f64 / 1_000.0)
    } else {
        format!("{:.2}ms", ns as f64 / 1_000_000.0)
    }
}

/// 用 /proc/mounts 查 `path` 落在哪块设备、什么文件系统、哪个挂载点（最长匹配优先）。
/// 非 Linux 上这些字段返回 "unknown"。
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

/// 在压测使用的文件系统上量单次 fsync 的平均延迟，用来对照表里的绝对数字和磁盘本身的成本。
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
