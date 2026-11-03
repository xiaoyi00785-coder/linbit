-- +goose Up
-- Home-page operations slots. One table for every slot (popup, splash, grid,
-- carousel, promo_bar, search_hint); "banners" are slot=carousel. Managed from
-- admin /admin/v1/home/placements (and the /admin/v1/banners alias), served
-- publicly as GET /home/placements and GET /home/banners.
-- Numbered 00004 because 00003 is taken by feat/market-categories-more.
CREATE TABLE home_placements (
    id               BIGSERIAL PRIMARY KEY,
    slot             TEXT NOT NULL CHECK (slot IN ('popup','splash','grid','carousel','promo_bar','search_hint')),
    title            TEXT NOT NULL,
    image_url        TEXT NOT NULL DEFAULT '',
    image_dark_url   TEXT NOT NULL DEFAULT '',
    link_url         TEXT NOT NULL DEFAULT '',
    platform         TEXT NOT NULL DEFAULT 'all' CHECK (platform IN ('web','app','all')),
    locale           TEXT NOT NULL DEFAULT '',                 -- '' = every locale
    audience         TEXT NOT NULL DEFAULT 'all' CHECK (audience IN ('all','new','existing')),
    regions          TEXT[] NOT NULL DEFAULT '{}',             -- ISO-3166 alpha-2; {} = everywhere
    app_min_version  TEXT NOT NULL DEFAULT '',                 -- inclusive, '' = no bound
    app_max_version  TEXT NOT NULL DEFAULT '',                 -- inclusive, '' = no bound
    frequency        TEXT NOT NULL DEFAULT 'always' CHECK (frequency IN ('always','once','daily','session')),
    countdown_end_at TIMESTAMPTZ,
    tracking_id      TEXT NOT NULL DEFAULT '',
    earn_product_id  TEXT NOT NULL DEFAULT '',                 -- APR filled live from the earn service
    content          JSONB NOT NULL DEFAULT '{}',              -- slot-specific: grid icon/badge, hint text...
    sort             INT  NOT NULL DEFAULT 0,                  -- ascending
    start_at         TIMESTAMPTZ,                              -- NULL = immediately
    end_at           TIMESTAMPTZ,                              -- NULL = no end
    enabled          BOOLEAN NOT NULL DEFAULT true,
    updated_by       TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (end_at IS NULL OR start_at IS NULL OR end_at > start_at)
);
CREATE INDEX idx_home_placements_slot ON home_placements (slot, enabled, sort);

-- Admin-pinned symbols shown first in a hot-list tab, by sort.
CREATE TABLE home_hot_pins (
    list_type  TEXT NOT NULL CHECK (list_type IN ('hot','gainers','losers','new','volume')),
    symbol     TEXT NOT NULL,
    sort       INT  NOT NULL DEFAULT 0,
    updated_by TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (list_type, symbol)
);

-- App feature switches (GET /app/features). Only overrides are stored; the
-- defaults live in code (convert=true, everything unbuilt=false).
CREATE TABLE app_features (
    key        TEXT PRIMARY KEY CHECK (key ~ '^[a-zA-Z][a-zA-Z0-9]{0,47}$'),
    enabled    BOOLEAN NOT NULL,
    updated_by TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE app_features;
DROP TABLE home_hot_pins;
DROP TABLE home_placements;
