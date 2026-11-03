//! Linbit spot matching engine service.
//!
//! Configuration (all via environment variables):
//!
//! * `BIND`            — listen address (default `0.0.0.0:8083`).
//! * `SYMBOLS`         — comma list of `SYMBOL:qtyPrecision` (default
//!                       `BTCUSDT:6,ETHUSDT:5,SOLUSDT:3,BNBUSDT:3,TRXUSDT:1,AAPLUSDT:4,TSLAUSDT:4,NVDAUSDT:4,MSFTUSDT:4,GOOGLUSDT:4,AMZNUSDT:4,METAUSDT:4,COINUSDT:4,SPYUSDT:4,PAXGUSDT:5,XAUTUSDT:5`). Append only: list order fixes each symbol's
//!                       trade-id namespace.
//! * `DATA_DIR`        — root for per-symbol WAL + snapshot files (default
//!                       `./data`). Each symbol gets `DATA_DIR/<symbol>/`.
//! * `SNAPSHOT_EVERY`  — snapshot after this many commands (default 1000).
//! * `INTERNAL_TOKEN`  — shared token required on `/engine/*` (empty = open).
//! * `EVENT_SINK_URL`  — if set, engine events are also POSTed here (the local
//!                       pub/sub stand-in); otherwise events are logged.
//! * `WAL_FSYNC`       — durability policy: `group` (default) group-commits a
//!                       batch with a single fsync; `per_command` fsyncs every
//!                       command on its own.
//! * `WAL_BATCH_MAX`   — group commit: max commands coalesced per fsync
//!                       (default 1024).
//! * `WAL_BATCH_WINDOW_US` — group commit: max microseconds a command waits for
//!                       batch-mates before the fsync fires (default 1000 = 1ms).

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

/// Read the WAL durability policy from the environment (see module docs).
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
