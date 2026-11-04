-- +goose Up
-- The fee announcement seed wrongly claimed VIP tiers get lower fees. Fees are
-- uniform for all users (order/config.go, GET /account/fee-rates); fix the
-- already-seeded row in place (only if untouched by an admin edit).
UPDATE announcements
SET body = replace(body, 'VIP 等级可享受更低费率；', '所有用户（含 VIP）适用统一费率，无等级折扣；'), updated_at = now()
WHERE created_by = 'system-seed' AND title = '交易手续费说明' AND body LIKE '%VIP 等级可享受更低费率%';

-- +goose Down
SELECT 1;
