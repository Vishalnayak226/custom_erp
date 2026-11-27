---
doc_id: KB-PLATFORM-001
title: Control Panel and Data Console (platform operators)
type: procedure
status: draft
owner: operations-owner
approvers: [operations-owner, documentation-maintainer]
audience: platform operator
applies_to: source release 0.2.0; platform operators listed in ERP_PLATFORM_OPERATORS
authority: canonical
confidentiality: internal
last_verified: 2026-10-18
review_by: 2026-11-18
supersedes: none
superseded_by: none
section: Module Handbooks
order: 90
summary: For whoever runs the platform - every client on one page, which release is live and what number it carries, pausing a client or giving it a free trial, and repairing a record in any client with the Data Console.
screens: [control-panel, platform-console, tenant-entitlements, tenant-usage]
screen_sections: [control-panel=the-control-panel,platform-console=the-data-console,tenant-entitlements=tenant-entitlements,tenant-usage=tenant-usage]
topic_type: how-to
module: platform
task: Run the platform for every client
prerequisites: A Super Admin login listed in the server setting ERP_PLATFORM_OPERATORS
failure_behavior: Reload the screen before retrying; keep the client id, the record reference and the error code when you escalate.
---

# Control Panel and Data Console (platform operators)

These two screens are for the people who run the platform, not for a client's
own staff. Being a client's Super Admin is not enough: you must also be listed
in the server setting `ERP_PLATFORM_OPERATORS` as `client:username` (for
example `default:admin`). It is set on the server and cannot be set from inside
the app. Anyone else who opens these screens sees a short note saying so, or
the error `PLATFORM-0317`.

## The Control Panel

> [!WHAT]
> **Settings → Platform** holds one page per job: **Control Panel** (the
> running release, client counts, hosting domain and the last backup),
> **Clients**, **Releases**, **Certificates**, **Slow queries**, **Query plan**
> and **Capability maturity**.

> [!WHY]
> Each client row shows its version, active users, records and errors in the
> last 24 hours. **Pause / Resume** signs a client out without deleting
> anything, and your own client's row hides Pause and Trial so you cannot lock
> yourself out.

> [!USECASE]
> A client's trial ends and the client is paused automatically. You open
> **Settings → Platform → Clients**, check the row, and click **Mark paid** or
> extend the **Trial** in days.

> [!HOW]

1. Open **Settings → Platform → Control Panel** and read the release, clients
   and backup cards. Click a card to open its page.
2. On **Clients**, on a client's row, use **Modules**, **Data** or **Attributes** to work on
   that client.
3. Use **Pause / Resume** or **Trial** with a reason. Nothing is deleted.

**Settings → Platform → Control Panel** shows four cards. **Running release**,
**Clients** and **Hosted at** open their own pages.

- **Running release** - the version number, commit and date of the build serving
  the page, and the environment (`production` on the live server).
- **Clients** - how many, how many are on trial, paused or sandboxes, and how
  many have a problem.
- **Hosted at** - the domain each client's own address sits under.
- **Last backup** - the newest nightly backup, its outcome and size.

**Settings → Platform → Clients** has one row per client: status, address, created date, **Version** (the
release it is on), modules on and off, active users, records, last activity
and errors in the last 24 hours. From the row:

- **Modules** opens Tenant Entitlements for that client.
- **Data** opens the Data Console on that client.
- **Attributes** gives the client its product attributes from the industry library.
- **Pause / Resume** signs everyone at the client out within a minute, or lets
  them back in. You are asked for a reason; nothing is deleted.
- **Trial** starts or extends a free trial in days. When it ends, the client is
  paused automatically until you extend it or click **Mark paid**.

Your own client's row says *your client* instead of Pause and Trial, so you
cannot lock yourself out.

## Releases and version numbers

Every release to production gets a version number, worked out when it is
deployed from the changes it carries since the release that was live:

| The release contains | The number | Example |
|---|---|---|
| at least one new feature | the middle number goes up, the last goes back to 0 | 0.1.0 → 0.2.0 |
| only fixes and documentation | the last number goes up | 0.2.0 → 0.2.1 |
| go-live or a deliberate break | the first number, only when chosen by hand | 0.9.4 → 1.0.0 |

Once the new build is running and healthy it is written to the **Releases**
table (when, environment, version, commit, who, status) and every client's
**Version** changes to it. The table's **What changed** column says in a few
words what that release brought since the one before it (for example *CRM ·
Store app · Bulk purchase upload*). All clients run the same build, so they all show
the same number. A client is shown on another version only if it has been
pinned to one. The number also appears at the bottom-right of every user's
profile chip, with the release date.

## The Data Console

> [!WHAT]
> **Settings → Data Console** opens any client's records for a repair that has
> no screen of its own.

> [!WHY]
> The console does not re-run the business rules a screen does, so you prefer
> the normal screen when there is one. Passwords and secrets show as `••••••`,
> and every change is recorded in the client's Activity Log as
> `PLATFORM_DATA_EDIT` with before and after values.

> [!USECASE]
> A user reports an error. You open the `system_error_logs` table in the
> console, search the correlation id from the error dialog, and read the log
> behind it.

**Settings → Data Console** opens any client's records for a repair that has
no screen of its own. Prefer the normal screen when there is one: the console
does not re-run the business rules a screen does.

> [!HOW]

1. Choose the **Client** and the **Table** (one per record type, for example
   PurchaseOrder, GRN or Vendor, each with its row count).
2. Type a number, code or name in **Find** and click **Edit** on the row.
3. Change the fields you need. Tick **empty** to clear one.
4. Click **Save changes** and confirm. Only the fields you changed are written.

What it protects: passwords, secrets, credentials and customer personal details
show as `••••••` and keep their stored value; record numbers cannot be changed;
audit tables and error logs are view only. Every change is recorded in that
client's Activity Log as `PLATFORM_DATA_EDIT`, with the before and after values.

To find the log behind a client's problem, open its `system_error_logs` table
in the console and search by the correlation id the user saw in the error
dialog. `audit_logs` holds every change.

## Tenant Entitlements

> [!WHAT]
> Which modules each client has: start a new client with chosen packages, and
> switch a client's modules on or off afterwards.

> [!WHY]
> A client pays for the modules it uses. The modules it has not bought stay
> off, so its menu shows only what it can use and the server does no work for
> the rest.

> [!USECASE]
> A new chain buys the store and warehouse packages with a 14-day free trial.
> The operator provisions it with just those two packages. A month later the
> chain adds manufacturing, so the operator switches that module on for it.

> [!HOW]

1. Open **Settings → Platform → Tenant Entitlements**.
2. To start a client, click **+ Provision New Tenant**, enter the **Tenant
   ID** and **Schema Name**, tick its packages (none ticked means the full
   suite), set the **Free trial (days)**, and click **Provision Tenant**.
3. To change a client, pick it in **Tenant**.
4. Click a plan under **Apply a Plan** to match a package exactly, or switch
   single modules on or off under **Module Entitlements**.

Applying a plan switches off every optional module not in it. **Always On**
modules cannot be switched off. Only a platform operator sees this screen.

## Tenant Usage

> [!WHAT]
> A live view of every client's load: users signed in, requests running now,
> and the limits each client is set to.

> [!WHY]
> When one client is slow, this shows at once whether it is at its own limit
> or the whole server is busy.

> [!USECASE]
> A client reports that screens hang at 11 am. The operator opens **Tenant
> Usage**: that client's in-flight requests are red at its cap while the
> others are green, so its limit is raised instead of the server.

> [!HOW]

1. Open **Settings → Platform → Tenant Usage**.
2. Read **Tenants**, **In-Flight Requests (all tenants)** and **Tenants At
   Concurrency Cap** at the top.
3. In the table, check each client's **Active Users** and **In-Flight
   Requests** against its **Configured Limits**.

A red badge means the client is at its limit; amber means half way. The
screen is read-only. Only a platform operator sees it.
