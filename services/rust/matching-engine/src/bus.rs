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

/// Publishes one result-stream entry (the settlement log), as opposed to the
/// trade/depth [`EngineEvent`]s. The order service consumes these.
pub trait ReportPublisher: Send + Sync {
    fn publish_report(&self, entry: &crate::types::ReportEntry);
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
            eprintln!("[event] post to {} failed: {e}", redact_url(&self.url));
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

// --- Kafka -------------------------------------------------------------------
//
// One background producer. Matching threads only enqueue. The result stream
// (settlement) and the trade/depth events go to two topics. A full queue drops
// the record and logs: the on-disk report log is still the source of truth and
// the order service gap-fills over HTTP.

use std::sync::mpsc::{sync_channel, Receiver, SyncSender, TrySendError};
use std::thread;
use std::time::Duration;

use rskafka::client::partition::{Compression, UnknownTopicHandling};
use rskafka::client::{ClientBuilder, SaslConfig};
use rskafka::record::Record;

enum BusMsg {
    Report { key: Vec<u8>, body: Vec<u8> },
    Event { key: Vec<u8>, body: Vec<u8> },
}

/// Kafka producer for engine output. Clone is cheap (it clones the queue sender).
#[derive(Clone)]
pub struct KafkaBus {
    tx: SyncSender<BusMsg>,
}

impl KafkaBus {
    /// Spawn the producer. It retries the broker forever; a down broker does
    /// not stop the engine. `brokers` is a comma-separated list. SASL PLAIN is
    /// used when both `KAFKA_SASL_USERNAME` and `KAFKA_SASL_PASSWORD` are set.
    pub fn start(brokers: &str, reports_topic: String, events_topic: String) -> Result<Self, String> {
        let list: Vec<String> = brokers
            .split(',')
            .map(|s| s.trim().to_string())
            .filter(|s| !s.is_empty())
            .collect();
        if list.is_empty() {
            return Err("KAFKA_BROKERS is empty".into());
        }
        let (tx, rx) = sync_channel(8192);
        let sasl = match (std::env::var("KAFKA_SASL_USERNAME"), std::env::var("KAFKA_SASL_PASSWORD")) {
            (Ok(u), Ok(p)) if !u.is_empty() => {
                Some(SaslConfig::Plain(rskafka::client::Credentials { username: u, password: p }))
            }
            _ => None,
        };
        thread::Builder::new()
            .name("kafka-produce".into())
            .spawn(move || {
                let rt = tokio::runtime::Builder::new_multi_thread()
                    .enable_all()
                    .build()
                    .expect("kafka runtime");
                rt.block_on(produce_loop(list, reports_topic, events_topic, sasl, rx));
            })
            .map_err(|e| format!("spawn kafka producer: {e}"))?;
        Ok(Self { tx })
    }

    fn enqueue(&self, msg: BusMsg) {
        match self.tx.try_send(msg) {
            Ok(()) => {}
            Err(TrySendError::Full(_)) => {
                eprintln!("[kafka] queue full; HTTP result stream remains the source of truth");
            }
            Err(TrySendError::Disconnected(_)) => eprintln!("[kafka] producer stopped"),
        }
    }
}

impl ReportPublisher for KafkaBus {
    fn publish_report(&self, entry: &crate::types::ReportEntry) {
        let Ok(body) = serde_json::to_vec(entry) else { return };
        let key = entry.report.symbol.as_bytes().to_vec();
        self.enqueue(BusMsg::Report { key, body });
    }
}

impl EventSink for KafkaBus {
    fn publish(&self, event: &EngineEvent) {
        let Ok(body) = serde_json::to_vec(event) else { return };
        let key = match event {
            EngineEvent::Trade(t) => t.symbol.as_bytes().to_vec(),
            EngineEvent::Depth(d) => d.symbol.as_bytes().to_vec(),
        };
        self.enqueue(BusMsg::Event { key, body });
    }
}

async fn produce_loop(
    brokers: Vec<String>,
    reports_topic: String,
    events_topic: String,
    sasl: Option<SaslConfig>,
    rx: Receiver<BusMsg>,
) {
    loop {
        match connect(&brokers, &reports_topic, &events_topic, sasl.clone()).await {
            Ok((reports, events)) => {
                if !drain(&reports, &events, &rx).await {
                    return; // sender dropped; process is exiting
                }
            }
            Err(e) => {
                eprintln!("[kafka] connect: {e}");
                tokio::time::sleep(Duration::from_secs(2)).await;
            }
        }
    }
}

async fn connect(
    brokers: &[String],
    reports_topic: &str,
    events_topic: &str,
    sasl: Option<SaslConfig>,
) -> Result<(rskafka::client::partition::PartitionClient, rskafka::client::partition::PartitionClient), String> {
    let mut b = ClientBuilder::new(brokers.to_vec()).client_id("linbit-matching-engine");
    if let Some(sasl) = sasl {
        b = b.sasl_config(sasl);
    }
    let client = b.build().await.map_err(|e| e.to_string())?;
    // Partition 0 keeps one symbol's records ordered because every record for a
    // topic shares that partition. The order service reads every partition.
    let reports = client
        .partition_client(reports_topic, 0, UnknownTopicHandling::Error)
        .await
        .map_err(|e| e.to_string())?;
    let events = client
        .partition_client(events_topic, 0, UnknownTopicHandling::Error)
        .await
        .map_err(|e| e.to_string())?;
    Ok((reports, events))
}

/// Returns false when the queue is closed.
async fn drain(
    reports: &rskafka::client::partition::PartitionClient,
    events: &rskafka::client::partition::PartitionClient,
    rx: &Receiver<BusMsg>,
) -> bool {
    loop {
        let msg = match rx.recv_timeout(Duration::from_secs(30)) {
            Ok(m) => m,
            Err(std::sync::mpsc::RecvTimeoutError::Timeout) => continue,
            Err(std::sync::mpsc::RecvTimeoutError::Disconnected) => return false,
        };
        let (client, key, body) = match msg {
            BusMsg::Report { key, body } => (reports, key, body),
            BusMsg::Event { key, body } => (events, key, body),
        };
        let record = Record {
            key: Some(key),
            value: Some(body),
            headers: Default::default(),
            timestamp: kafka_now(),
        };
        if let Err(e) = client.produce(vec![record], Compression::NoCompression).await {
            eprintln!("[kafka] produce: {e}");
            return true; // reconnect; the record was already taken off the queue
        }
    }
}

fn kafka_now() -> rskafka::chrono::DateTime<rskafka::chrono::Utc> {
    use rskafka::chrono::{DateTime, TimeZone, Utc};
    let ms = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_millis() as i64)
        .unwrap_or(0);
    Utc.timestamp_millis_opt(ms).single().unwrap_or(DateTime::UNIX_EPOCH)
}

/// Masks the password of a `scheme://user:pass@host` URL before it is logged.
pub fn redact_url(u: &str) -> String {
    let Some(i) = u.find("://") else { return u.to_string() };
    let rest = &u[i + 3..];
    let end = rest.find('/').unwrap_or(rest.len());
    let Some(at) = rest[..end].rfind('@') else { return u.to_string() };
    let cred = &rest[..at];
    match cred.find(':') {
        Some(c) => format!("{}{}:***{}", &u[..i + 3], &cred[..c], &rest[at..]),
        None => u.to_string(),
    }
}

#[cfg(test)]
mod redact_tests {
    use super::redact_url;
    #[test]
    fn masks_password() {
        assert_eq!(redact_url("http://u:p@ss@h:1/x"), "http://u:***@h:1/x");
        assert_eq!(redact_url("http://h:8080/events"), "http://h:8080/events");
    }
}
