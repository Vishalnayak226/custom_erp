---
title: Which journey is mine
section: Role Journeys
order: 1
summary: Six jobs, four shipped roles, and how the two line up - read this before picking a journey.
audience: everyone
last_verified: 2026-08-17
screens: [roles]
---

# Which journey is mine

The articles in this section are organised by **job**, not by the role record on
your account. Those are two different things and it is worth being clear about
which is which before you read further.

## The four roles the system ships with

| Role | Reaches |
|---|---|
| **Cashier** | The till, customer lookup, and very little else |
| **Store Manager** | Store operations, stock, orders, procurement, local reports |
| **Super Admin** | Everything, including users, roles and system settings |
| **Supplier** | Only Supplier Submissions, and only its own company's |

Anything beyond these four is a role your administrator creates on
**Settings » Roles**, granting read, create, update and delete per record type.
The system fails closed: a record type with no grant row for a role is denied to
that role, never allowed by default.

## The six journeys, and who usually does them

| Journey | Usually held by | Why |
|---|---|---|
| [Cashier](journey-cashier.md) | Cashier | Matches the shipped role almost exactly |
| [Store manager](journey-store-manager.md) | Store Manager | Matches the shipped role almost exactly |
| [Warehouse operator](journey-warehouse-operator.md) | Store Manager, or a custom role | The shipped roles have no warehouse-only variant |
| [Category manager](journey-category-manager.md) | Super Admin, or a custom role | Needs the Item master and PIM, which Store Manager only partly reaches |
| [Finance](journey-finance.md) | Super Admin, or a custom role | Ledger, payments and reconciliation are not in Store Manager's grants |
| [Administrator](journey-admin.md) | Super Admin | By definition |

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
