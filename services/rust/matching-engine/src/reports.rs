//! The ordered, durable result stream of a symbol.
//!
//! Every WAL-logged command yields exactly one [`ReportEntry`] (seq-ordered).
//! The order service consumes the stream with its own cursor and drives *all*
//! settlement from it, so a timed-out or dropped HTTP reply can never lose a
//! fill, and fills are settled in engine order (no cancel-vs-fill races).
//!
//! Durability without an extra fsync per batch: entries are a deterministic
//! function of snapshot + WAL, so the report log is only appended (not
//! fsynced) per batch. The invariant that makes recovery exact is enforced at
//! snapshot time: before the WAL is truncated, the log is rewritten (unacked
//! entries only) and fsynced, so every entry up to the snapshot seq survives
//! either in the log or as acknowledged. On startup, entries missing from the
//! log tail are regenerated while replaying the WAL.
//!
//! Retention: entries are dropped only after the consumer acknowledges them
//! (`ack`); unacknowledged entries are never evicted.

use std::collections::VecDeque;
use std::fs::{self, File, OpenOptions};
use std::io::{BufRead, BufReader, Write};
use std::path::{Path, PathBuf};

use serde::{Deserialize, Serialize};

use crate::types::ReportEntry;
use crate::wal::repair_jsonl;

/// Warn once the unacknowledged backlog grows beyond this many entries.
const BACKLOG_WARN: usize = 100_000;

#[derive(Debug, Default, Serialize, Deserialize)]
struct AckFile {
    acked: u64,
}

/// In-memory queue of unacknowledged entries, optionally backed by files.
pub struct ReportLog {
    queue: VecDeque<ReportEntry>,
    acked: u64,
    /// Highest seq present in the log or acknowledged (entries above it must be
    /// regenerated from the WAL during recovery).
    last_seq: u64,
    dir: Option<PathBuf>,
    file: Option<File>,
    warned: bool,
}

/// What `ReportLog::open` found on disk.
pub struct Loaded {
    pub log: ReportLog,
    /// True when neither a report log nor an ack file existed: the engine is
    /// adopting the stream for the first time (upgrade from a version without
    /// it). Historical commands were already settled through the old
    /// synchronous path, so they must NOT be regenerated into the stream.
    pub fresh: bool,
}

impl ReportLog {
    /// A purely in-memory log (tests, benchmark).
    pub fn in_memory() -> Self {
        Self { queue: VecDeque::new(), acked: 0, last_seq: 0, dir: None, file: None, warned: false }
    }

    /// Load the log and ack marker from `dir`, repairing a torn tail.
    pub fn open(dir: &Path) -> std::io::Result<Loaded> {
        fs::create_dir_all(dir)?;
        let log_path = dir.join("reports.log");
        let ack_path = dir.join("reports.ack");
        let fresh = !log_path.exists() && !ack_path.exists();

        let acked = match fs::read(&ack_path) {
            Ok(b) => serde_json::from_slice::<AckFile>(&b).map(|a| a.acked).unwrap_or(0),
            Err(_) => 0,
        };
        repair_jsonl::<ReportEntry>(&log_path)?;
        let mut queue = VecDeque::new();
        let mut last_seq = acked;
        if let Ok(f) = File::open(&log_path) {
            for line in BufReader::new(f).lines() {
                let line = line?;
                if line.trim().is_empty() {
                    continue;
                }
                let e: ReportEntry = serde_json::from_str(&line)
                    .map_err(|e| std::io::Error::new(std::io::ErrorKind::InvalidData, e))?;
                last_seq = last_seq.max(e.seq);
                if e.seq > acked {
                    queue.push_back(e);
                }
            }
        }
        let file = OpenOptions::new().create(true).append(true).open(&log_path)?;
        Ok(Loaded {
            log: Self { queue, acked, last_seq, dir: Some(dir.to_path_buf()), file: Some(file), warned: false },
            fresh,
        })
    }

    /// Highest seq covered by the log or by an acknowledgement.
    pub fn last_seq(&self) -> u64 {
        self.last_seq
    }

    pub fn acked(&self) -> u64 {
        self.acked
    }

    /// Number of unacknowledged entries.
    pub fn backlog(&self) -> usize {
        self.queue.len()
    }

    /// Append a batch of entries (in seq order). Written to the OS but not
    /// fsynced: recovery regenerates any lost tail from the WAL.
    pub fn append(&mut self, entries: Vec<ReportEntry>) {
        if entries.is_empty() {
            return;
        }
        if let Some(f) = self.file.as_mut() {
            let mut buf = Vec::new();
            for e in &entries {
                if let Ok(mut line) = serde_json::to_vec(e) {
                    line.push(b'\n');
                    buf.extend_from_slice(&line);
                }
            }
            if let Err(e) = f.write_all(&buf) {
                // Not fatal: the WAL is durable and the tail is regenerated on
                // recovery; the in-memory queue still serves consumers.
                eprintln!("report log append failed: {e}");
            }
        }
        for e in entries {
            self.last_seq = self.last_seq.max(e.seq);
            self.queue.push_back(e);
        }
        if self.queue.len() > BACKLOG_WARN && !self.warned {
            self.warned = true;
            eprintln!("WARNING: {} unacknowledged report entries — is the order service consuming?", self.queue.len());
        } else if self.queue.len() <= BACKLOG_WARN {
            self.warned = false;
        }
    }

    /// Entries with seq > `after`, at most `limit`.
    pub fn after(&self, after: u64, limit: usize) -> Vec<ReportEntry> {
        // The queue is seq-ordered; skip the acknowledged-or-consumed prefix.
        let start = self.queue.partition_point(|e| e.seq <= after);
        self.queue.iter().skip(start).take(limit).cloned().collect()
    }

    /// Acknowledge everything up to `seq` (inclusive). Idempotent; never moves
    /// backwards. The ack marker is replaced atomically.
    pub fn ack(&mut self, seq: u64) -> std::io::Result<()> {
        let seq = seq.min(self.last_seq);
        if seq <= self.acked {
            return Ok(());
        }
        self.acked = seq;
        while self.queue.front().is_some_and(|e| e.seq <= seq) {
            self.queue.pop_front();
        }
        if let Some(dir) = &self.dir {
            write_atomic(&dir.join("reports.ack"), &serde_json::to_vec(&AckFile { acked: seq }).unwrap_or_default(), false)?;
        }
        Ok(())
    }

    /// Mark the stream as starting after `seq` (first adoption: nothing before
    /// it is ever delivered).
    pub fn adopt_at(&mut self, seq: u64) -> std::io::Result<()> {
        self.last_seq = self.last_seq.max(seq);
        self.ack(seq)
    }

    /// Rewrite the log with only the unacknowledged entries and fsync it. Must
    /// run *before* the WAL is truncated at snapshot time (see module docs).
    pub fn compact_and_sync(&mut self) -> std::io::Result<()> {
        let Some(dir) = self.dir.clone() else { return Ok(()) };
        let mut buf = Vec::new();
        for e in &self.queue {
            let mut line = serde_json::to_vec(e).map_err(|e| std::io::Error::new(std::io::ErrorKind::InvalidData, e))?;
            line.push(b'\n');
            buf.extend_from_slice(&line);
        }
        let path = dir.join("reports.log");
        write_atomic(&path, &buf, true)?;
        self.file = Some(OpenOptions::new().create(true).append(true).open(&path)?);
        // The ack marker must be durable too, or a crash could resurrect
        // acknowledged entries that the rewritten log no longer contains.
        write_atomic(&dir.join("reports.ack"), &serde_json::to_vec(&AckFile { acked: self.acked }).unwrap_or_default(), true)?;
        Ok(())
    }
}

fn write_atomic(path: &Path, bytes: &[u8], sync: bool) -> std::io::Result<()> {
    // "<name>.tmp" (not with_extension, which would map both reports.log and
    // reports.ack onto the same reports.tmp).
    let tmp = PathBuf::from(format!("{}.tmp", path.display()));
    {
        let mut f = File::create(&tmp)?;
        f.write_all(bytes)?;
        if sync {
            f.sync_all()?;
        }
    }
    fs::rename(&tmp, path)
}
