---
doc_id: OPS-INSTALL-001
title: Install from zero, and the sandbox rehearsal
type: procedure
status: draft
owner: operations-owner
approvers: [operations-owner, engineering-owner]
audience: [operators, maintainers]
applies_to: new host install check and sandbox rehearsal; scoped release acceptance required
authority: source
confidentiality: internal
last_verified: 2026-11-10
review_by: 2027-02-10
supersedes: none
superseded_by: none
verification_scope: installcheck and the sandbox built and tested on a scratch database on 2026-11-10 (erp-5c); not yet run on a real new host
---

# Install from zero, and the sandbox rehearsal

Stage 47.12.6 (sandbox) and 47.12.8 (install check). Operator steps, in order.

## Part 1 - Install a new host

Before any client exists. Run as the user that will own the app (usually `erp`).

1. **Install the prerequisites.** 64-bit Linux, PostgreSQL 14 or newer, Caddy, OpenSSL 3, at least 1 GB of memory and 10 GB of free disk. Create the `erp` database role and the database.
2. **Copy the release** to `/opt/erp` (binary, `deploy/` folder, `public/`). Check its signature before you run it (step 5).
3. **Create the env file and its secrets.**

   ```
   installcheck -env /etc/erp/erp.env -init-secrets -backup-dir /opt/erp/backups -channel stable
   ```

   - The file is created with mode 600 and filled with random `JWT_SECRET`, `BACKUP_ENCRYPTION_KEY`, `CHANNEL_CREDENTIAL_KEY`, `STORED_VALUE_KEY` and `AUDIT_SIGNING_KEY`.
   - A value that is already set is **never** replaced, on this run or any later one. A `CHANGE_ME` placeholder counts as not set.
   - **Back the file up now** (a password manager, not the server). A lost backup key means the backups cannot be opened.
   - Add `DATABASE_URL` yourself (copy the line from `deploy/erp.env.example`). The tool does not invent a database password.
4. **Point the domain** at the host (an A record), then run the full check:

   ```
   installcheck -env /etc/erp/erp.env -domain app.example.com -backup-dir /opt/erp/backups -deploy-dir /opt/erp/deploy
   ```

   Add `-public-ip <address>` behind NAT, a load balancer or Cloudflare. Add `-json` for a machine-readable report and `-strict` to count warnings as failures.
5. **Check the release signature** before installing it:

   ```
   installcheck -env /etc/erp/erp.env -release-bin ./erp-server -release-sig ./erp-server.sig -pub release/signing.pub
   ```

   A file that was changed, or built anywhere else, fails. Do not install it.
6. **Turn on TLS:** `sudo bash /opt/erp/deploy/enable_tls.sh app.example.com you@example.com`. Run `installcheck -domain ...` again; the certificate row now reads PASS.
7. **Schedule backups and prove one restores:** `sudo bash /opt/erp/deploy/install_backup_cron.sh`, then, after the first backup, `deploy/restore_drill.sh`.
8. **First client:** `tenantctl provision` (see the Admin Guide, Part C). A hosted client on a server that is already running needs only this step.

What the table checks: operating system, PostgreSQL version and reachability, free disk, memory, ports (app, 80, 443), clock (against a public time server), Caddy and OpenSSL, the domain's DNS, the live certificate, the secrets and the env file's permissions, the recorded upgrade channel, a writable backup folder, the restore script, and the release signature. Every FAIL and WARN row prints the exact next step. The exit code is 1 when anything FAILs.

Ports 80 and 443 need root to test. As a normal user those rows read WARN, not PASS.

**Upgrade channel.** `stable`, `candidate` or `hold` is written once to `UPGRADE_CHANNEL` in the env file and never overwritten. The Deploy click (gate D0d) reads it: a `hold` host takes no build, a `stable` host takes no candidate build (`release/channel.txt` says which a build is; default stable).

**Container image.** The repository has a `Dockerfile`; it has not been run for this check, so it is not part of the supported install path.

## Part 2 - The sandbox: create, rehearse, reset

A sandbox is a client flagged as one when it is made. It holds invented data only, and nothing it does leaves the server.

```
tenantctl sandbox-create -actor you -yes
tenantctl list                                  (SANDBOX column)
tenantctl sandbox-effects -tenant <id>          (what it would have sent)
tenantctl sandbox-reset -tenant <id> -reason "rehearsal over" -actor you -yes
```

- **Create** makes the client and fills it from the same seeded generator the performance laboratory uses (`-seed N`, default 1; the same seed always gives the same data). The admin password is shown once. Only the `small` shape is allowed on a real server; larger shapes need a scratch database.
- **Nothing goes out.** Email, SMS, webhooks, notifications, push, connector calls, extension hooks and the image drive are all stopped for a sandbox, whatever the server's own setting is. Each stopped call is recorded (`sandbox-effects`), so you can see what a live client would have sent. Nothing leaves the process.
- **Nothing real goes in.** No command copies a client's records into a sandbox. `tenantctl sandbox-copy` exists only to say so.
- **Reset** empties the sandbox and fills it again from the seed. Logins are kept. It refuses any client that is not a sandbox, and the database refuses to flag a client older than an hour as one, so a real client cannot be turned into a reset target. Reset needs no suspension or second approver because a sandbox holds only invented data; resetting a **real** client is the separate Stage 91 reset.
- **Marking.** A signed-in sandbox shows the existing top banner: "SIMULATED ENVIRONMENT (SANDBOX) - outside calls are OFF". The Platform console already lists it with a sandbox badge. A sandbox is suspended when its expiry passes; a reset gives it a new one.

### Rehearse before a connector goes live

When a client saves a channel, courier, payment or CRM connector credential, or switches one on, the audit log records one of:

- `CONNECTOR_REHEARSED` - a sandbox in this database has stopped a connector call in the last 90 days.
- `CONNECTOR_ENABLED_WITHOUT_REHEARSAL` (warning) - none has.
- `CONNECTOR_ENABLE_REFUSED` - none has, and the client turned on **Settings > Platform > Require a sandbox rehearsal**.

The setting is **Off** as shipped, so no existing client's behaviour changes: it only adds an audit row.

**Recommendation:** turn it on for a client once a sandbox rehearsal is part of its go-live (the first time it connects Shopify, a courier or a payment gateway). Run the rehearsal in a sandbox first, look at `sandbox-effects`, then switch the real connector on.
