-- +goose Up
-- Non-custodial Web3 wallet: one row per user holds the encrypted BIP39
-- mnemonic. The mnemonic NEVER leaves the server in plaintext; everything
-- that would normally need the seed (signing, address derivation) happens in
-- the web3wallet service while the user's PIN is unlocked in memory. The
-- exchange-ledger wallet (`w_*` tables in services/go/wallet) is deliberately
-- unrelated — a user can use one, the other or both.
CREATE TABLE web3_wallets (
    uid                 TEXT PRIMARY KEY,
    encrypted_mnemonic  TEXT NOT NULL,                 -- v1.<salt>.<nonce>.<ct>
    pin_version         INT  NOT NULL DEFAULT 1,       -- bumped on every change-pin
    pin_failures        INT  NOT NULL DEFAULT 0,
    locked_until        TIMESTAMPTZ,
    backup_at           TIMESTAMPTZ,                   -- user acknowledged cloud/paper backup
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Public addresses derived from the mnemonic, one per chain. Materialised on
-- wallet-create so the Flutter app can show deposit QRs without unlocking.
CREATE TABLE web3_addresses (
    uid              TEXT NOT NULL REFERENCES web3_wallets(uid) ON DELETE CASCADE,
    chain            TEXT NOT NULL,
    address          TEXT NOT NULL,
    derivation_path  TEXT NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (uid, chain)
);
CREATE INDEX web3_addresses_addr ON web3_addresses (chain, address);

-- Per-user custom tokens (ERC-20 / BEP-20 / SPL / TRC-20 / ...) the Flutter
-- "Add token" flow writes. The service also ships a hardcoded catalogue
-- (tokens_builtin.go); the two are unioned on read.
CREATE TABLE web3_custom_tokens (
    id            BIGSERIAL PRIMARY KEY,
    uid           TEXT NOT NULL,
    chain         TEXT NOT NULL,
    contract      TEXT NOT NULL,
    symbol        TEXT NOT NULL,
    name          TEXT NOT NULL DEFAULT '',
    decimals      INT  NOT NULL,
    logo_url      TEXT NOT NULL DEFAULT '',
    added_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (uid, chain, contract)
);
CREATE INDEX web3_custom_tokens_uid ON web3_custom_tokens (uid);

-- Local mirror of transactions this wallet originated. Written straight after
-- broadcast with status='pending'; the background confirmer bumps it to
-- 'confirmed' or 'failed' based on receipt lookups. Inbound deposits are NOT
-- indexed here (the Flutter app pulls those from an explorer API on demand).
CREATE TABLE web3_transactions (
    id            BIGSERIAL PRIMARY KEY,
    uid           TEXT NOT NULL,
    chain         TEXT NOT NULL,
    tx_hash       TEXT NOT NULL,
    from_addr     TEXT NOT NULL,
    to_addr       TEXT NOT NULL,
    value         NUMERIC(60,0) NOT NULL DEFAULT 0,  -- base units (wei, lamports, sun, sat)
    token         TEXT NOT NULL DEFAULT '',          -- '' for native
    gas           NUMERIC(60,0) NOT NULL DEFAULT 0,
    status        TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','confirmed','failed','dropped')),
    kind          TEXT NOT NULL DEFAULT 'send' CHECK (kind IN ('send','swap','bridge','contract','approve','walletconnect')),
    detail        JSONB NOT NULL DEFAULT '{}',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    confirmed_at  TIMESTAMPTZ,
    UNIQUE (chain, tx_hash)
);
CREATE INDEX web3_transactions_uid ON web3_transactions (uid, created_at DESC);
CREATE INDEX web3_transactions_pending ON web3_transactions (status) WHERE status = 'pending';

-- WalletConnect v2 sessions the user has paired. The relay/transport runs on
-- the Flutter client; the server only remembers which dapp metadata belongs
-- to which user so a session survives app restarts.
CREATE TABLE web3_wc_sessions (
    id            BIGSERIAL PRIMARY KEY,
    uid           TEXT NOT NULL,
    topic         TEXT NOT NULL UNIQUE,
    peer_name     TEXT NOT NULL DEFAULT '',
    peer_url      TEXT NOT NULL DEFAULT '',
    peer_icon     TEXT NOT NULL DEFAULT '',
    namespaces    JSONB NOT NULL DEFAULT '{}',
    status        TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','disconnected')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    disconnected_at TIMESTAMPTZ
);
CREATE INDEX web3_wc_sessions_uid ON web3_wc_sessions (uid, status);

-- +goose Down
DROP TABLE IF EXISTS web3_wc_sessions;
DROP TABLE IF EXISTS web3_transactions;
DROP TABLE IF EXISTS web3_custom_tokens;
DROP TABLE IF EXISTS web3_addresses;
DROP TABLE IF EXISTS web3_wallets;
