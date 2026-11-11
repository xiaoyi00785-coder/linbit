//! 崩溃恢复用的预写日志和快照。
//!
//! 每条被接受的命令先追加到 WAL 并落盘，然后才改订单簿，所以已经确认的订单不会因为崩溃丢失。
//! 定期把整个订单簿写成快照并截断 WAL；启动时加载最新快照，再重放 WAL 尾部，
//! 恢复到崩溃前的状态（包括成交号）。
//!
//! 两种 fsync 策略都保证：字节落盘之后才会确认命令。
//!
//! * 逐条：[`Wal::append`] 写一条并立刻 fsync。
//! * 组提交：[`Wal::append_buffered`] 先把若干条写入文件但不 fsync，再由一次 [`Wal::sync`]
//!   让整批同时落盘，把占大头的 fsync 成本摊到这一批上。调用方必须等 `sync` 返回后才应用并确认。

use std::fs::{self, File, OpenOptions};
use std::io::{BufRead, BufReader, Write};
use std::path::{Path, PathBuf};

use serde::{Deserialize, Serialize};

use crate::types::Command;

/// 一条 WAL 记录：该交易对上的序号，加上原始命令。重放时按序号顺序应用。
#[derive(Debug, Serialize, Deserialize)]
pub struct WalRecord {
    pub seq: u64,
    pub command: Command,
}

/// 单文件追加的预写日志。
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

    /// 追加一条记录并 fsync。返回时字节已经落盘。这是逐条策略（每条一次 fsync）。
    pub fn append(&mut self, rec: &WalRecord) -> std::io::Result<()> {
        self.append_buffered(rec)?;
        self.sync()
    }

    /// 把一条记录写入 WAL 文件，但不 fsync。数据到了操作系统，还不能算落盘；
    /// 接下来的 [`Wal::sync`] 把上次 sync 之后写入的记录一次做成持久的。
    /// 这是组提交的写路径：只调用过 `append_buffered`、对应的 `sync` 还没返回的命令，不能确认。
    pub fn append_buffered(&mut self, rec: &WalRecord) -> std::io::Result<()> {
        let mut line = serde_json::to_vec(rec).map_err(to_io)?;
        line.push(b'\n');
        self.file.write_all(&line)?;
        Ok(())
    }

    /// 把之前 [`Wal::append_buffered`] 写入的内容 flush 并 fsync。返回时这一批已经落盘。
    pub fn sync(&mut self) -> std::io::Result<()> {
        self.file.flush()?;
        self.file.sync_data()
    }

    /// 按顺序读出 WAL 里现有的全部记录。
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
            // 最后一行写了一半（fsync 中断）就停在这里丢掉，不当成致命错误。它属于尚未确认的那一批。
            match serde_json::from_str::<WalRecord>(&line) {
                Ok(r) => out.push(r),
                Err(_) => break,
            }
        }
        Ok(out)
    }

    /// 把 WAL 截成空文件。只在快照已经写成功之后调用。
    pub fn truncate(&mut self) -> std::io::Result<()> {
        self.file.set_len(0)?;
        self.file.sync_all()?;
        // 重新以追加方式打开，写偏移回到 0。set_len(0) 不会自己移动已打开文件的偏移。
        self.file = OpenOptions::new().create(true).append(true).open(&self.path)?;
        Ok(())
    }
}

/// 把 `state` 原子地写到 `dir/snapshot.json`：先写临时文件并 fsync，再 rename。
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

/// 从 `dir` 读取快照。还没有快照时返回 `None`。
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
