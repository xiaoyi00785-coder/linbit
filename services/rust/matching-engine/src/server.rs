//! Local command transport: a minimal blocking HTTP server that fronts the
//! per-symbol matching loops.
//!
//! Each symbol runs on its own thread; the HTTP handler routes a command to the
//! right symbol over a channel and blocks on a reply channel, so matching stays
//! single-threaded per symbol while the service handles many connections. This
//! is the *local* implementation of the replaceable messaging interface; the
//! production path would consume commands from Kafka and drive the same loops
//! (see `bus.rs` for the event-side Kafka skeleton).

use std::collections::HashMap;
use std::io::{Read, Write};
use std::net::{TcpListener, TcpStream};
use std::sync::mpsc::{sync_channel, Receiver, RecvTimeoutError, SyncSender};
use std::sync::Arc;
use std::thread;
use std::time::{Duration, Instant};

use crate::book::SymbolEngine;
use crate::types::{Command, DepthEvent, ExecReport};

/// WAL durability policy for the matching loops.
#[derive(Clone, Copy, Debug)]
pub enum CommitMode {
    /// Each command is fsync'd on its own before it is applied/acknowledged.
    PerCommand,
    /// Commands are coalesced into a batch — bounded by `max_batch` records or a
    /// `window` of wall-clock time, whichever comes first — and the batch is made
    /// durable with a single fsync before any of its commands are
    /// applied/acknowledged.
    Group { max_batch: usize, window: Duration },
}

/// A request handed to a symbol's matching thread.
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

/// Routes commands to per-symbol matching threads.
#[derive(Clone)]
pub struct Dispatcher {
    senders: Arc<HashMap<String, SyncSender<Job>>>,
}

impl Dispatcher {
    /// Spawn one matching thread per engine and return a dispatcher over them.
    /// `mode` selects the WAL durability policy the threads use.
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

/// Per-command policy: handle each job as it arrives, fsyncing every command on
/// its own before replying.
fn run_per_command(mut engine: SymbolEngine, rx: Receiver<Job>) {
    for job in rx {
        let resp = match job.req {
            Req::Cmd(cmd) => Resp::Report(Box::new(engine.process(&cmd))),
            Req::Depth => Resp::Depth(Box::new(engine.depth_snapshot())),
        };
        let _ = job.reply.send(resp);
    }
}

/// Group-commit policy: coalesce jobs that are waiting in the channel into a
/// batch (bounded by `max_batch` or `window`), make the batch's commands durable
/// with a single fsync, then apply and reply to all of them.
fn run_group_commit(mut engine: SymbolEngine, rx: Receiver<Job>, max_batch: usize, window: Duration) {
    // Block for the first job of a batch; once one arrives, keep pulling already-
    // queued (or soon-to-arrive within `window`) jobs until the batch is full or
    // the window closes. A lone command therefore waits at most `window` before
    // its fsync; under load, batches fill to `max_batch` and the window is moot.
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
                // Senders gone: drain what we have, then the outer recv() ends.
                Err(RecvTimeoutError::Disconnected) => break,
            }
        }
        process_batch_jobs(&mut engine, jobs);
    }
}

/// Apply a collected batch of jobs in arrival order. Consecutive commands are
/// coalesced into one group-committed WAL batch; a depth query flushes any
/// pending commands first so it observes their effects, preserving the exact
/// ordering a per-command loop would have produced.
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

/// Group-commit the buffered commands (one fsync) and reply with each report.
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

/// Run the HTTP server until the process is killed.
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

    // Health check is unauthenticated.
    if method == "GET" && path == "/healthz" {
        return write_response(&mut stream, 200, "text/plain", b"ok");
    }

    // All other endpoints require the shared internal token when one is set.
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

/// Parse an HTTP/1.1 request: method, path, lowercased headers, and body.
fn read_request(
    stream: &mut TcpStream,
) -> std::io::Result<(String, String, HashMap<String, String>, Vec<u8>)> {
    let mut buf = Vec::new();
    let mut tmp = [0u8; 4096];
    // Read until we have the full header block.
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
