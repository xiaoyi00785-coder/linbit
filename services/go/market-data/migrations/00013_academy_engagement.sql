-- +goose Up
-- Academy engagement: views counter, likes, bookmarks and (optional)
-- comments on published articles. Reader identity is the uid header forwarded
-- by the gateway; counts stay server-side so the UI can show totals.
ALTER TABLE academy_articles ADD COLUMN views  BIGINT NOT NULL DEFAULT 0;
ALTER TABLE academy_articles ADD COLUMN likes  BIGINT NOT NULL DEFAULT 0;
ALTER TABLE academy_articles ADD COLUMN tags   TEXT[] NOT NULL DEFAULT '{}'::TEXT[];
CREATE INDEX academy_articles_views ON academy_articles (views DESC);

CREATE TABLE academy_likes (
    article_id BIGINT NOT NULL REFERENCES academy_articles(id) ON DELETE CASCADE,
    uid        TEXT NOT NULL,
    at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (article_id, uid)
);

CREATE TABLE academy_bookmarks (
    article_id BIGINT NOT NULL REFERENCES academy_articles(id) ON DELETE CASCADE,
    uid        TEXT NOT NULL,
    at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (article_id, uid)
);
CREATE INDEX academy_bookmarks_uid ON academy_bookmarks (uid, at DESC);

CREATE TABLE academy_comments (
    id         BIGSERIAL PRIMARY KEY,
    article_id BIGINT NOT NULL REFERENCES academy_articles(id) ON DELETE CASCADE,
    uid        TEXT NOT NULL,
    body       TEXT NOT NULL CHECK (char_length(body) BETWEEN 1 AND 2000),
    status     TEXT NOT NULL DEFAULT 'visible' CHECK (status IN ('visible','hidden')),
    at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX academy_comments_article ON academy_comments (article_id, id DESC);

-- +goose Down
DROP INDEX IF EXISTS academy_comments_article;
DROP TABLE academy_comments;
DROP INDEX IF EXISTS academy_bookmarks_uid;
DROP TABLE academy_bookmarks;
DROP TABLE academy_likes;
DROP INDEX IF EXISTS academy_articles_views;
ALTER TABLE academy_articles DROP COLUMN tags;
ALTER TABLE academy_articles DROP COLUMN likes;
ALTER TABLE academy_articles DROP COLUMN views;
