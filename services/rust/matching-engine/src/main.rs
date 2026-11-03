//! Linbit spot matching engine service.
//!
//! Configuration (all via environment variables):
//!
//! * `BIND`            — listen address (default `0.0.0.0:8083`).
//! * `SYMBOLS`         — comma list of `SYMBOL:qtyPrecision` (default
//!                       `BTCUSDT:6,ETHUSDT:5`). List order fixes each symbol's
//!                       trade-id namespace.
//! * `DATA_DIR`        — root for per-symbol WAL + snapshot files (default
//!                       `./data`). Each symbol gets `DATA_DIR/<symbol>/`.
//! * `SNAPSHOT_EVERY`  — snapshot after this many commands (default 1000).
//! * `INTERNAL_TOKEN`  — shared token required on `/engine/*` (empty = open).
//! * `EVENT_SINK_URL`  — if set, engine events are also POSTed here (the local
//!                       pub/sub stand-in); otherwise events are logged.

use std::path::PathBuf;
use std::sync::Arc;

use matching_engine::book::SymbolEngine;
use matching_engine::bus::{EventSink, HttpSink, LogSink, MultiSink};
use matching_engine::server::{serve, Dispatcher};

fn main() {
    let bind = env("BIND", "0.0.0.0:8083");
    let data_dir = PathBuf::from(env("DATA_DIR", "./data"));
    let snapshot_every: u64 = env("SNAPSHOT_EVERY", "1000").parse().unwrap_or(1000);
    let token = env("INTERNAL_TOKEN", "");
    let symbols = parse_symbols(&env("SYMBOLS", "BTCUSDT:6,ETHUSDT:5"));

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

    let dispatcher = Dispatcher::spawn(engines);
    if let Err(e) = serve(&bind, token, dispatcher) {
        eprintln!("serve: {e}");
        std::process::exit(1);
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
