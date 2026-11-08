-- +goose Up
-- Academy articles. Empty until an operator publishes one. No sample rows.
CREATE TABLE academy_categories (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    sort       INT NOT NULL DEFAULT 0,
    enabled    BOOLEAN NOT NULL DEFAULT TRUE,
    updated_by TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE academy_articles (
    id           BIGSERIAL PRIMARY KEY,
    category_id  TEXT NOT NULL REFERENCES academy_categories(id),
    locale       TEXT NOT NULL DEFAULT '',
    title        TEXT NOT NULL,
    summary      TEXT NOT NULL DEFAULT '',
    body         TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published')),
    published_at TIMESTAMPTZ,
    updated_by   TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX academy_articles_pub ON academy_articles (status, id DESC);

-- +goose Down
DROP TABLE academy_articles;
DROP TABLE academy_categories;
