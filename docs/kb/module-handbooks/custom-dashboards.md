---
title: Custom dashboards
section: Module Handbooks
order: 95
summary: Build your own charts and number tiles from the ERP's data - sales, stock, buying, customers, money - filtered by date and store; share them, make one a role's home screen, export or email them.
audience: owner, store manager, admin
last_verified: 2026-10-22
screens: [reports, home]
doc_id: DOC-337C0297D3
type: procedure
status: draft
owner: documentation-maintainer
approvers: [documentation-maintainer, documentation-maintainer]
applies_to: source documentation; scoped release acceptance required
authority: canonical
confidentiality: internal
review_by: 2026-11-21
supersedes: none
superseded_by: none
verification_scope: metadata and lifecycle classification; domain acceptance pending
topic_type: how-to
module: reports
task: Custom dashboards
prerequisites: Signed-in account with the Reports module; building needs Create on Dashboard Layout
---

# Custom dashboards

A custom dashboard is a page of widgets - numbers, bars, lines, donuts,
tables and pivots - drawn from the ERP's own records. Open them under
**Reports → Custom dashboards**.

## What you see

- **Ready-made** dashboards for **Sales**, **Stock**, **Buying**,
  **Customers** and **Money** open for everyone. **Copy** one to change it.
- Every figure is for **the stores you may see**, and money only if your
  role may see it. A store manager and the owner can open the same
  dashboard and each sees their own figures.
- **Dates**, **Store**, **Brand**, **Category** and **Channel** at the top
  apply to every widget (a widget can keep its own dates and store).
- A number shows its **change on the period before**. Point at a mark to
  see its value; **click it** to list the bills, orders or receipts behind
  it.
- The page **refreshes itself** every few minutes while it is open.

## Building one

1. **+ New dashboard** (or **Copy** an existing one).
2. **+ Add widget**: a title, a dataset (sales lines, POS bills, stock on
   hand, stock movements, purchase orders, goods receipts, transfers,
   customers, loyalty points, online orders, expenses, ledger postings) or
   a report, a chart type, what to show it **by**, and what to **add up**.
   The preview draws as you choose.
3. Arrange widgets with ◀ ▶, − + and ↕; edit with ✎, remove with ✕.
4. **Share and home**: users, roles or stores; and the roles that get it
   as their **Home** screen.
5. **Save**.

## Taking it with you

- **CSV** / **XLSX** on a widget download its rows.
- **PDF** saves the whole dashboard as a landscape A4 page.
- **Email me this** sends it Daily, Weekly or Monthly - numbers and bars in
  the email, every row in a CSV - and lists it under **Reports →
  Schedules**.

## If something is missing

- *No "+ New dashboard"*: your role cannot build dashboards - an
  administrator gives **Create** on **Dashboard Layout** in Roles.
- *A dataset is not offered*: your role cannot read that record type.
- *An amount says "hidden"*: your role does not see money figures.
- *"This widget's data cannot be shown"* (REPORT-0988): edit the widget and
  pick its fields again.

Full steps: User Guide §9.2. Administration: Admin Guide, "Custom
dashboards".
