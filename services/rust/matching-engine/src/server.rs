//! Local command transport: a minimal blocking HTTP server that fronts the
//! per-symbol matching loops.
//!
//! Each symbol runs on its own thread; the HTTP handler routes a command to the
//! right symbol over a channel and blocks on a reply channel, so matching stays
//! single-threaded per symbol while the service handles many connections. This
//! is the *local* implementation of the replaceable messaging interface; the
//! production path would consume commands from Kafka and drive the same loops
//! (see `bus.rs` for the Kafka producer).

use std::collections::HashMap;
use std::fs::File;
use std::io::{BufReader, ErrorKind, Read, Write};
use std::net::{TcpListener, TcpStream};
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::mpsc::{sync_channel, Receiver, RecvTimeoutError, SyncSender};
use std::sync::{Arc, Once};
use std::thread;
use std::time::{Duration, Instant};

use serde::Deserialize;

use crate::book::{StreamPage, SymbolEngine};
use crate::types::{Command, DepthEvent, ExecReport, RestingView};

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
    /// Result-stream page after `after`, at most `limit` entries.
    Reports { after: u64, limit: usize },
    /// Acknowledge stream entries up to and including this seq.
    Ack(u64),
    /// All resting orders (reconciliation only).
    Orders,
}

enum Resp {
    Report(Box<ExecReport>),
    Depth(Box<DepthEvent>),
    Page(Box<StreamPage>),
    Orders(Vec<RestingView>),
    Acked,
    /// The request itself is invalid (e.g. unknown symbol): it was definitely
    /// NOT applied, so callers may treat it as a rejection.
    BadRequest(String),
    /// The outcome is unknown (matching thread gone, no reply, I/O failure).
    /// Callers must NOT treat this as a rejection; the result stream is the
    /// source of truth for whether a command executed.
    Unavailable(String),
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
            return Resp::BadRequest(format!("unknown symbol {symbol}"));
        };
        let (rtx, rrx) = sync_channel(1);
        if tx.send(Job { req, reply: rtx }).is_err() {
            return Resp::Unavailable("matching thread gone".into());
        }
        rrx.recv().unwrap_or_else(|_| Resp::Unavailable("no reply".into()))
    }
}

/// Serve every non-command request on the symbol's matching thread, so reads
/// observe a consistent engine state (and `stream_page` advances the engine
/// clock on the same thread that stamps commands).
fn handle_query(engine: &mut SymbolEngine, req: Req) -> Resp {
    match req {
        Req::Depth => Resp::Depth(Box::new(engine.depth_snapshot())),
        Req::Reports { after, limit } => Resp::Page(Box::new(engine.stream_page(after, limit))),
        Req::Ack(seq) => match engine.ack_reports(seq) {
            Ok(()) => Resp::Acked,
            Err(e) => Resp::Unavailable(format!("ack failed: {e}")),
        },
        Req::Orders => Resp::Orders(engine.resting_orders()),
        Req::Cmd(_) => Resp::BadRequest("not a query".into()),
    }
}

/// Per-command policy: handle each job as it arrives, fsyncing every command on
/// its own before replying.
fn run_per_command(mut engine: SymbolEngine, rx: Receiver<Job>) {
    for job in rx {
        let resp = match job.req {
            Req::Cmd(cmd) => Resp::Report(Box::new(engine.process(&cmd))),
            other => handle_query(&mut engine, other),
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
            other => {
                flush_command_batch(engine, &mut cmds, &mut replies);
                let _ = job.reply.send(handle_query(engine, other));
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

/// Hard limits for the hand-rolled HTTP/1.1 front. Headers past this are
/// rejected before the body is read; a Content-Length above the body cap is
/// rejected without allocating that many bytes.
const MAX_HEADER_BYTES: usize = 8 * 1024;
const MAX_BODY_BYTES: usize = 64 * 1024;
const KEEPALIVE_IDLE: Duration = Duration::from_secs(30);
const MAX_KEEPALIVE_REQUESTS: usize = 100;

static REQ_SEQ: AtomicU64 = AtomicU64::new(1);

/// Optional TLS material. Both files are PEM.
pub struct TlsPaths {
    pub cert: String,
    pub key: String,
}

/// Run the HTTP server until the process is killed. Plain TCP.
pub fn serve(bind: &str, token: String, dispatcher: Dispatcher) -> std::io::Result<()> {
    serve_with(bind, token, dispatcher, None)
}

/// Like [`serve`], with optional TLS. `tls` requires both a certificate chain
/// and a private key; the listener then speaks TLS to every connection.
pub fn serve_with(
    bind: &str,
    token: String,
    dispatcher: Dispatcher,
    tls: Option<TlsPaths>,
) -> std::io::Result<()> {
    let tls_cfg = match tls {
        Some(p) => Some(load_tls(&p.cert, &p.key).map_err(|e| {
            std::io::Error::new(std::io::ErrorKind::InvalidInput, e)
        })?),
        None => None,
    };
    let listener = TcpListener::bind(bind)?;
    let mode = if tls_cfg.is_some() { "tls" } else { "plain" };
    println!("matching-engine listening on {bind} ({mode})");
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
        let tls_cfg = tls_cfg.clone();
        thread::spawn(move || {
            let net = match tls_cfg {
                None => Net::Plain(stream),
                Some(cfg) => match rustls::ServerConnection::new(cfg) {
                    Ok(conn) => Net::Tls(rustls::StreamOwned::new(conn, stream)),
                    Err(e) => {
                        eprintln!("tls handshake setup: {e}");
                        return;
                    }
                },
            };
            if let Err(e) = handle_conn(net, &token, &d) {
                if !quiet_io(&e) {
                    eprintln!("conn: {e}");
                }
            }
        });
    }
    Ok(())
}

fn quiet_io(e: &std::io::Error) -> bool {
    matches!(
        e.kind(),
        ErrorKind::UnexpectedEof
            | ErrorKind::ConnectionReset
            | ErrorKind::ConnectionAborted
            | ErrorKind::BrokenPipe
            | ErrorKind::TimedOut
            | ErrorKind::WouldBlock
    )
}

enum Net {
    Plain(TcpStream),
    Tls(rustls::StreamOwned<rustls::ServerConnection, TcpStream>),
}

impl Net {
    fn set_read_timeout(&mut self, t: Option<Duration>) -> std::io::Result<()> {
        match self {
            Net::Plain(s) => s.set_read_timeout(t),
            Net::Tls(s) => s.sock.set_read_timeout(t),
        }
    }
}

impl Read for Net {
    fn read(&mut self, buf: &mut [u8]) -> std::io::Result<usize> {
        match self {
            Net::Plain(s) => s.read(buf),
            Net::Tls(s) => s.read(buf),
        }
    }
}

impl Write for Net {
    fn write(&mut self, buf: &[u8]) -> std::io::Result<usize> {
        match self {
            Net::Plain(s) => s.write(buf),
            Net::Tls(s) => s.write(buf),
        }
    }
    fn flush(&mut self) -> std::io::Result<()> {
        match self {
            Net::Plain(s) => s.flush(),
            Net::Tls(s) => s.flush(),
        }
    }
}

fn load_tls(cert_path: &str, key_path: &str) -> Result<Arc<rustls::ServerConfig>, String> {
    static ONCE: Once = Once::new();
    ONCE.call_once(|| {
        let _ = rustls::crypto::ring::default_provider().install_default();
    });
    let mut cert_r = BufReader::new(File::open(cert_path).map_err(|e| format!("open TLS cert: {e}"))?);
    let certs: Vec<rustls::pki_types::CertificateDer<'static>> = rustls_pemfile::certs(&mut cert_r)
        .collect::<Result<Vec<_>, _>>()
        .map_err(|e| format!("parse TLS cert: {e}"))?;
    if certs.is_empty() {
        return Err("TLS cert file contained no certificates".into());
    }
    let mut key_r = BufReader::new(File::open(key_path).map_err(|e| format!("open TLS key: {e}"))?);
    let key = rustls_pemfile::private_key(&mut key_r)
        .map_err(|e| format!("parse TLS key: {e}"))?
        .ok_or_else(|| "TLS key file contained no private key".to_string())?;
    let config = rustls::ServerConfig::builder()
        .with_no_client_auth()
        .with_single_cert(certs, key)
        .map_err(|e| format!("tls config: {e}"))?;
    Ok(Arc::new(config))
}

#[derive(Debug)]
struct HttpRequest {
    method: String,
    path: String,
    version: String,
    headers: HashMap<String, String>,
    body: Vec<u8>,
}

#[derive(Debug)]
enum ReadFail {
    Io(std::io::Error),
    Status(u16, &'static str),
}

fn handle_conn(mut net: Net, token: &str, d: &Dispatcher) -> std::io::Result<()> {
    let mut served = 0usize;
    loop {
        net.set_read_timeout(Some(KEEPALIVE_IDLE))?;
        let req = match read_request(&mut net) {
            Ok(r) => r,
            Err(ReadFail::Io(e)) if quiet_io(&e) => return Ok(()),
            Err(ReadFail::Io(e)) => return Err(e),
            Err(ReadFail::Status(code, msg)) => {
                let id = next_request_id(&HashMap::new());
                let _ = write_http(&mut net, code, msg.as_bytes(), &id, false);
                eprintln!("req id={id} -> {code} {msg}");
                return Ok(());
            }
        };
        served += 1;
        let id = next_request_id(&req.headers);
        let close = want_close(&req.version, &req.headers, served);
        let status = match route(&mut net, &req, token, d, &id, !close) {
            Ok(s) => s,
            Err(e) if quiet_io(&e) => return Ok(()),
            Err(e) => return Err(e),
        };
        eprintln!("req id={id} {} {} -> {status}", req.method, path_only(&req.path));
        if close {
            return Ok(());
        }
    }
}

fn route(
    net: &mut Net,
    req: &HttpRequest,
    token: &str,
    d: &Dispatcher,
    id: &str,
    keepalive: bool,
) -> std::io::Result<u16> {
    if req.method == "GET" && path_only(&req.path) == "/healthz" {
        write_http(net, 200, b"ok", id, keepalive)?;
        return Ok(200);
    }
    if !token_ok(token, req.headers.get("x-internal-token").map(String::as_str).unwrap_or("")) {
        write_http(net, 401, br#"{"error":"unauthorized"}"#, id, keepalive)?;
        return Ok(401);
    }
    let symbol = query_param(&req.path, "symbol").unwrap_or_default();
    match (req.method.as_str(), path_only(&req.path)) {
        ("POST", "/engine/commands") => {
            let cmd: Command = match serde_json::from_slice(&req.body) {
                Ok(c) => c,
                Err(e) => {
                    let body = err_json(&format!("bad command: {e}"));
                    write_http(net, 400, &body, id, keepalive)?;
                    return Ok(400);
                }
            };
            let symbol = cmd.symbol().to_string();
            match d.submit(&symbol, Req::Cmd(cmd)) {
                Resp::Report(r) => {
                    let body = serde_json::to_vec(&*r).unwrap_or_default();
                    write_http(net, 200, &body, id, keepalive)?;
                    Ok(200)
                }
                other => write_failure(net, other, 400, id, keepalive),
            }
        }
        ("GET", "/engine/depth") => match d.submit(&symbol, Req::Depth) {
            Resp::Depth(dep) => {
                let body = serde_json::to_vec(&*dep).unwrap_or_default();
                write_http(net, 200, &body, id, keepalive)?;
                Ok(200)
            }
            other => write_failure(net, other, 404, id, keepalive),
        },
        ("GET", "/engine/reports") => {
            let after: u64 = query_param(&req.path, "after").and_then(|v| v.parse().ok()).unwrap_or(0);
            let limit: usize = query_param(&req.path, "limit")
                .and_then(|v| v.parse().ok())
                .unwrap_or(500usize)
                .clamp(1, 2000);
            match d.submit(&symbol, Req::Reports { after, limit }) {
                Resp::Page(p) => {
                    let body = serde_json::to_vec(&*p).unwrap_or_default();
                    write_http(net, 200, &body, id, keepalive)?;
                    Ok(200)
                }
                other => write_failure(net, other, 404, id, keepalive),
            }
        }
        ("POST", "/engine/reports/ack") => {
            let ack: AckBody = match serde_json::from_slice(&req.body) {
                Ok(a) => a,
                Err(e) => {
                    let body = err_json(&format!("bad ack: {e}"));
                    write_http(net, 400, &body, id, keepalive)?;
                    return Ok(400);
                }
            };
            match d.submit(&ack.symbol, Req::Ack(ack.seq)) {
                Resp::Acked => {
                    write_http(net, 200, br#"{"ok":true}"#, id, keepalive)?;
                    Ok(200)
                }
                other => write_failure(net, other, 400, id, keepalive),
            }
        }
        ("GET", "/engine/orders") => match d.submit(&symbol, Req::Orders) {
            Resp::Orders(o) => {
                let body = serde_json::to_vec(&o).unwrap_or_default();
                write_http(net, 200, &body, id, keepalive)?;
                Ok(200)
            }
            other => write_failure(net, other, 404, id, keepalive),
        },
        _ => {
            write_http(net, 404, br#"{"error":"not found"}"#, id, keepalive)?;
            Ok(404)
        }
    }
}

#[derive(Deserialize)]
struct AckBody {
    symbol: String,
    seq: u64,
}

fn err_json(msg: &str) -> Vec<u8> {
    serde_json::json!({ "error": msg }).to_string().into_bytes()
}

fn write_failure(net: &mut Net, resp: Resp, bad_status: u16, id: &str, keepalive: bool) -> std::io::Result<u16> {
    match resp {
        Resp::BadRequest(e) => {
            write_http(net, bad_status, &err_json(&e), id, keepalive)?;
            Ok(bad_status)
        }
        Resp::Unavailable(e) => {
            write_http(net, 503, &err_json(&e), id, keepalive)?;
            Ok(503)
        }
        _ => {
            write_http(net, 500, br#"{"error":"bad response"}"#, id, keepalive)?;
            Ok(500)
        }
    }
}

/// Constant-time comparison so a wrong internal token does not leak the prefix.
fn token_ok(expected: &str, got: &str) -> bool {
    if expected.is_empty() {
        return true;
    }
    let a = expected.as_bytes();
    let b = got.as_bytes();
    let mut diff = a.len() ^ b.len();
    let n = a.len().max(b.len());
    for i in 0..n {
        let x = if i < a.len() { a[i] } else { 0 };
        let y = if i < b.len() { b[i] } else { 0 };
        diff |= (x ^ y) as usize;
    }
    diff == 0
}

fn next_request_id(headers: &HashMap<String, String>) -> String {
    if let Some(raw) = headers.get("x-request-id") {
        let id = raw.trim();
        if is_safe_id(id) {
            return id.to_string();
        }
    }
    let n = REQ_SEQ.fetch_add(1, Ordering::Relaxed);
    format!("{n:x}")
}

fn is_safe_id(id: &str) -> bool {
    (1..=128).contains(&id.len())
        && id.bytes().all(|b| b.is_ascii_alphanumeric() || matches!(b, b'-' | b'_' | b'.'))
}

fn want_close(version: &str, headers: &HashMap<String, String>, served: usize) -> bool {
    if served >= MAX_KEEPALIVE_REQUESTS {
        return true;
    }
    let conn = headers.get("connection").map(|v| v.to_ascii_lowercase()).unwrap_or_default();
    let has = |name: &str| conn.split(',').any(|p| p.trim() == name);
    if has("close") {
        return true;
    }
    if has("keep-alive") {
        return false;
    }
    // HTTP/1.1 defaults to keep-alive; HTTP/1.0 defaults to close.
    version != "HTTP/1.1"
}

/// Parse one HTTP/1.1 request. A declared body larger than [`MAX_BODY_BYTES`]
/// or a header block larger than [`MAX_HEADER_BYTES`] is rejected before the
/// oversized bytes are buffered. Chunked bodies are refused.
fn read_request<R: Read>(stream: &mut R) -> Result<HttpRequest, ReadFail> {
    let mut buf = Vec::new();
    let mut tmp = [0u8; 1024];
    let header_end = loop {
        if let Some(pos) = find_subslice(&buf, b"\r\n\r\n") {
            if pos > MAX_HEADER_BYTES {
                return Err(ReadFail::Status(431, "headers too large"));
            }
            break pos;
        }
        if buf.len() > MAX_HEADER_BYTES {
            return Err(ReadFail::Status(431, "headers too large"));
        }
        let n = stream.read(&mut tmp).map_err(ReadFail::Io)?;
        if n == 0 {
            return Err(ReadFail::Io(std::io::Error::new(ErrorKind::UnexpectedEof, "eof")));
        }
        buf.extend_from_slice(&tmp[..n]);
    };

    let head = String::from_utf8_lossy(&buf[..header_end]).to_string();
    let mut lines = head.split("\r\n");
    let request_line = lines.next().unwrap_or("");
    let mut parts = request_line.split_whitespace();
    let method = parts.next().unwrap_or("").to_string();
    let path = parts.next().unwrap_or("").to_string();
    let version = parts.next().unwrap_or("HTTP/1.1").to_string();

    let mut headers = HashMap::new();
    for line in lines {
        if let Some((k, v)) = line.split_once(':') {
            headers.insert(k.trim().to_ascii_lowercase(), v.trim().to_string());
        }
    }

    if headers
        .get("transfer-encoding")
        .map(|v| v.to_ascii_lowercase().contains("chunked"))
        .unwrap_or(false)
    {
        return Err(ReadFail::Status(411, "chunked transfer encoding is not supported"));
    }
    let content_len: usize = match headers.get("content-length") {
        Some(v) => v.parse().map_err(|_| ReadFail::Status(400, "bad content-length"))?,
        None => 0,
    };
    if content_len > MAX_BODY_BYTES {
        return Err(ReadFail::Status(413, "body too large"));
    }

    let mut body = buf[header_end + 4..].to_vec();
    while body.len() < content_len {
        let n = stream.read(&mut tmp).map_err(ReadFail::Io)?;
        if n == 0 {
            return Err(ReadFail::Io(std::io::Error::new(ErrorKind::UnexpectedEof, "truncated body")));
        }
        let need = content_len - body.len();
        body.extend_from_slice(&tmp[..n.min(need)]);
    }
    body.truncate(content_len);
    Ok(HttpRequest { method, path, version, headers, body })
}

fn write_http(stream: &mut Net, status: u16, body: &[u8], req_id: &str, keepalive: bool) -> std::io::Result<()> {
    let reason = match status {
        200 => "OK",
        400 => "Bad Request",
        401 => "Unauthorized",
        404 => "Not Found",
        411 => "Length Required",
        413 => "Payload Too Large",
        431 => "Request Header Fields Too Large",
        503 => "Service Unavailable",
        _ => "Internal Server Error",
    };
    let conn = if keepalive { "keep-alive" } else { "close" };
    let content_type = if body == b"ok" { "text/plain" } else { "application/json" };
    let header = format!(
        "HTTP/1.1 {status} {reason}\r\nContent-Type: {content_type}\r\nContent-Length: {}\r\nConnection: {conn}\r\nKeep-Alive: timeout={}\r\nX-Request-Id: {req_id}\r\n\r\n",
        body.len(),
        KEEPALIVE_IDLE.as_secs(),
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

#[cfg(test)]
mod http_tests {
    use super::*;
    use std::io::Cursor;

    #[test]
    fn rejects_oversized_body_before_buffering_it() {
        let req = b"POST /engine/commands HTTP/1.1\r\nHost: x\r\nContent-Length: 999999\r\n\r\n";
        match read_request(&mut Cursor::new(&req[..])) {
            Err(ReadFail::Status(413, _)) => {}
            other => panic!("{other:?}"),
        }
    }

    #[test]
    fn rejects_oversized_headers() {
        let mut req = b"GET /healthz HTTP/1.1\r\n".to_vec();
        req.extend(std::iter::repeat(b'A').take(9000));
        req.extend(b"\r\n\r\n");
        match read_request(&mut Cursor::new(req)) {
            Err(ReadFail::Status(431, _)) => {}
            other => panic!("{other:?}"),
        }
    }

    #[test]
    fn rejects_chunked_bodies() {
        let req = b"POST /x HTTP/1.1\r\nTransfer-Encoding: chunked\r\n\r\n";
        match read_request(&mut Cursor::new(&req[..])) {
            Err(ReadFail::Status(411, _)) => {}
            other => panic!("{other:?}"),
        }
    }

    #[test]
    fn parses_small_request() {
        let req = b"POST /engine/commands HTTP/1.1\r\nContent-Length: 2\r\nX-Request-Id: abc-1\r\n\r\n{}";
        let got = read_request(&mut Cursor::new(&req[..])).unwrap();
        assert_eq!(got.method, "POST");
        assert_eq!(got.path, "/engine/commands");
        assert_eq!(got.version, "HTTP/1.1");
        assert_eq!(got.body, b"{}");
        assert_eq!(got.headers.get("x-request-id").map(String::as_str), Some("abc-1"));
        assert!(!want_close(&got.version, &got.headers, 1));
    }

    #[test]
    fn http10_closes_unless_keepalive() {
        let mut h = HashMap::new();
        assert!(want_close("HTTP/1.0", &h, 1));
        h.insert("connection".into(), "keep-alive".into());
        assert!(!want_close("HTTP/1.0", &h, 1));
        assert!(want_close("HTTP/1.1", &h, MAX_KEEPALIVE_REQUESTS));
    }

    #[test]
    fn token_compare_rejects_a_prefix() {
        assert!(token_ok("", "anything"));
        assert!(token_ok("secret", "secret"));
        assert!(!token_ok("secret", "secre"));
        assert!(!token_ok("secret", "secretX"));
        assert!(!token_ok("secret", ""));
    }
}
