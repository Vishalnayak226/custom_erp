---
title: HR & Payroll
section: Module Handbooks
order: 90
summary: Track employees, attendance and leave, then run payroll against a configured salary structure and post it straight to the GL.
audience: HR manager, admin
last_verified: 2026-09-03
screens: [hr]
---

# HR & Payroll

HR here is two layers that build on each other. The bottom layer — Employee,
Attendance, Leave — is plain record-keeping: who works here, were they in
today, did their leave get approved. The top layer — salary structures,
payroll runs, loans — is a real payroll *processing* engine that reads that
bottom layer to compute what to pay, and posts the result straight to the
general ledger. A separate, older **Payroll Export** exists for a business
that runs payroll in outside software and just needs the attendance/leave
numbers handed over; it never touches the GL and keeps working unchanged
whether or not you also use the processing engine.

Everything on this page lives under one **HR** screen with tabs across the
top: Attendance, Leave, Payroll Export, Roster, Payroll, Loans, Onboarding,
Appraisals, Training, Grievances.

## Setting up an employee

**Setup → Master Data → Employee**: **Employee ID**, **Name**, and
**Status** (Active/Inactive) are the only required fields — Department,
Designation, Location, Reporting Manager and a linked ERP User ID are all
optional. Two of these matter beyond record-keeping:

- **Linked ERP User ID** is what makes an Employee's status control login
  access: deactivate the Employee and the linked user account is
  deactivated with it on the same save, no separate step. An Employee with
  no linked user is unaffected either way.
- **Bank Account Number** and **Bank IFSC** (added for payroll) are not
  required to create an Employee, but a payslip cannot be posted without
  them — see [Posting a payslip](#posting-a-payslip) below.

A duplicate **Employee ID** is rejected outright (`HRPAYR-0149`). An
**Exit Date** earlier than **Date of Joining** is rejected the same way
(`HRPAYR-0156`) — always a typo, not a real employment history.

## Attendance and leave

**Attendance** records one row per employee per day: Present, Absent, Late,
Leave, Holiday or Weekly Off. **Leave** is a separate request/approval
record (Casual, Sick, Earned or Unpaid, with a day count) that a Store
Manager or HR/Admin approves — a Cashier login can *submit* their own leave
request but not approve it, the same self-service shape every other
employee-facing role in this system uses. Payroll only ever reads
**Approved** leave; a request still sitting Applied or already Rejected has
no effect on pay.

**Shifts and the roster** are a separate opt-in layer: define a **Shift**
(start/end time, break minutes) under Setup, then assign employees to one
via **ShiftAssignment** on the Roster tab. An employee with at least one
roster assignment is checked against it going forward (`HR-0269` if
attendance is marked with no shift assigned that day) — an employee who has
never been rostered is simply never checked, so adopting shifts is
per-employee, not all-or-nothing.

## Payroll Export — for external payroll software

**HR → Payroll Export** (`GET /api/v1/hr/payroll-export`, a date range) is
the older, simpler path: it does not compute pay or touch the GL at all — it
totals each employee's Present/Absent/Late days and Approved leave days for
the period and hands that back as a flat export, for you to feed into
whatever payroll software or accountant you already use. It keeps working
exactly the same whether or not you ever configure a salary structure for
the processing engine below.

## Running payroll — the processing engine

This path computes an actual payslip from a configured salary structure,
rather than just exporting attendance totals for someone else to price.

### 1. Configure a salary structure

**Setup → Master Data → Salary Structure**, one Active row per employee:
**Basic**, **HRA**, **Other Allowances**, plus the statutory deduction
rates — **PF % of Basic**, **ESI % of Gross**, **Professional Tax (flat)**
— and an **Effective From** date. Payroll always reads the employee's
*latest* Active structure as of the run, so a raise is a new row, not an
edit to the old one. Running payroll for an employee with no Active
structure at all is refused (`HRPAYR-0154`).

**ESI is conditional, not a flat percentage of every payslip**: it only
applies if gross pay is at or under a configurable ceiling
(`hr.esi_wage_ceiling`, **Settings → Configuration**, default ₹21,000) —
matching India's real statutory ESI applicability rule. Above the ceiling,
ESI is simply zero for that employee.

### 2. Preview, then run

`GET /api/v1/hr/salary-components` (an employee id) returns gross pay and
the PF/ESI/PT breakdown without creating anything — a preview before
committing to a run.

**HR → Payroll tab → Run Payroll** (`POST /api/v1/hr/run-payroll`, employee
+ period) computes and saves a **Draft Payslip**:

1. Confirms attendance exists for the employee in that period at all —
   refused with `HRPAYR-0150` otherwise, since payroll without attendance
   isn't a shortfall to silently zero out, it's a period nobody has
   regularized yet.
2. Computes gross pay and statutory deductions from the salary structure
   above.
3. Estimates TDS-on-salary via the same TDS engine vendor-invoice TDS
   uses, against a seeded `SALARY` TDS section (a flat, illustrative
   monthly-equivalent default — a real deployment retunes the threshold and
   rate to its own statutory numbers, the same generic doctype-table edit
   any other TDS section uses).
4. Deducts this month's share of every Active loan the employee has (see
   [Loans against salary](#loans-against-salary)), each capped at its own
   remaining balance so a loan's last installment can never over-deduct.
5. Nets everything down to **Net Pay** (floored at zero) and saves the
   result as a Draft `Payslip` — nothing is posted to the GL yet.

### Posting a payslip

**HR → Payroll tab → Post to GL** (`POST /api/v1/hr/post-payslip`) is a
separate, explicit step — computing a payslip and disbursing it are
deliberately not the same action. Posting requires the employee to have a
**Bank Account Number** on file (`HRPAYR-0155`) — a payslip can be computed
and reviewed without one, but not actually marked ready to pay. Posting also
respects whichever accounting period the payslip's period-end date falls
in: a locked period refuses the post (`HRPAYR-0153`) the same way it refuses
any other backdated posting.

The GL entry debits **Salary Expense** for the gross amount and credits
**Salary Payable** for net pay, plus **PF/ESI/PT/TDS Payable** for whichever
deductions are non-zero, and **Staff Loans Receivable** for any loan
deduction taken — a single balanced multi-credit entry, same shape as the
existing vendor-TDS posting. Only a Draft payslip can be posted; posting is
refused outright once it's already Posted.

## Loans against salary

**HR → Loans tab**, one `EmployeeLoan` record per loan: **Principal
Amount** and **Monthly Deduction**, created as Draft. **Disburse Loan**
(`POST /api/v1/hr/disburse-loan`) moves it to Active — debiting **Staff
Loans Receivable** and crediting Cash/Bank for the principal, and setting
**Outstanding Balance** to the full principal — a loan cannot be disbursed
twice (refused once Active or Closed). From there, every payroll run for
that employee automatically deducts that month's share (capped at whatever
remains) and reduces the outstanding balance; a loan whose balance reaches
zero closes itself, with no separate "close this loan" action needed.

## Onboarding & offboarding

**HR → Onboarding tab** (`OnboardingChecklist`): one record per employee per
direction (Onboarding or Offboarding), holding a checklist of tasks and a
document locker as plain JSON lists — a lightweight tracker, not a workflow
engine with its own approval chain.

## Appraisals, training and grievances

These three tabs exist and give ordinary record-keeping (create, list, edit,
same generic form every other doctype gets) — confirmed by grep, there is no
dedicated engine file backing any of them the way payroll, attendance and
leave each have one. Full appraisal cycles, training-completion tracking and
grievance escalation as *workflows* remain explicitly out of scope for this
build. Use these tabs to keep a record; don't expect them to drive a process
for you yet.

## Self-service

A Cashier-role login with a **Linked ERP User ID** on their own Employee
record can submit their own Leave requests and read (but not edit) their own
Payslip, EmployeeLoan and ShiftAssignment history — `GET /api/v1/hr/my-employee`
resolves the logged-in user's own Employee record so a self-service screen
can prefill and scope requests without them needing to know their own
employee code. It returns an empty result rather than an error for a login
with no linked Employee at all (e.g. an admin account that isn't itself
staff).

## Reports

**Attendance Summary** (**Reports → HR**) — present/absent/late/leave/
holiday/weekly-off counts and an attendance percentage, per employee, for a
date range. Exports and schedules like any other registered report.

## Error codes reference

| Code | Meaning |
|---|---|
| `HRPAYR-0149` | Employee ID already exists. |
| `HRPAYR-0150` | No attendance recorded for this employee in the payroll period — regularize attendance before running payroll. |
| `HRPAYR-0151` | Leave requested exceeds the employee's leave balance. |
| `HRPAYR-0152` | Leave request overlaps an existing leave record for the same employee. |
| `HRPAYR-0153` | The accounting period this posting falls in is locked. |
| `HRPAYR-0154` | No Active Salary Structure configured for this employee. |
| `HRPAYR-0155` | Employee has no bank account on file — required before a payslip can be posted. |
| `HRPAYR-0156` | Date of Exit is earlier than Date of Joining. |
| `HR-0267` | Login blocked — the linked Employee is Inactive. |
| `HR-0268` | Attendance marked from a device/location that doesn't match what's expected. |
| `HR-0269` | Attendance marked with no shift assigned for that day, for an employee who has at least one roster assignment on file. |

## Troubleshooting

**"No Active Salary Structure configured" when running payroll.** Create
one under Setup → Master Data → Salary Structure with today's date on or
before **Effective From**, status Active. Payroll always uses the latest
Active row, so an old Inactive one won't be picked up.

**Payroll refuses to run, citing missing attendance.** The employee has zero
Attendance rows inside the period you asked for — mark attendance (or
confirm the date range) before running payroll again.

**A payslip won't post — "bank account on file" error.** Add the employee's
Bank Account Number under their Employee record. The payslip itself can
still be computed and reviewed without it; only posting requires it.

**A loan deduction seems to have stopped early.** Check the loan's
Outstanding Balance — a loan closes itself automatically once its balance
reaches zero, and a closed loan is no longer deducted from.

**An employee can't log in even though their password is correct.** Check
their linked Employee record's Status — an Inactive Employee's linked user
account is deactivated automatically, and re-activating the Employee is
what restores login (`HR-0267`).

## What is not here yet

**Appraisals, training and grievances are record-keeping only** — see
[above](#appraisals-training-and-grievances). There is no appraisal-cycle
automation, no training-completion tracking logic, and no grievance
escalation workflow behind these tabs today.

**No progressive income-tax slab engine.** TDS-on-salary is a single flat
rate/threshold pair on a seeded `SALARY` TDS section, an illustrative
default meant to be retuned per deployment — not a full slab-based
computation.

**Onboarding/offboarding checklists are plain lists**, not a guided,
step-gated workflow — nothing stops you marking a task done out of order or
skipping the document locker entirely.