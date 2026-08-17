---
title: A day as an administrator
section: Role Journeys
order: 7
summary: Set the system up once so it stops needing you, then watch the four things that actually tell you it is healthy.
audience: admin
last_verified: 2026-08-17
screens: [users, roles, approval-rules, prefix-configs, dynamic-labels, doctype-builder, configuration, audit-logs, system-status, tenant-entitlements, tenant-usage, extension-hooks]
---

# A day as an administrator

A well-configured system barely needs you, which is the point. Most of this job
is front-loaded: get the setup right and the daily part becomes four checks and
a queue.

## Setup, in dependency order

Doing these out of order is the usual cause of a frustrating first week - each
one needs the one above it.

**1. Configuration.** **Settings » Configuration** holds the tenant-wide
decisions: your default country, which drives every phone field in the system;
GST mode for purchase orders; retention periods; the thresholds that send things
to approval.

**2. Prefix Configs.** Numbering rules for every document type. Set these before
anyone raises a document, because the numbers already issued do not change
retrospectively.

**3. Roles.** **Settings » Roles** grants read, create, update and delete per
record type. The system fails closed - a record type with no grant row is denied
to that role rather than allowed by default. Four roles ship: Cashier, Store
Manager, Super Admin, Supplier. Create more when a real job does not fit one.

The application also *hides* what a role cannot do: no **New** or **Bulk
Import** button without create, no row **Edit** or **Delete** icons without
those permissions. So a role's grants also predict what its holders actually
see, which makes testing a role a matter of signing in as one.

**4. Users.** **Settings » Users**. Assign the role, not a pile of individual
permissions.

**5. Approval Rules.** Which documents need a second pair of eyes, above what
threshold, decided by whom. Self-approval is refused everywhere and is not
configurable - the person who raises a document cannot be the person who
approves it.

**6. Master data.** Locations, then Vendors, then Items. Nothing transactional
works until these exist, and the application will tell users so with a banner
naming exactly what is missing.

## Shaping the system to the business

**Dynamic Labels** renames a field's label across the application without
touching the data underneath. Use it when your business calls something by a
different name; do not use it to repurpose a field for a different meaning.

**Database Schema Design** adds fields and record types. It supports both create
and edit, addressed by row - so a typo'd label or a wrong field type is fixed in
place rather than by deleting and recreating, which would discard that field's
stored data on every existing document.

> [!WARNING]
> Renaming a field's *fieldname* means data stored under the old key stops being
> displayed. It is not deleted, and renaming back restores it - but the screen
> will look empty in the meantime, so do it deliberately.

**Extension Hooks** run your own logic at defined points. **Extension Hook Log**
shows what they did, which is where to look when a hook is suspected.

## The daily four

| Check | Screen | Looking for |
|---|---|---|
| Is it up and healthy | **System Status** | Anything not green |
| What happened | **Activity Log** | Unexpected deletions, permission changes, after-hours activity |
| Is anyone blocked | **Approvals** | Documents waiting on a person who is away |
| Are we within our plan | **Tenant Usage** | Approaching an entitlement limit before it refuses someone |

**Tenant Entitlements** is what your business has licensed; a module absent from
everyone's sidebar is usually an entitlement, not a permission, and the two are
worth telling apart before you go changing roles.

## The requests you will actually get

**"I can't see a screen."** Almost always the role. Check the grants; check the
entitlement if it is a whole module.

**"I'm locked out."** Five failed sign-ins in a minute is a rate limit, not a
lockout - a minute's wait fixes it. A genuine lockout, or a lost second factor,
you resolve on the user's record.

**"I lost my phone and my recovery codes."** Reset their two-factor setup. They
will be asked to set it up again at next sign-in. Recovery codes are stored only
as a fingerprint, so nobody - including you - can look up an existing one.

**"Can you change this number for me?"** Ask whether the document is approved
first. Editing an approved document sends it back for re-approval on purpose,
and routing around that with an admin account defeats the control you configured
in step 5.

## Before going live

- [ ] Sign in as each role and confirm the sidebar matches what that job needs.
- [ ] Raise and approve one document of each approval-gated type, using two
      accounts.
- [ ] Confirm numbering looks right on a real document of each type.
- [ ] Check **System Status** is clean and backups are running.
- [ ] Walk [Open a shop and make your first
      sale](open-a-shop-and-make-your-first-sale.md) end to end on real data.

## Next

- [Country codes and phone number rules](country-phone-rules.md) - what
  `localization.default_country` actually changes.
- [Error code reference](error-code-reference.md) - every code, for when someone
  quotes one at you.
