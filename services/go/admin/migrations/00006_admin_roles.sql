-- +goose Up
-- 角色编辑：内置角色的权限覆盖 + 自定义角色。没有行 = 使用 rbac.go 的代码默认值。
-- 资金/冻结类权限点（lockedPerms）由代码强制保持默认，表里写什么都不会生效。
CREATE TABLE admin_roles (
    code       TEXT PRIMARY KEY CHECK (code ~ '^[a-z][a-z0-9_]{1,31}$'),
    name       TEXT NOT NULL DEFAULT '',
    perms      TEXT[] NOT NULL DEFAULT '{}',
    builtin    BOOLEAN NOT NULL DEFAULT false,
    updated_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE admin_roles;
