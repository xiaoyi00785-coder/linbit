-- +goose Up
-- Announcements become fully admin-managed (create / edit / pin / delete via
-- /admin/v1/announcements) and get a small real seed so GET /announcements is
-- never empty on a fresh deploy. Seed copy deliberately quotes no fee/reward
-- numbers: those live in config (fee page, /growth/config) and must not drift.
ALTER TABLE announcements ADD COLUMN IF NOT EXISTS pinned BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE announcements ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();
CREATE INDEX IF NOT EXISTS idx_announcements_publish ON announcements (publish_at DESC);

INSERT INTO announcements (category, title, body, publish_at, created_by, pinned)
SELECT v.category, v.title, v.body, v.publish_at, 'system-seed', v.pinned
FROM (VALUES
  ('最新公告', 'Linbit 平台正式上线',
   'Linbit 现货、USDT 本位永续合约、理财与跟单功能已开放。请通过官网和官方 App 访问，谨防仿冒网站。',
   TIMESTAMPTZ '2026-11-01 00:00:00+00', true),
  ('最新公告', '交易手续费说明',
   '现货与合约手续费按「费率」页面公示的 Maker/Taker 费率收取，VIP 等级可享受更低费率；提现仅收取链上网络费用。费率调整将至少提前 24 小时公告。',
   TIMESTAMPTZ '2026-11-01 00:05:00+00', false),
  ('活动', '新人福利上线',
   '完成注册、身份认证、首次入金与首笔交易等新人任务即可领取奖励，具体奖励以「福利中心」页面为准。',
   TIMESTAMPTZ '2026-10-31 00:00:00+00', false),
  ('最新公告', '关于加强提现安全验证的公告',
   '为保障资金安全，建议开启双重验证（2FA）与资金密码。修改登录密码、重置 2FA 等敏感操作后，提现与站内转账将临时暂停。',
   TIMESTAMPTZ '2026-10-30 00:00:00+00', false)
) AS v(category, title, body, publish_at, pinned)
WHERE NOT EXISTS (SELECT 1 FROM announcements a WHERE a.title = v.title);

-- +goose Down
DELETE FROM announcements WHERE created_by = 'system-seed';
DROP INDEX IF EXISTS idx_announcements_publish;
ALTER TABLE announcements DROP COLUMN IF EXISTS updated_at;
ALTER TABLE announcements DROP COLUMN IF EXISTS pinned;
