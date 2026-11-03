//! Replaceable messaging interface for engine output events.
//!
//! The engine never names a concrete transport. It publishes [`EngineEvent`]s
//! through the [`EventSink`] trait, so the deployment picks the implementation:
//!
//! * [`InMemorySink`] — used by unit tests to assert on emitted events.
//! * [`LogSink`] — the local default; prints events (useful in devstack).
//! * [`HttpSink`] — posts newline-delimited JSON to a consumer URL (the local
//!   "pub/sub" stand-in for market-data).
//! * `KafkaSink` — compiled only with `--features kafka`; an interface skeleton
//!   marking where an rdkafka producer would be wired for production. It does
//!   not require a running broker.

use std::sync::Mutex;

use crate::types::EngineEvent;

/// A sink for engine output events. Implementations must be cheap to call from
/// the per-symbol matching threads and must not block them for long.
pub trait EventSink: Send + Sync {
    fn publish(&self, event: &EngineEvent);
}

/// Collects events in memory. Test-only convenience.
#[derive(Default)]
pub struct InMemorySink {
    pub events: Mutex<Vec<EngineEvent>>,
}

impl EventSink for InMemorySink {
    fn publish(&self, event: &EngineEvent) {
        self.events.lock().unwrap().push(event.clone());
    }
}

/// Logs each event to stdout. The local default when no sink URL is configured.
pub struct LogSink;

impl EventSink for LogSink {
    fn publish(&self, event: &EngineEvent) {
        match serde_json::to_string(event) {
            Ok(s) => println!("[event] {s}"),
            Err(e) => eprintln!("[event] serialize error: {e}"),
        }
    }
}

/// Posts each event as one JSON line to a consumer endpoint over HTTP. This is
/// the local stand-in for a durable event stream; a failed post is logged and
/// dropped (events are best-effort; settlement does not depend on this path).
pub struct HttpSink {
    url: String,
    token: String,
}

impl HttpSink {
    pub fn new(url: String, token: String) -> Self {
        Self { url, token }
    }
}

impl EventSink for HttpSink {
    fn publish(&self, event: &EngineEvent) {
        let body = match serde_json::to_vec(event) {
            Ok(b) => b,
            Err(e) => {
                eprintln!("[event] serialize error: {e}");
                return;
            }
        };
        if let Err(e) = crate::httpc::post(&self.url, &self.token, &body) {
            eprintln!("[event] post to {} failed: {e}", self.url);
        }
    }
}

/// Fan-out to several sinks (e.g. log + http).
pub struct MultiSink(pub Vec<Box<dyn EventSink>>);

impl EventSink for MultiSink {
    fn publish(&self, event: &EngineEvent) {
        for s in &self.0 {
            s.publish(event);
        }
    }
}

// --- Kafka skeleton (production path) ---------------------------------------
//
// Compiled only with `--features kafka`. This is deliberately an interface
// skeleton: it shows exactly where a Kafka producer would serialize and emit
// each event, without pulling in rdkafka or requiring a broker to build/test.
#[cfg(feature = "kafka")]
pub struct KafkaSink {
    pub brokers: String,
    pub topic: String,
}

#[cfg(feature = "kafka")]
impl KafkaSink {
    pub fn new(brokers: String, topic: String) -> Self {
        Self { brokers, topic }
    }
}

#[cfg(feature = "kafka")]
impl EventSink for KafkaSink {
    fn publish(&self, event: &EngineEvent) {
        // Production: serialize `event` and produce to `self.topic` on
        // `self.brokers` keyed by symbol so a symbol's events stay ordered,
        // e.g. with rdkafka's FutureProducer. Left unimplemented so the crate
        // builds without a broker.
        let _ = (event, &self.brokers, &self.topic);
        unimplemented!("wire an rdkafka producer here for the Kafka deployment");
    }
}
