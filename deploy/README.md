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
`backup.sh` (all run on the box), and `deploy.ps1` (runs on your dev machine).

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

With `BACKUP_ENCRYPTION_KEY` set in `/etc/erp/erp.env`, register a cron for the
`erp` user:

```bash
sudo crontab -u erp -e
# add:
0 2 * * *  /bin/bash -c 'source /etc/erp/erp.env && /opt/erp/deploy/backup.sh' >> /opt/erp/backups/backup.log 2>&1
```

Test it once by hand, then do a **restore drill** (go-live doc section 14) into a
scratch database to prove the mechanism — the restore one-liner is documented at
the top of `backup.sh`.

---

## Part F — redeploys

Every subsequent deploy is just:

```powershell
.\deploy\deploy.ps1 -Target deploy@erp.yourdomain.com
```

Build → ship → migrate → restart, same as `promote.ps1` does for the Windows
`live` environment.

---

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
| Apply migrations manually | `source /etc/erp/erp.env && bash /opt/erp/deploy/migrate.sh` |
| Backup now | `source /etc/erp/erp.env && /opt/erp/deploy/backup.sh` |
| Redeploy | `.\deploy\deploy.ps1 -Target deploy@<host>` |
| Version running | `curl -s https://erp.yourdomain.com/api/v1/version` |
