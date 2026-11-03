-- +goose Up
-- Per-user watchlist (自选). market separates spot and futures lists; sort is
-- the user's order (ascending).
CREATE TABLE user_favorites (
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    market     TEXT   NOT NULL DEFAULT 'spot' CHECK (market IN ('spot','futures')),
    symbol     TEXT   NOT NULL,
    sort       INT    NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, market, symbol)
);

-- +goose Down
DROP TABLE user_favorites;
