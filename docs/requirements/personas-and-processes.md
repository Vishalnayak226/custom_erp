---
doc_id: DOC-REQ-PROCESS
title: Personas and business processes
type: normative
status: draft
owner: product-owner
approvers: [product-owner, process-owners]
audience: [product, process-owners, QA, implementation]
applies_to: proposed reference configurations
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-07
review_by: 2026-10-07
supersedes: none
superseded_by: none
---

# Personas and business processes

These are research hypotheses derived from current module intent, not validated interviews.
Process owners must observe real users, record sample/context and accept their requirements.

| Persona | Job and success evidence | Research owner |
|---|---|---|
| Business owner / CEO | Understand supported scope, configure business, act on reconciled exceptions | Product |
| Cashier / store manager | Sell, recover payment, return and close cash without duplicates | Store process |
| Warehouse worker / supervisor | Receive, locate, pick, load and resolve scans within owner scope | Warehouse process |
| Finance / CFO | Explain subledger-to-GL balances, approve payments and close | Finance process |
| OMS / PIM / steward | Resolve orders and master conflicts with lineage | Order/data |
| Tenant admin | Assign safe identity/scope and review permission migration | Customer admin |
| Integrator / developer | Reproduce API behavior and extend without bypassing controls | Engineering |
| Operator / implementer / QA | Provision, migrate, test, restore and hand over accepted configuration | Operations/implementation/QA |
| Security / privacy / legal | Determine applicable controls and inspect scoped evidence | Qualified control owner |

## Process catalog

| Process | Start → end | Owner | Control and final reconciliation |
|---|---|---|---|
| Lead/order-to-cash | Demand → accepted tender/settlement | Order/store | Approved price and custody; order/shipment/invoice/payment agree |
| Procure-to-pay | Approved demand → supplier settlement | Procurement/finance | PO/receipt/invoice match; payable agrees to GL |
| Receive-to-ship | Expected receipt → handed-over shipment | Warehouse | Lot/serial/owner and QC; received/reserved/picked/shipped agree |
| Return-to-refund | Sale-linked request → inspected goods and final refund | Store/finance | Eligibility and original economics; return/refund/stock/GL agree |
| Record-to-report | Economic event → accepted period close | Finance | Balanced posting and exceptions; subledgers agree to GL |
| Plan-to-produce | Demand/BOM → accepted finished goods | Manufacturing | Approved revision/issues; WIP/output/scrap/variance agree |
| Project/service | Agreement/request → accepted delivery and bill | Service | Scope/budget/time approval; cost and revenue agree |
| Hire-to-retire | Approved employment → revoked access and retained records | HR/admin | Privacy/payroll review; identities and payment outcomes agree |
| Master lifecycle | Proposal → effective downstream version | Data | Steward quality/approval; rejects/merges/published versions agree |

## Observation record

Record participant role, release/configuration, fixture/device, task boundaries, errors,
interventions, outcome, reconciled totals and owner. Unobserved claims stay assumptions;
developer walkthroughs cannot replace customer acceptance. Use the
[release acceptance template](../governance/release-acceptance.md) for evidence pointers.

## Stable persona and process identities

These identifiers refer to the research hypotheses and control boundaries above; validation remains pending.

### PER-CEO

Business owner / product leadership. The persona table above owns the job, success evidence and research owner.

### PER-CASHIER

Cashier and store manager. The persona table above owns the job, success evidence and research owner.

### PER-WMS

Warehouse and floor operator. The persona table above owns the job, success evidence and research owner.

### PER-FIN

Finance and CFO. The persona table above owns the job, success evidence and research owner.

### PER-DATA

OMS / PIM / data steward. The persona table above owns the job, success evidence and research owner.

### PER-ADMIN

Tenant administrator. The persona table above owns the job, success evidence and research owner.

### PER-ENG

Integrator and developer. The persona table above owns the job, success evidence and research owner.

### PER-OPS

Operator, implementer and QA. The persona table above owns the job, success evidence and research owner.

### PER-SEC

Security, privacy and qualified legal reviewer. The persona table above owns the job, success evidence and research owner.

### PROC-O2C

Lead and order to cash. The process catalog above owns the start/end, accountable owner and final reconciliation.

### PROC-P2P

Procure to pay. The process catalog above owns the start/end, accountable owner and final reconciliation.

### PROC-R2S

Receive to ship. The process catalog above owns the start/end, accountable owner and final reconciliation.

### PROC-RET

Return to refund. The process catalog above owns the start/end, accountable owner and final reconciliation.

### PROC-R2R

Record to report and close. The process catalog above owns the start/end, accountable owner and final reconciliation.

### PROC-MFG

Plan to produce. The process catalog above owns the start/end, accountable owner and final reconciliation.

### PROC-SRV

Project and service delivery. The process catalog above owns the start/end, accountable owner and final reconciliation.

### PROC-HR

Hire to retire. The process catalog above owns the start/end, accountable owner and final reconciliation.

### PROC-MDM

Master data lifecycle. The process catalog above owns the start/end, accountable owner and final reconciliation.
