<!-- GENERATED from docs/product/capability-register.json by cmd/doclint. DO NOT EDIT. -->

# Capability catalog

Source status: **draft**. Owner: **product-owner**. Version label: **0.1.0 development tree; not released support**.

Maturity is scoped to the configurations below. Source and test-file links are implementation evidence, not proof that a deployed release passed. No Production or Certified claim is authorized without the signed evidence and approver required by the registry validator.

## Reference configurations

| ID | Industry / country | Deployment / devices | Owner model | Limit |
|---|---|---|---|---|
| REF-RETAIL-IN | single/multi-store retail / India | one Go service + PostgreSQL; tenant schemas / desktop browser; mobile/RF not certified | retailer-owned stock | Preview evaluation; tenant-role migration, deployment controls and release acceptance remain required. |
| REF-WAREHOUSE-IN | wholesale distribution with dedicated-owner warehouses / India | one Go service + PostgreSQL; tenant schemas / browser/RF candidate; physical verification pending | one stock owner per warehouse; Stage 47.5 single_owner guard | Preview evaluation of the selected single-owner configuration; deployment and release acceptance required. mixed_unsupported mode provides no allocation/picking isolation and is outside supported use. |

## Capability status

Module keys link this product register to the executable module catalog. The registry schema version and release label identify the mapping version; source checks derive DocTypes, role vocabulary, routes, screens, workers, reports, exports and core dependencies from their existing declarations.

| Capability | Module keys | Maturity / configuration | Owner | Limits / remaining gates |
|---|---|---|---|---|
| CAP-POS — Point of sale | sales | Preview / REF-RETAIL-IN, REF-WAREHOUSE-IN | store-process-owner | Source/test locations available; no approved deployment-specific release/UAT evidence attached. Stage gates remain authoritative. |
| CAP-RET — Returns and refunds | sales, oms | Preview / REF-RETAIL-IN, REF-WAREHOUSE-IN | store-process-owner | Source/test locations available; no approved deployment-specific release/UAT evidence attached. Stage gates remain authoritative. |
| CAP-WMS — Warehouse operations | wms, stickers, inventory | Experimental / REF-RETAIL-IN, REF-WAREHOUSE-IN | warehouse-process-owner | Single-owner-per-warehouse guard implemented; mixed-owner operation explicitly unsupported. Physical device, deployment and qualified release acceptance remain open. |
| CAP-OMS — Order management | oms | Preview / REF-RETAIL-IN, REF-WAREHOUSE-IN | order-process-owner | Source/test locations available; no approved deployment-specific release/UAT evidence attached. Stage gates remain authoritative. |
| CAP-PIM — Product and master data | pim, master_data | Preview / REF-RETAIL-IN, REF-WAREHOUSE-IN | data-owner | Source/test locations available; no approved deployment-specific release/UAT evidence attached. Stage gates remain authoritative. |
| CAP-BUY — Procurement | procurement, rfq | Preview / REF-RETAIL-IN, REF-WAREHOUSE-IN | procurement-process-owner | Source/test locations available; no approved deployment-specific release/UAT evidence attached. Stage gates remain authoritative. |
| CAP-INV — Inventory and transfers | inventory | Preview / REF-RETAIL-IN, REF-WAREHOUSE-IN | inventory-process-owner | Source/test locations available; no approved deployment-specific release/UAT evidence attached. Stage gates remain authoritative. |
| CAP-FIN — Finance and tax | finance, assets, expenses | Preview / REF-RETAIL-IN, REF-WAREHOUSE-IN | finance-process-owner | Source/test locations available; no approved deployment-specific release/UAT evidence attached. Stage gates remain authoritative. |
| CAP-CRM — Customer relationship and loyalty | crm_loyalty | Preview / REF-RETAIL-IN, REF-WAREHOUSE-IN | customer-process-owner | Source/test locations available; no approved deployment-specific release/UAT evidence attached. Stage gates remain authoritative. |
| CAP-HR — People and payroll | hr | Preview / REF-RETAIL-IN, REF-WAREHOUSE-IN | hr-process-owner | Source/test locations available; no approved deployment-specific release/UAT evidence attached. Stage gates remain authoritative. |
| CAP-MFG — Manufacturing and planning | manufacturing, quality | Preview / REF-RETAIL-IN, REF-WAREHOUSE-IN | manufacturing-process-owner | Source/test locations available; no approved deployment-specific release/UAT evidence attached. Stage gates remain authoritative. |
| CAP-SRV — Projects and service | service | Experimental / REF-RETAIL-IN, REF-WAREHOUSE-IN | service-process-owner | Source/test locations available; no approved deployment-specific release/UAT evidence attached. Stage gates remain authoritative. |
| CAP-API — Platform and integrations | core, integrations, reports | Preview / REF-RETAIL-IN, REF-WAREHOUSE-IN | engineering-owner | Source/test locations available; no approved deployment-specific release/UAT evidence attached. Stage gates remain authoritative. |
| CAP-KB — Knowledge Center | core | Preview / REF-RETAIL-IN, REF-WAREHOUSE-IN | documentation-maintainer | Source/test locations available; no approved deployment-specific release/UAT evidence attached. Stage gates remain authoritative. |
