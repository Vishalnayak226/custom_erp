---
title: Which journey is mine
section: Role Journeys
order: 1
summary: Six jobs, four shipped roles, and how the two line up - read this before picking a journey.
audience: everyone
last_verified: 2026-11-24
screens: [roles]
doc_id: DOC-6794A2722A
type: procedure
status: draft
owner: documentation-maintainer
approvers: [documentation-maintainer, documentation-maintainer]
applies_to: source documentation; scoped release acceptance required
authority: canonical
confidentiality: internal
review_by: 2027-02-22
supersedes: none
superseded_by: none
verification_scope: metadata and lifecycle classification; domain acceptance pending
topic_type: tutorial
module: cross-module
task: Which journey is mine
prerequisites: Signed-in account with permission for the described task; observe the article prerequisites
---

# Which journey is mine

The articles in this section are organised by **job**, not by the role record on
your account. Those are two different things and it is worth being clear about
which is which before you read further.

## The four roles a business starts with

| Role | Reaches |
|---|---|
| **Admin** | The whole business: every module, store, approval, user, role and setting |
| **Store Manager** | Store operations, stock, orders, procurement, local reports |
| **Cashier** | The till, its returns, and customer lookup |
| **Supplier** | Only Supplier Submissions, and only its own company's |

**Super Admin** is the platform provider's account, not the business's. It adds
only the developer tools: Database Schema Design, Extension Hooks and System
Status.

Anything beyond these four is a role your administrator creates on
**Settings » Roles**. A role named after a shipped template - Picker, Warehouse
Manager, Accounts Payable, Accounts Receivable, Accountant, HR Manager, Employee
Self-Service, Auditor, Integrator or Merchandiser - starts with that template's
access; any other role is granted read, create, update and delete per record
type. The system fails closed: a record type with no grant row for a role is
denied to that role, never allowed by default.

## The six journeys, and who usually does them

| Journey | Usually held by | Why |
|---|---|---|
| [Cashier](journey-cashier.md) | Cashier | Matches the shipped role almost exactly |
| [Store manager](journey-store-manager.md) | Store Manager | Matches the shipped role almost exactly |
| [Warehouse operator](journey-warehouse-operator.md) | Store Manager, or a Picker / Warehouse Manager role | The four starting roles have no warehouse-only variant; the templates do |
| [Category manager](journey-category-manager.md) | Admin, or a Merchandiser role | Needs the Item master and PIM, which Store Manager only partly reaches |
| [Finance](journey-finance.md) | Admin, or an Accountant role | Ledger, payments and reconciliation are not in Store Manager's grants |
| [Administrator](journey-admin.md) | Admin | By definition |

> [!NOTE]
> If a journey describes a screen you cannot open, that is your role, not a
> fault or a missing feature. Ask an administrator, and point them at the record
> types the journey names.

## Reading a journey

Each one is written as a day rather than a feature list: what you do first,
what you do when something is wrong, and what you check before you go home. They
assume you have already read [What this system
is](what-is-this-system.md) and can find your way around.

Where a journey needs detail it does not have room for, it links to the module
handbook for that area rather than repeating it.
