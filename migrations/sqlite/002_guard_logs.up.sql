CREATE TABLE guard_logs (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 ts INTEGER NOT NULL,
 last_ts INTEGER NOT NULL, hits INTEGER NOT NULL DEFAULT 1, event_key TEXT NOT NULL UNIQUE,
 ip TEXT NOT NULL, host TEXT NOT NULL, path TEXT NOT NULL, method TEXT NOT NULL, ua TEXT NOT NULL,
 rule_host TEXT NOT NULL, mode TEXT NOT NULL, reason TEXT NOT NULL,
 geo TEXT NOT NULL, cc TEXT NOT NULL, tag TEXT NOT NULL,
 allow_ips TEXT NOT NULL, allow_tags TEXT NOT NULL
);
CREATE INDEX idx_guard_logs_ts ON guard_logs(ts);
CREATE INDEX idx_guard_logs_rule_ts ON guard_logs(rule_host, ts);
CREATE INDEX idx_guard_logs_ip_ts ON guard_logs(ip, ts);
CREATE INDEX idx_guard_logs_last_ts ON guard_logs(last_ts);
