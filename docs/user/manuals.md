---
doc_id: DOC-USER-001
title: User and administrator manuals
type: reference
status: draft
owner: documentation-maintainer
approvers: [product-owner, implementation-owner]
audience: [end-user, tenant-administrator, implementation-consultant]
applies_to: source release 0.1.0; repository reading and print editions
authority: navigation
confidentiality: internal
last_verified: 2026-09-08
review_by: 2026-10-08
supersedes: none
superseded_by: none
---

# User and administrator manuals

Start with the [User manual](user-manual.html) or the
[Tenant administrator manual](tenant-admin-manual.html) in a browser. Both include
a contents list, keyboard links, print styling and canonical source references.
The signed-in Knowledge Center remains the task help delivered with the ERP.

These are draft curated editions built from the existing KB topics, with their
individual verification dates retained. They do not certify a supported customer
configuration. Check the [capability catalog](../generated/capability-catalog.md)
and complete the process-owner walkthrough before adopting an edition locally.
The admin edition starts with the application; platform installation belongs in
[deployment](../../deploy/README.md) and [developer setup](../engineering/developer-setup.md).

## Edit and regenerate

Edit canonical Markdown under `docs/kb`; choose reading order in
[manual-selection.json](../governance/manual-selection.json). Run
`pwsh -NoProfile -File docs/update-docs.ps1 -Group Content`, then repeat with
`-Check`. The manuals are generated, hashed and checked with the KB. They are
not embedded in the server and require no frontend framework or JavaScript.

References to a topic outside the curated edition lead to that source in the
repository. Keep the repository layout when distributing these files. For a
single-file print export, open the edition in a browser and print to PDF; review
page breaks and table readability for the chosen paper size.

The older [User Guide](../guides/USER_GUIDE.md) and
[Admin Guide](../guides/ADMIN_GUIDE.md) remain transitional sources until their
unique sections and published links have passed parity review. The
[User SOP](../guides/USER_SOP.md) and [Admin SOP](../guides/ADMIN_SOP.md) likewise
remain inputs to customer-approved procedures. Generation has not completed that
migration or authorized deleting any legacy guide.

Use the [customer operating procedure template](../implementation/operating-procedure-template.md)
to record local roles, controls, reconciliation and approval while linking the
canonical product task instead of maintaining another walkthrough copy.
