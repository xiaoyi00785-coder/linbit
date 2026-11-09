//! 现货撮合引擎库。
//!
//! 服务进程（`main.rs`）和压测二进制（`bin/bench.rs`）共用同一套撮合核心，
//! 单测和集成测试也可以不经过传输层直接驱动它。

pub mod book;
pub mod bus;
pub mod fixed;
pub mod httpc;
pub mod server;
pub mod types;
pub mod wal;
