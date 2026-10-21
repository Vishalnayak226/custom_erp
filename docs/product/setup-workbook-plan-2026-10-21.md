---
doc_id: DOC-SETUP-WORKBOOK-PLAN
title: Setup Workbook Plan
type: reference
status: draft
owner: documentation-maintainer
approvers: [documentation-maintainer, documentation-maintainer]
audience: [maintainers]
applies_to: Stage 128
authority: transition-copy
confidentiality: internal
last_verified: 2026-10-21
review_by: 2026-11-21
supersedes: none
superseded_by: none
verification_scope: plan only
---

# Setup Workbook Plan

Configure a whole client for purchase orders from one Excel workbook: download it, fill it in, upload it, preview it, apply it, and upload it again whenever something needs changing. The same mechanism serves every client from day one. Tracker: `micro_checklist.md` Stage 128. The steps it automates are the ones in [the PO setup guide](../guides/po-setup-guide.md).

## Decisions

| # | Decision | Why |
|---|---|---|
| D1 | **One Excel workbook per client**, one sheet per section, in the order the sections depend on each other, plus an Instructions sheet. | The user asked for Excel. One file covers a whole client and can be reviewed before upload. |
| D2 | **Uploaded in the app by the user** (Settings → Setup Workbook) on each client, including app and minn. Claude prepares the file; it does not write to production. | Production data is applied by the user (data-safety rule). The same screen then works for every client. |
| D3 | **Match by key, update only filled cells, never delete.** Each sheet has a key (GSTIN or name, short code, HSN, section code…). A matching row updates the record with the cells that are filled; a blank cell leaves the value as it is; a missing row creates the record. Nothing is deleted. A record is retired with Status = Inactive. | Re-uploading is always safe. A partial sheet never wipes data. |
| D4 | **Codes are a key only in the organisation they came from.** An exported workbook carries its organisation; uploaded there, its codes (`V00001`) find their records, so renames work. Uploaded to another organisation, codes are ignored and the natural keys match. | Copying one client's workbook to the next client must never update an unrelated `V00001`. |
| D5 | **Preview before Apply.** The upload lists, per sheet, what is new, what changes (old → new per field), what is unchanged and what is wrong, and changes nothing. | The user sees the effect before any record moves. |
| D6 | **Every row goes through the screen's own save.** Each row is replayed in-process through the same handler the screen calls (record save, settings, number series, approval rules, roles, role access, store access, users, industry, item fields), with the uploader's identity, so the same checks, numbering, audit entries and side effects apply. | No second copy of any rule, and nothing a screen would refuse gets in. |
| D7 | **Users are created, never re-passworded.** A new user gets the first password from the sheet and must set their own at first sign-in. An existing user's password is never touched. Super Admin cannot be created here. | The no-password-change rule; Stage 125 keeps Super Admin for the operator. |
| D8 | **No database schema change.** Each run is kept as an ImportJob record with its file, its preview and its result. | Additive and reversible; nothing to migrate or undo. |
| D9 | **Approval rules are replaced per record type.** The bands listed for a record type become its bands; other record types are untouched. If the new set cannot be saved, the old bands are put back. | Bands must not overlap or leave gaps, so they cannot be merged one row at a time. |

## Order of work

| Order | Item | Done when |
|---|---|---|
| 1 | 128.1 Reader and applier | A workbook previews and applies on a scratch server; re-applying the same file changes nothing. |
| 2 | 128.2 Template and export | The blank template and "this organisation's setup" download and open in Excel; an exported file re-applies with no changes. |
| 3 | 128.3 Screen | Download, upload, preview, apply and history work in the browser. |
| 4 | 128.4 Docs, tests, checks | Admin Guide section; Go tests; route capability and attack surface; build, vet and tests clean. |
| 5 | 128.5 First workbooks | app and minn workbooks filled from the user's answers, ready to upload after release and deploy. |

## Phases

### 128.1 Reader and applier

- `engines/xlsx_reader.go`: standard-library reader (zip + XML, shared and inline strings), lifted from `cmd/genattrlib`.
- `internal/server/setup_workbook.go`: the sheet registry (sheet → doctype or admin action, keys, columns), parse, plan (preview) and apply in dependency order. Rows are replayed in-process through the existing handlers after the same route-capability and module checks `apiMiddleware` runs. Each run is recorded as an ImportJob.

Sheets, in order: Settings (and Industry), Legal Entities, Locations, Departments, HSN Codes, Units, Number Series, Categories, MRP Rules, Product Types, Item Attributes, Item Names, Item Fields, Vendors, Receipt Rules, Reason Codes, TDS Sections, Roles, Role Access, Users, Approval Rules.

### 128.2 Template and export

- The record sheets take their columns from each record type's own field list, so a field added later appears in the template by itself. A hint row under each header says what a column takes (required, the choices, what a link points at).
- Export writes the organisation's current setup into the same workbook, so it can be corrected and uploaded again, or copied to start the next client.

### 128.3 Screen

- **Settings → Setup Workbook** for an Admin or Super Admin: Download template, Download this organisation's setup, Upload → Preview → Apply, and past runs, each with its file.

### 128.4 Docs, tests, checks

- Admin Guide section; a pointer from the PO setup guide.
- Tests: reader round trip; match and merge; a re-apply is a no-op; a password is never changed; codes from another organisation are ignored.
- Route capabilities and the attack surface regenerated; a live check on a scratch server.

### 128.5 First workbooks

- Filled from the user's answers; the user uploads them on app and minn, checks them there, and re-uploads to fix anything.

## Not adopted

| Option | Why not |
|---|---|
| A script Claude runs against each client's API | Writes to production from outside the app, needs a login per client, and the rate limiter throttles bulk calls. |
| A one-off SQL or migration per client | Cannot be re-run or corrected safely; bypasses every check. |
| Writing the records directly (as the CSV import does) | An update would replace the whole record, and the screen's side effects (cost centre per location, numbering, audit) would not run. |
| Items (SKUs) in the workbook | Not needed to start POs; items are created from the PO or through the existing CSV import. |

## Guardrails

- Never deletes. Never changes a set password. Never creates a Super Admin.
- Admin and Super Admin only; each replayed call is checked as if it came from the screen.
- Additive code only: no schema change, no new dependency, no build step.
