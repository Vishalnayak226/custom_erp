<!-- GENERATED from docs/product/capability-register.json by cmd/doclint. DO NOT EDIT. -->

# Requirements and evidence traceability

This projection links requirement, design, implementation work, tests, help and release evidence. Missing signed release evidence is explicitly shown as pending. Approval remains with the accountable product, process, security and QA owners.

## Shared requirements

These requirements apply to every capability and configuration below: [docs/requirements/nonfunctional-requirements.md](../../docs/requirements/nonfunctional-requirements.md)

## CAP-POS — Point of sale

- Requirements: [docs/requirements/modules/pos.md#fr-pos-001](../../docs/requirements/modules/pos.md#fr-pos-001), [docs/requirements/modules/pos.md#fr-pos-002](../../docs/requirements/modules/pos.md#fr-pos-002), [docs/requirements/modules/pos.md#fr-pos-003](../../docs/requirements/modules/pos.md#fr-pos-003)
- Design/control: [engines/pos_checkout.go](../../engines/pos_checkout.go)
- Automated tests: [internal/server/pos_atomic_checkout_stage47_3_test.go](../../internal/server/pos_atomic_checkout_stage47_3_test.go), [internal/server/pos_pricing_stage47_2_test.go](../../internal/server/pos_pricing_stage47_2_test.go)
- User help: [docs/kb/module-handbooks/pos-operations.md](../../docs/kb/module-handbooks/pos-operations.md)
- Work items: 47.2, 47.3
- Verification/review: 2026-09-09 / 2026-10-09
- Signed release evidence: pending

- Business need: [docs/requirements/business-requirements.md#br-001](../../docs/requirements/business-requirements.md#br-001), [docs/requirements/business-requirements.md#br-002](../../docs/requirements/business-requirements.md#br-002), [docs/requirements/business-requirements.md#br-003](../../docs/requirements/business-requirements.md#br-003), [docs/requirements/business-requirements.md#br-004](../../docs/requirements/business-requirements.md#br-004), [docs/requirements/business-requirements.md#br-005](../../docs/requirements/business-requirements.md#br-005)
- Personas: [docs/requirements/personas-and-processes.md#per-cashier](../../docs/requirements/personas-and-processes.md#per-cashier)
- Processes: [docs/requirements/personas-and-processes.md#proc-o2c](../../docs/requirements/personas-and-processes.md#proc-o2c)
- Security/data/operations/legal control: [docs/requirements/control-requirements.md#sec-001](../../docs/requirements/control-requirements.md#sec-001), [docs/requirements/control-requirements.md#sec-002](../../docs/requirements/control-requirements.md#sec-002), [docs/requirements/control-requirements.md#sec-003](../../docs/requirements/control-requirements.md#sec-003), [docs/requirements/control-requirements.md#data-001](../../docs/requirements/control-requirements.md#data-001), [docs/requirements/control-requirements.md#data-002](../../docs/requirements/control-requirements.md#data-002), [docs/requirements/control-requirements.md#ops-001](../../docs/requirements/control-requirements.md#ops-001), [docs/requirements/control-requirements.md#ops-002](../../docs/requirements/control-requirements.md#ops-002)

## CAP-RET — Returns and refunds

- Requirements: [docs/requirements/modules/returns.md#fr-ret-001](../../docs/requirements/modules/returns.md#fr-ret-001), [docs/requirements/modules/returns.md#fr-ret-002](../../docs/requirements/modules/returns.md#fr-ret-002), [docs/requirements/modules/returns.md#fr-ret-003](../../docs/requirements/modules/returns.md#fr-ret-003)
- Design/control: [engines/returns_atomic.go](../../engines/returns_atomic.go)
- Automated tests: [internal/server/returns_stage47_4_test.go](../../internal/server/returns_stage47_4_test.go)
- User help: [docs/kb/module-handbooks/returns-and-refunds.md](../../docs/kb/module-handbooks/returns-and-refunds.md)
- Work items: 47.4
- Verification/review: 2026-09-09 / 2026-10-09
- Signed release evidence: pending

- Business need: [docs/requirements/business-requirements.md#br-001](../../docs/requirements/business-requirements.md#br-001), [docs/requirements/business-requirements.md#br-002](../../docs/requirements/business-requirements.md#br-002), [docs/requirements/business-requirements.md#br-003](../../docs/requirements/business-requirements.md#br-003), [docs/requirements/business-requirements.md#br-004](../../docs/requirements/business-requirements.md#br-004), [docs/requirements/business-requirements.md#br-005](../../docs/requirements/business-requirements.md#br-005)
- Personas: [docs/requirements/personas-and-processes.md#per-cashier](../../docs/requirements/personas-and-processes.md#per-cashier)
- Processes: [docs/requirements/personas-and-processes.md#proc-ret](../../docs/requirements/personas-and-processes.md#proc-ret)
- Security/data/operations/legal control: [docs/requirements/control-requirements.md#sec-001](../../docs/requirements/control-requirements.md#sec-001), [docs/requirements/control-requirements.md#sec-002](../../docs/requirements/control-requirements.md#sec-002), [docs/requirements/control-requirements.md#sec-003](../../docs/requirements/control-requirements.md#sec-003), [docs/requirements/control-requirements.md#data-001](../../docs/requirements/control-requirements.md#data-001), [docs/requirements/control-requirements.md#data-002](../../docs/requirements/control-requirements.md#data-002), [docs/requirements/control-requirements.md#ops-001](../../docs/requirements/control-requirements.md#ops-001), [docs/requirements/control-requirements.md#ops-002](../../docs/requirements/control-requirements.md#ops-002)

## CAP-WMS — Warehouse operations

- Requirements: [docs/requirements/modules/wms.md#fr-wms-001](../../docs/requirements/modules/wms.md#fr-wms-001), [docs/requirements/modules/wms.md#fr-wms-002](../../docs/requirements/modules/wms.md#fr-wms-002), [docs/requirements/modules/wms.md#fr-wms-003](../../docs/requirements/modules/wms.md#fr-wms-003)
- Design/control: [engines/wms_owner_stock.go](../../engines/wms_owner_stock.go), [engines/wms_single_owner.go](../../engines/wms_single_owner.go)
- Automated tests: [engines/stage47_a05_owner_allocation_redteam_test.go](../../engines/stage47_a05_owner_allocation_redteam_test.go), [engines/wms_single_owner_test.go](../../engines/wms_single_owner_test.go)
- User help: [docs/kb/module-handbooks/inventory-wms-operations.md](../../docs/kb/module-handbooks/inventory-wms-operations.md)
- Work items: 47.5, 47.6, 42
- Verification/review: 2026-09-10 / 2026-10-09
- Signed release evidence: pending

- Business need: [docs/requirements/business-requirements.md#br-001](../../docs/requirements/business-requirements.md#br-001), [docs/requirements/business-requirements.md#br-002](../../docs/requirements/business-requirements.md#br-002), [docs/requirements/business-requirements.md#br-003](../../docs/requirements/business-requirements.md#br-003), [docs/requirements/business-requirements.md#br-004](../../docs/requirements/business-requirements.md#br-004), [docs/requirements/business-requirements.md#br-005](../../docs/requirements/business-requirements.md#br-005)
- Personas: [docs/requirements/personas-and-processes.md#per-wms](../../docs/requirements/personas-and-processes.md#per-wms)
- Processes: [docs/requirements/personas-and-processes.md#proc-r2s](../../docs/requirements/personas-and-processes.md#proc-r2s)
- Security/data/operations/legal control: [docs/requirements/control-requirements.md#sec-001](../../docs/requirements/control-requirements.md#sec-001), [docs/requirements/control-requirements.md#sec-002](../../docs/requirements/control-requirements.md#sec-002), [docs/requirements/control-requirements.md#sec-003](../../docs/requirements/control-requirements.md#sec-003), [docs/requirements/control-requirements.md#data-001](../../docs/requirements/control-requirements.md#data-001), [docs/requirements/control-requirements.md#data-002](../../docs/requirements/control-requirements.md#data-002), [docs/requirements/control-requirements.md#ops-001](../../docs/requirements/control-requirements.md#ops-001), [docs/requirements/control-requirements.md#ops-002](../../docs/requirements/control-requirements.md#ops-002)

## CAP-OMS — Order management

- Requirements: [docs/requirements/modules/oms.md#fr-oms-001](../../docs/requirements/modules/oms.md#fr-oms-001), [docs/requirements/modules/oms.md#fr-oms-002](../../docs/requirements/modules/oms.md#fr-oms-002), [docs/requirements/modules/oms.md#fr-oms-003](../../docs/requirements/modules/oms.md#fr-oms-003)
- Design/control: [engines/fulfillment.go](../../engines/fulfillment.go)
- Automated tests: [engines/oms_console_stage35_test.go](../../engines/oms_console_stage35_test.go)
- User help: [docs/kb/module-handbooks/oms-order-management.md](../../docs/kb/module-handbooks/oms-order-management.md)
- Work items: 35, 47.5
- Verification/review: 2026-09-09 / 2026-10-09
- Signed release evidence: pending

- Business need: [docs/requirements/business-requirements.md#br-001](../../docs/requirements/business-requirements.md#br-001), [docs/requirements/business-requirements.md#br-002](../../docs/requirements/business-requirements.md#br-002), [docs/requirements/business-requirements.md#br-003](../../docs/requirements/business-requirements.md#br-003), [docs/requirements/business-requirements.md#br-004](../../docs/requirements/business-requirements.md#br-004), [docs/requirements/business-requirements.md#br-005](../../docs/requirements/business-requirements.md#br-005)
- Personas: [docs/requirements/personas-and-processes.md#per-data](../../docs/requirements/personas-and-processes.md#per-data)
- Processes: [docs/requirements/personas-and-processes.md#proc-o2c](../../docs/requirements/personas-and-processes.md#proc-o2c)
- Security/data/operations/legal control: [docs/requirements/control-requirements.md#sec-001](../../docs/requirements/control-requirements.md#sec-001), [docs/requirements/control-requirements.md#sec-002](../../docs/requirements/control-requirements.md#sec-002), [docs/requirements/control-requirements.md#sec-003](../../docs/requirements/control-requirements.md#sec-003), [docs/requirements/control-requirements.md#data-001](../../docs/requirements/control-requirements.md#data-001), [docs/requirements/control-requirements.md#data-002](../../docs/requirements/control-requirements.md#data-002), [docs/requirements/control-requirements.md#ops-001](../../docs/requirements/control-requirements.md#ops-001), [docs/requirements/control-requirements.md#ops-002](../../docs/requirements/control-requirements.md#ops-002)

## CAP-PIM — Product and master data

- Requirements: [docs/requirements/modules/pim.md#fr-pim-001](../../docs/requirements/modules/pim.md#fr-pim-001), [docs/requirements/modules/pim.md#fr-pim-002](../../docs/requirements/modules/pim.md#fr-pim-002), [docs/requirements/modules/pim.md#fr-pim-003](../../docs/requirements/modules/pim.md#fr-pim-003)
- Design/control: [engines/pim_tasks.go](../../engines/pim_tasks.go)
- Automated tests: [engines/pim_tasks_test.go](../../engines/pim_tasks_test.go), [engines/pim_import_test.go](../../engines/pim_import_test.go)
- User help: [docs/kb/module-handbooks/pim-pxm.md](../../docs/kb/module-handbooks/pim-pxm.md)
- Work items: 36, 47.13
- Verification/review: 2026-09-09 / 2026-10-09
- Signed release evidence: pending

- Business need: [docs/requirements/business-requirements.md#br-001](../../docs/requirements/business-requirements.md#br-001), [docs/requirements/business-requirements.md#br-002](../../docs/requirements/business-requirements.md#br-002), [docs/requirements/business-requirements.md#br-003](../../docs/requirements/business-requirements.md#br-003), [docs/requirements/business-requirements.md#br-004](../../docs/requirements/business-requirements.md#br-004), [docs/requirements/business-requirements.md#br-005](../../docs/requirements/business-requirements.md#br-005)
- Personas: [docs/requirements/personas-and-processes.md#per-data](../../docs/requirements/personas-and-processes.md#per-data)
- Processes: [docs/requirements/personas-and-processes.md#proc-mdm](../../docs/requirements/personas-and-processes.md#proc-mdm)
- Security/data/operations/legal control: [docs/requirements/control-requirements.md#sec-001](../../docs/requirements/control-requirements.md#sec-001), [docs/requirements/control-requirements.md#sec-002](../../docs/requirements/control-requirements.md#sec-002), [docs/requirements/control-requirements.md#sec-003](../../docs/requirements/control-requirements.md#sec-003), [docs/requirements/control-requirements.md#data-001](../../docs/requirements/control-requirements.md#data-001), [docs/requirements/control-requirements.md#data-002](../../docs/requirements/control-requirements.md#data-002), [docs/requirements/control-requirements.md#ops-001](../../docs/requirements/control-requirements.md#ops-001), [docs/requirements/control-requirements.md#ops-002](../../docs/requirements/control-requirements.md#ops-002)

## CAP-BUY — Procurement

- Requirements: [docs/requirements/modules/procurement.md#fr-buy-001](../../docs/requirements/modules/procurement.md#fr-buy-001), [docs/requirements/modules/procurement.md#fr-buy-002](../../docs/requirements/modules/procurement.md#fr-buy-002), [docs/requirements/modules/procurement.md#fr-buy-003](../../docs/requirements/modules/procurement.md#fr-buy-003)
- Design/control: [engines/rfq.go](../../engines/rfq.go)
- Automated tests: [engines/engines_test.go](../../engines/engines_test.go)
- User help: [docs/kb/module-handbooks/procurement.md](../../docs/kb/module-handbooks/procurement.md)
- Work items: 17, 47.13
- Verification/review: 2026-09-09 / 2026-10-09
- Signed release evidence: pending

- Business need: [docs/requirements/business-requirements.md#br-001](../../docs/requirements/business-requirements.md#br-001), [docs/requirements/business-requirements.md#br-002](../../docs/requirements/business-requirements.md#br-002), [docs/requirements/business-requirements.md#br-003](../../docs/requirements/business-requirements.md#br-003), [docs/requirements/business-requirements.md#br-004](../../docs/requirements/business-requirements.md#br-004), [docs/requirements/business-requirements.md#br-005](../../docs/requirements/business-requirements.md#br-005)
- Personas: [docs/requirements/personas-and-processes.md#per-fin](../../docs/requirements/personas-and-processes.md#per-fin)
- Processes: [docs/requirements/personas-and-processes.md#proc-p2p](../../docs/requirements/personas-and-processes.md#proc-p2p)
- Security/data/operations/legal control: [docs/requirements/control-requirements.md#sec-001](../../docs/requirements/control-requirements.md#sec-001), [docs/requirements/control-requirements.md#sec-002](../../docs/requirements/control-requirements.md#sec-002), [docs/requirements/control-requirements.md#sec-003](../../docs/requirements/control-requirements.md#sec-003), [docs/requirements/control-requirements.md#data-001](../../docs/requirements/control-requirements.md#data-001), [docs/requirements/control-requirements.md#data-002](../../docs/requirements/control-requirements.md#data-002), [docs/requirements/control-requirements.md#ops-001](../../docs/requirements/control-requirements.md#ops-001), [docs/requirements/control-requirements.md#ops-002](../../docs/requirements/control-requirements.md#ops-002)

## CAP-INV — Inventory and transfers

- Requirements: [docs/requirements/modules/inventory.md#fr-inv-001](../../docs/requirements/modules/inventory.md#fr-inv-001), [docs/requirements/modules/inventory.md#fr-inv-002](../../docs/requirements/modules/inventory.md#fr-inv-002), [docs/requirements/modules/inventory.md#fr-inv-003](../../docs/requirements/modules/inventory.md#fr-inv-003)
- Design/control: [engines/inventory.go](../../engines/inventory.go)
- Automated tests: [engines/traceability_test.go](../../engines/traceability_test.go)
- User help: [docs/kb/module-handbooks/traceability-batch-serial.md](../../docs/kb/module-handbooks/traceability-batch-serial.md)
- Work items: 42, 47.5
- Verification/review: 2026-09-09 / 2026-10-09
- Signed release evidence: pending

- Business need: [docs/requirements/business-requirements.md#br-001](../../docs/requirements/business-requirements.md#br-001), [docs/requirements/business-requirements.md#br-002](../../docs/requirements/business-requirements.md#br-002), [docs/requirements/business-requirements.md#br-003](../../docs/requirements/business-requirements.md#br-003), [docs/requirements/business-requirements.md#br-004](../../docs/requirements/business-requirements.md#br-004), [docs/requirements/business-requirements.md#br-005](../../docs/requirements/business-requirements.md#br-005)
- Personas: [docs/requirements/personas-and-processes.md#per-wms](../../docs/requirements/personas-and-processes.md#per-wms)
- Processes: [docs/requirements/personas-and-processes.md#proc-r2s](../../docs/requirements/personas-and-processes.md#proc-r2s)
- Security/data/operations/legal control: [docs/requirements/control-requirements.md#sec-001](../../docs/requirements/control-requirements.md#sec-001), [docs/requirements/control-requirements.md#sec-002](../../docs/requirements/control-requirements.md#sec-002), [docs/requirements/control-requirements.md#sec-003](../../docs/requirements/control-requirements.md#sec-003), [docs/requirements/control-requirements.md#data-001](../../docs/requirements/control-requirements.md#data-001), [docs/requirements/control-requirements.md#data-002](../../docs/requirements/control-requirements.md#data-002), [docs/requirements/control-requirements.md#ops-001](../../docs/requirements/control-requirements.md#ops-001), [docs/requirements/control-requirements.md#ops-002](../../docs/requirements/control-requirements.md#ops-002)

## CAP-FIN — Finance and tax

- Requirements: [docs/requirements/modules/finance.md#fr-fin-001](../../docs/requirements/modules/finance.md#fr-fin-001), [docs/requirements/modules/finance.md#fr-fin-002](../../docs/requirements/modules/finance.md#fr-fin-002), [docs/requirements/modules/finance.md#fr-fin-003](../../docs/requirements/modules/finance.md#fr-fin-003)
- Design/control: [engines/finance.go](../../engines/finance.go)
- Automated tests: [engines/finance_statement_builder_test.go](../../engines/finance_statement_builder_test.go)
- User help: [docs/kb/module-handbooks/finance-tax.md](../../docs/kb/module-handbooks/finance-tax.md)
- Work items: 37, 47.7, 47.16
- Verification/review: 2026-09-09 / 2026-10-09
- Signed release evidence: pending

- Business need: [docs/requirements/business-requirements.md#br-001](../../docs/requirements/business-requirements.md#br-001), [docs/requirements/business-requirements.md#br-002](../../docs/requirements/business-requirements.md#br-002), [docs/requirements/business-requirements.md#br-003](../../docs/requirements/business-requirements.md#br-003), [docs/requirements/business-requirements.md#br-004](../../docs/requirements/business-requirements.md#br-004), [docs/requirements/business-requirements.md#br-005](../../docs/requirements/business-requirements.md#br-005)
- Personas: [docs/requirements/personas-and-processes.md#per-fin](../../docs/requirements/personas-and-processes.md#per-fin)
- Processes: [docs/requirements/personas-and-processes.md#proc-r2r](../../docs/requirements/personas-and-processes.md#proc-r2r)
- Security/data/operations/legal control: [docs/requirements/control-requirements.md#sec-001](../../docs/requirements/control-requirements.md#sec-001), [docs/requirements/control-requirements.md#sec-002](../../docs/requirements/control-requirements.md#sec-002), [docs/requirements/control-requirements.md#sec-003](../../docs/requirements/control-requirements.md#sec-003), [docs/requirements/control-requirements.md#data-001](../../docs/requirements/control-requirements.md#data-001), [docs/requirements/control-requirements.md#data-002](../../docs/requirements/control-requirements.md#data-002), [docs/requirements/control-requirements.md#ops-001](../../docs/requirements/control-requirements.md#ops-001), [docs/requirements/control-requirements.md#ops-002](../../docs/requirements/control-requirements.md#ops-002)

## CAP-CRM — Customer relationship and loyalty

- Requirements: [docs/requirements/modules/crm.md#fr-crm-001](../../docs/requirements/modules/crm.md#fr-crm-001), [docs/requirements/modules/crm.md#fr-crm-002](../../docs/requirements/modules/crm.md#fr-crm-002), [docs/requirements/modules/crm.md#fr-crm-003](../../docs/requirements/modules/crm.md#fr-crm-003)
- Design/control: [engines/loyalty.go](../../engines/loyalty.go)
- Automated tests: [engines/crm_analytics_test.go](../../engines/crm_analytics_test.go)
- User help: [docs/kb/module-handbooks/crm-loyalty.md](../../docs/kb/module-handbooks/crm-loyalty.md)
- Work items: 26.7, 47.3
- Verification/review: 2026-09-09 / 2026-10-09
- Signed release evidence: pending

- Business need: [docs/requirements/business-requirements.md#br-001](../../docs/requirements/business-requirements.md#br-001), [docs/requirements/business-requirements.md#br-002](../../docs/requirements/business-requirements.md#br-002), [docs/requirements/business-requirements.md#br-003](../../docs/requirements/business-requirements.md#br-003), [docs/requirements/business-requirements.md#br-004](../../docs/requirements/business-requirements.md#br-004), [docs/requirements/business-requirements.md#br-005](../../docs/requirements/business-requirements.md#br-005)
- Personas: [docs/requirements/personas-and-processes.md#per-ceo](../../docs/requirements/personas-and-processes.md#per-ceo)
- Processes: [docs/requirements/personas-and-processes.md#proc-o2c](../../docs/requirements/personas-and-processes.md#proc-o2c)
- Security/data/operations/legal control: [docs/requirements/control-requirements.md#sec-001](../../docs/requirements/control-requirements.md#sec-001), [docs/requirements/control-requirements.md#sec-002](../../docs/requirements/control-requirements.md#sec-002), [docs/requirements/control-requirements.md#sec-003](../../docs/requirements/control-requirements.md#sec-003), [docs/requirements/control-requirements.md#data-001](../../docs/requirements/control-requirements.md#data-001), [docs/requirements/control-requirements.md#data-002](../../docs/requirements/control-requirements.md#data-002), [docs/requirements/control-requirements.md#ops-001](../../docs/requirements/control-requirements.md#ops-001), [docs/requirements/control-requirements.md#ops-002](../../docs/requirements/control-requirements.md#ops-002)

## CAP-HR — People and payroll

- Requirements: [docs/requirements/modules/hr.md#fr-hr-001](../../docs/requirements/modules/hr.md#fr-hr-001), [docs/requirements/modules/hr.md#fr-hr-002](../../docs/requirements/modules/hr.md#fr-hr-002), [docs/requirements/modules/hr.md#fr-hr-003](../../docs/requirements/modules/hr.md#fr-hr-003)
- Design/control: [engines/hr.go](../../engines/hr.go)
- Automated tests: [engines/sensitive_fields_test.go](../../engines/sensitive_fields_test.go)
- User help: [docs/kb/module-handbooks/hr-payroll.md](../../docs/kb/module-handbooks/hr-payroll.md)
- Work items: 26.8, 47.1, 47.16
- Verification/review: 2026-09-09 / 2026-10-09
- Signed release evidence: pending

- Business need: [docs/requirements/business-requirements.md#br-001](../../docs/requirements/business-requirements.md#br-001), [docs/requirements/business-requirements.md#br-002](../../docs/requirements/business-requirements.md#br-002), [docs/requirements/business-requirements.md#br-003](../../docs/requirements/business-requirements.md#br-003), [docs/requirements/business-requirements.md#br-004](../../docs/requirements/business-requirements.md#br-004), [docs/requirements/business-requirements.md#br-005](../../docs/requirements/business-requirements.md#br-005)
- Personas: [docs/requirements/personas-and-processes.md#per-admin](../../docs/requirements/personas-and-processes.md#per-admin)
- Processes: [docs/requirements/personas-and-processes.md#proc-hr](../../docs/requirements/personas-and-processes.md#proc-hr)
- Security/data/operations/legal control: [docs/requirements/control-requirements.md#sec-001](../../docs/requirements/control-requirements.md#sec-001), [docs/requirements/control-requirements.md#sec-002](../../docs/requirements/control-requirements.md#sec-002), [docs/requirements/control-requirements.md#sec-003](../../docs/requirements/control-requirements.md#sec-003), [docs/requirements/control-requirements.md#data-001](../../docs/requirements/control-requirements.md#data-001), [docs/requirements/control-requirements.md#data-002](../../docs/requirements/control-requirements.md#data-002), [docs/requirements/control-requirements.md#ops-001](../../docs/requirements/control-requirements.md#ops-001), [docs/requirements/control-requirements.md#ops-002](../../docs/requirements/control-requirements.md#ops-002)

## CAP-MFG — Manufacturing and planning

- Requirements: [docs/requirements/modules/manufacturing.md#fr-mfg-001](../../docs/requirements/modules/manufacturing.md#fr-mfg-001), [docs/requirements/modules/manufacturing.md#fr-mfg-002](../../docs/requirements/modules/manufacturing.md#fr-mfg-002), [docs/requirements/modules/manufacturing.md#fr-mfg-003](../../docs/requirements/modules/manufacturing.md#fr-mfg-003)
- Design/control: [engines/manufacturing.go](../../engines/manufacturing.go)
- Automated tests: [engines/manufacturing_scheduling_test.go](../../engines/manufacturing_scheduling_test.go)
- User help: [docs/kb/module-handbooks/manufacturing-mrp.md](../../docs/kb/module-handbooks/manufacturing-mrp.md)
- Work items: 37.10, 26.9
- Verification/review: 2026-09-09 / 2026-10-09
- Signed release evidence: pending

- Business need: [docs/requirements/business-requirements.md#br-001](../../docs/requirements/business-requirements.md#br-001), [docs/requirements/business-requirements.md#br-002](../../docs/requirements/business-requirements.md#br-002), [docs/requirements/business-requirements.md#br-003](../../docs/requirements/business-requirements.md#br-003), [docs/requirements/business-requirements.md#br-004](../../docs/requirements/business-requirements.md#br-004), [docs/requirements/business-requirements.md#br-005](../../docs/requirements/business-requirements.md#br-005)
- Personas: [docs/requirements/personas-and-processes.md#per-wms](../../docs/requirements/personas-and-processes.md#per-wms)
- Processes: [docs/requirements/personas-and-processes.md#proc-mfg](../../docs/requirements/personas-and-processes.md#proc-mfg)
- Security/data/operations/legal control: [docs/requirements/control-requirements.md#sec-001](../../docs/requirements/control-requirements.md#sec-001), [docs/requirements/control-requirements.md#sec-002](../../docs/requirements/control-requirements.md#sec-002), [docs/requirements/control-requirements.md#sec-003](../../docs/requirements/control-requirements.md#sec-003), [docs/requirements/control-requirements.md#data-001](../../docs/requirements/control-requirements.md#data-001), [docs/requirements/control-requirements.md#data-002](../../docs/requirements/control-requirements.md#data-002), [docs/requirements/control-requirements.md#ops-001](../../docs/requirements/control-requirements.md#ops-001), [docs/requirements/control-requirements.md#ops-002](../../docs/requirements/control-requirements.md#ops-002)

## CAP-SRV — Projects and service

- Requirements: [docs/requirements/modules/service.md#fr-srv-001](../../docs/requirements/modules/service.md#fr-srv-001), [docs/requirements/modules/service.md#fr-srv-002](../../docs/requirements/modules/service.md#fr-srv-002), [docs/requirements/modules/service.md#fr-srv-003](../../docs/requirements/modules/service.md#fr-srv-003)
- Design/control: [engines/project.go](../../engines/project.go), [engines/service_management.go](../../engines/service_management.go)
- Automated tests: [engines/project_test.go](../../engines/project_test.go), [engines/service_management_test.go](../../engines/service_management_test.go)
- User help: [docs/kb/reference/report-catalog.md](../../docs/kb/reference/report-catalog.md)
- Work items: 37.7, 37.8
- Verification/review: 2026-09-09 / 2026-10-09
- Signed release evidence: pending

- Business need: [docs/requirements/business-requirements.md#br-001](../../docs/requirements/business-requirements.md#br-001), [docs/requirements/business-requirements.md#br-002](../../docs/requirements/business-requirements.md#br-002), [docs/requirements/business-requirements.md#br-003](../../docs/requirements/business-requirements.md#br-003), [docs/requirements/business-requirements.md#br-004](../../docs/requirements/business-requirements.md#br-004), [docs/requirements/business-requirements.md#br-005](../../docs/requirements/business-requirements.md#br-005)
- Personas: [docs/requirements/personas-and-processes.md#per-ceo](../../docs/requirements/personas-and-processes.md#per-ceo)
- Processes: [docs/requirements/personas-and-processes.md#proc-srv](../../docs/requirements/personas-and-processes.md#proc-srv)
- Security/data/operations/legal control: [docs/requirements/control-requirements.md#sec-001](../../docs/requirements/control-requirements.md#sec-001), [docs/requirements/control-requirements.md#sec-002](../../docs/requirements/control-requirements.md#sec-002), [docs/requirements/control-requirements.md#sec-003](../../docs/requirements/control-requirements.md#sec-003), [docs/requirements/control-requirements.md#data-001](../../docs/requirements/control-requirements.md#data-001), [docs/requirements/control-requirements.md#data-002](../../docs/requirements/control-requirements.md#data-002), [docs/requirements/control-requirements.md#ops-001](../../docs/requirements/control-requirements.md#ops-001), [docs/requirements/control-requirements.md#ops-002](../../docs/requirements/control-requirements.md#ops-002)

## CAP-API — Platform and integrations

- Requirements: [docs/requirements/modules/platform.md#fr-api-001](../../docs/requirements/modules/platform.md#fr-api-001), [docs/requirements/modules/platform.md#fr-api-002](../../docs/requirements/modules/platform.md#fr-api-002), [docs/requirements/modules/platform.md#fr-api-003](../../docs/requirements/modules/platform.md#fr-api-003)
- Design/control: [internal/server/routes_public_api_v1.go](../../internal/server/routes_public_api_v1.go)
- Automated tests: [engines/public_api_runtime_test.go](../../engines/public_api_runtime_test.go)
- User help: [docs/kb/reference/public-api-idempotency.md](../../docs/kb/reference/public-api-idempotency.md)
- Work items: 38, 49
- Verification/review: 2026-09-09 / 2026-10-09
- Signed release evidence: pending

- Business need: [docs/requirements/business-requirements.md#br-001](../../docs/requirements/business-requirements.md#br-001), [docs/requirements/business-requirements.md#br-002](../../docs/requirements/business-requirements.md#br-002), [docs/requirements/business-requirements.md#br-003](../../docs/requirements/business-requirements.md#br-003), [docs/requirements/business-requirements.md#br-004](../../docs/requirements/business-requirements.md#br-004), [docs/requirements/business-requirements.md#br-005](../../docs/requirements/business-requirements.md#br-005)
- Personas: [docs/requirements/personas-and-processes.md#per-eng](../../docs/requirements/personas-and-processes.md#per-eng), [docs/requirements/personas-and-processes.md#per-admin](../../docs/requirements/personas-and-processes.md#per-admin), [docs/requirements/personas-and-processes.md#per-sec](../../docs/requirements/personas-and-processes.md#per-sec)
- Processes: [docs/requirements/personas-and-processes.md#proc-mdm](../../docs/requirements/personas-and-processes.md#proc-mdm)
- Security/data/operations/legal control: [docs/requirements/control-requirements.md#sec-001](../../docs/requirements/control-requirements.md#sec-001), [docs/requirements/control-requirements.md#sec-002](../../docs/requirements/control-requirements.md#sec-002), [docs/requirements/control-requirements.md#sec-003](../../docs/requirements/control-requirements.md#sec-003), [docs/requirements/control-requirements.md#data-001](../../docs/requirements/control-requirements.md#data-001), [docs/requirements/control-requirements.md#data-002](../../docs/requirements/control-requirements.md#data-002), [docs/requirements/control-requirements.md#ops-001](../../docs/requirements/control-requirements.md#ops-001), [docs/requirements/control-requirements.md#ops-002](../../docs/requirements/control-requirements.md#ops-002)

## CAP-KB — Knowledge Center

- Requirements: [docs/requirements/modules/knowledge.md#fr-kb-001](../../docs/requirements/modules/knowledge.md#fr-kb-001), [docs/requirements/modules/knowledge.md#fr-kb-002](../../docs/requirements/modules/knowledge.md#fr-kb-002), [docs/requirements/modules/knowledge.md#fr-kb-003](../../docs/requirements/modules/knowledge.md#fr-kb-003)
- Design/control: [internal/kb/build.go](../../internal/kb/build.go)
- Automated tests: [internal/kb/build_test.go](../../internal/kb/build_test.go)
- User help: [docs/kb/getting-started/finding-your-way-around.md](../../docs/kb/getting-started/finding-your-way-around.md)
- Work items: 39, 48.7
- Verification/review: 2026-09-09 / 2026-10-09
- Signed release evidence: pending

- Business need: [docs/requirements/business-requirements.md#br-001](../../docs/requirements/business-requirements.md#br-001), [docs/requirements/business-requirements.md#br-002](../../docs/requirements/business-requirements.md#br-002), [docs/requirements/business-requirements.md#br-003](../../docs/requirements/business-requirements.md#br-003), [docs/requirements/business-requirements.md#br-004](../../docs/requirements/business-requirements.md#br-004), [docs/requirements/business-requirements.md#br-005](../../docs/requirements/business-requirements.md#br-005)
- Personas: [docs/requirements/personas-and-processes.md#per-ops](../../docs/requirements/personas-and-processes.md#per-ops)
- Processes: [docs/requirements/personas-and-processes.md#proc-mdm](../../docs/requirements/personas-and-processes.md#proc-mdm)
- Security/data/operations/legal control: [docs/requirements/control-requirements.md#sec-001](../../docs/requirements/control-requirements.md#sec-001), [docs/requirements/control-requirements.md#sec-002](../../docs/requirements/control-requirements.md#sec-002), [docs/requirements/control-requirements.md#sec-003](../../docs/requirements/control-requirements.md#sec-003), [docs/requirements/control-requirements.md#data-001](../../docs/requirements/control-requirements.md#data-001), [docs/requirements/control-requirements.md#data-002](../../docs/requirements/control-requirements.md#data-002), [docs/requirements/control-requirements.md#ops-001](../../docs/requirements/control-requirements.md#ops-001), [docs/requirements/control-requirements.md#ops-002](../../docs/requirements/control-requirements.md#ops-002)

## Reverse lookup

| Requirement / need / process / persona | Capabilities |
|---|---|
| [docs/requirements/business-requirements.md#br-001](../../docs/requirements/business-requirements.md#br-001) | CAP-POS, CAP-RET, CAP-WMS, CAP-OMS, CAP-PIM, CAP-BUY, CAP-INV, CAP-FIN, CAP-CRM, CAP-HR, CAP-MFG, CAP-SRV, CAP-API, CAP-KB |
| [docs/requirements/business-requirements.md#br-002](../../docs/requirements/business-requirements.md#br-002) | CAP-POS, CAP-RET, CAP-WMS, CAP-OMS, CAP-PIM, CAP-BUY, CAP-INV, CAP-FIN, CAP-CRM, CAP-HR, CAP-MFG, CAP-SRV, CAP-API, CAP-KB |
| [docs/requirements/business-requirements.md#br-003](../../docs/requirements/business-requirements.md#br-003) | CAP-POS, CAP-RET, CAP-WMS, CAP-OMS, CAP-PIM, CAP-BUY, CAP-INV, CAP-FIN, CAP-CRM, CAP-HR, CAP-MFG, CAP-SRV, CAP-API, CAP-KB |
| [docs/requirements/business-requirements.md#br-004](../../docs/requirements/business-requirements.md#br-004) | CAP-POS, CAP-RET, CAP-WMS, CAP-OMS, CAP-PIM, CAP-BUY, CAP-INV, CAP-FIN, CAP-CRM, CAP-HR, CAP-MFG, CAP-SRV, CAP-API, CAP-KB |
| [docs/requirements/business-requirements.md#br-005](../../docs/requirements/business-requirements.md#br-005) | CAP-POS, CAP-RET, CAP-WMS, CAP-OMS, CAP-PIM, CAP-BUY, CAP-INV, CAP-FIN, CAP-CRM, CAP-HR, CAP-MFG, CAP-SRV, CAP-API, CAP-KB |
| [docs/requirements/control-requirements.md#data-001](../../docs/requirements/control-requirements.md#data-001) | CAP-POS, CAP-RET, CAP-WMS, CAP-OMS, CAP-PIM, CAP-BUY, CAP-INV, CAP-FIN, CAP-CRM, CAP-HR, CAP-MFG, CAP-SRV, CAP-API, CAP-KB |
| [docs/requirements/control-requirements.md#data-002](../../docs/requirements/control-requirements.md#data-002) | CAP-POS, CAP-RET, CAP-WMS, CAP-OMS, CAP-PIM, CAP-BUY, CAP-INV, CAP-FIN, CAP-CRM, CAP-HR, CAP-MFG, CAP-SRV, CAP-API, CAP-KB |
| [docs/requirements/control-requirements.md#ops-001](../../docs/requirements/control-requirements.md#ops-001) | CAP-POS, CAP-RET, CAP-WMS, CAP-OMS, CAP-PIM, CAP-BUY, CAP-INV, CAP-FIN, CAP-CRM, CAP-HR, CAP-MFG, CAP-SRV, CAP-API, CAP-KB |
| [docs/requirements/control-requirements.md#ops-002](../../docs/requirements/control-requirements.md#ops-002) | CAP-POS, CAP-RET, CAP-WMS, CAP-OMS, CAP-PIM, CAP-BUY, CAP-INV, CAP-FIN, CAP-CRM, CAP-HR, CAP-MFG, CAP-SRV, CAP-API, CAP-KB |
| [docs/requirements/control-requirements.md#sec-001](../../docs/requirements/control-requirements.md#sec-001) | CAP-POS, CAP-RET, CAP-WMS, CAP-OMS, CAP-PIM, CAP-BUY, CAP-INV, CAP-FIN, CAP-CRM, CAP-HR, CAP-MFG, CAP-SRV, CAP-API, CAP-KB |
| [docs/requirements/control-requirements.md#sec-002](../../docs/requirements/control-requirements.md#sec-002) | CAP-POS, CAP-RET, CAP-WMS, CAP-OMS, CAP-PIM, CAP-BUY, CAP-INV, CAP-FIN, CAP-CRM, CAP-HR, CAP-MFG, CAP-SRV, CAP-API, CAP-KB |
| [docs/requirements/control-requirements.md#sec-003](../../docs/requirements/control-requirements.md#sec-003) | CAP-POS, CAP-RET, CAP-WMS, CAP-OMS, CAP-PIM, CAP-BUY, CAP-INV, CAP-FIN, CAP-CRM, CAP-HR, CAP-MFG, CAP-SRV, CAP-API, CAP-KB |
| [docs/requirements/modules/crm.md#fr-crm-001](../../docs/requirements/modules/crm.md#fr-crm-001) | CAP-CRM |
| [docs/requirements/modules/crm.md#fr-crm-002](../../docs/requirements/modules/crm.md#fr-crm-002) | CAP-CRM |
| [docs/requirements/modules/crm.md#fr-crm-003](../../docs/requirements/modules/crm.md#fr-crm-003) | CAP-CRM |
| [docs/requirements/modules/finance.md#fr-fin-001](../../docs/requirements/modules/finance.md#fr-fin-001) | CAP-FIN |
| [docs/requirements/modules/finance.md#fr-fin-002](../../docs/requirements/modules/finance.md#fr-fin-002) | CAP-FIN |
| [docs/requirements/modules/finance.md#fr-fin-003](../../docs/requirements/modules/finance.md#fr-fin-003) | CAP-FIN |
| [docs/requirements/modules/hr.md#fr-hr-001](../../docs/requirements/modules/hr.md#fr-hr-001) | CAP-HR |
| [docs/requirements/modules/hr.md#fr-hr-002](../../docs/requirements/modules/hr.md#fr-hr-002) | CAP-HR |
| [docs/requirements/modules/hr.md#fr-hr-003](../../docs/requirements/modules/hr.md#fr-hr-003) | CAP-HR |
| [docs/requirements/modules/inventory.md#fr-inv-001](../../docs/requirements/modules/inventory.md#fr-inv-001) | CAP-INV |
| [docs/requirements/modules/inventory.md#fr-inv-002](../../docs/requirements/modules/inventory.md#fr-inv-002) | CAP-INV |
| [docs/requirements/modules/inventory.md#fr-inv-003](../../docs/requirements/modules/inventory.md#fr-inv-003) | CAP-INV |
| [docs/requirements/modules/knowledge.md#fr-kb-001](../../docs/requirements/modules/knowledge.md#fr-kb-001) | CAP-KB |
| [docs/requirements/modules/knowledge.md#fr-kb-002](../../docs/requirements/modules/knowledge.md#fr-kb-002) | CAP-KB |
| [docs/requirements/modules/knowledge.md#fr-kb-003](../../docs/requirements/modules/knowledge.md#fr-kb-003) | CAP-KB |
| [docs/requirements/modules/manufacturing.md#fr-mfg-001](../../docs/requirements/modules/manufacturing.md#fr-mfg-001) | CAP-MFG |
| [docs/requirements/modules/manufacturing.md#fr-mfg-002](../../docs/requirements/modules/manufacturing.md#fr-mfg-002) | CAP-MFG |
| [docs/requirements/modules/manufacturing.md#fr-mfg-003](../../docs/requirements/modules/manufacturing.md#fr-mfg-003) | CAP-MFG |
| [docs/requirements/modules/oms.md#fr-oms-001](../../docs/requirements/modules/oms.md#fr-oms-001) | CAP-OMS |
| [docs/requirements/modules/oms.md#fr-oms-002](../../docs/requirements/modules/oms.md#fr-oms-002) | CAP-OMS |
| [docs/requirements/modules/oms.md#fr-oms-003](../../docs/requirements/modules/oms.md#fr-oms-003) | CAP-OMS |
| [docs/requirements/modules/pim.md#fr-pim-001](../../docs/requirements/modules/pim.md#fr-pim-001) | CAP-PIM |
| [docs/requirements/modules/pim.md#fr-pim-002](../../docs/requirements/modules/pim.md#fr-pim-002) | CAP-PIM |
| [docs/requirements/modules/pim.md#fr-pim-003](../../docs/requirements/modules/pim.md#fr-pim-003) | CAP-PIM |
| [docs/requirements/modules/platform.md#fr-api-001](../../docs/requirements/modules/platform.md#fr-api-001) | CAP-API |
| [docs/requirements/modules/platform.md#fr-api-002](../../docs/requirements/modules/platform.md#fr-api-002) | CAP-API |
| [docs/requirements/modules/platform.md#fr-api-003](../../docs/requirements/modules/platform.md#fr-api-003) | CAP-API |
| [docs/requirements/modules/pos.md#fr-pos-001](../../docs/requirements/modules/pos.md#fr-pos-001) | CAP-POS |
| [docs/requirements/modules/pos.md#fr-pos-002](../../docs/requirements/modules/pos.md#fr-pos-002) | CAP-POS |
| [docs/requirements/modules/pos.md#fr-pos-003](../../docs/requirements/modules/pos.md#fr-pos-003) | CAP-POS |
| [docs/requirements/modules/procurement.md#fr-buy-001](../../docs/requirements/modules/procurement.md#fr-buy-001) | CAP-BUY |
| [docs/requirements/modules/procurement.md#fr-buy-002](../../docs/requirements/modules/procurement.md#fr-buy-002) | CAP-BUY |
| [docs/requirements/modules/procurement.md#fr-buy-003](../../docs/requirements/modules/procurement.md#fr-buy-003) | CAP-BUY |
| [docs/requirements/modules/returns.md#fr-ret-001](../../docs/requirements/modules/returns.md#fr-ret-001) | CAP-RET |
| [docs/requirements/modules/returns.md#fr-ret-002](../../docs/requirements/modules/returns.md#fr-ret-002) | CAP-RET |
| [docs/requirements/modules/returns.md#fr-ret-003](../../docs/requirements/modules/returns.md#fr-ret-003) | CAP-RET |
| [docs/requirements/modules/service.md#fr-srv-001](../../docs/requirements/modules/service.md#fr-srv-001) | CAP-SRV |
| [docs/requirements/modules/service.md#fr-srv-002](../../docs/requirements/modules/service.md#fr-srv-002) | CAP-SRV |
| [docs/requirements/modules/service.md#fr-srv-003](../../docs/requirements/modules/service.md#fr-srv-003) | CAP-SRV |
| [docs/requirements/modules/wms.md#fr-wms-001](../../docs/requirements/modules/wms.md#fr-wms-001) | CAP-WMS |
| [docs/requirements/modules/wms.md#fr-wms-002](../../docs/requirements/modules/wms.md#fr-wms-002) | CAP-WMS |
| [docs/requirements/modules/wms.md#fr-wms-003](../../docs/requirements/modules/wms.md#fr-wms-003) | CAP-WMS |
| [docs/requirements/nonfunctional-requirements.md](../../docs/requirements/nonfunctional-requirements.md) | CAP-POS, CAP-RET, CAP-WMS, CAP-OMS, CAP-PIM, CAP-BUY, CAP-INV, CAP-FIN, CAP-CRM, CAP-HR, CAP-MFG, CAP-SRV, CAP-API, CAP-KB |
| [docs/requirements/personas-and-processes.md#per-admin](../../docs/requirements/personas-and-processes.md#per-admin) | CAP-HR, CAP-API |
| [docs/requirements/personas-and-processes.md#per-cashier](../../docs/requirements/personas-and-processes.md#per-cashier) | CAP-POS, CAP-RET |
| [docs/requirements/personas-and-processes.md#per-ceo](../../docs/requirements/personas-and-processes.md#per-ceo) | CAP-CRM, CAP-SRV |
| [docs/requirements/personas-and-processes.md#per-data](../../docs/requirements/personas-and-processes.md#per-data) | CAP-OMS, CAP-PIM |
| [docs/requirements/personas-and-processes.md#per-eng](../../docs/requirements/personas-and-processes.md#per-eng) | CAP-API |
| [docs/requirements/personas-and-processes.md#per-fin](../../docs/requirements/personas-and-processes.md#per-fin) | CAP-BUY, CAP-FIN |
| [docs/requirements/personas-and-processes.md#per-ops](../../docs/requirements/personas-and-processes.md#per-ops) | CAP-KB |
| [docs/requirements/personas-and-processes.md#per-sec](../../docs/requirements/personas-and-processes.md#per-sec) | CAP-API |
| [docs/requirements/personas-and-processes.md#per-wms](../../docs/requirements/personas-and-processes.md#per-wms) | CAP-WMS, CAP-INV, CAP-MFG |
| [docs/requirements/personas-and-processes.md#proc-hr](../../docs/requirements/personas-and-processes.md#proc-hr) | CAP-HR |
| [docs/requirements/personas-and-processes.md#proc-mdm](../../docs/requirements/personas-and-processes.md#proc-mdm) | CAP-PIM, CAP-API, CAP-KB |
| [docs/requirements/personas-and-processes.md#proc-mfg](../../docs/requirements/personas-and-processes.md#proc-mfg) | CAP-MFG |
| [docs/requirements/personas-and-processes.md#proc-o2c](../../docs/requirements/personas-and-processes.md#proc-o2c) | CAP-POS, CAP-OMS, CAP-CRM |
| [docs/requirements/personas-and-processes.md#proc-p2p](../../docs/requirements/personas-and-processes.md#proc-p2p) | CAP-BUY |
| [docs/requirements/personas-and-processes.md#proc-r2r](../../docs/requirements/personas-and-processes.md#proc-r2r) | CAP-FIN |
| [docs/requirements/personas-and-processes.md#proc-r2s](../../docs/requirements/personas-and-processes.md#proc-r2s) | CAP-WMS, CAP-INV |
| [docs/requirements/personas-and-processes.md#proc-ret](../../docs/requirements/personas-and-processes.md#proc-ret) | CAP-RET |
| [docs/requirements/personas-and-processes.md#proc-srv](../../docs/requirements/personas-and-processes.md#proc-srv) | CAP-SRV |
