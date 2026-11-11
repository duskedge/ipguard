-- 封禁/解封审计日志
CREATE TABLE IF NOT EXISTS audit_logs (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at DATETIME NOT NULL,
    operator   TEXT     NOT NULL DEFAULT '',
    client_ip  TEXT     NOT NULL DEFAULT '',
    action     TEXT     NOT NULL,              -- ban | unban
    target     TEXT     NOT NULL,
    duration   TEXT     NOT NULL DEFAULT '',
    reason     TEXT     NOT NULL DEFAULT '',
    source     TEXT     NOT NULL DEFAULT '',   -- 解封时被删决策的来源：manual | auto | community
    result     TEXT     NOT NULL,              -- ok | fail
    error      TEXT     NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs (created_at);
CREATE INDEX IF NOT EXISTS idx_audit_logs_target ON audit_logs (target);
