# In-House ERP: Multi-Industry Schema & Configuration Specification

> **Status: partially built.** Only 4 industry profiles actually exist today — Jewelry, Food & Beverage, Automobile, Clothing (`public/profiles/*.json`, loaded via `SwitchIndustryProfile` in `engines/doctype.go`). §2.1–2.6 below (Pharma, Metal & Steel, Construction, Medical Devices, Semiconductors, Agriculture) are specification only — no profile file or code path exists for them yet. See `docs/operations/hardening_roadmap.md` for current priorities and `docs/specs/pdf_blueprint_gap_analysis.md` §4 / `docs/micro_checklist.md` Stage 12.1 for tracking.

This document defines the technical design and data structures for the **Dynamic Multi-Industry Configurator**. It explains how a single, lightweight Go ERP Kernel can instantly transform its master data, UI forms, navigation sidebars, workflow rules, and reports based on a user's selected industry (from Jewelry and Pharma to Steel Fabrication and Construction).

---

## 1. The Industry Configurator Flow

Instead of writing separate codebase versions for separate industries, the ERP Kernel uses **JSON Industry Configuration Packages**.

```
[User Selects Industry: e.g. "PHARMA"] ---> [POST /api/v1/admin/industry]
                                                      |
                                                      v
                                        [Go Kernel reads PHARMA.json]
                                                      |
                                                      v
                                     +----------------------------------+
                                     |   Re-Registers Metadata Tables:  |
                                     |   - doctype_meta                 |
                                     |   - doctype_fields               |
                                     |   - workflow_rules               |
                                     |   - report_definitions           |
                                     +----------------------------------+
                                                      |
                                                      v
                                    [Frontend SPA calls /doctype/meta]
                                                      |
                                                      v
                                        [Dynamic Form Render Engine]
                                   - Sidebar displays Pharma submenus
                                   - Forms render FDA, Batch & Expiry
                                   - Tables translate to "Lot Number"
```

---

## 2. Industry-Specific Master Data & Logic Mapping

Below is the specification mapping the custom fields, workflows, validation rules, and reports required when switching between your target industries:

### 2.1 Pharmaceuticals & Biotechnology
- **Nomenclature overrides**: Parent = *Product Recipe*, Child = *Batch Number* or *Lot ID*.
- **Custom Schema fields**: Expiry Date, Manufacturing Date, Batch Purity (%), FDA Approval Number, Storage Condition (Select: Room/Refrigerated/Frozen).
- **Core workflows**: Dual-signature verification (Maker-Checker matching FDA 21 CFR Part 11 compliance) on batch approvals.
- **Validation rules**: Block sales of any batch where current date exceeds Expiry Date.
- **Custom reports**: Batch Traceability Report (tracks a batch from raw materials to customer invoice).

### 2.2 Metal & Steel Fabrication
- **Nomenclature overrides**: Parent = *Material Grade*, Child = *Heat Number*.
- **Custom Schema fields**: Cut Length (mm), Thickness (mm), Base Alloy Weight (kg), Metallurgy Certification Code.
- **Core workflows**: Quality Certificate check on GRN.
- **Validation rules**: Prevent dispatch if a Heat Number has no uploaded Metallurgy Cert.
- **Custom reports**: Yield Recovery Report (tracks raw metal sheets consumed vs. fabricated yield).

### 2.3 Construction & Contracting
- **Nomenclature overrides**: Parent = *Project Phase*, Child = *Subcontractor Work Item*.
- **Custom Schema fields**: Job Code, Estimated Hours, subcontractor ID, Certified Payroll flag, Progress billing threshold.
- **Core workflows**: Progress billing verification (approvals mapped to percentage completion milestones).
- **Validation rules**: Block subcontractor payment if total billing exceeds the Job Cost estimate.
- **Custom reports**: Job-Cost Margin Analysis (budgeted vs. actual labor/material expenditures).

### 2.4 Medical Devices
- **Nomenclature overrides**: Parent = *Device Master Record (DMR)*, Child = *Unique Device Identifier (UDI)*.
- **Custom Schema fields**: UDI Code, Device History Record (DHR) reference, Sterilization Batch, FDA Class (1/2/3).
- **Core workflows**: Sterilization quality sign-off before inventory release.
- **Validation rules**: Block shipment if the UDI barcode is not scanned at dispatch.
- **Custom reports**: UDI Registry and Recall Traceability Report.

### 2.5 Semiconductors
- **Nomenclature overrides**: Parent = *Wafer Template*, Child = *Die Barcode*.
- **Custom Schema fields**: Wafer Batch ID, Clean-room Yield Rate, Silicon Thickness, Purity Grade.
- **Validation rules**: Enforce zero-negative inventory on chemical raw materials. Block waver batch if clean-room yield falls below 95%.
- **Custom reports**: Wafer Purity & Defect Density logs.

### 2.6 Agriculture & Perishable Goods
- **Nomenclature overrides**: Parent = *Crop/Livestock Category*, Child = *Harvest Lot ID*.
- **Custom Schema fields**: Harvest Date, Cold-Chain Temp Limit, Soil Treatment Code, Grade (A/B/C).
- **Validation rules**: Enforce First-Expired-First-Out (FEFO) picking priority.
- **Custom reports**: Yield per Acre analytics and Cold-Chain log metrics.

---

## 3. The Industry Configuration JSON Schema

Each industry profile is defined as a standardized JSON structure stored in the Go Kernel's `/assets/profiles/` directory:

```json
{
  "industry_code": "METAL_FAB",
  "industry_name": "Metal and Steel Fabrication",
  "sidebar_layout": [
    { "module": "Master Data", "visible": true },
    { "module": "Procurement", "visible": true },
    { "module": "Job Costing", "visible": true },
    { "module": "POS Checkout", "visible": false }
  ],
  "doctype_overrides": [
    {
      "doctype": "Brand",
      "new_label": "Material Grade",
      "fields": [
        { "name": "alloy_composition", "label": "Alloy Composition (%)", "fieldtype": "Text", "mandatory": true }
      ]
    },
    {
      "doctype": "Combination",
      "new_label": "Heat Number",
      "fields": [
        { "name": "cut_length", "label": "Cut Length (mm)", "fieldtype": "Decimal", "mandatory": true },
        { "name": "heat_cert_code", "label": "Metallurgy Cert Code", "fieldtype": "Text", "mandatory": false }
      ]
    }
  ],
  "workflow_presets": [
    {
      "doctype": "GRN",
      "stages": ["L1 Quality Check", "L2 Manager Sign-off"]
    }
  ]
}
```

---

## 4. API Specification for Industry Switching

- **`GET /api/v1/admin/industries`**
  - Returns a list of all available pre-configured industry profiles.
- **`POST /api/v1/admin/industry`**
  - Updates the active industry.
  - **Body**: `{ "industry_code": "METAL_FAB" }`
  - **Action**: Reloads the `doctype_fields` database tables, flushes the dynamic label translations cache, and logs an audit log entry.
