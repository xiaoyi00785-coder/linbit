-- +goose Up
-- Real-chain support (WALLET_CHAIN=testnet|mainnet). The mock chain ignores
-- these columns/tables.

-- HD index of the deposit key (xpub/0/<hd_index>); NULL for mock addresses.
ALTER TABLE deposit_addresses ADD COLUMN hd_index BIGINT;
CREATE SEQUENCE deposit_hd_index_seq MINVALUE 0 START 0;

-- Per (network, asset) deposit discovery cursor (block height / slot / ms timestamp,
-- chain specific).
CREATE TABLE chain_cursors (
    network   TEXT NOT NULL,
    asset     TEXT NOT NULL,
    next_from BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (network, asset)
);

-- Sweeps of credited deposit addresses into the hot (or cold) wallet.
--   topup_sent -> sweep_sent -> done ; any -> failed
CREATE TABLE sweeps (
    id          BIGSERIAL PRIMARY KEY,
    network     TEXT NOT NULL,
    asset       TEXT NOT NULL,
    address     TEXT NOT NULL,
    hd_index    BIGINT NOT NULL,
    stage       TEXT NOT NULL,
    amount      NUMERIC(36,18) NOT NULL DEFAULT 0,
    destination TEXT NOT NULL DEFAULT '',
    topup_tx    TEXT NOT NULL DEFAULT '',
    sweep_tx    TEXT NOT NULL DEFAULT '',
    error       TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX uq_sweeps_active ON sweeps (network, address)
    WHERE stage IN ('topup_sent', 'sweep_sent');

-- +goose Down
DROP TABLE sweeps;
DROP TABLE chain_cursors;
DROP SEQUENCE deposit_hd_index_seq;
ALTER TABLE deposit_addresses DROP COLUMN hd_index;
