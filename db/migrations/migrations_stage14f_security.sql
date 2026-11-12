-- Stage 14.21-14.24: Security Hardening Additions
--
-- Account-level brute-force lockout. The existing login rate limiter
-- (rateLimitCategory in main.go, Stage 13.14) is IP-scoped only - 5/min per
-- IP - so a distributed attempt spread across many IPs against one account
-- isn't slowed by it at all. This is a second, independent layer keyed by
-- account instead of network address; both stay in effect (genuine
-- defense-in-depth, not a replacement).
ALTER TABLE tenant_default.users ADD COLUMN IF NOT EXISTS failed_login_count INT NOT NULL DEFAULT 0;
ALTER TABLE tenant_default.users ADD COLUMN IF NOT EXISTS locked_until TIMESTAMP;
