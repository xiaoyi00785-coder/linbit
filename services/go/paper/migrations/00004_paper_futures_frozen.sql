-- +goose Up
-- Margin reserved by a resting paper-futures order. Cancel and fill release this
-- amount exactly; the fill then books position margin once at the fill price.
ALTER TABLE paper_fut_orders
    ADD COLUMN frozen_margin NUMERIC(36,18) NOT NULL DEFAULT 0 CHECK (frozen_margin >= 0);

-- +goose Down
ALTER TABLE paper_fut_orders DROP COLUMN frozen_margin;
