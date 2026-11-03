# Build context is this directory:  docker build services/rust/matching-engine
FROM rust:1.85 AS build
WORKDIR /src
COPY Cargo.toml Cargo.lock ./
COPY src ./src
COPY tests ./tests
RUN cargo build --release --bin matching-engine

FROM debian:bookworm-slim
COPY --from=build /src/target/release/matching-engine /matching-engine
# WAL + snapshots live here; mount a volume to persist across restarts.
ENV DATA_DIR=/data
EXPOSE 8083
ENTRYPOINT ["/matching-engine"]
