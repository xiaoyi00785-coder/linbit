//! 引擎输出事件的可替换出口。
//!
//! 引擎不绑定具体传输，只通过 [`EventSink`] 发布 [`EngineEvent`]。部署时再选实现：
//!
//! * [`InMemorySink`] — 单测里收集事件。
//! * [`LogSink`] — 没配下游地址时的默认实现，把事件打到标准输出。
//! * [`HttpSink`] — 把 JSON 行 POST 到消费者地址，本地用来代替行情服务的消息流。
//! * `KafkaSink` — 仅 `--features kafka` 编译。标出生产环境接入 rdkafka 的位置，构建和测试不需要 broker。

use std::sync::Mutex;

use crate::types::EngineEvent;

/// 引擎输出事件的出口。实现要能在各交易对的撮合线程上调用，不能长时间堵住撮合。
pub trait EventSink: Send + Sync {
    fn publish(&self, event: &EngineEvent);
}

/// 把事件收进内存。只给测试用。
#[derive(Default)]
pub struct InMemorySink {
    pub events: Mutex<Vec<EngineEvent>>,
}

impl EventSink for InMemorySink {
    fn publish(&self, event: &EngineEvent) {
        self.events.lock().unwrap().push(event.clone());
    }
}

/// 把每条事件打到标准输出。没有配置下游地址时的本地默认实现。
pub struct LogSink;

impl EventSink for LogSink {
    fn publish(&self, event: &EngineEvent) {
        match serde_json::to_string(event) {
            Ok(s) => println!("[event] {s}"),
            Err(e) => eprintln!("[event] serialize error: {e}"),
        }
    }
}

/// 把每条事件作为一行 JSON，经 HTTP POST 给消费者。这是本地用来代替持久消息流的实现。
/// POST 失败只记日志然后丢掉：事件是尽力而为，结算不走这条路径。
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

/// 同时发给多个出口（例如日志加 HTTP）。
pub struct MultiSink(pub Vec<Box<dyn EventSink>>);

impl EventSink for MultiSink {
    fn publish(&self, event: &EngineEvent) {
        for s in &self.0 {
            s.publish(event);
        }
    }
}

// --- Kafka 占位（生产路径）-------------------------------------------------
//
// 仅在 `--features kafka` 下编译。这里故意只留接口：标出 producer 序列化并发送事件的位置，
// 不引入 rdkafka，构建和测试也不需要 broker。
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
        // 生产环境：把 `event` 序列化后发到 `self.brokers` 上的 `self.topic`，
        // 以交易对为 key，保证同一交易对的事件有序。例如用 rdkafka 的 FutureProducer。
        // 尚未接入，这样不装 broker 也能编译。
        let _ = (event, &self.brokers, &self.topic);
        unimplemented!("wire an rdkafka producer here for the Kafka deployment");
    }
}

/// 打日志前把 `scheme://user:pass@host` 里的密码换成 `***`。
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
