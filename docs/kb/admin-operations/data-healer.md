---
title: Data Healer
section: Admin & Operations
order: 40
summary: Run the one-time fixes a release brings for records saved before it, such as item aliases for existing items.
audience: admin
last_verified: 2026-11-15
screens: [datahealer]
doc_id: DOC-881E90D4D0
type: procedure
status: draft
owner: documentation-maintainer
approvers: [documentation-maintainer, documentation-maintainer]
applies_to: source documentation; scoped release acceptance required
authority: canonical
confidentiality: internal
review_by: 2027-02-15
supersedes: none
superseded_by: none
verification_scope: metadata and lifecycle classification; domain acceptance pending
topic_type: how-to
module: operations
task: Data Healer
prerequisites: Signed-in Super Admin to run a heal; Store Manager to view one
---

# Data Healer

**Settings > Data Healer** (short code `DH`) lists the **heals**. A heal is a
one-time fix that a release brings for records saved before it. A release never
changes your records when it is installed. Instead, the heal waits here until
someone runs it. Once run, it is **Done** for good, and running it again changes
nothing.

The first heal is **Item aliases**. Items and Item Names saved before the item
alias existed get the alias made from their name.

## While a heal waits

- A strip shows above every screen and cannot be closed. Whoever can run the
  heal sees **Fix now**; everyone else sees *Ask your admin*.
- The list of each record type the heal covers (for Item aliases: Item and
  Item Name) shows the heal at the top, with **Run heal** and **Review**.
- Saving one of those records waits for the heal. **Heal first** offers to run
  it; once it has run, the save goes on. Bills, GRNs, POS and imports keep
  working.

## Run a heal

1. Open **Settings > Data Healer**, or click **Fix now** on the strip.
2. Click **Open** on a heal marked *Waiting*. Each record shows the value it
   will get.
3. To give a record a different value, type it over the proposed one. An item
   alias can be 14 characters at most.
4. Click **Run heal** and confirm. The toast says how many records were healed.

The heal then shows as *Done* with how many records were healed, how many were
typed, the date and who ran it. Each run is in the Activity Log as
`DATA_HEAL_RUN`.

**Run heal** on a covered list does the same, using the proposed values.

## Change an item's name on past documents

Renaming an item in the Item master changes every screen and new print. Some
documents keep a copy of the name on their lines, and the first print of each
document is kept as it was printed. **Past names** changes the name there too.
Super Admin only.

1. Rename the item in the Item master.
2. Open **Settings > Data Healer** and click **Past names**.
3. Type the **Old name** and the **New name**. Pick **From** and **To** dates
   to change only documents made on those days, or leave them empty.
4. Click **Preview**. It lists each record type with its documents, lines and
   prints. Untick a type to leave it out.
5. Click **Apply**, type **YES** and confirm.

The whole name must match; case does not matter. A document filed with GST
(IRN or e-way bill) is listed as *filed, not changed*. Each changed document
gets a new version and an Activity Log row `PAST_NAME_CHANGE`. To undo, run it
again with the names swapped.

## When something looks wrong

- *Ask your admin*: only a Super Admin can run a heal. A Store Manager can open
  it and see the records.
- `PIM-9363` *The item alias can be 14 characters at most*: shorten the typed
  value. Nothing was written.
- `MASTER-1703` *That heal was not found*: refresh the screen.
- `MASTER-1704` *Heals are recorded by Run heal only*: a heal cannot be created
  or changed by hand or by import.
- A heal with nothing to fix is marked Done by *system* the first time anyone
  looks, so it never shows a strip.
- `MASTER-1705` to `MASTER-1708` come from **Past names**: type both names,
  pick a From date on or before To, type YES, or narrow the dates when more
  than 20,000 documents match.

The admin guide, *Security and operations* C.2.2, covers what a heal is and how
a release adds one.
