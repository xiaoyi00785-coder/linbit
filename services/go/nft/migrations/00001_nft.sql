-- +goose Up
-- Ledger-style NFT marketplace: collections, items, listings, offers, trades,
-- favourites and activity. No chain state; NFT ownership is tracked as rows
-- here and payments move via the asset service's /internal/user-transfer.

CREATE TABLE nft_collections (
    id            BIGSERIAL PRIMARY KEY,
    slug          TEXT NOT NULL UNIQUE,
    name          TEXT NOT NULL,
    description   TEXT NOT NULL DEFAULT '',
    image         TEXT NOT NULL DEFAULT '',
    banner        TEXT NOT NULL DEFAULT '',
    creator_id    TEXT NOT NULL,
    royalty_bps   INT NOT NULL DEFAULT 0 CHECK (royalty_bps >= 0 AND royalty_bps <= 2000),
    pay_asset     TEXT NOT NULL DEFAULT 'USDT',
    category      TEXT NOT NULL DEFAULT '',
    item_count    INT NOT NULL DEFAULT 0,
    owner_count   INT NOT NULL DEFAULT 0,
    floor_price   NUMERIC(36,8),
    volume_total  NUMERIC(36,8) NOT NULL DEFAULT 0,
    volume_24h    NUMERIC(36,8) NOT NULL DEFAULT 0,
    sales_total   BIGINT NOT NULL DEFAULT 0,
    sales_24h     BIGINT NOT NULL DEFAULT 0,
    status        TEXT NOT NULL DEFAULT 'online' CHECK (status IN ('online','offline')),
    created_at    TIMESTAMPTZ NOT NULL
);
CREATE INDEX nft_collections_cat ON nft_collections (category, status);

CREATE TABLE nft_items (
    id              BIGSERIAL PRIMARY KEY,
    collection_id   BIGINT NOT NULL REFERENCES nft_collections(id),
    token_id        TEXT NOT NULL,
    owner_id        TEXT NOT NULL,
    name            TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    image           TEXT NOT NULL DEFAULT '',
    animation_url   TEXT NOT NULL DEFAULT '',
    metadata_uri    TEXT NOT NULL DEFAULT '',
    attributes      JSONB NOT NULL DEFAULT '[]',
    listed          BOOLEAN NOT NULL DEFAULT false,
    list_price      NUMERIC(36,8),
    last_sale_price NUMERIC(36,8),
    favorite_count  INT NOT NULL DEFAULT 0,
    status          TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','burned')),
    created_at      TIMESTAMPTZ NOT NULL,
    UNIQUE (collection_id, token_id)
);
CREATE INDEX nft_items_owner  ON nft_items (owner_id);
CREATE INDEX nft_items_coll   ON nft_items (collection_id, listed, list_price);

CREATE TABLE nft_listings (
    id          BIGSERIAL PRIMARY KEY,
    item_id     BIGINT NOT NULL REFERENCES nft_items(id),
    seller_id   TEXT NOT NULL,
    price       NUMERIC(36,8) NOT NULL CHECK (price > 0),
    asset       TEXT NOT NULL,
    status      TEXT NOT NULL CHECK (status IN ('active','sold','cancelled','expired')),
    created_at  TIMESTAMPTZ NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    closed_at   TIMESTAMPTZ
);
CREATE INDEX nft_listings_active ON nft_listings (item_id) WHERE status = 'active';
CREATE INDEX nft_listings_seller ON nft_listings (seller_id, status);
CREATE INDEX nft_listings_exp    ON nft_listings (expires_at) WHERE status = 'active';

CREATE TABLE nft_offers (
    id          BIGSERIAL PRIMARY KEY,
    item_id     BIGINT NOT NULL REFERENCES nft_items(id),
    bidder_id   TEXT NOT NULL,
    price       NUMERIC(36,8) NOT NULL CHECK (price > 0),
    asset       TEXT NOT NULL,
    status      TEXT NOT NULL CHECK (status IN ('active','accepted','cancelled','expired')),
    created_at  TIMESTAMPTZ NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    closed_at   TIMESTAMPTZ
);
CREATE INDEX nft_offers_item   ON nft_offers (item_id, status);
CREATE INDEX nft_offers_bidder ON nft_offers (bidder_id, status);
CREATE INDEX nft_offers_exp    ON nft_offers (expires_at) WHERE status = 'active';

CREATE TABLE nft_trades (
    id             BIGSERIAL PRIMARY KEY,
    item_id        BIGINT NOT NULL REFERENCES nft_items(id),
    collection_id  BIGINT NOT NULL REFERENCES nft_collections(id),
    seller_id      TEXT NOT NULL,
    buyer_id       TEXT NOT NULL,
    price          NUMERIC(36,8) NOT NULL,
    asset          TEXT NOT NULL,
    royalty_amount NUMERIC(36,8) NOT NULL DEFAULT 0,
    royalty_to     TEXT NOT NULL DEFAULT '',
    source         TEXT NOT NULL CHECK (source IN ('listing','offer')),
    source_id      BIGINT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL
);
CREATE INDEX nft_trades_item ON nft_trades (item_id, created_at DESC);
CREATE INDEX nft_trades_coll ON nft_trades (collection_id, created_at DESC);
CREATE INDEX nft_trades_buyer ON nft_trades (buyer_id, created_at DESC);

CREATE TABLE nft_favorites (
    uid         TEXT NOT NULL,
    item_id     BIGINT NOT NULL REFERENCES nft_items(id),
    created_at  TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (uid, item_id)
);
CREATE INDEX nft_favorites_item ON nft_favorites (item_id);

-- Unified activity feed: one row per event (mint/transfer/list/unlist/offer/
-- offer_cancel/sale/expire). Shown on item pages and user profile.
CREATE TABLE nft_activity (
    id             BIGSERIAL PRIMARY KEY,
    item_id        BIGINT NOT NULL REFERENCES nft_items(id),
    collection_id  BIGINT NOT NULL REFERENCES nft_collections(id),
    kind           TEXT NOT NULL,
    from_uid       TEXT NOT NULL DEFAULT '',
    to_uid         TEXT NOT NULL DEFAULT '',
    price          NUMERIC(36,8),
    asset          TEXT NOT NULL DEFAULT '',
    detail         JSONB NOT NULL DEFAULT '{}',
    at             TIMESTAMPTZ NOT NULL
);
CREATE INDEX nft_activity_item ON nft_activity (item_id, id DESC);
CREATE INDEX nft_activity_coll ON nft_activity (collection_id, id DESC);
CREATE INDEX nft_activity_user ON nft_activity (from_uid, id DESC);
CREATE INDEX nft_activity_to   ON nft_activity (to_uid, id DESC);

-- +goose Down
DROP TABLE nft_activity;
DROP TABLE nft_favorites;
DROP TABLE nft_trades;
DROP TABLE nft_offers;
DROP TABLE nft_listings;
DROP TABLE nft_items;
DROP TABLE nft_collections;
