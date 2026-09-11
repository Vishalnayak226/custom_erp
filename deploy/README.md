---
doc_id: DOC-0DB1627664
title: Deploying the ERP to a Linux Droplet
type: reference
status: draft
owner: engineering-owner
approvers: [documentation-maintainer, engineering-owner]
audience: [maintainers, engineering-owner]
applies_to: source documentation; scoped release acceptance required
authority: source
confidentiality: internal
last_verified: 2026-09-09
review_by: 2026-10-09
supersedes: none
superseded_by: none
verification_scope: metadata and lifecycle classification; domain acceptance pending
---

# Deploying the ERP to a Linux Droplet

This is the runbook for the production/staging deploy path described in
[go_live_decisions.md](../docs/operations/go_live_decisions.md) section 4. It
targets the choices made for the first box: **self-hosted PostgreSQL on the
Droplet**, **Caddy for automatic TLS**, and a **staging/shakeout box first**
before real production.

The app is one static Go binary + `public/` + `db/` migrations. There is **no
Go toolchain on the Droplet** — you cross-compile the binary on your Windows dev
machine and ship it. All ops that `manage.ps1` / `promote.ps1` do on Windows are
handled here by `systemd` (supervision), `migrate.sh` (migrations), `Caddyfile`
(TLS), and `backup.sh` (backups).

Target architecture on the box:

```
Internet ──▶ Caddy (:443, auto Let's Encrypt) ──▶ erp-server (:8080) ──▶ PostgreSQL (:5432, localhost)
                         [/etc/caddy/Caddyfile]     [systemd: erp.service]    [self-hosted]
```

Files in this kit: `erp.service`, `Caddyfile`, `erp.env.example`, `migrate.sh`,
`backup.sh`, `postgres_harden.sql` (all run on the box), and `deploy.ps1`
(runs on your dev machine).

---

## Part A — one-time box setup

SSH in as root (or the DigitalOcean default user), then:

### A1. Create a non-root service user + firewall

```bash
adduser --system --group --home /opt/erp --shell /usr/sbin/nologin erp   # runs the server, no login
adduser deploy && usermod -aG sudo deploy                                # your SSH/deploy user
# copy your SSH public key to the deploy user, then harden:
ufw allow OpenSSH && ufw allow 80,443/tcp && ufw --force enable
apt update && apt upgrade -y
```

From here on, SSH in as `deploy` and use `sudo`.

### A2. Install PostgreSQL, create the database + role

```bash
sudo apt install -y postgresql
sudo -u postgres psql <<'SQL'
CREATE ROLE erp LOGIN PASSWORD 'CHANGE_ME_STRONG_DB_PASSWORD';
CREATE DATABASE custom_erp OWNER erp;
SQL
```

The `erp` role owns the database so migrations can create schemas/tables. Default
cluster listens on `localhost:5432` — no `pg_hba.conf` change needed (loopback).

**PostgreSQL baseline (49.7.4).** Use a currently-supported PostgreSQL major
version (this repo's dev/CI both run 16.x; anything on the [PostgreSQL
versioning
policy](https://www.postgresql.org/support/versioning/)'s supported list is
fine — never one past its final minor release). After creating the database,
confirm the defaults are what this deployment actually wants:

```sql
SHOW server_version;                 -- supported, patched major version
SELECT extname FROM pg_extension;    -- expect plpgsql only; dblink,
                                      -- postgres_fdw, adminpack, file_fdw and
                                      -- plpythonu/plperlu/plperl have no use
                                      -- in this codebase and must never
                                      -- appear here
SHOW log_connections;                -- 'on' - monitors failed auth (49.7.4)
SHOW log_disconnections;             -- 'on'
SHOW log_min_duration_statement;     -- e.g. '5s', catches runaway/DDL activity
                                      -- without logging every ordinary query
```

Set the ones that are not already `on`/reasonable in `postgresql.conf` (path
via `SHOW config_file;`) and `sudo systemctl reload postgresql`. These are
cluster-wide, file-based settings outside what a per-role `ALTER ROLE ... SET`
can reach — deploy/postgres_harden.sql (next section) covers the per-role
grants and timeouts; this is the file-level half of the same item.

### A2.5. Least-privilege database roles (49.7.1 / 49.7.4)

The single `erp` role above works, but it means the running server, the
migration runner and the nightly backup all share **one** database
credential — compromise of the running process is then schema-modify **and**
full-dump ability, not just a data read (risk register R-07). Run
`deploy/postgres_harden.sql` once, in a short maintenance window, to split it
into three least-privilege roles instead. It is idempotent, reviewed, and
documents itself — read its header comment before running it:

```bash
psql "$DATABASE_URL" -v current_owner=erp \
     -v app_password="$(openssl rand -hex 24)" \
     -v migrate_password="$(openssl rand -hex 24)" \
     -v backup_password="$(openssl rand -hex 24)" \
     -f /opt/erp/deploy/postgres_harden.sql
```

It prints the resulting role posture and schema ownership at the end — confirm
`erp_app`/`erp_backup` show `superuser=no createrole=no createdb=no` and every
schema is owned by `erp_migrate` before proceeding.

**What to repoint immediately (safe, tested against a scratch database):**

- `deploy/backup.sh` — give it `erp_backup`'s connection string. It only ever
  needs `SELECT`, and now provably cannot write.
- `deploy/migrate.sh` and any `tenantctl` invocation — give them
  `erp_migrate`'s connection string. `tenantctl provision`/`deprovision`/`purge`
  are `CREATE SCHEMA`/`DROP SCHEMA` operations (docs/security/README.md's
  "operator commands, not routes" note), so they need `erp_migrate`, not the
  DML-only role below. `engines/tenant_lifecycle.go`'s
  `ProvisionTenantSchema` grants `erp_app`/`erp_backup` access on every schema
  it creates automatically (whichever role runs it already owns what it just
  created), so a tenant provisioned this way needs no manual follow-up grant.

**What is NOT yet safe to repoint — `/etc/erp/erp.env`'s own `DATABASE_URL`,
i.e. what the running `erp-server` process itself connects as.** Found while
building this: two existing HTTP routes —
`POST /api/v1/admin/tenant/provision` (`handleProvisionTenant`) and
`POST /api/v1/admin/sandbox-tenants` (`handleProvisionSandboxTenant`,
Stage 38.7) — call
`engines.ProvisionTenantSchema` directly from the running server process using
its own database connection, and provisioning a tenant is `CREATE SCHEMA`.
Both are Super-Admin-gated, but "gated" is an application-layer control, not a
database-layer one — the database connection itself still needs
schema-creation rights for these two routes to keep working. Pointing
`DATABASE_URL` at `erp_app` (DML only) today would make both routes fail with
`permission denied for database` on their very first use.

Until one of the following happens, leave `/etc/erp/erp.env`'s `DATABASE_URL`
on the schema-owning role (`erp`, or `erp_migrate` if `current_owner` was
reassigned) — the backup/migrate split above is still real, independent
progress on R-07 even with this one left open:

1. Those two routes are refactored to use a second, separately-configured
   connection pool scoped to `erp_migrate` (only for provisioning), while
   ordinary request handling moves to `erp_app` — the architecturally clean
   fix, and a larger, separate change than this item attempted.
2. Or a decision is made that tenant/sandbox provisioning should only ever
   happen via `tenantctl` (matching what docs/security/README.md already
   states as the intended design) and both HTTP routes are removed —
   smaller, but a product/API-surface decision, not an infrastructure one.

`tenantctl db-privilege` reports the connected role's posture at any time —
run it after any of the changes above to confirm what actually changed.

> **If you put Postgres anywhere other than this box** — a managed instance
> (DO/RDS/Cloud SQL), a second droplet, a container on another host — the
> `sslmode=disable` in the shipped `DATABASE_URL` is no longer safe: it sends the
> DB password and every row of tenant data over that network in cleartext. Change
> the host and the sslmode together (`sslmode=require` at minimum,
> `verify-full` + `sslrootcert=` preferred). `deploy/erp.env.example` documents
> each mode and how to verify what you actually negotiated.

### A3. Install Caddy (for TLS in Part C)

```bash
sudo apt install -y debian-keyring debian-archive-keyring apt-transport-https curl
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | sudo tee /etc/apt/sources.list.d/caddy-stable.list
sudo apt update && sudo apt install -y caddy
```

### A4. Create the app dir, env file, and scripts

```bash
sudo mkdir -p /opt/erp /etc/erp
sudo chown -R erp:erp /opt/erp
# copy the deploy/ scripts up (from your dev machine, or git clone into /tmp and copy):
#   scp -r deploy deploy@<droplet>:/tmp/erp-deploy   then   sudo cp -r /tmp/erp-deploy /opt/erp/deploy
sudo chmod +x /opt/erp/deploy/migrate.sh /opt/erp/deploy/backup.sh

sudo cp /opt/erp/deploy/erp.env.example /etc/erp/erp.env
sudo nano /etc/erp/erp.env      # fill DATABASE_URL password + JWT_SECRET (see below); leave ENV commented for now
sudo chown root:erp /etc/erp/erp.env && sudo chmod 640 /etc/erp/erp.env
```

Generate the secrets the env file asks for:

```bash
openssl rand -hex 48   # -> JWT_SECRET
openssl rand -hex 32   # -> BACKUP_ENCRYPTION_KEY (optional now, needed for Part E)
```

### A5. Install the systemd unit

```bash
sudo cp /opt/erp/deploy/erp.service /etc/systemd/system/erp.service
sudo systemctl daemon-reload
sudo systemctl enable erp     # start-on-boot; we start it after the first deploy
```

---

## Part B — first deploy (from your Windows dev machine)

From the repo root, with the box reachable over SSH as the `deploy` user:

```powershell
.\deploy\deploy.ps1 -Target deploy@<droplet-ip>
```

This cross-compiles `erp-server` (linux/amd64), ships it plus `public/` and
`db/`, runs `migrate.sh`, swaps the binary in, and restarts the service. The
first run applies every migration in `db/` in order.

> Prereq: passwordless sudo for the restart. On the box:
> `echo 'deploy ALL=(root) NOPASSWD: /bin/systemctl restart erp' | sudo tee /etc/sudoers.d/erp-deploy`

Verify:

```bash
systemctl status erp            # active (running)
journalctl -u erp -n 40         # startup log, no fatal errors
curl -s localhost:8080/api/v1/version   # {"version":..., "git_commit":...}
```

At this point the app is up on `http://<droplet-ip>:8080` internally. Because
`ENV` is still unset, the **seed admin** login works so you can smoke-test.

---

## Part C — domain + automatic TLS

1. Buy the domain (Cloudflare Registrar is wholesale-priced and pairs with the
   free Cloudflare WAF in section 5 of the go-live doc).
2. Add a DNS **A record** for e.g. `erp.yourdomain.com` → the Droplet's public IP.
   (If you put Cloudflare in front, set the record to **DNS-only / grey-cloud**
   first so Caddy can complete the Let's Encrypt challenge, then switch to
   proxied afterward.)
3. Edit the domain in the Caddyfile and load it:

```bash
sudo cp /opt/erp/deploy/Caddyfile /etc/caddy/Caddyfile
sudo nano /etc/caddy/Caddyfile        # set erp.yourdomain.com
sudo systemctl reload caddy
```

Caddy provisions TLS automatically. Browse to `https://erp.yourdomain.com`.
Then set `CORS_ALLOWED_ORIGINS=https://erp.yourdomain.com` in `/etc/erp/erp.env`
and `sudo systemctl restart erp`.

---

## Part D — create a real admin, then flip to production mode

`ENV=production` **refuses to start** while the seed admin credential is still
active (routes.go:40), so do this in order:

1. Log in as the seed admin, create a real admin user with a strong password and
   MFA, and disable/rotate the seed admin (per the app's own admin flow).
2. Uncomment `ENV=production` in `/etc/erp/erp.env`.
3. `sudo systemctl restart erp` and confirm it comes back up
   (`journalctl -u erp -n 20`). A refusal here means the seed credential is still
   active — fix that first.

Now the staging box mirrors real production behavior.

---

## Part E — nightly backups

With `BACKUP_ENCRYPTION_KEY` set in `/etc/erp/erp.env`, run the installer — do
**not** hand-edit the crontab:

```bash
sudo bash /opt/erp/deploy/install_backup_cron.sh
```

It preflights the env file, installs a marker-tagged `0 2 * * *` line for the
`erp` user (idempotent — re-running replaces its own line and touches no other
job), and **runs one backup immediately**, because a crontab entry existing is
not evidence that a backup works.

> **This section used to tell you to paste in
> `0 2 * * * /bin/bash -c 'source /etc/erp/erp.env && backup.sh'` by hand. That
> line can never work.** `/etc/erp/erp.env` holds bare `KEY=value` lines with no
> `export` — and must, since systemd reads the same file via `EnvironmentFile=`,
> which rejects an `export ` prefix. `source` therefore sets `DATABASE_URL` as a
> *shell* variable, `backup.sh` runs as a **child process**, never inherits it,
> and dies on `set DATABASE_URL (source /etc/erp/erp.env)` — silently, at 02:00,
> into a log nobody reads. Anything that sources this env file for a child
> process must use `set -a; . /etc/erp/erp.env; set +a` instead. Found on
> 2026-08-07 when the cron was first genuinely installed on the droplet.

Then do a **restore drill** into a scratch database to prove the mechanism —
`docs/operations/backup_restore.md` has the invocation that actually works on
production (the `erp` role is deliberately not allowed to create databases, so
the drill needs `DRILL_ADMIN_URL`).

---

## Part F — redeploys

Every subsequent deploy is just:

```powershell
.\deploy\deploy.ps1 -Target deploy@erp.yourdomain.com
```

Build → ship → migrate → restart, same as `promote.ps1` does for the Windows
`live` environment.

---

## Part G — network boundary verification (49.7.3)

Run these from a **second machine** (your laptop, not the droplet itself) —
several of them are meaningless run locally, since loopback traffic never
crosses the firewall being tested.

```bash
# 1. Default-deny + only the declared ports are open. `ufw status verbose`
#    on the box should show "Default: deny (incoming)" and exactly
#    OpenSSH/80/443 as ALLOW rules - Part A1 sets this up; this just proves
#    nothing has drifted since.
ssh deploy@<host> sudo ufw status verbose

# 2. PostgreSQL is not reachable from outside this box at all (it should
#    only ever listen on loopback - Part A2's default cluster config).
#    A successful TCP connect here is a finding, not a success.
nc -zv -w3 <host> 5432 && echo "FINDING: Postgres port reachable from outside" || echo "OK: refused/timed out"

# 3. Direct-origin test: the Go server's own port must not be reachable
#    directly, bypassing Caddy (HOST=127.0.0.1 in erp.env - Part A4/D).
nc -zv -w3 <host> 8080 && echo "FINDING: app port reachable directly" || echo "OK: refused/timed out"

# 4. Alternate-port test: nothing else is listening that a port scan would
#    find. Adjust the range for how thorough you want this to be.
nmap -Pn -p1-65535 <host>   # expect only 22, 80, 443 open (plus 25/587 if
                             # this box also relays its own outbound mail,
                             # which it does not by default)

# 5. IPv4/IPv6 parity - repeat 1-4 against the box's IPv6 address if it has
#    one. `ufw` rules and Caddy's listener apply per-protocol; a firewall
#    that is correctly closed on IPv4 and wide open on IPv6 is a real,
#    previously-seen failure mode on cloud providers that assign a public
#    IPv6 address by default without anyone asking for one.
ssh deploy@<host> "ip -6 addr show scope global"   # any address printed here
                                                    # needs the same checks
```

Administrative access (SSH) is covered by whatever the droplet provider's own
key-based auth already enforces (Part A1 never enables password SSH); this
repo does not add a second admin channel to audit here. If a bastion/VPN
tunnel is used instead of direct SSH, repeat step 1 against the tunnel
endpoint, not the droplet's public IP.

`[needs deployment]`: every command above needs a real, reachable host to run
against - none of it is checkable from a dev tree with no droplet. Record the
actual output (not just "looks fine") in the deployment's own runbook the
first time this is run for real, so a later drift shows as a diff against
something, not a fresh guess.

## What this closes / unblocks in the go-live doc

- **Section 4** (production hosting) — done once Parts A–D are complete.
- **Section 5** (edge WAF) — put Cloudflare in front after Part C.
- **Sections 13/14/15/16** (pen-test, DR drill, UAT, hypercare) — now have a real
  box to run against. Run these on this staging box before flipping it (or a
  clone of it) to real production.

## Quick reference

| Task | Command |
|---|---|
| Status / logs | `systemctl status erp` · `journalctl -u erp -f` |
| Restart | `sudo systemctl restart erp` |
| Apply migrations manually | `set -a; . /etc/erp/erp.env; set +a; bash /opt/erp/deploy/migrate.sh` |
| Backup now | `set -a; . /etc/erp/erp.env; set +a; /opt/erp/deploy/backup.sh` |
| Install nightly backup cron | `sudo bash /opt/erp/deploy/install_backup_cron.sh` |
| Check the nightly backup ran | `ls -lt /opt/erp/backups/custom_erp_*.dump.enc \| head -3; tail -20 /var/log/erp-backup.log` |
| Redeploy | `.\deploy\deploy.ps1 -Target deploy@<host>` |
| Version running | `curl -s https://erp.yourdomain.com/api/v1/version` |
| Harden database roles (once, 49.7.1/49.7.4) | `psql "$DATABASE_URL" -v current_owner=erp -v app_password=... -v migrate_password=... -v backup_password=... -f deploy/postgres_harden.sql` |
| Check the app's own DB role posture | `tenantctl db-privilege` |
| Check systemd hardening actually applied | `systemd-analyze security erp.service` |
