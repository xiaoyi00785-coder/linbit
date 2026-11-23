-- +goose Up
-- 风控规则（只读检测 + 告警）。命中只写入 admin_risk_hits 供人工处理，
-- 不会自动冻结账户、拦截或改变任何资金流程。
CREATE TABLE admin_risk_rules (
    code        TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    kind        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    params      JSONB NOT NULL DEFAULT '{}',
    severity    TEXT NOT NULL DEFAULT 'medium' CHECK (severity IN ('low','medium','high')),
    enabled     BOOLEAN NOT NULL DEFAULT false,
    notify      BOOLEAN NOT NULL DEFAULT false,
    last_run_at TIMESTAMPTZ,
    last_hits   INT NOT NULL DEFAULT 0,
    last_error  TEXT NOT NULL DEFAULT '',
    updated_by  TEXT NOT NULL DEFAULT '',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE admin_risk_hits (
    id         BIGSERIAL PRIMARY KEY,
    rule_code  TEXT NOT NULL REFERENCES admin_risk_rules(code),
    uid        TEXT NOT NULL DEFAULT '',
    subject    TEXT NOT NULL,
    summary    TEXT NOT NULL,
    detail     JSONB NOT NULL DEFAULT '{}',
    severity   TEXT NOT NULL,
    status     TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','ack','ignored')),
    hit_count  INT NOT NULL DEFAULT 1,
    first_seen TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen  TIMESTAMPTZ NOT NULL DEFAULT now(),
    handled_by TEXT,
    handled_at TIMESTAMPTZ,
    note       TEXT
);
CREATE UNIQUE INDEX uq_admin_risk_hits_open ON admin_risk_hits (rule_code, subject) WHERE status = 'open';
CREATE INDEX idx_admin_risk_hits_status ON admin_risk_hits (status, last_seen DESC);
CREATE INDEX idx_admin_risk_hits_subject ON admin_risk_hits (rule_code, subject, handled_at);

INSERT INTO admin_risk_rules (code, name, kind, description, params, severity, enabled) VALUES
 ('large_withdrawal', '大额提现', 'large_withdrawal', '单笔提现金额达到阈值（按币种）', '{"assets":"USDT,USDC","minAmount":10000,"lookbackHours":24,"cooldownHours":24,"excludeUids":""}', 'high', true),
 ('withdraw_frequency', '高频提现', 'withdraw_frequency', '同一用户回看窗口内提现次数达到阈值', '{"minCount":5,"lookbackHours":24,"cooldownHours":24,"excludeUids":""}', 'medium', true),
 ('quick_out', '充值后快速提现', 'quick_out', '充值到账后短时间内发起提现（可能为洗钱/跑分）', '{"maxHours":2,"lookbackHours":24,"cooldownHours":24,"excludeUids":""}', 'high', true),
 ('large_position', '大额持仓', 'large_position', '单个合约持仓名义价值（开仓均价×数量）达到阈值', '{"minNotional":500000,"cooldownHours":24,"excludeUids":""}', 'medium', true),
 ('high_leverage', '高杠杆持仓', 'high_leverage', '杠杆倍数与名义价值同时达到阈值', '{"minLeverage":50,"minNotional":10000,"cooldownHours":24,"excludeUids":""}', 'medium', true),
 ('liquidation_burst', '连续强平', 'liquidation_burst', '同一用户短时间内多次被强平', '{"minCount":3,"lookbackHours":1,"cooldownHours":6,"excludeUids":""}', 'medium', true),
 ('apikey_no_ip', 'API Key 未绑定 IP', 'apikey_no_ip', '有效 API Key 未设置 IP 白名单', '{"cooldownHours":168,"excludeUids":""}', 'low', true);

-- +goose Down
DROP TABLE admin_risk_hits;
DROP TABLE admin_risk_rules;
