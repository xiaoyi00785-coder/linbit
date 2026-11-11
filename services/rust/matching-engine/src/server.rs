//! 本地命令入口：一个阻塞的 HTTP 服务，后面是按交易对拆开的撮合循环。
//!
//! 每个交易对一个线程。HTTP 处理函数把命令经 channel 交给对应交易对，并阻塞等回复，
//! 因此单个交易对的撮合始终是单线程的，服务本身可以同时接很多连接。
//! 这是可替换消息接口的本地实现；生产环境可以从 Kafka 消费命令，驱动同一套循环
//! （事件侧的 Kafka 占位见 `bus.rs`）。

use std::collections::HashMap;
use std::io::{Read, Write};
use std::net::{TcpListener, TcpStream};
use std::sync::mpsc::{sync_channel, Receiver, RecvTimeoutError, SyncSender};
use std::sync::Arc;
use std::thread;
use std::time::{Duration, Instant};

use crate::book::SymbolEngine;
use crate::types::{Command, DepthEvent, ExecReport};

/// 撮合循环使用的 WAL 落盘策略。
#[derive(Clone, Copy, Debug)]
pub enum CommitMode {
    /// 每条命令单独 fsync，落盘之后才应用并确认。
    PerCommand,
    /// 命令合成一批再落盘。批的大小由 `max_batch` 条或 `window` 墙钟时间先到者为准。
    /// 整批一次 fsync，成功之后才应用并确认批内的命令。
    Group { max_batch: usize, window: Duration },
}

/// 交给某个交易对撮合线程的请求。
enum Req {
    Cmd(Command),
    Depth,
}

enum Resp {
    Report(Box<ExecReport>),
    Depth(Box<DepthEvent>),
    Error(String),
}

struct Job {
    req: Req,
    reply: SyncSender<Resp>,
}

/// 把命令分发到各个交易对的撮合线程。
#[derive(Clone)]
pub struct Dispatcher {
    senders: Arc<HashMap<String, SyncSender<Job>>>,
}

impl Dispatcher {
    /// 每个引擎起一个撮合线程，返回它们的分发器。`mode` 决定这些线程用哪种 WAL 落盘策略。
    pub fn spawn(engines: Vec<SymbolEngine>, mode: CommitMode) -> Self {
        let mut senders = HashMap::new();
        for engine in engines {
            let (tx, rx): (SyncSender<Job>, Receiver<Job>) = sync_channel(1024);
            let symbol = engine.symbol.clone();
            thread::Builder::new()
                .name(format!("match-{symbol}"))
                .spawn(move || match mode {
                    CommitMode::PerCommand => run_per_command(engine, rx),
                    CommitMode::Group { max_batch, window } => {
                        run_group_commit(engine, rx, max_batch.max(1), window)
                    }
                })
                .expect("spawn matching thread");
            senders.insert(symbol, tx);
        }
        Self { senders: Arc::new(senders) }
    }

    fn submit(&self, symbol: &str, req: Req) -> Resp {
        let Some(tx) = self.senders.get(symbol) else {
            return Resp::Error(format!("unknown symbol {symbol}"));
        };
        let (rtx, rrx) = sync_channel(1);
        if tx.send(Job { req, reply: rtx }).is_err() {
            return Resp::Error("matching thread gone".into());
        }
        rrx.recv().unwrap_or_else(|_| Resp::Error("no reply".into()))
    }
}

/// 逐条策略：来一条处理一条，每条命令单独 fsync，落盘之后才回复。
fn run_per_command(mut engine: SymbolEngine, rx: Receiver<Job>) {
    for job in rx {
        let resp = match job.req {
            Req::Cmd(cmd) => Resp::Report(Box::new(engine.process(&cmd))),
            Req::Depth => Resp::Depth(Box::new(engine.depth_snapshot())),
        };
        let _ = job.reply.send(resp);
    }
}

/// 组提交：把 channel 里已经在等的任务合成一批（不超过 `max_batch` 条，也不超过 `window`），
/// 整批命令一次 fsync 落盘，然后再应用并逐条回复。
fn run_group_commit(mut engine: SymbolEngine, rx: Receiver<Job>, max_batch: usize, window: Duration) {
    // 先阻塞等来批里的第一条。来了之后继续取已经排队的，或在 `window` 内到达的任务，
    // 直到批满或窗口结束。单独一条命令最多等 `window` 才 fsync；
    // 忙的时候批会凑满 `max_batch`，窗口就不起作用了。
    while let Ok(first) = rx.recv() {
        let mut jobs = vec![first];
        let deadline = Instant::now() + window;
        while jobs.len() < max_batch {
            let now = Instant::now();
            if now >= deadline {
                break;
            }
            match rx.recv_timeout(deadline - now) {
                Ok(job) => jobs.push(job),
                Err(RecvTimeoutError::Timeout) => break,
                // 发送端没了：先处理手里已经有的，外层 recv() 随后结束。
                Err(RecvTimeoutError::Disconnected) => break,
            }
        }
        process_batch_jobs(&mut engine, jobs);
    }
}

/// 按到达顺序处理一批任务。连续的命令合成一次组提交；深度查询会先把前面的命令刷掉，
/// 这样它看到的是这些命令已经生效后的订单簿，顺序和逐条循环一致。
fn process_batch_jobs(engine: &mut SymbolEngine, jobs: Vec<Job>) {
    let mut cmds: Vec<Command> = Vec::new();
    let mut replies: Vec<SyncSender<Resp>> = Vec::new();
    for job in jobs {
        match job.req {
            Req::Cmd(cmd) => {
                cmds.push(cmd);
                replies.push(job.reply);
            }
            Req::Depth => {
                flush_command_batch(engine, &mut cmds, &mut replies);
                let dep = engine.depth_snapshot();
                let _ = job.reply.send(Resp::Depth(Box::new(dep)));
            }
        }
    }
    flush_command_batch(engine, &mut cmds, &mut replies);
}

/// 把缓冲的命令做一次组提交（一次 fsync），并按报告逐条回复。
fn flush_command_batch(engine: &mut SymbolEngine, cmds: &mut Vec<Command>, replies: &mut Vec<SyncSender<Resp>>) {
    if cmds.is_empty() {
        return;
    }
    let reports = engine.process_batch(cmds);
    for (report, reply) in reports.into_iter().zip(replies.drain(..)) {
        let _ = reply.send(Resp::Report(Box::new(report)));
    }
    cmds.clear();
}

/// 跑 HTTP 服务，直到进程被杀掉。每个连接一个线程。
pub fn serve(bind: &str, token: String, dispatcher: Dispatcher) -> std::io::Result<()> {
    let listener = TcpListener::bind(bind)?;
    println!("matching-engine listening on {bind}");
    for stream in listener.incoming() {
        let stream = match stream {
            Ok(s) => s,
            Err(e) => {
                eprintln!("accept: {e}");
                continue;
            }
        };
        let d = dispatcher.clone();
        let token = token.clone();
        thread::spawn(move || {
            if let Err(e) = handle_conn(stream, &token, &d) {
                eprintln!("conn: {e}");
            }
        });
    }
    Ok(())
}

fn handle_conn(mut stream: TcpStream, token: &str, d: &Dispatcher) -> std::io::Result<()> {
    let (method, path, headers, body) = read_request(&mut stream)?;

    // 健康检查不校验内部令牌。
    if method == "GET" && path == "/healthz" {
        return write_response(&mut stream, 200, "text/plain", b"ok");
    }

    // 配了 INTERNAL_TOKEN 时，其余接口都要带同一个 X-Internal-Token。空令牌表示不校验。
    if !token.is_empty() && headers.get("x-internal-token").map(String::as_str) != Some(token) {
        return write_json(&mut stream, 401, br#"{"error":"unauthorized"}"#);
    }

    match (method.as_str(), path_only(&path)) {
        ("POST", "/engine/commands") => {
            let cmd: Command = match serde_json::from_slice(&body) {
                Ok(c) => c,
                Err(e) => {
                    let msg = format!(r#"{{"error":"bad command: {e}"}}"#);
                    return write_json(&mut stream, 400, msg.as_bytes());
                }
            };
            let symbol = cmd.symbol().to_string();
            match d.submit(&symbol, Req::Cmd(cmd)) {
                Resp::Report(r) => {
                    let bytes = serde_json::to_vec(&*r).unwrap_or_default();
                    write_json(&mut stream, 200, &bytes)
                }
                Resp::Error(e) => {
                    let msg = format!(r#"{{"error":"{e}"}}"#);
                    write_json(&mut stream, 400, msg.as_bytes())
                }
                _ => write_json(&mut stream, 500, br#"{"error":"bad response"}"#),
            }
        }
        ("GET", "/engine/depth") => {
            let symbol = query_param(&path, "symbol").unwrap_or_default();
            match d.submit(&symbol, Req::Depth) {
                Resp::Depth(dep) => {
                    let bytes = serde_json::to_vec(&*dep).unwrap_or_default();
                    write_json(&mut stream, 200, &bytes)
                }
                Resp::Error(e) => {
                    let msg = format!(r#"{{"error":"{e}"}}"#);
                    write_json(&mut stream, 404, msg.as_bytes())
                }
                _ => write_json(&mut stream, 500, br#"{"error":"bad response"}"#),
            }
        }
        _ => write_json(&mut stream, 404, br#"{"error":"not found"}"#),
    }
}

/// 解析一个 HTTP/1.1 请求：方法、路径、小写后的头，以及 body。
fn read_request(
    stream: &mut TcpStream,
) -> std::io::Result<(String, String, HashMap<String, String>, Vec<u8>)> {
    let mut buf = Vec::new();
    let mut tmp = [0u8; 4096];
    // 一直读到完整的头部块（空行 \r\n\r\n）。头部超过 8MiB 直接拒绝。
    let header_end = loop {
        if let Some(pos) = find_subslice(&buf, b"\r\n\r\n") {
            break pos;
        }
        let n = stream.read(&mut tmp)?;
        if n == 0 {
            break buf.len();
        }
        buf.extend_from_slice(&tmp[..n]);
        if buf.len() > 8 * 1024 * 1024 {
            return Err(std::io::Error::new(std::io::ErrorKind::InvalidData, "headers too large"));
        }
    };

    let head = String::from_utf8_lossy(&buf[..header_end]).to_string();
    let mut lines = head.split("\r\n");
    let request_line = lines.next().unwrap_or("");
    let mut parts = request_line.split_whitespace();
    let method = parts.next().unwrap_or("").to_string();
    let path = parts.next().unwrap_or("").to_string();

    let mut headers = HashMap::new();
    for line in lines {
        if let Some((k, v)) = line.split_once(':') {
            headers.insert(k.trim().to_ascii_lowercase(), v.trim().to_string());
        }
    }

    let content_len: usize = headers.get("content-length").and_then(|v| v.parse().ok()).unwrap_or(0);
    let mut body = buf[(header_end + 4).min(buf.len())..].to_vec();
    while body.len() < content_len {
        let n = stream.read(&mut tmp)?;
        if n == 0 {
            break;
        }
        body.extend_from_slice(&tmp[..n]);
    }
    body.truncate(content_len);
    Ok((method, path, headers, body))
}

fn write_json(stream: &mut TcpStream, status: u16, body: &[u8]) -> std::io::Result<()> {
    write_response(stream, status, "application/json", body)
}

fn write_response(stream: &mut TcpStream, status: u16, content_type: &str, body: &[u8]) -> std::io::Result<()> {
    let reason = match status {
        200 => "OK",
        400 => "Bad Request",
        401 => "Unauthorized",
        404 => "Not Found",
        _ => "Internal Server Error",
    };
    let header = format!(
        "HTTP/1.1 {status} {reason}\r\nContent-Type: {content_type}\r\nContent-Length: {}\r\nConnection: close\r\n\r\n",
        body.len()
    );
    stream.write_all(header.as_bytes())?;
    stream.write_all(body)?;
    stream.flush()
}

fn path_only(path: &str) -> &str {
    path.split('?').next().unwrap_or(path)
}

fn query_param(path: &str, key: &str) -> Option<String> {
    let q = path.split_once('?')?.1;
    for pair in q.split('&') {
        if let Some((k, v)) = pair.split_once('=') {
            if k == key {
                return Some(v.to_string());
            }
        }
    }
    None
}

fn find_subslice(haystack: &[u8], needle: &[u8]) -> Option<usize> {
    haystack.windows(needle.len()).position(|w| w == needle)
}
