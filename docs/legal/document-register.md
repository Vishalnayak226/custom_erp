---
doc_id: LEGAL-REG-001
title: Legal and customer document register
type: reference
status: draft
owner: legal-owner
approvers: [legal-owner]
audience: [engineering, operations, implementation, QA, domain reviewers]
applies_to: source release 0.1.0; configuration-specific acceptance required
authority: canonical
confidentiality: internal
last_verified: 2026-09-09
review_by: 2026-10-09
supersedes: none
superseded_by: none
---

# Legal and customer document register

This register separates reusable drafts, signed instruments and deployment-specific applicability decisions. Signed contracts, personal information and privileged advice belong in approved restricted storage; register only their safe identifier and review status here.

| ID | Document class / current source | Status | Owner / approval gate |
|---|---|---|---|
| LEGAL-TPL-001 | [Developer agreement](<../Contract/Developer Contract.md>) | Draft; existing path retained pending controlled move to legal/templates | Qualified counsel + business owner before use/signature |
| LEGAL-PRIV-001 | [Privacy applicability worksheet](privacy-applicability.md) | Reusable draft, no customer decision | Qualified privacy/legal + business owner |
| LEGAL-RET-001 | [Data lifecycle and retention](../data/data-lifecycle.md) | Proposed policy; no universal duration | Qualified legal + data/operations |
| LEGAL-CUST-001 | Customer order/terms/service agreement | No approved instrument registered | Commercial/legal owner must supply negotiated scope and signature reference |
| LEGAL-DPA-001 | Processing/subprocessor terms and notices | No approved instrument registered | Qualified privacy/legal owner |
| LEGAL-IP-001 | Dependency licenses and source/IP rights | Release review required | Engineering + qualified counsel where needed |

For an accepted instrument record version, parties by safe identifier, jurisdiction, effective/expiry/review dates, signer authority, signed artifact reference/hash, replacement and retention/hold treatment. Preserve superseded instruments. A product template is not a signed customer agreement, and generated module status is not a warranty or compliance statement.
