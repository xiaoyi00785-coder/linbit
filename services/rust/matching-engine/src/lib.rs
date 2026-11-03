//! Linbit spot matching engine library.
//!
//! The crate is split so both the service binary (`main.rs`) and the benchmark
//! (`bin/bench.rs`) can drive the same matching core, and so unit/integration
//! tests can exercise it without any transport.

pub mod book;
pub mod bus;
pub mod fixed;
pub mod httpc;
pub mod server;
pub mod types;
pub mod wal;
