---
title: Security, Roles & Approvals
section: Module Handbooks
order: 40
summary: How roles gate what a user can see and do, how the maker-checker approval engine routes and locks high-value documents, and how field-level permission and MFA controls layer on top — the cross-cutting reference every other module handbook points back to.
audience: admin, auditor
last_verified: 2026-09-03
screens: [users, roles, approvals, approval-rules, profile, audit-logs]
---

# Security, Roles & Approvals

Three separate mechanisms decide who can do what in this ERP, and they stack
rather than substitute for one another. **Roles** gate which record types a
user can read, create, update, or delete at all. The **maker-checker
approval engine** gates specific documents above a configured amount,
independent of role-level access — someone can have full create/update
rights on Purchase Orders and still need a second person's sign-off once the
amount crosses a threshold. **Field permissions** narrow visibility and
editability down to individual fields within a record type a role can
otherwise fully access. None of this is enforced only in the browser: every
check is re-run server-side on every request, so a restriction here holds
even against a hand-written API call. This article is the reference other
module handbooks link back to rather than re-explain.

## Roles: what exists today

Three roles are built into the codebase by name (`engines/roles.go`):

| Constant | Stored value | Meaning |
|---|---|---|
| `RoleSuperAdmin` | `Super Admin` | Unrestricted access to everything, everywhere. Never needs a permission row — a missing grant means denial for every other role, but Super Admin is exempt from that check entirely. |
| `RoleStoreManager` | `Store Manager` | Broad operational access, scoped mostly to read/create/update rather than delete. |
| `RoleCashier` | `Cashier` | POS-facing subset — mostly read plus a handful of create/update rights (own cart, own session, expense claims, leave requests). |

A tenant is not limited to these three. `Supplier` is a real fourth role
seen throughout the app and the permission matrix (a supplier login scoped
to their own vendor's PIM submissions — see `ADMIN_GUIDE.md` §B.2), and
because permissions are just rows in a table keyed by role name, any string
can be granted rights and used as a role. The catch: the **Roles** screen's
"Add or Update a Grant" form populates its Role dropdown from
`GET /api/v1/admin/roles`, which runs `SELECT DISTINCT role FROM users` —
it lists roles currently in use, not a fixed catalog. **To grant permissions
to a brand-new custom role name, create at least one user with that role
first**; only then does it appear in the Roles screen's dropdown to grant
against.

### Super Admin was renamed from "HR/Admin"

Stage 40.3 renamed the top role's **display name** from `HR/Admin` to
`Super Admin` — the old name was inherited from the very first migration and
read like a job title rather than a privilege level. This is precise to get
right because the rename is partial by design:

- The migration (`db/migrations_stage40_3_super_admin_role.sql`) rewrote the
  stored value across `users`, `role_permissions`, `approval_rules`,
  `approval_log` and `field_permissions` — new rows and freshly-migrated
  tenants store the literal string `Super Admin`.
- The **legacy literal `HR/Admin` is still accepted everywhere it might show
  up** — a session token minted before the migration, an external script or
  saved API payload, or a tenant schema provisioned from an older snapshot.
- Every privilege check in the codebase goes through one function,
  `engines.IsSuperAdmin(role)`, rather than comparing role strings directly —
  this collapsed 37 scattered string comparisons across nine files into one
  predicate, so a future rename is one edit rather than a repeat sweep. The
  check is case- and space-insensitive (`"super admin"`, `"SuperAdmin"`, and
  `"HR/Admin"` all resolve true) deliberately, because a role string arriving
  from a token, header, CSV import, or hand-typed API payload failing to
  match on casing alone would be a silent-denial security bug, not a
  cosmetic one.
- One place the old name still surfaces in plain text: a non-Super-Admin
  hitting an admin-only endpoint gets back the literal message **"Only
  HR/Admin can access this"** (`requireHRAdmin`, `handlers_admin_identity.go`)
  — cosmetic, not a functional gap, but worth knowing so it isn't mistaken
  for the role itself having reverted.

`engines.CanonicalRole(role)` maps either name to `Super Admin` for anything
that needs to *display* or *write* the canonical form; anything not
recognised as Super Admin passes through untouched, so a tenant's own custom
role names are never rewritten.

## Record-type permissions: the Roles screen

**Settings → Roles** (Super Admin only, screen id `roles`) is where an admin
grants or edits what a role may do with a record type. Two API calls back
it: `GET /api/v1/admin/role-permissions` lists every currently-granted
(role, doctype) row, and `POST /api/v1/admin/role-permissions` creates or
updates one (`role`, `doctype_name`, `allow_read`, `allow_create`,
`allow_update`, `allow_delete` — an upsert keyed on `(role, doctype_name)`,
so saving a grant for a role/doctype pair that already has one overwrites it
rather than adding a duplicate). There is deliberately no delete endpoint:
to revoke a grant, save it again with every checkbox off rather than
removing the row.

**This system fails closed.** A role with no row at all for a record type
has no access to it — a missing grant is a denial, never a default allow.
Super Admin is the sole exception and is never listed in the permission
table for exactly that reason.

Since Stage 30.5.7 the app also *hides* what a role cannot do, not just
blocks it after the fact: no **New** or **Bulk Import** button appears
without create rights, no row-level **Edit**/**Delete** icon appears without
update/delete rights. So `docs/guides/PERMISSION_MATRIX.md` — generated
directly from the live `role_permissions` table via
`go run ./cmd/gendocs -db "postgres://..."`, not hand-maintained — doubles
as a prediction of what each role's screens actually show, not just what the
API will accept.

### Reading the permission matrix

`PERMISSION_MATRIX.md` (generated 2026-08-07 as of this writing) covers
**108 record types across 4 roles** — Cashier, Super Admin, Store Manager,
Supplier. Legend: **R** read, **C** create, **U** update, **D** delete, `-`
none. A representative slice:

| Record type | Cashier | Super Admin | Store Manager | Supplier |
|---|---|---|---|---|
| Item | R | R C U D | R | R |
| PurchaseOrder | - | R C U D | R U | - |
| SalesOrder | R C | R C U D | R C U | - |
| GRN | - | R C U D | - | - |
| SupplierSubmission | - | R C U D | R | R C U |
| GLPost | - | R C | - | - |
| StockLedgerEntry | R | R C | - | - |

Two patterns worth internalising from the full table: Store Manager gets no
row at all for the tightest-controlled doctypes (GRN, TransferOrder,
RoboticsIntegrationCredential), and Supplier's only rows anywhere are Item
(read) and SupplierSubmission (its own PIM workflow) — a Supplier login is
otherwise blind to the rest of the system by design, not by omission. Since
this file regenerates from the live table, treat any other claim about "what
role X can do with doctype Y" as provisional until checked against a fresh
regeneration or the Roles screen directly — this article does not attempt to
reproduce all 108 rows.

## Field-level permissions

Below record-type grants sits a finer-grained layer: **field permissions**,
one row per (role, doctype, field) with independent `allow_read` and
`allow_write` flags (`engines/field_permissions.go`, table
`field_permissions`, migrated in `db/migrations_stage16_field_permissions.sql`).
Three functions apply it at different points the generic document engine
already goes through (`internal/server/handlers_core_doc_engine.go`):

- `FilterFieldsForRole` strips any field marked not-readable before a
  document is returned to the caller — both on single-document GET and on
  the create-response echo.
- `RejectRestrictedFieldWrites` refuses the whole request (HTTP 403) if the
  submitted payload touches any field marked not-writable for that role.
- `FilterFieldMetaForRole` applies the same read filter to field *metadata*
  (labels, types), so a restricted field doesn't even appear in a
  role-aware form definition.
- The bulk PIM edit path (`engines/pim_bulk.go`) calls the same write
  restriction explicitly — added in Stage 36.7.6 specifically because the
  single-document path already enforced it and the bulk path, editing the
  same documents through a different door, had not yet been taught to.

> [!WARNING]
> **There is no admin screen or API endpoint to manage field permissions.**
> Confirmed by grep: `field_permissions` is read by the four enforcement
> points above and referenced in the tenant-provisioning seed list
> (`engines/saas.go`), but no handler in `internal/server/routes.go` exposes
> it for create/update/delete, and no view in `public/app.js` references it
> at all. The only rows that exist today are whatever a migration inserted —
> in the base install that is exactly two: `('Cashier', 'Item', 'cost_price', ...)`
> and `('Cashier', 'Item', 'gst_rate', ...)`, both read- and write-denied
> (`db/migrations_stage16_field_permissions.sql`). Setting up any other
> field-level restriction today means writing directly to the
> `field_permissions` table — there is no self-service path from the app.

## The maker-checker approval engine

Approval is a separate mechanism from record-type permissions: it gates
**specific documents** once their amount crosses a configured threshold,
regardless of whether the acting role otherwise has full rights over that
doctype. A doctype with zero configured rules simply isn't approval-gated —
there is no default-on behaviour.

### How a rule routes

An **approval rule** (`approval_rules` table, `engines.ApprovalRule`) is one
amount-slab-to-role mapping: `doctype`, `min_amount`, `max_amount` (nullable
= no upper bound), `required_role`. When a document is submitted, the engine
picks the rule for that doctype whose range contains the document's amount
(`ORDER BY min_amount DESC LIMIT 1` — the highest-min_amount matching row
wins), and that rule's `required_role` is who must decide it. Saving a rule
that overlaps another rule's range for the same doctype is refused at save
time (`UpsertApprovalRule`'s own overlap check) — the table itself does not
enforce this, so this check exists specifically to prevent two rules
silently competing for the same amount.

The amount compared is the document's **base-currency** amount
(`DocumentBaseAmount`), not a raw stored figure, so a rule written in the
tenant's own currency is never compared against a foreign-currency amount.
Which field on the document counts as "the amount" is doctype-specific — the
engine checks `total_amount` first, then falls back through `amount`,
`discount_amount` (POSCart's discount-percentage gate), `variance_qty`
(CycleCountLine, an absolute-quantity check, not a rupee one),
`invoice_amount` (VendorInvoice override routing), and `points_value`
(LoyaltyRedemptionRequest, rupee value of the points burned).

### The lifecycle: Submit → Decide

1. **Submit** (`SubmitForApproval`, `POST /api/v1/approval/submit`) moves a
   `Draft` document to `Pending Approval`. Fails with `ADMINC-0032`
   ("Approval workflow missing... nothing to route this to") if the doctype
   has no rule covering the amount at all.
2. **Decide** (`DecideApproval`, `POST /api/v1/approval/decide`) approves or
   rejects a `Pending Approval` document, enforcing three checks inside one
   transaction that row-locks the document (`SELECT ... FOR UPDATE`) for its
   whole duration — specifically to stop two concurrent decide calls (a
   double-click, a retry, two checkers racing) from both reading
   `Pending Approval` before either commits and both writing a decision:
   - **Maker-checker segregation.** The actor can never decide a document
     they themselves submitted, regardless of role — Super Admin included.
   - **Role match.** The actor's role must equal the amount slab's
     `required_role`, unless the actor is Super Admin (which overrides this
     check the same way it overrides every other role gate in the
     codebase). A mismatch surfaces as `PURCHA-0083` for PurchaseOrder
     specifically, or a generic 422 for every other doctype.
   - **Location match.** A non-Super-Admin actor's location must match the
     document's `location` field, when the document has one.
   - Rejecting requires a non-empty comment — an empty one is refused with
     `APPROV-0159` ("Rejection reason is required").
3. Approval, not submission, is what actually finalizes several doctypes'
   side effects — the pattern this codebase calls "the approval decision
   itself is the authorization to post": an approved `POSCart` completes
   checkout (`FinalizePOSCheckout`), an approved `CycleCountLine` posts its
   inventory adjustment, an approved `VendorInvoice` override posts the GL
   entry and pays it, an approved `JournalVoucher`/`IntercompanyTransaction`/
   `PrepaidExpenseSchedule` posts, an approved `PriceListVersion` supersedes
   the prior active version, an approved `LoyaltyRedemptionRequest` burns
   the points, and an approved `HoldReleaseRequest` releases the hold. A
   **rejected** document never ran those side effects in the first place, so
   there is nothing to undo.

### Editing an approved document re-opens it

`ResetToPendingOnEdit` sends an already-`Approved` document back to
`Pending Approval` the moment it's edited, rather than letting the edit
stand silently approved — the approval amount is re-derived from the freshly
saved data, so a change to the routing-relevant field (e.g. `total_amount`)
re-evaluates against the right slab next time, not the one it was approved
under.

### Bulk decisions

`BulkDecideApproval` (`POST /api/v1/approval/bulk-decide`) applies one
decision to up to the tenant's configured bulk-edit limit of documents.
Each document still runs through `DecideApproval`'s full transaction
independently — this is a tolerant loop, not one all-or-nothing
transaction, so one document failing a check (say, a maker-checker
violation) doesn't block the rest of the batch. It deliberately skips the
doctype-specific finalize-on-approve side effects listed above — it's
scoped to bulk-approving PIM `ProductContent` from the Workbench, which has
none, and adding those unconditionally would silently change behaviour for
any other doctype selected in bulk.

### Who sees what in the approvals inbox

`GET /api/v1/approval/pending` (screen id `approvals`) lists every
`Pending Approval` document across every approval-gated doctype. Super Admin
sees everything tenant-wide; every other role sees only documents at their
own location (or with no location on the document at all).

### Managing the rules themselves

**Settings → Approval Rules** (screen id `approval-rules`) is where a Super
Admin configures the amount-slab/role routing. `GET /api/v1/approval/rules`
is open to any authenticated role (routing decisions during submit need to
read the rules too); `POST` and `DELETE` on the same endpoint are Super
Admin only. Deleting the last rule for a doctype makes it ungated again —
the same as if a rule had never existed.

### Audit trail

Every submit/approve/reject/modified action writes one row to
`approval_log` (`ListApprovalLog`, `GET /api/v1/approval/log`) — actor,
role, amount, and comment (a rejection's is mandatory, everything else's is
optional). `LogAuditEvent` also records `APPROVAL_DECISION` and
`APPROVAL_BULK_DECISION` entries visible from **Audit Logs** (screen id
`audit-logs`).

## MFA: who must enroll, and how recovery works

`RequiresMFA(role)` is `IsSuperAdmin(role)` — today the only role gated into
mandatory TOTP enrollment is Super Admin (the codebase's stand-in for the
broader "admin/finance/IT/super users" group SEC-V2 §12 names, since there
is no distinct Finance/IT role yet). Deliberately routed through
`IsSuperAdmin` rather than a role-keyed map, so a session still carrying the
legacy `HR/Admin` name is still MFA-gated — a map lookup keyed on the
literal string would have silently dropped MFA for exactly the accounts
that most need it.

Enrollment is standard TOTP (RFC 6238, SHA-1, 6 digits, 30-second step),
with clock-drift tolerance configurable per tenant
(`security.totp_skew_steps`, default 1 step / ±30s). Recovery has two
self-service paths, both reachable without an admin:

- **Ten single-use recovery codes** (`RecoveryCodeCount`), issued and shown
  once at enrollment, accepted on the login screen in place of a 6-digit
  code. Only a SHA-256 hash is ever stored — a database leak alone cannot
  reconstruct a working code, and support cannot look a user's codes up for
  them.
- **My Profile → Two-Factor Recovery** (screen id `profile`) lets a user
  move to a new device (confirmed with their password, not a code, since
  the old device may be gone) and regenerate a fresh set of codes, which
  invalidates every previously-issued code including unused ones.

When both are gone, an admin uses **Reset 2FA** on the **Users** screen
(`POST /api/v1/admin/users/reset-mfa`, Super Admin only) — it clears the
user's authenticator and every recovery code and forces re-enrollment at
next login; it does **not** turn MFA off for the account. The
command-line-only `cmd/reset_mfa` utility still exists as the last resort
for a tenant where nobody can log in at all, needing shell and database
access on the server. The ADMIN_GUIDE's advice to keep a **second Super
Admin account enrolled on a different device** exists precisely because a
single admin account with both the phone and the recovery codes lost has no
self-service or in-app recovery path at all.

## Managing users

**Settings → Users** (screen id `users`, Super Admin only) covers the full
lifecycle, all through `internal/server/handlers_admin_identity.go`:

| Action | Endpoint | Notes |
|---|---|---|
| Create | `POST /api/v1/admin/users` | Username + role required, password 8+ characters, defaults to location `HO`. Refused once a configured `max_users` tenant limit would be exceeded. |
| List | `GET /api/v1/admin/users` | Includes `supplier_code` (blank for non-Supplier accounts). |
| Deactivate / reactivate | `POST /api/v1/admin/users/status` | You cannot deactivate the account you're currently logged in as. |
| Change location | `POST /api/v1/admin/users/location` | E.g. a Store Manager transferred to a different store. |
| Reset MFA | `POST /api/v1/admin/users/reset-mfa` | See MFA section above. |
| Link a Supplier login to its Vendor | `POST /api/v1/admin/users/supplier` | Until linked, a Supplier account can sign in but every screen refuses it — deliberate, since an unscoped supplier session is exactly what the row-level PIM scoping exists to prevent. |

**A status, role, or location change takes effect on the user's live
session**, not at their next login — the auth middleware re-reads
active-status/role/location on every request rather than trusting what was
in the token at issue time, cached for `AUTH_STATE_CACHE_SECONDS` (30s
default). A change made directly in the database rather than through these
endpoints only takes effect once that cache window elapses.

## Troubleshooting

**"Only HR/Admin can access this" on an admin screen, even though the role
picker shows "Super Admin."** This is the literal, unmodernized text
`requireHRAdmin` returns for any non-Super-Admin caller — it is not a sign
the rename broke or reverted. Confirm the acting user's actual role; the
message wording is cosmetic.

**A newly-typed custom role name doesn't show up on the Roles screen's
grant form.** The dropdown is populated from roles currently assigned to at
least one user (`SELECT DISTINCT role FROM users`), not a fixed catalog.
Create a user with that role first, then it appears to grant against.

**A permission grant "won't go away."** There is no delete endpoint for
`role_permissions` — save the grant again with every checkbox unchecked
rather than expecting a row to be removable.

**A role has read access to a record type but a screen still shows fields
missing or a save is rejected as not writable.** Check `field_permissions`
directly — no UI exists for it (see the warning above), and the base
install ships exactly one restriction: Cashier is denied both read and
write on Item's `cost_price` and `gst_rate`.

**A document sits in `Pending Approval` and nobody with the right role can
act on it.** Check `required_role` on the matching approval rule
(**Settings → Approval Rules**) against the actor's actual role — a mismatch
is a 422 (`PURCHA-0083` for PurchaseOrder specifically, generic for every
other doctype), not a permission error, so it can look like a bug rather
than a routing gap. Also check location: a non-Super-Admin approver at a
different location than the document is rejected outright.

**"Approval workflow is not configured for this transaction" (`ADMINC-0032`)
on submit.** No approval rule's amount range covers this document's amount
for this doctype at all — add one on **Settings → Approval Rules**, or
widen an existing rule's range.

**A reject action is refused.** `APPROV-0159` — a comment is mandatory to
reject; approving never requires one.

**The same person who created a document also approves it, without
error.** Should not happen — `DecideApproval` refuses this unconditionally,
Super Admin included. If it did happen, the account making the decision is
not actually recorded as the document's `created_by`; check which user
account actually created it.

**Someone lost their phone and their recovery codes, and there's no other
Super Admin account.** No self-service or admin-UI recovery exists for that
combination — it needs shell and database access on the server via
`cmd/reset_mfa`. This is exactly the scenario the "keep a second Super Admin
account on a different device" advice exists to avoid.

## What is not here yet

**Field permissions have no admin surface.** As documented above, the
enforcement (`FilterFieldsForRole`, `RejectRestrictedFieldWrites`,
`FilterFieldMetaForRole`, and the bulk-edit path) is real and wired into
every read/write/bulk-edit path the generic document engine handles, but
setting up a new field-level restriction — or removing the two shipped with
the base install — has no screen and no API endpoint. It is a direct-SQL-only
capability today.

**Role-permission grants can only be widened or narrowed in place, never
deleted.** `role_permissions` has no DELETE endpoint; a mistaken grant is
corrected by re-saving it with every flag off, not by removing the row.

**There is no fixed role catalog beyond the three built-in constants.** A
role is, mechanically, any string a user account carries. The Roles screen's
own dropdown for granting permissions is derived from existing users rather
than an independently maintained list, which means the sequence for
onboarding a genuinely new role is "create a user with that role name,
then grant it" rather than the other way around.

**MFA is mandatory for Super Admin only.** There is no per-tenant setting to
extend the mandatory-MFA gate to Store Manager, Cashier, or a custom role —
`RequiresMFA` is a direct alias for `IsSuperAdmin` with no configuration
point in between.
