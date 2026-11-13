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
//! * `KAFKA_BROKERS`   — comma-separated brokers. When set, result-stream
//!                       entries are produced to `KAFKA_REPORTS_TOPIC`
//!                       (default `engine.reports`) and trade/depth events to
//!                       `KAFKA_EVENTS_TOPIC` (default `engine.events`).
//! * `KAFKA_SASL_USERNAME` / `KAFKA_SASL_PASSWORD` — optional SASL PLAIN.
//! * `TLS_CERT` / `TLS_KEY` — PEM files. Both set: the listener speaks TLS.
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
use matching_engine::bus::{EventSink, HttpSink, KafkaBus, LogSink, MultiSink};
use matching_engine::server::{serve_with, CommitMode, Dispatcher, TlsPaths};

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

    let kafka = match std::env::var("KAFKA_BROKERS") {
        Ok(b) if !b.trim().is_empty() => {
            let reports = env("KAFKA_REPORTS_TOPIC", "engine.reports");
            let events = env("KAFKA_EVENTS_TOPIC", "engine.events");
            match KafkaBus::start(&b, reports.clone(), events) {
                Ok(bus) => {
                    println!("kafka result stream -> {reports} ({b})");
                    Some(bus)
                }
                Err(e) => {
                    eprintln!("kafka: {e}");
                    std::process::exit(1);
                }
            }
        }
        _ => None,
    };
    let mut parts: Vec<Box<dyn EventSink>> = vec![Box::new(LogSink)];
    if let Ok(url) = std::env::var("EVENT_SINK_URL") {
        if !url.is_empty() {
            parts.push(Box::new(HttpSink::new(url, token.clone())));
        }
    }
    if let Some(bus) = &kafka {
        parts.push(Box::new(bus.clone()));
    }
    let sink: Arc<dyn EventSink> = if parts.len() == 1 {
        Arc::new(LogSink)
    } else {
        Arc::new(MultiSink(parts))
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
    if let Some(bus) = &kafka {
        let publisher: Arc<dyn matching_engine::bus::ReportPublisher> = Arc::new(bus.clone());
        for engine in &mut engines {
            engine.set_report_publisher(Arc::clone(&publisher));
        }
    }

    match commit_mode {
        CommitMode::PerCommand => println!("WAL durability: per-command fsync"),
        CommitMode::Group { max_batch, window } => println!(
            "WAL durability: group commit (max_batch={max_batch}, window={}µs)",
            window.as_micros()
        ),
    }

    let dispatcher = Dispatcher::spawn(engines, commit_mode);
    let tls = match (std::env::var("TLS_CERT"), std::env::var("TLS_KEY")) {
        (Ok(c), Ok(k)) if !c.trim().is_empty() && !k.trim().is_empty() => {
            Some(TlsPaths { cert: c, key: k })
        }
        (Ok(c), _) if !c.trim().is_empty() => {
            eprintln!("matching-engine: TLS_CERT is set but TLS_KEY is empty");
            std::process::exit(1);
        }
        (_, Ok(k)) if !k.trim().is_empty() => {
            eprintln!("matching-engine: TLS_KEY is set but TLS_CERT is empty");
            std::process::exit(1);
        }
        _ => None,
    };
    if let Err(e) = serve_with(&bind, token, dispatcher, tls) {
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
