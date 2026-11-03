//! Minimal blocking HTTP/1.1 client — just enough to POST a JSON body to a
//! local consumer. No external HTTP crate is needed for the local deployment.

use std::io::{Read, Write};
use std::net::TcpStream;
use std::time::Duration;

/// POST `body` to `url` with `Content-Type: application/json` and an optional
/// `X-Internal-Token`. Returns the HTTP status code on success.
pub fn post(url: &str, token: &str, body: &[u8]) -> Result<u16, String> {
    let (host, port, path) = parse_url(url)?;
    let addr = format!("{host}:{port}");
    let mut stream =
        TcpStream::connect(&addr).map_err(|e| format!("connect {addr}: {e}"))?;
    stream
        .set_read_timeout(Some(Duration::from_secs(5)))
        .map_err(|e| e.to_string())?;
    stream
        .set_write_timeout(Some(Duration::from_secs(5)))
        .map_err(|e| e.to_string())?;

    let mut req = format!(
        "POST {path} HTTP/1.1\r\nHost: {host}\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n",
        body.len()
    );
    if !token.is_empty() {
        req.push_str(&format!("X-Internal-Token: {token}\r\n"));
    }
    req.push_str("\r\n");
    stream
        .write_all(req.as_bytes())
        .and_then(|_| stream.write_all(body))
        .map_err(|e| format!("write: {e}"))?;

    let mut resp = Vec::new();
    stream
        .read_to_end(&mut resp)
        .map_err(|e| format!("read: {e}"))?;
    let head = String::from_utf8_lossy(&resp);
    let status = head
        .split_whitespace()
        .nth(1)
        .and_then(|s| s.parse::<u16>().ok())
        .ok_or_else(|| "malformed status line".to_string())?;
    Ok(status)
}

fn parse_url(url: &str) -> Result<(String, u16, String), String> {
    let rest = url
        .strip_prefix("http://")
        .ok_or_else(|| format!("only http:// urls supported: {url}"))?;
    let (authority, path) = match rest.find('/') {
        Some(i) => (&rest[..i], &rest[i..]),
        None => (rest, "/"),
    };
    let (host, port) = match authority.split_once(':') {
        Some((h, p)) => (h.to_string(), p.parse().map_err(|_| "bad port")?),
        None => (authority.to_string(), 80),
    };
    Ok((host, port, path.to_string()))
}
