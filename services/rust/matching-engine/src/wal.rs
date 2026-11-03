//! Write-ahead log and snapshots for crash recovery.
//!
//! Every accepted command is appended to the WAL and flushed to disk *before*
//! it is applied to the book, so a crash can never lose an acknowledged order.
//! Periodically the full book is written out as a snapshot and the WAL is
//! truncated; on startup the engine loads the latest snapshot and replays the
//! WAL tail, reproducing the exact pre-crash state (including trade ids).
//!
//! Durability can use one of two fsync policies, both of which guarantee that a
//! command is only acknowledged after its bytes are durable:
//!
//! * **Per-command** — [`Wal::append`] writes one record and `fsync`s it.
//! * **Group commit** — [`Wal::append_buffered`] stages several records with no
//!   `fsync`, then a single [`Wal::sync`] makes the whole batch durable at once,
//!   amortizing the (dominant) fsync cost across the batch. Callers apply and
//!   acknowledge a batch only after its `sync` returns.

use std::fs::{self, File, OpenOptions};
use std::io::{BufRead, BufReader, Write};
use std::path::{Path, PathBuf};

use serde::{Deserialize, Serialize};

use crate::types::Command;

/// One durable WAL record: the global-per-symbol sequence plus its command.
#[derive(Debug, Serialize, Deserialize)]
pub struct WalRecord {
    pub seq: u64,
    pub command: Command,
}

/// Append-only write-ahead log backed by a single file.
pub struct Wal {
    path: PathBuf,
    file: File,
}

impl Wal {
    pub fn open(dir: &Path) -> std::io::Result<Self> {
        fs::create_dir_all(dir)?;
        let path = dir.join("wal.log");
        let file = OpenOptions::new().create(true).append(true).open(&path)?;
        Ok(Self { path, file })
    }

    /// Append a record and fsync it. Returns only after the bytes are durable.
    /// This is the per-command policy (one fsync per record).
    pub fn append(&mut self, rec: &WalRecord) -> std::io::Result<()> {
        self.append_buffered(rec)?;
        self.sync()
    }

    /// Stage a record into the WAL file *without* an fsync. The bytes reach the
    /// OS but are not yet guaranteed durable; a subsequent [`Wal::sync`] makes
    /// every record staged since the last sync durable in one call. This is the
    /// group-commit write path — never acknowledge a command whose record was
    /// only `append_buffered`'d until a `sync` has returned.
    pub fn append_buffered(&mut self, rec: &WalRecord) -> std::io::Result<()> {
        let mut line = serde_json::to_vec(rec).map_err(to_io)?;
        line.push(b'\n');
        self.file.write_all(&line)?;
        Ok(())
    }

    /// Flush and fsync everything staged by prior [`Wal::append_buffered`] calls.
    /// Returns only after the batch is durable.
    pub fn sync(&mut self) -> std::io::Result<()> {
        self.file.flush()?;
        self.file.sync_data()
    }

    /// Read every record currently in the WAL, in order.
    pub fn read_all(dir: &Path) -> std::io::Result<Vec<WalRecord>> {
        let path = dir.join("wal.log");
        let file = match File::open(&path) {
            Ok(f) => f,
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(vec![]),
            Err(e) => return Err(e),
        };
        let mut out = Vec::new();
        for line in BufReader::new(file).lines() {
            let line = line?;
            if line.trim().is_empty() {
                continue;
            }
            // A torn final line (partial fsync) is dropped rather than fatal.
            match serde_json::from_str::<WalRecord>(&line) {
                Ok(r) => out.push(r),
                Err(_) => break,
            }
        }
        Ok(out)
    }

    /// Truncate the WAL to empty (called right after a snapshot).
    pub fn truncate(&mut self) -> std::io::Result<()> {
        self.file.set_len(0)?;
        self.file.sync_all()?;
        // Re-open in append mode so the write offset resets to 0.
        self.file = OpenOptions::new().create(true).append(true).open(&self.path)?;
        Ok(())
    }
}

/// Persist `state` to `dir/snapshot.json` atomically (write temp, then rename).
pub fn write_snapshot<S: Serialize>(dir: &Path, state: &S) -> std::io::Result<()> {
    fs::create_dir_all(dir)?;
    let tmp = dir.join("snapshot.json.tmp");
    let final_path = dir.join("snapshot.json");
    let bytes = serde_json::to_vec(state).map_err(to_io)?;
    {
        let mut f = File::create(&tmp)?;
        f.write_all(&bytes)?;
        f.sync_all()?;
    }
    fs::rename(&tmp, &final_path)?;
    Ok(())
}

/// Load a snapshot from `dir`, or `None` if there is none yet.
pub fn read_snapshot<S: for<'de> Deserialize<'de>>(dir: &Path) -> std::io::Result<Option<S>> {
    let path = dir.join("snapshot.json");
    match fs::read(&path) {
        Ok(bytes) => Ok(Some(serde_json::from_slice(&bytes).map_err(to_io)?)),
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(None),
        Err(e) => Err(e),
    }
}

fn to_io(e: serde_json::Error) -> std::io::Error {
    std::io::Error::new(std::io::ErrorKind::InvalidData, e)
}
