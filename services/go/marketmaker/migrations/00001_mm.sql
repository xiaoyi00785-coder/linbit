-- +goose Up
-- Market-maker configuration. One row per (market, symbol); every parameter is
-- editable from the admin backend (RBAC + 2FA + audit). No stop-loss by design.
CREATE TABLE mm_pairs (
    market          TEXT NOT NULL CHECK (market IN ('spot','futures')),
    symbol          TEXT NOT NULL,
    enabled         BOOLEAN NOT NULL DEFAULT TRUE,
    spread_bps      NUMERIC(12,4) NOT NULL DEFAULT 10,    -- full bid/ask spread at level 0 (10 bps = 0.1%)
    levels          INT NOT NULL DEFAULT 5,               -- levels per side
    level_step_bps  NUMERIC(12,4) NOT NULL DEFAULT 5,     -- extra distance per level
    level_notional  NUMERIC(36,8) NOT NULL DEFAULT 1000,  -- quote (USDT) size per level
    size_growth     NUMERIC(12,4) NOT NULL DEFAULT 1.2,   -- size multiplier per level further out
    refresh_ms      INT NOT NULL DEFAULT 3000,
    requote_bps     NUMERIC(12,4) NOT NULL DEFAULT 2,     -- keep a resting order if within this of target
    target_base_ratio NUMERIC(12,6) NOT NULL DEFAULT 0.5, -- spot: wanted base share of pair inventory value
    max_position    NUMERIC(36,8) NOT NULL DEFAULT 50000, -- futures: max |position| notional (USDT); spot: max base value
    max_skew_bps    NUMERIC(12,4) NOT NULL DEFAULT 20,    -- inventory skew of the mid at full imbalance
    leverage        INT NOT NULL DEFAULT 5,               -- futures only
    updated_by      TEXT NOT NULL DEFAULT 'system',
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (market, symbol)
);

-- Singleton run state (start/stop from admin).
CREATE TABLE mm_state (
    id         INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    running    BOOLEAN NOT NULL DEFAULT FALSE,
    updated_by TEXT NOT NULL DEFAULT 'system',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO mm_state (id) VALUES (1);

-- +goose Down
DROP TABLE mm_state;
DROP TABLE mm_pairs;
