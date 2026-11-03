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
use std::sync::mpsc::{sync_channel, Receiver, SyncSender};
use std::sync::Arc;
use std::thread;

use crate::book::SymbolEngine;
use crate::types::{Command, DepthEvent, ExecReport};

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
    pub fn spawn(engines: Vec<SymbolEngine>) -> Self {
        let mut senders = HashMap::new();
        for mut engine in engines {
            let (tx, rx): (SyncSender<Job>, Receiver<Job>) = sync_channel(1024);
            let symbol = engine.symbol.clone();
            thread::Builder::new()
                .name(format!("match-{symbol}"))
                .spawn(move || {
                    for job in rx {
                        let resp = match job.req {
                            Req::Cmd(cmd) => Resp::Report(Box::new(engine.process(&cmd))),
                            Req::Depth => Resp::Depth(Box::new(engine.depth_snapshot())),
                        };
                        let _ = job.reply.send(resp);
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
