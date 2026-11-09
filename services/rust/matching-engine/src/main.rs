//! 现货撮合引擎进程。
//!
//! 配置全部来自环境变量：
//!
//! * `BIND`            — 监听地址（默认 `0.0.0.0:8083`）。
//! * `SYMBOLS`         — 逗号分隔的 `SYMBOL:qtyPrecision`（默认
//!                       `BTCUSDT:6,ETHUSDT:5,SOLUSDT:3,BNBUSDT:3,TRXUSDT:1,AAPLUSDT:4,TSLAUSDT:4,NVDAUSDT:4,MSFTUSDT:4,GOOGLUSDT:4,AMZNUSDT:4,METAUSDT:4,COINUSDT:4,SPYUSDT:4,PAXGUSDT:5,XAUTUSDT:5`）。
//!                       只能在末尾追加：列表顺序决定每个交易对的成交号区间。
//! * `DATA_DIR`        — 各交易对 WAL 和快照的根目录（默认 `./data`）。每个交易对使用 `DATA_DIR/<symbol>/`。
//! * `SNAPSHOT_EVERY`  — 每这么多条命令打一次快照（默认 1000）。
//! * `INTERNAL_TOKEN`  — `/engine/*` 要求的共享令牌（空表示不校验）。
//! * `EVENT_SINK_URL`  — 若设置，引擎事件会 POST 到这里（本地的消息流替身）；否则只打日志。
//! * `WAL_FSYNC`       — 落盘策略：`group`（默认）整批一次 fsync；`per_command` 每条命令单独 fsync。
//! * `WAL_BATCH_MAX`   — 组提交时，每次 fsync 最多合并的命令数（默认 1024）。
//! * `WAL_BATCH_WINDOW_US` — 组提交时，一条命令最多等待同伴的微秒数，到点就 fsync（默认 1000，即 1ms）。

use std::path::PathBuf;
use std::sync::Arc;
use std::time::Duration;

use matching_engine::book::SymbolEngine;
use matching_engine::bus::{EventSink, HttpSink, LogSink, MultiSink};
use matching_engine::server::{serve, CommitMode, Dispatcher};

fn main() {
    let bind = env("BIND", "0.0.0.0:8083");
    let data_dir = PathBuf::from(env("DATA_DIR", "./data"));
    let snapshot_every: u64 = env("SNAPSHOT_EVERY", "1000").parse().unwrap_or(1000);
    let token = env("INTERNAL_TOKEN", "");
    if env("LINBIT_ENV", "").eq_ignore_ascii_case("production")
        && (token.trim().is_empty() || token.to_ascii_lowercase().contains("change-me"))
    {
        eprintln!("matching-engine: refusing to start in production: INTERNAL_TOKEN is empty or a change-me default");
        std::process::exit(1);
    }
    let symbols = parse_symbols(&env("SYMBOLS", "BTCUSDT:6,ETHUSDT:5,SOLUSDT:3,BNBUSDT:3,TRXUSDT:1,AAPLUSDT:4,TSLAUSDT:4,NVDAUSDT:4,MSFTUSDT:4,GOOGLUSDT:4,AMZNUSDT:4,METAUSDT:4,COINUSDT:4,SPYUSDT:4,PAXGUSDT:5,XAUTUSDT:5"));
    let commit_mode = parse_commit_mode();

    let sink: Arc<dyn EventSink> = match std::env::var("EVENT_SINK_URL") {
        Ok(url) if !url.is_empty() => Arc::new(MultiSink(vec![
            Box::new(LogSink),
            Box::new(HttpSink::new(url, token.clone())),
        ])),
        _ => Arc::new(LogSink),
    };

    let mut engines = Vec::new();
    for (idx, (symbol, qty_prec)) in symbols.iter().enumerate() {
        let dir = data_dir.join(symbol);
        let engine = SymbolEngine::open(
            dir,
            symbol,
            (idx + 1) as u64,
            *qty_prec,
            snapshot_every,
            sink.clone(),
        )
        .unwrap_or_else(|e| {
            eprintln!("open engine {symbol}: {e}");
            std::process::exit(1);
        });
        println!("symbol {symbol} ready (qtyPrecision={qty_prec})");
        engines.push(engine);
    }

    match commit_mode {
        CommitMode::PerCommand => println!("WAL durability: per-command fsync"),
        CommitMode::Group { max_batch, window } => println!(
            "WAL durability: group commit (max_batch={max_batch}, window={}µs)",
            window.as_micros()
        ),
    }

    let dispatcher = Dispatcher::spawn(engines, commit_mode);
    if let Err(e) = serve(&bind, token, dispatcher) {
        eprintln!("serve: {e}");
        std::process::exit(1);
    }
}

/// 从环境变量读取 WAL 落盘策略，含义见本文件顶部的模块注释。
fn parse_commit_mode() -> CommitMode {
    match env("WAL_FSYNC", "group").to_ascii_lowercase().as_str() {
        "per_command" | "per-command" | "each" | "sync" => CommitMode::PerCommand,
        _ => {
            let max_batch: usize = env("WAL_BATCH_MAX", "1024").parse().unwrap_or(1024).max(1);
            let window_us: u64 = env("WAL_BATCH_WINDOW_US", "1000").parse().unwrap_or(1000);
            CommitMode::Group { max_batch, window: Duration::from_micros(window_us) }
        }
    }
}

fn parse_symbols(s: &str) -> Vec<(String, u32)> {
    s.split(',')
        .filter(|p| !p.trim().is_empty())
        .map(|p| {
            let (sym, prec) = p.split_once(':').unwrap_or((p, "8"));
            (sym.trim().to_string(), prec.trim().parse().unwrap_or(8))
        })
        .collect()
}

fn env(key: &str, def: &str) -> String {
    std::env::var(key).ok().filter(|v| !v.is_empty()).unwrap_or_else(|| def.to_string())
}
