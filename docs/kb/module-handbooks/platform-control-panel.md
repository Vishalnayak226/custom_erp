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
screens: [control-panel, platform-console]
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

**Settings → Control Panel** shows every client on one page.

- **Running release** - the version number, commit and date of the build serving
  the page, and the environment (`production` on the live server).
- **Clients** - how many, how many are on trial, paused or sandboxes, and how
  many have a problem.
- **Hosted at** - the domain each client's own address sits under.
- **Last backup** - the newest nightly backup, its outcome and size.

Each client has one row: status, address, created date, **Version** (the
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

**Settings → Data Console** opens any client's records for a repair that has
no screen of its own. Prefer the normal screen when there is one: the console
does not re-run the business rules a screen does.

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
