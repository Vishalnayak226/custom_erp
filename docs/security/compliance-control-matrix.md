---
doc_id: SEC-COMPMATRIX-001
title: India compliance control matrix
type: reference
status: draft
owner: security-owner
approvers: [security-owner, privacy-lead]
audience: [security-owner, privacy-lead, statutory auditor, GST practitioner, customers]
applies_to: source release 0.18.0; India deployments
authority: proposed-policy
confidentiality: customer
last_verified: 2026-10-28
review_by: 2027-01-28
supersedes: none
superseded_by: none
---

# India compliance control matrix

Stage 47.16.1. The matrix itself is [`compliance-control-matrix.json`](compliance-control-matrix.json) - **version 1**, effective 2026-10-28. This page explains it and summarises the rows; the JSON is the source.

It maps each Indian rule that plausibly applies to a client of this ERP to the product control that serves it, says who is responsible - the **product**, the **customer** (the client running its business on it, or the platform operator hosting it), or **shared** - whether the control prevents or detects, the evidence that proves it (a test in this repository or a document), how long the record is kept, who owns the row and when it must be reviewed again. It is not legal advice and not a certification: no row says "compliant". Applicability, durations and sufficiency are for qualified counsel, the privacy lead, a GST practitioner and the statutory auditor.

## Rows

| Row | Rule | Responsibility | Status |
|---|---|---|---|
| CM-IN-001 | DPDP s.8(5) security safeguards | shared | implemented |
| CM-IN-002 | DPDP s.8(6) breach notice | shared | gap |
| CM-IN-003 | DPDP s.8(7) erasure when the purpose ends | shared | partial |
| CM-IN-004 | DPDP ss.11-13 data principal rights | shared | partial |
| CM-IN-005 | DPDP s.6 marketing consent | shared | implemented |
| CM-IN-006 | DPDP ss.4-6 purpose and minimisation | product | partial |
| CM-IN-007 | CERT-In 180-day logs in India | shared | partial |
| CM-IN-008 | CERT-In clock synchronisation | shared | gap |
| CM-IN-009 | CERT-In six-hour incident reporting | shared | partial |
| CM-IN-010 | IT Act s.43A / SPDI Rules security practices | product | implemented |
| CM-IN-011 | Accounts Rules 3(1) audit trail | product | partial |
| CM-IN-012 | Companies Act s.128(5) eight-year books | shared | needs-decision |
| CM-IN-013 | Closed periods stay closed | shared | implemented |
| CM-IN-014 | CGST s.36 / Rule 56 72-month records | shared | needs-decision |
| CM-IN-015 | CGST Rule 48(4) e-invoice | shared | partial |
| CM-IN-016 | CGST Rule 138 e-way bill | shared | partial |
| CM-IN-017 | Card data never stored (RBI, PCI DSS) | shared | partial |

Every row that is not `implemented` carries its gap in the JSON. The supporting pages are the [CERT-In log inventory](cert-in-log-inventory.md), the field-purpose register (`engines/personal_data_register.go`), the data classification registry (`engines/data_classification.go`) and the [incident runbook](../operations/incident_runbook.md).

## Changing it

1. Edit the row in the JSON, raise `version` by one and add a `changelog` entry with the date and what changed. Update the version and the table on this page.
2. Run `go test ./internal/securityscan -run ComplianceMatrix`. It fails when a row is incomplete, a field is outside its vocabulary, a dated field is malformed, a review date is more than a year after the last review, an evidence test or file does not exist, this page misses a row, or the version here and in the JSON differ. A row past its `review_by` is printed as overdue - review it, then move both dates.
3. A row moves to `implemented` only when every clause of its rule that the product can serve has passing evidence; the customer's part stays in `customer_duty`.
