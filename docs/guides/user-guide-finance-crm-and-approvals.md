---
doc_id: DOC-78F8FF850A-08
title: User Guide: Finance, service, CRM, people and approvals
type: procedure
status: draft
owner: documentation-maintainer
approvers: [documentation-maintainer, documentation-maintainer]
audience: [maintainers, documentation-maintainer]
applies_to: source documentation; scoped release acceptance required
authority: transition-copy
confidentiality: internal
last_verified: 2026-09-09
review_by: 2026-10-09
supersedes: none
superseded_by: none
verification_scope: metadata and lifecycle classification; domain acceptance pending
---

# User Guide: Finance, service, CRM, people and approvals

Part 8 of 9 of the [User Guide](user-guide.md). Previous: [Reports and customer orders](user-guide-reports-and-orders.md). Next: [Store app, profile and help](user-guide-store-app-and-help.md).

## 9B. Finance work to review

Open **Financial Accounting → Work to review**. It starts with direct paths to Payables, Receivables, Payment Proposals, Bank Reconciliation, **Who posted what**, **General journal** and **Period close**. Open the destination and inspect each source document before posting or paying it; this landing page does not show balances, ageing, bank differences or approval counts.

**Chart of Accounts** lists every account with its parent and flags. To add an account, rename one, set its parent, mark it **No manual posting** or block it, choose **New** (or Block / Change on the row), fill the short form and **Send for approval**. An Accountant approves it under **Approvals**; nothing changes before that. A blocked account takes no new postings but keeps its history. A General journal line on a **No manual posting** account is refused.

**Who posted what** opens the **GL Posting Review (by poster)** report: for the dates you choose, one row per person, job or integration that posted to the ledger, with how many lines and documents and the totals. **View Details** lists exactly those lines. **General journal** lists every posted line with its source document, account and **Posted by** / **Source command**; filter it by a username, by **Attribution** (*Attributed* or *Before attribution*) or by document type. Lines posted before this record existed show **before attribution** — that means "not recorded", not "nobody". A job shows as `service:job`, an API key as `service:api-key:…`, and a background process as `service:background`. When your admin turns on **GL source line**, **GL Drill-down** also shows **Line**: the line of the invoice, till sale, GRN or bill a row came from (1, 2, ...).

For a supplier bill, match it to its PO and GRN in **Vendor Invoice** before a payment proposal. For a customer bill, use **Sales Invoice** to distinguish Draft, Approved (posted) and Paid. An Account Head and a qualified CA still have separate review and signoff responsibilities.

### 9B.2 Closing a month (Accounting Periods)

**Financial Accounting → Accounting Periods** lists your periods with how many of their five close tasks are signed. **+ New Period** creates one (name, start and end date). Click **Close tasks** on a period to open its close board.

Two more checks hold the close (140.12): **No till open or awaiting approval** - every till opened by the period's end is closed - and **No offline sales waiting to sync** - every sale a till reported while offline has reached the server. **Close** is refused while either fails.

1. **Every period has five close tasks:** Bank reconciliation, Stock valuation tie-out, Payables (AP) tie-out, Receivables (AR) tie-out, and Marketplace / POS settlement. Each shows a **System check (live)** — for example "3 statement lines in the period still unmatched" or "tied - detail 48,000, summary 48,000, ledger 48,000" — and an **Open** link to the screen or report where you do the work.
2. **An Accountant, Admin or Super Admin assigns an owner** to each task: type the owner's username and click **Assign**.
3. **Only the owner signs the task off.** The owner clicks **Sign off**, picks **Done** or **Not applicable**, writes the **Evidence** (what was checked and what it showed — at least 10 characters) and an optional **Reference**. The system check as it read at that moment is saved with the sign-off. A check that does not pass can still be signed when the evidence explains the difference. **Undo sign-off** (owner, Accountant or Super Admin, with a reason) returns it to open.
4. **Close period** is only enabled once all five are signed, and only for an Accountant, Admin or Super Admin. If a pre-close check still fails, the close asks **why** (at least 10 characters). The reason and the failing checks are saved in the period's close log and the audit log. After closing, any posting dated inside the period is refused.
5. **Reopening needs a second person.** On a closed period an Accountant, Admin or Super Admin writes why it must reopen and clicks **Request reopen**. The request waits in **Approvals**; the person who asked cannot approve it. When another Accountant, Admin or Super Admin approves, the period reopens, starts **close round 2** and every task goes back to unsigned. Corrections are posted as **new reversing entries** — nothing already posted is edited — and the period is closed again through the same five tasks.
6. **History** at the bottom of the board records every assignment, sign-off, undo, close, reopen request and reopen, with who and when.

**Hold a period, and lock a filed month (148.25).** On the Accounting Periods list, **Hold** puts an Open period on hold: sales invoices and POS sales dated in it are refused until you click **Release**. A held period can still be closed. **GSTR-1 months** lists the months you marked filed; pick a finished month and click **Mark filed**. When an admin has switched on *Lock a month after GSTR-1* (Settings > Finance), that month's sales documents can no longer be posted or cancelled; correct them with a credit or debit note. Receipts and returns are not blocked. Only an admin can **Unmark** a month, and must give a reason; it is kept in the audit log.

**GSTR-1 file, GSTR-2B match and exports under LUT (170.35-170.37).** On **Reports > GST Return Summary**, click **GSTR-1 / 2B files**.
- *GSTR-1 file.* Pick the month, type the 15-character GSTIN and click **Download JSON**. The file holds b2b, b2cl, b2cs, cdnr, cdnur, exp, nil, hsn and doc_issue as the GST portal expects. It is built from the month's approved sales invoices and posted credit notes (the same lines the e-invoice uses), the till bills posted in the month and the credit notes of till returns refunded in it. A till bill to a customer with a GSTIN goes in b2b; a walk-in bill adds to b2cs. Exempt and nil-rated lines go in the nil table, not in b2b or b2cs. doc_issue has one row per number series. Under the button, one line says whether the totals agree with the GST Return Summary, then what went in (invoices, till bills, credit notes, amendments) and any reasons (returns of online orders and unposted documents are not in the file). Upload the file on the portal yourself; nothing is sent from here.
- *Amendments.* When you click **Mark filed** for a month, the app keeps what that month's file reported. If one of those documents, or its customer, changes after that, the next month's file reports it again: in b2ba, b2cla, cdnra, cdnura or expa with the number it was filed under, or as a corrected b2cs row for that month in b2csa. A B2C bill whose buyer turns out to have a GSTIN is listed in this month's b2b, and its b2cs row is corrected in b2csa. Once you mark the next month filed, the amendment is recorded and not reported again. A document cancelled after filing, or one that moved to another table, is listed as a reason for you to amend on the portal by hand. Months marked before this was built kept nothing, so their documents are not compared.
- *GSTR-2B.* Download the GSTR-2B JSON from the portal, pick its month, choose the file and click **Import**. Each line is matched to a vendor bill by supplier GSTIN and invoice number (case, dashes and leading zeros ignored), then by amount and date. Status is **Matched**, **Mismatch** (the Why column says amount, tax or date), **In 2B not in books** or **In books not in 2B**. Filter by status, set a **Decision** (Accepted, Rejected, Query raised) on a 2B line, and click **Match again** after you enter more bills. Importing the same month again replaces its lines and keeps a decision on every line that did not change. A supplier's credit note in the file is matched to your debit note for that vendor, and a supplier's debit note to your vendor credit note, in the same way; a note is dated by the day it was posted. The Document column shows the type before the number for a note. A bill from a vendor with no GSTIN in the vendor master is matched by number and shown as **Mismatch** with the GSTIN the 2B gives: add it to the vendor. Such a vendor's other bills are not listed, since they cannot be on a 2B. The same list is the report **GSTR-2B reconciliation**.
- *Exports.* On a new Sales Invoice choose **Export**: *Export under LUT* or *Export with payment of IGST*, then the shipping bill number and date, the port code and the destination country. They print on the tax invoice ("Supply meant for export under LUT ... without payment of IGST") and fill the exp section of the GSTR-1 file. Keep each year's LUT in **Finance > LUT register** (LUT / ARN number, financial year, valid from and to; set Withdrawn when it ends).

### 9B.3 Reconciling a bank account

**Financial Accounting → Bank Reconciliation.** Choose the **Bank Account**; the screen shows the statement lines (default: the last three months plus anything still open), how many are Unmatched, Exceptions, Matched / Explained, the book balance, and how much was matched with a difference.

**Card / UPI settlements (140.12).** **Settlement file** opens the list where the acquirer's daily settlement file is imported (settlement date, Card or UPI, gross, fee, net paid, UTR - **Import** takes the CSV). **Match card / UPI** pairs each row with the unmatched bank credit of the same net amount dated from a day before to three days after the settlement date, and books it (Dr bank + Dr 5230 fee / Cr 1101 card or 1102 UPI clearing). The server tries again every hour, so a payout whose bank line arrives later is matched by itself; what cannot be matched is listed with the reason.

**UPI statements from BharatPe, PhonePe or Paytm (185.2).** Download the provider's settlement statement as CSV and **Import** it on **Settlement file** as it is: the provider's own column names are read (transaction date, amount, RRN / UPI ref, charges, settlement date, settlement UTR). Payments that failed, are pending or were refunded are left out, and the import says how many. **Match card / UPI** checks each payment against the till bill that took that UPI ref, adds up each payout's payments and matches the total to the bank credit. A payment with no till bill, on two bills, or for a different amount than the bill took is listed under **Bills to check**: open that bill and check the UTR the cashier typed. The same UPI ref imported twice is shown as an exception and never booked twice.

1. **Import the statement** with **Statement Lines / Import CSV** (one row per line: bank account, date, description, reference, amount, Debit/Credit).
2. **Exact match** pairs lines with book entries of exactly the same amount within three days.
3. **Rule match** opens three settings: **Amount tolerance** (for example 10 rupees of bank charges), **Date window** in days, and **Use the statement reference**. A line is matched only when exactly one book entry fits; a match within the tolerance records its **difference**, which you then post (for example a bank-charges Journal Voucher). Lines with several possible entries are left for you.
4. **Find match** on a line lists the closest book entries. Tick one or more; **Match selected** is only enabled when they add up exactly to the line.
5. **Raise exception** gives a line you cannot match yet an **Owner** and a **Reason**; it shows its age in days. **Explain** (Accountant, Admin or Super Admin only) closes an exception that nothing in the books will ever match, with a note.
6. **Payout** on a money-in line: choose a **marketplace settlement batch** (its payouts must add up to the line), or the **card** or **UPI** clearing account and the **acquirer fee** deducted. A card / UPI payout posts the money from clearing (1101 / 1102) to the bank, the fee to 5230, and matches it.
7. **Unmatch** (with a reason) undoes a match. A line dated in a **closed** period cannot be changed — request a reopen first.
8. **Part match (170.2).** On **Find match**, tick book entries that add up to *less* than the line and press **Part match**. The line shows **Part Matched** with what is matched and what is still open; the open part can be matched later, or sent to **Raise exception**. A book entry can also be spread across several statement lines. **Unmatch** gives every part back.
9. **Bank files (170.3, 170.4).** **Bank files** (top of the screen) has two loaders. **Statement**: pick the bank account, the format (CSV or MT940) and the file; lines already loaded are skipped, so the same file can be loaded twice safely, and the result lists what was loaded and skipped. **Bank response**: enter the Executed payment proposal, pick the bank account and the bank's CSV (payment_reference, utr, status, reason). A paid row records the UTR; a rejected row raises a bank exception with the bank's reason and reverses nothing. A reference the proposal does not hold is listed, not fatal.

### 9B.3a Raising a sales invoice (Stage 130.16)

**Financial Accounting → Sales Invoice → + New Sales Invoice** bills a customer on credit.

**Stock, cost and cancelling (140.12).** Posting an invoice takes its stock items out of the invoice's store at their cost, §6.15 (Dr 5100 Cost of Goods Sold / Cr 1200 Inventory) and writes the Stock Ledger, in the same step as the receivable. A tracked item's line takes **Piece codes** or **Batch**, and the invoice books their own cost. If the goods already went out on a delivery challan, type its number in **Delivery challan**: the invoice then posts money only. An invoice the system raises from an online order, a shipment or a package never moves stock again. A store that does not hold enough is refused ("only 3 of TEE at GTS") - receive or transfer the stock in first. **Cancel** on a Draft, or on a posted invoice that has taken no money, asks why, reverses everything it posted (revenue, GST, cost of goods) and puts the stock back; an invoice with receipts, an IRN or a deferred-revenue schedule is corrected with a credit note instead. Posting, settling and cancelling need a finance role (Accountant, Accounts Receivable, Accounts Payable or Admin); a refusal keeps its code (for example the credit limit). A customer set to **Stop invoices over the limit** is refused (`FIN-0861`); any other customer over their limit is only warned. An order that carries a shipping charge (a Shopify or marketplace order) shows it as its own **Shipping charge** line, with GST, on the invoice, the e-invoice and the printout.

1. Choose the **Customer** - their **Buyer GSTIN** and **Place of supply** (state code) fill in from the customer; type them for a registered buyer that is not on file (a GSTIN is checked, including its last character). Choose the **Store** the invoice is issued from, the **Invoice date**, and optionally a **Due date** (blank fills from the customer's payment terms) and the **Customer reference** (their PO number).
2. Add the items; each line takes its **Qty**, **Rate (before GST)**, a **Discount (₹)** - what comes off that line before GST, nothing by default - and **GST %** (from the item). The line shows taxable value (qty × rate − discount), GST and amount; the discount prints in the invoice's Discount column.
3. The screen says how the tax splits: **CGST + SGST** when the place of supply is the store's own state, **IGST** across states (the store's state comes from its legal entity - a store with none splits CGST + SGST and says so).
**A line with no item (148.22).** Type a description in **Add a line by description** and press Enter. Give it a rate and GST %. In **Income account** type the revenue account it should credit; blank credits Sales (4100) as before. The account must be a Revenue account in the chart, or the post is refused. A return of a coded line goes back through the same account.

4. **Save as draft**, then **Post** it from the list: the receivable, the revenue and the output GST are booked. **Print** prints it as a tax invoice with the buyer's GSTIN and place of supply.

### 9B.3a-1 Custom jewellery order (Stage 148.36, module Custom orders)

Only when your administrator has switched on the **Custom orders** module. Open **Sales → Custom Order Quote** and press **New order**.

1. Pick the **Metal**, **Purity**, **Making type** and, if wanted, the **Stone** and **Size**; type the **Weight (g)** and, for a stone, how many **Stones**.
2. Press **Price**. Each part shows: the metal (weight × today's rate), the making charge, and one line per setting or stone rule. If today's rate has not been keyed the screen says so (**SALESP-0761**); ask the administrator to add it.
3. Type the **Customer**, **Phone** and **Address**, then press **Make order**. This makes the item (with the next Item number) and a **Draft** sales order. Open the order from Sales Orders and submit it as usual.

The quote keeps the rate it used; a later rate change never reprices it. A quote cannot be typed in by hand (**SALESP-0768**).

### 9B.3b Many sales invoices from one Excel file (Upload from Excel)

Sales rung up elsewhere - a wholesale counter, an exhibition, a day's bills on paper - can be billed in one go from one Excel file, each line found by its barcode with its own discount, instead of typing each invoice.

1. On **Financial Accounting → Sales Invoice**, click **Upload from Excel**, then **Download template**. Its **Start Here** sheet says how to fill it and **Example** shows filled rows. The **Store** and **State** columns have drop-downs with your stores and the states.
2. **Sales** sheet: one row per item sold.
   - **Invoice Ref** — your name for the bill; rows with the same Invoice Ref and Customer become **one invoice**. **Customer** — their name, phone or code (on the Customers sheet, or already in Wholeops). **Store** — where it is sold from. **Invoice Date** — an Excel date, or typed in your date order (§3 "Dates in a file"). All rows of one invoice have the same store and date.
   - **Barcode** — the item's barcode or code (a receipt-date barcode works too). **Qty** — a whole number.
   - **Rate** (per piece, **including GST**) — leave it blank to bill the item's own **sale price** (or MRP). **Discount** — what comes off: an amount **per piece** (`12.50`) or a **percentage** (`10%`). It is taken off before GST and prints in the invoice's Discount column.
3. **Customers** sheet: a customer who is not in Wholeops yet - **Customer Name**, and their **Phone**, **GSTIN**, **State** and **City** if you have them. A customer already there is found by GSTIN, phone or name and left as it is; only customers a Sales row names are made. The **State** (or the GSTIN) decides **CGST + SGST** or **IGST** against the store's state; without either, the sale is billed within the store's state. A store with no State (on the store or its legal entity) is billed CGST + SGST too, and the preview says so on each of its invoices - give the store a State first if any of those buyers are in another state.
4. Back on **Upload from Excel**: **Choose file**, then **Preview**. Nothing changes yet. The preview lists the customers and each invoice with its rows - the item found, its rate, discount and amount - and says whether it will be made and posted, made as a Draft, or is already there. Anything with a **Problem** (a barcode no item has, an unknown store, a date that is not a date) marks its whole invoice as left out, so one bill is never made with a line missing; fix it in the file and preview again.
5. Click **Make and post … invoices** and confirm. Each customer is made, each invoice is saved (GST included, the place of supply from the customer) and **posted as you** - the receivable, the revenue and the output GST are booked and the items leave the store's stock - where your role may post invoices. If it may not, the invoices are saved as **Drafts** and the preview says so; someone who may post posts them from the list. An invoice that cannot be posted (the customer is over their credit limit, the store does not hold the stock) is left as a Draft with the reason.
6. **Print** any of them from the list as usual; each prints the Discount column.

**Uploading the same file again is safe.** An invoice already made from it (same customer and Invoice Ref) is found and not made again; a Draft one is posted if you may post. So fix what was marked Failed or Problem and upload the same file. **Past uploads** lists every upload with its result; **Details** shows it again and **Download file** gives back the file.

**Who can.** Anyone who can create sales invoices; posting needs the right to post invoices, as on the list. Each part is checked as if done on its own screen (making customers, saving and posting invoices), and the preview says when your role may not do one of them.

### 9B.4 Receiving and paying in parts, and credit notes against an invoice

**Customer receipts.** Open **Financial Accounting → Sales Invoice**. Each invoice shows its **Amount** and its **Outstanding** balance.

**+ Receipt (140.12)** on the Sales Invoice list takes one payment across a customer's invoices: choose the customer, how it came (**Bank Transfer**, **Cheque**, **UPI**, **Card**, **Cash**), the **Bank account** (needed for a cheque or bank transfer), the reference, the **Amount received** and any **TDS deducted**. The customer's open invoices are filled oldest first - change any **Pay** amount. Their open advances are listed too: **Use** applies an advance to the invoices. What is not allocated is kept **on account** as a new customer advance (no till needed) that later invoices or till bills can use. The receipt posts Dr bank (or the mode's clearing account) / Dr 1310 TDS Receivable / Cr 1300, and each invoice's **Payments** history shows it. **Reports -> Finance -> Customer receipts** (R5085) lists receipts. A return against a credit invoice is valued at what the customer was charged (rate + GST) and first reduces what they still owe on it; only the rest is paid back. Credit notes and purchase returns are numbered in their source document's store series. The customer **Statement** shows the whole account: invoices and their receipts, advances and store credit taken, used and refunded, and till bills.

1. Click **Settle** on a posted (Approved) invoice. The box offers the whole outstanding balance.
2. To record full payment, click **OK**. To record part of it, type the amount received (no more than the outstanding balance) and click **OK**.
3. A message confirms the amount and what is still outstanding. The invoice shows **Part paid** until nothing is left, then **Paid**.
4. **Payments (n)** on the row lists every receipt and applied credit note, with date, reference, amount and who recorded it.

A foreign-currency invoice can only be settled in full.

**Pay link (185.3).** Click **Pay link** on a posted (Approved) invoice to send the customer a Razorpay payment link for what the invoice still owes. The link is copied, and shown so you can paste it into WhatsApp or an e-mail. Clicking again gives the same link. If part was paid since, the old link is cancelled and a new one made for the rest. When the customer pays, the receipt is booked by itself and the invoice shows **Part paid** or **Paid**. Anything paid over what the invoice owed is kept as the customer's advance. A foreign-currency invoice gets no link. **Pay link** needs the Razorpay keys (ask your administrator).

**Changing a sale after it is issued.** You can still open a posted sales invoice, a confirmed sales order or a paid till bill with **Edit** to correct something such as a due date or a note. When you **Save**, a box asks *Reason for the change*. Type why, for example *customer asked for month end*, and click **OK** to save. Click **Cancel** to leave the record as it was. Who changed what, from what to what, and the reason are kept in **Reports → Sales → Post-Sale Edit History**. Saving without changing anything does not ask. A Draft can be edited freely. **The money is fixed once it has posted (Stage 73.D.8):** on a posted invoice, credit or debit note, paid supplier bill or posted journal voucher, the amounts, tax split, currency (and a journal's lines and date) cannot be changed — the save is refused (`FIN-0844`); correct the figures with a credit or debit note, or reverse the voucher. Nor can a save take a posted one back out of the accounts (Stage 47.16.4): setting its status to Cancelled, Draft or Void is refused the same way - raise a credit note against it instead. A Draft invoice can still be cancelled. Everything the form does not show (payments received, the GST split, the outstanding balance) is kept as it was when you save.

**Supplier payments.** Open **Financial Accounting → Vendor Invoice**. On a **Matched** bill click **Pay**; the box offers the whole outstanding balance — accept it or type a part amount. Each payment clears the goods value and the input GST in proportion, and the last payment clears both exactly. The bill shows **Part paid** until it is fully paid, and **Payments (n)** lists every payment. **Pay w/ TDS** is only offered before the first payment, because TDS is withheld on the whole bill; finish a part-paid bill with **Pay**. A payment proposal pays only what is still outstanding on each bill.

**Credit notes against an invoice.** In **Financial Accounting → Customer Credit Notes**, a credit note that names a **Sales Invoice** reduces that invoice's outstanding balance when you **Post** it. If the note is larger than what is left, the rest is booked as a refund to the customer. The **Invoice** column shows how much was applied and how much refunded. A note against an invoice that is already Paid is a refund. **The GST comes back too:** when the invoice charged GST, the note takes back its share of that GST (the note's share of the invoice; never more than is left after earlier notes on the same invoice), so your GST return is not overstated. The note records the split — taxable value, CGST / SGST or IGST. A note with no invoice takes back no GST.

**Ageing.** **Receivables Ageing** ages what is still outstanding. **Payables Ageing** shows goods received and not yet paid, one line per goods receipt, aged from the bill's due date (or the receipt date when no bill has a due date); its control totals tie to 2100 GRN Suspense. Open purchase orders are in **Reports → Procurement → Open Purchase Commitments**.

### 9B.1a Setting off what a customer owes against what you owe them (148.23)

When one business is both your customer and your vendor, the **Party pairs** list (*Financial Accounting > Work to review > Party pairs*; the link shows once your admin has picked the set-off clearing account) lists the pairs. **Suggested pairs** shows customers and vendors that share a GSTIN or PAN; **Pair** makes the pair (nothing is paired for you). **Balances** shows what they owe you, what you owe them, and the most that can be set off. Then open **Set-off vouchers** on the same page and click **New**: pick the pair, the amount (not more than either balance), the date and, if you want it undone later, a reverse-on date. Submit it for approval; when it is approved it clears the oldest open invoices and bills for that amount through the clearing account, and shows in both ledgers. **Statement** prints what was cleared; **Reverse** undoes it once, on or after its date.

### 9B.1 Raising a journal voucher (accruals, corrections, opening balances)

For an entry no other screen makes — a month-end accrual, a correction, an opening balance — use **Financial Accounting → Journal Vouchers**.

**Who can raise one (57.26(c)).** A role that can post to the accounts (the **finance.post** permission: Accountant, Accounts Payable, Accounts Receivable, an administrator). Anyone else is refused with **Journal vouchers are saved by a finance role.**, from the screen, an import or the API.

1. The screen opens on the list of vouchers. Click **+ New Journal Voucher** for a blank voucher. Enter the **Date** and a **Narration** an auditor will understand — the voucher is booked on that date, so one dated last month shows in last month's reports even when it is approved today. **Cost Center** and **Department** are optional — pick the store's cost center when the voucher is about one store (rent, a store's electricity); a voucher has no store of its own, so one left blank stays under *Unassigned* in the Cost Center P&L. Sales, purchases and the other store documents carry their store's cost center by themselves. (**Clear** in the bar starts the voucher again.)
2. Add one row per account with **+ Add line**: pick the **Account** (type its name or code), then a **Debit** or a **Credit**. Amounts take paise (up to two decimals, for example 17.70); a third decimal is refused (FIN-0843). The **Debit** and **Credit** totals in the bar at the top must match — it says **Balanced** — before you can save. The same rule holds when vouchers arrive by import or the API: unbalanced, two-sided or accountless lines are refused (FIN-0843), and the voucher's total is always the lines' own sum.
3. **Save as Draft** — you're back on the list — then click **Submit for Approval** on its row. An approver — not you — decides on **Approvals** (§10); approval is what posts it to the accounts.
4. A posted voucher is never edited. **Reverse** creates a new voucher with every debit and credit swapped, and it goes through approval like any other. If a voucher was approved but its posting failed, its row shows **Retry Post**.
5. **Line details.** Click **Details** on a line to give it its own **Cost centre**, **Department**, **Store**, **Employee**, **Tag** or **Narration**. A line left empty takes the voucher's cost centre and department, and reports that group by them read the line's value. A cost centre or department that has been suspended is refused on a new line; vouchers already posted keep it. If your admin has set a required dimension for an account range (ADMIN_GUIDE, Journal dimension rules), a line on those accounts without it is refused and says which line. Line details are not available on a voucher in another currency.
6. **Templates.** **Save as template** keeps the lines you have typed under a name, either as fixed **Amounts** or as **Percent of an amount**. **Use template** fills a blank voucher from one (for a percent template it asks for the total). **Copy last month** lists last month's vouchers and fills this one from the one you pick, dated one month later. A template never posts by itself: you still save, submit and approve the voucher. The list also shows the recurring templates, with their frequency and next run date.
7. **FX revaluation and Intercompany entry** are buttons above the list. In **FX revaluation** pick the **As of** date and **Preview** to see the change per open foreign-currency document and the net gain and loss; **Post** books it after you confirm. **Intercompany entry** takes the date, narration, the two entities with their accounts, and the amount; it saves as a Draft, then offers to send it for approval. Both need the Finance posting role.

### 9B.5 E-invoices (IRN): the workbench

For a business whose GSTIN e-invoices (your admin switches it on — ADMIN_GUIDE B.3.17). Every B2B sales invoice and customer credit note then needs an **IRN** from the government's Invoice Registration Portal (IRP) before it is valid; the ERP gets it for you. Sales to customers without a GSTIN (B2C) are never sent. A **B2B bill from the till** (§4.10a) is sent like a sales invoice.

1. Post the invoice or credit note as usual. Within a minute it appears in **Financial Accounting → E-Invoice (IRN)** under **Needs action** (or goes straight out if your admin set it to send automatically). **Check now** picks up what you just posted at once.
2. **What to do** says what each one is waiting for. Click **Open**:
   - **To fix before it can be sent** lists each problem with the field and the fix — typically an item without a 6-digit HSN, an item whose unit is not a GST unit (PCS, NOS, KGS …), a customer without a PIN code, or a number longer than 16 characters. Fix it on the record named (the item, the customer, Prefix Configs), come back and press **Validate**.
   - **Submit** sends a valid one. Its state becomes **IRN generated**, with the IRN, Ack No, Ack Date and the signed QR.
   - **Rejected** — the IRP refused it; the reason is shown. Fix, **Validate**, **Submit** again.
   - **Unknown** — the IRP did not answer. The ERP checks whether it was registered before sending again; **Check status** asks now. Do not make it on the portal while it is Unknown.
3. The **Sales Invoice** and **Customer Credit Notes** lists show each document's IRN state next to its status. A B2B till bill is listed here under its bill number; **Reprint** in **POS → Sales History** prints its IRN and QR once it has one. **Print** on an invoice with an IRN shows the IRN, Ack and the QR; one still on its way prints **IRN PENDING**. A B2C invoice prints as before.
4. **Cancelling an IRN** — only within 24 hours of the Ack, and only the Accountant: open it → **Cancel IRN** → reason code and reason → **Send for approval**. Another Accountant approves it under **Approvals**, then it is cancelled at the IRP. The invoice number can never be used again. After 24 hours, issue a credit note instead (the record shows *Manual review*).
5. Once it has an IRN, the invoice's customer, store, lines and amounts cannot be edited (the save says why).
6. **When the IRP is down** — see USER_SOP §11.1 (make it on the portal, then **Enter portal IRN**, verified by a second person).

Reports → Finance: **E-Invoice / IRN Status Register** (everything, with IRN and Ack), **E-Invoice Error / Retry Report** (what was refused or is being retried, with what to fix), **IRN Portal Manual Upload Report** (what to report on the portal by hand).

### 9B.6 E-way bills (Stage 130.16)

**Financial Accounting → E-Invoice (IRN) → E-Way Bills** shows every delivery challan, sales invoice and stock transfer of the last 30 days once it is approved, with whether it needs an e-way bill and why: goods worth the threshold or more (₹50,000 unless your administrator set another), or job work going to another state whatever the value; services and HSN codes your company is exempt for are left out. A sales invoice's value includes its GST, as the rule counts it: goods of ₹48,800 at 3% are ₹50,264, so they need a bill.

- **Required** lists the ones still to make. **Generate** (when your company has an e-way bill provider connected) asks for the transport - **Mode**, **Vehicle no** or the **Transporter ID**, and the **Distance (km)** - checks everything the bill needs first (the store's GSTIN and PIN, the receiver's state, HSN on every line, a document number of at most 16 characters, the distance and the vehicle) and says exactly what to fix. A bill is valid a day per 200 km, until midnight.
- **Until a provider is connected**, make the bill on the government e-way bill portal and use **Record from portal**: its 12-digit number and valid-until date and time. The register and the expiry watch then know it.
- **Generated** bills: **Vehicle** changes the vehicle (Part B) on the way; **Extend** works within 8 hours before or after the bill expires, for the distance still to go and a reason; **Cancel** works within 24 hours of making it, with a reason. **Expiring** lists the bills inside that 8-hour window; a bill past its time is marked **Expired**.
- **Failed** shows a bill the provider refused, with its reason; fix it and **Generate** again. A bill that timed out is looked up before it is ever sent again, so it is never made twice.

**Reports → Inventory → E-way bill register** lists every document's decision, bill, validity and vehicle.

> A document number longer than 16 characters (a long store code makes *DC/STORECODE/2627/00001* too long) cannot carry an e-way bill or a GST invoice number: keep store codes short, or have your administrator shorten the series.

### 9B.7 Cash position (176.1)

**Reports → Finance → Cash position** (R5173) shows the cash you hold and what is about to move. Set **As of** (blank is today) and run it.

- **Cash and bank**: the balance on that day of each cash account (1100-1102) and of each active bank account's ledger account, then the total.
- **Receivables due** and **Payables due**: what customers still owe on open invoices and what you still owe on open supplier bills, by due date: **Now** (due already, or no due date), **Week 1** to **Week 4**, and **Later**.
- **Position**: the cash plus what comes in, less what goes out, carried week by week. **Total** is where it ends.

The amounts show only to users who see finance figures.

## 9C. Service — maintenance contracts and service tickets

Use this when you service equipment for customers: a customer calls, someone is sent, the fix is recorded.

**A maintenance contract (AMC), if the customer has one.** **Setup → Service Contract → + New Service Contract**. Enter the **Customer**, the **Asset** it covers (optional), the **Start** and **End Date**, and **Visits Included**. Leave **Visits Used** alone — the system counts it. Status **Active**. The contract number is generated on save.

**Log the call.** **Setup → Service Ticket → + New Service Ticket**:

1. The **Customer**, and the **Asset** if it is on the register. The **Ticket Number** is greyed out: it is given from the **TKT** series when you save (`TKT/HO/2627/00001`, 57.26(b)).
2. **Description** — what is wrong, in the customer's words — and **Priority** (Low, Medium, High, Critical).
3. **Respond By** and **Resolve By** — the dates you promised. A ticket past either date appears on the **Service SLA Breaches** report (§9).
4. **Service Contract**, if the visit is covered by one.
5. **Status Draft**. Leave the technician, resolution notes and cancellation reason empty — the buttons below fill them in. Save.

**Work it.** Each step is a button on the ticket's row; only the next step is offered:

| Status | Button | What it asks |
|---|---|---|
| Draft | **Assign** | The technician's username |
| Assigned | **Start** (or **Reassign**) | — |
| In progress | **Resolve** | What was done. A ticket cannot be resolved without it. |
| Resolved | **Close** | A confirmation. If the ticket has a contract, one visit is used from it. |

Until it is resolved, **Cancel** is also offered, and asks why. A closed or cancelled ticket has no further steps. Only these buttons move a ticket: the form's **Status** list does not offer their steps, and a save that sets one is refused (49.5.3, 57.26(a)). Maintenance orders (In progress, Completed), delivery challans (Dispatched, Returned, Closed) and vendor rebate claims (Settled) work the same way.

**The service desk.** **Setup → Service Ticket** opens with a **Service desk** above the list: every open ticket, overdue ones first, then by priority and Resolve By date. **Mine and unassigned** shows your tickets plus any nobody has taken; **All open** shows everyone's. Each row shows the customer, priority, status, technician, a **Due** badge (*Response overdue*, *Resolution overdue*, *Due today*), the contract with its coverage (*Covered - 2 visit(s) left*, *Ended …*, *All visits used*) and a **Next step** button (Assign, Start work, Resolve with notes, Close) that runs the same step as the row button below. Click a ticket number to see its status history — each change, when and by whom.

**A contract's tickets.** **Setup → Service Contract** opens with **Contract coverage**: pick or type a contract number and **Show** — whether it covers work today, visits used of included, its dates, and every ticket raised under it.

**A project's cost and revenue.** **Setup → Project** opens with **Project cost and revenue**: pick a project and **Show** — revenue, expense and net from everything posted with that project, and each source document behind the figures (Journal Vouchers and expense claims carry a Project field). An inactive project's history still shows. Timesheets, milestones and the project budget are in §9E.

**Field visits (176.4).** Press **Ctrl+K**, type **Service Visit** and open it, then **+ New**:

1. The **Ticket** (an open one), the **Engineer** and the **Visit date**, optionally a **Time**. Status **Scheduled**. Save.
2. On site, set status **Checked in** and save: the time is stamped.
3. When the work is done, fill **Work done**, **Parts used** and **Signed off by** (the customer's name), set status **Done** and save: the check-out time is stamped. Done needs a check-in and the sign-off name, and a Done visit cannot be changed.

If you log a visit afterwards, type the **Checked in** and **Checked out** times (`2026-11-14 10:30`) and they are kept.

**Help articles (176.5).** **Setup → Service Article** holds how-tos and known fixes: **Title**, **Category**, **Keywords** and the **Article**. Status **Draft** while it is written, **Published** when others may use it (a published article needs its text), **Archived** when it is out of date. The list's search reads the whole text. On a service ticket, **Help article** names the article that solved it; only a published one can be picked.

## 9D. CRM — customers, consent, campaigns, journeys, service and the sales pipeline

**CRM** in the sidebar opens the CRM workspace. Its tabs: **Overview · Customers · Segments · Templates · Campaigns · Journeys · Messages · Inbox · Tickets · Feedback · Worklist · Pipeline · Settings** (Settings only for those who manage CRM). The CRM menu also keeps **Loyalty**, **Vouchers** and **Service Tickets** (the older screens, still working). Browser **Back** and **Forward** move between CRM pages as you would expect.

**Overview** shows customers, how many can be reached on each channel, messages this month, running campaigns and journeys, NPS, open tickets and tasks, unread inbox threads and the weighted pipeline. Click a tile to open its tab. **To finish setting up** lists what is still missing (for example no SMS sender), and **Customers by lifecycle** splits customers into *New, Active repeat, Reactivated, At risk, Lapsed* and *Never bought*. Click a bar to open those customers.

### 9D.1 The customer page

1. **Customers**: type a name, code or phone. If your role does not show full phone numbers, type the **whole** number to find someone. Open a customer to see one page with:
   - the lifecycle, RFM group and tier;
   - bills, spend net of returns, average bill, last bill, points (and any *pending*), and open tickets and tasks;
   - **Profile**: phone, email, birthday, anniversary, gender, sizes, preferred store and salesman, tags, city, referral code. A field filled by an import shows where it came from when you hover over it;
   - **Consent**: for SMS, WhatsApp and email, whether they agreed to **Offers** and to **Service messages**, and the reason when they cannot be reached;
   - **Notes**, **Wish list** and the **Timeline**: bills, returns, online orders, invoices, points, vouchers, every message with what happened to it, tickets, notes, consent changes, feedback, appointments, tasks and journeys, newest first. **Show** filters it; **Show older** loads more.
2. **Edit profile** changes the profile fields. **Send a message**, **+ Task** and **+ Appointment** start those for this customer. **Give a referral code** makes their refer-a-friend code.
3. **Masked contacts.** Roles not listed in Configuration → CRM → *who sees full phone numbers* see phone and email as `91******99` everywhere: page, search, reports, exports. Saving a profile with a masked number keeps the real one.
4. **Possible duplicates** (Customers tab) lists customers who look like one person (the same email, or the same name and birthday - one phone number is always one customer). Pick a pair, choose **Keep A** or **Keep B**, then **Merge**. The preview shows what moves: bills, points, consent, wish list, active journeys. If your business has an approval rule for *Customer Merge*, the merge waits in Approvals; otherwise it happens at once. A customer who was waiting in a journey gets at most one copy of the next message.

### 9D.2 Consent: who may be messaged

- **No recorded consent, no offer.** An offer goes only to a customer who agreed to **Offers** on that channel. **Service messages** (about their bill, order, appointment or feedback) need no tick, but a customer who stopped them, or stopped everything, gets none.
- **Record what the customer said** (on their page): the channel (or **Every channel (stop all)**), what for, **Agrees / Withdraws / Says no**, and how you know (for example *"Said yes at the counter"*), then **Record**. Every change is kept, with who recorded it and when. **History** shows them all. The ticks on the Customer record follow the latest answer.
- **The customer's own link.** Offers carry a short link to the customer's **preferences page**, where they choose per channel (offers / service), **Stop all messages**, or **Start messages again**. Replying **STOP** to an SMS or WhatsApp works too, and so does the unsubscribe link in an email.
- **An import never gives consent back.** A file or Shopify cannot re-tick someone who said no. Only the customer can, or staff recording what they said.
- **Erasure.** When a privacy erasure is approved, the customer's queued messages and journeys stop, their details are removed, and their phone and email are kept only in a scrambled form so an import or Shopify cannot bring them back. Files attached to the customer are deleted, and saved views that carry their id, phone or e-mail are removed. Logs, the audit trail and backups are not rewritten; they age out by their retention periods.
- **Who opens these.** An admin opens a customer's erasure, correction, export or consent-withdrawal request from the **Privacy** button on the customer's row; a second admin approves it (admin guide C.10).
- **Correction.** An approved correction request writes the corrected name, contact, address, PAN, GSTIN or birthday to the customer, and the audit trail records which fields changed.

### 9D.3 Segments and uploaded lists

**Segments → + New segment.** Give it a **Name**, then build the rules: **All of these** or **Any of these**, **NOT (exclude these)** to turn a group around, **+ Rule** and **+ Group** for nested groups. The rules can use profile fields, spend, visits, last purchase, category or brand bought, store, tier, birthday month, consent, and bought / did-not-buy in the last N days. Days are counted in your business's time zone.

- **Who is in it now** counts as you type. Choose **For a message by** a channel to see how many of them can be reached on it (the count loses those without consent or a contact).
- **Why is a customer in it?** Pick a customer and **Check** to see each rule ticked or crossed.
- **Save** keeps a numbered version. Editing saves a new version, and a campaign remembers the version and the exact list of customers it used.

**Uploaded lists** (Segments → **Uploaded lists**): upload a CSV with a phone column, say where the numbers came from and **Use until** a date. A list never gives consent: everyone on it is still checked when a message goes, and an expired list matches nobody.

### 9D.4 Message templates

**Templates → + New template**: **Name**, **Channel** (WhatsApp, SMS, Email), **For** (*Offers*, which need consent, or *Service*), **Language** and the **Message**. Click a chip under the message to insert a value, such as the customer's first name, points, voucher code, item, appointment time or preferences link.

- `{{customer.first_name|there}}` uses *there* when the name is blank. A value with no fallback that is blank stops that one message (reason *A value the template needs was blank*, naming the value) instead of sending a gap.
- **SMS** needs the **DLT template ID**, and the text must match the template registered with DLT. The counter shows how many SMS parts it takes.
- **WhatsApp** offers and first messages need a template approved in WhatsApp Manager: type its **WhatsApp template name** and **language code**, then **Check approval with WhatsApp**.
- **Preview** shows it for a customer you pick, or with sample values. **Save** keeps a new version. Campaigns and journeys keep the version they were approved with.

### 9D.5 Campaigns

1. **Campaigns → + New campaign**: **Name**, **Send by**, **To segment**, **Message** (offer templates for that channel), **Start** (blank = as soon as approved), **Stop sending at**, **Budget** (blank = none), **Count a bill within (days)**, **Hold-out group %** and, if you like, a **voucher for each customer** (% or amount off, valid for N days, store / web shop). Each customer gets their own single-use code (`{{voucher.code}}` in the message). **Save draft**.
2. **Pre-send check** shows the count step by step (in the segment, minus no consent, minus no contact, minus hold-out), the estimated cost against the budget, and anything that is not ready.
3. **Send a test** sends it to one customer who is on the test list.
4. **Submit for approval.** It waits in **Approvals** for a store manager, or for whoever your approval rule names. When the second-approver switch is on, a campaign is never approved by the person who submitted it. **Changing anything after submitting sends it back to Draft**, and it must be approved again.
5. Once approved, the run starts at its time. Under **Runs**: **Pause**, **Resume**, **Stop**, and **Results**: matched, held out, not sent (with the reason for each), sent, accepted, delivered, read (blank when the channel does not report it), vouchers issued and used, bills and sales credited, and cost. A bill counts for the campaign if the customer bought within the days you set after the last offer they got, once per bill, less returns. With a hold-out, **Results** compares buyers who got it with those held back; with too few customers it says *Not enough data yet*.

Every message goes through the same checks before it leaves: consent, contact limits (by default at most 2 offers a day and 5 a week, 4 hours apart), quiet hours (21:00–09:00 by default; an offer waits until morning), budget, and the test list on a test server.

### 9D.6 Journeys — messages that send themselves

**Journeys → Add the ready-made journeys** adds: *Welcome*, *Thank you for the first bill*, *Birthday*, *Anniversary*, *Points about to expire*, *Win back (not seen for 90 days)*, *Product arrived* and *Cart left on the web shop*. Each comes switched **Off**.

1. Open a journey to see **Starts when** (an event such as a first bill, or a date such as the birthday with **Days before**), **Leave the journey early when** (for example *they buy*), **Do not start again for (days)**, and the steps: **Send a message** (channel, template, offers or service, optional voucher %), **Wait**, **Wait for something to happen**, **Check (yes / no)** with a step for each answer, **Create a task for staff**, **Give points** (a number of points for the birthday or anniversary, once a year per customer, never twice; blocked members get none) or **End**.
2. A WhatsApp step needs an approved WhatsApp template (*not ready* in the list until then).
3. **Save draft**, **Publish** (a new version — customers already in it finish the version they started), then **Switch on**.

A customer is in a journey once at a time. **Customers in it** shows who is where and why they left.

### 9D.7 Messages — what was sent and why not

**Messages** lists every message: when it was queued, customer, where it came from (campaign, journey, staff, feedback, reminder, test, automatic reply), channel, status, **Why**, masked contact and cost. Filter by **Status**, **Channel**, **From** and **Reason**.

- **Delivered / Accepted / Read**: it went. **Deferred**: waiting (quiet hours or the gap between offers) — *Why* says when it goes. **Suppressed / Expired / Cancelled / HeldOut**: not sent, with the reason (no consent, stopped, limit reached, budget used, test list...).
- **Unknown**: the provider did not answer, so the system cannot tell if it went. It is **never resent by itself**. Check with the provider, then **Settle** it as delivered, accepted or failed, with how you know.
- **+ New message** sends one message to one customer: a template, or your own text (WhatsApp only within 24 hours of the customer's last message; never by SMS).

### 9D.8 Inbox (WhatsApp replies)

Replies to your WhatsApp number arrive in **Inbox**, one thread per number, linked to the customer when the number is known. **Take over** to answer yourself (automatic replies stop), **Hand back** when done, **Make a ticket** to track a problem, **Close** the thread. A reply within 24 hours of their last message can be your own text; after that, choose a template. A customer writing to you is never taken as consent to offers. *STOP* from them stops messages.

### 9D.9 Tickets

**Tickets → + New ticket**: customer, bill (optional), priority and what they need. A ticket moves *New → Open → Waiting (on customer / internally) → Resolved → Closed*. Use **Assign to me**, **Waiting on customer**, **Waiting internally**, **Resolve**, **Close** and **Reopen**. **Respond by** and **Resolve by** count working hours only and show *Late* when passed. Notes can be private. Add a note **As** a private note (never sent) or as a reply by WhatsApp, SMS or email - a reply goes through the same checks as every message. Every ticket shows on the customer's timeline.

### 9D.10 Feedback and referrals

**Feedback → + New survey**: **Question**, **Scale** (0–10 NPS or 1–5 rating), **Send by**, the template (it must contain the feedback link), **Hours after the bill**, **Not again for (days)**, and **A score this low or lower opens a ticket**. Tick **On - ask after every bill**: after a paid bill the customer gets one link; their answer appears here with NPS and the number of answers. A low score opens one ticket. Changing the question starts a new version.

**Referrals** (same tab): when a new customer brings a friend's code, **Record** it with **New customer (the friend)** and **Referral code they gave**. The referrer's points are given once the friend's first bill is past the return window. Self-referrals and the same phone are flagged for you to **Allow** or **Cancel**.

### 9D.11 Worklist: tasks, appointments, holds

**Worklist** has three lists:

- **Tasks.** **+ Task** for custom orders, arrivals, alterations, follow-ups and calls, with a due time and owner. The list shows **Whose** (mine and my store's, or everyone's) and **Status**. Mark one **Done** with the outcome. Tasks also appear in your task inbox and the store app's **My tasks**.
- **Appointments.** **+ Appointment**: customer, store, kind (visit, trial, fitting), day, then **Show free slots** and **Book**. Slots and capacity per store are set in **Settings → Appointment hours at a store**. Reminders go before the appointment; cancelling stops them. **No-show** marks a missed one.
- **Hold requests.** **+ Hold request** asks a store to keep an item for a customer. The store **Confirm - kept** or **Decline** (with a reason). **Tell the customer** sends the "ready" message, and only after it is confirmed.

### 9D.12 Pipeline — business buyers

1. **Leads** (Pipeline → Leads, **+ New lead**): name, company, phone, email, source, owner. Leave **Owner** blank to let the assignment rules give it to the matching person with the fewest open leads, or leave it unassigned. Mark it **Contacted** or **Qualified**, **Disqualify** it with a reason, or **Convert** it: that makes the customer and a deal once, even if you click twice.
2. **Deals** (board): each stage shows its deals, count and value. Open a deal to change value, chance and expected close, add **+ Call, meeting or task**, and **Move** it. A stage can require a value, a close date, a quotation or an owner. **Won** needs an approved quotation: build it on the deal (**+ Line**, terms, valid until), **Save and send for approval**, and when it is approved, move the deal to Won. That raises one Sales Order (**Raise the Sales Order** if it did not). **Lost** needs a reason. Every change is kept in the deal's history.
3. **Forecast**: weighted pipeline, committed and booked shown apart. **Take a snapshot** keeps today's figures unchanged for comparison later.
4. **Stages and assignment** (CRM managers): stage names, chance %, what entering each needs, which count as committed, and the lead assignment rules.

### 9D.13 CRM reports and dashboard

**Reports → CRM** adds: CRM Customer Summary, Consent Audit, Segment Members, Campaign Summary, Message Detail, Journey Performance, Credited Sales, RFM and Lifecycle, Coupon Use, Service SLA, Clienteling, Feedback, Message Cost and Sales Pipeline. Each says which time zone and date it counts by. The Dashboards screen can chart CRM messages, consent, feedback and pipeline.

**Customer numbers and readiness (140.12).** **Customers** is a tile on the main dashboard (**Customer pulse**, R6075: active customers, new this month, repeat rate, average bill, lapsed high spenders). **Customer affinity and next best offer** (R6076) gives each customer's favourite category, its share of their spend and the category's best seller they have not bought yet. **Suggested segments** (R6077) lists segments worth building, how many customers are in each today and the rule to build it with. **CRM -> Settings -> Ready to send** checks each channel: an active sender with nothing missing, a ready template (Meta-approved for WhatsApp, with its DLT template ID for SMS) and the test allowlist. On a sender, **Fill from** fills MSG91, Gupshup or Twilio into the SMS gateway's parameter names (check them against the provider's own documentation), and **Test connection** asks the provider - WhatsApp looks up the phone number, email logs in, SMS sends one message to a number on the test list.

### 9D.14 Campaigns made before this version

Birthday and Lapsed Customer campaigns set up earlier (**Setup → Campaign**, with a Trigger Type) keep working as before and show as *Older trigger campaign* under Campaigns. **Reports → CRM → Campaign ROI** still reports them. The ready-made **Birthday** and **Win back** journeys replace them: when you switch a journey on, set the matching older campaign to **Inactive** so nobody gets both.

## 9E. People — HR, leave, payroll and expense claims

Each HR page is its own entry in the **HRM** menu, grouped by who does the work: **My work** (My Requests — your own leave, expense claims and grievances), **Team** (Attendance, Leave, Shift Roster) and **HR administration** (Payroll, Payroll Export, Loans/Advances, Onboarding, Appraisals, Training, Grievances). You only see the pages your role can open; Payroll, Payroll Export and Loans need the payroll permission.

**Applying for leave** (HRM → Leave → **+ Apply Leave**, or HRM → My Requests → **+ Request Leave**):

1. Choose the employee (on My Requests it is you), the **Leave Type**, **From** and **To**. **Days** fills itself in — calendar days, counting both ends.
2. Under the form you see the balance for that type this year — for example "Casual 2026: 12 days a year, 3 approved, 9 left (1 awaiting approval - up to 8 more can be applied for)": *left* drops when leave is approved, and days awaiting approval are held back from a new application — and a warning if the dates overlap leave already on the list. You only ever see your own balance unless you are HR or a store manager.
3. Save. The server still checks the balance and overlaps, so a request over the limit is refused with the reason.

**Grievances.** Raise your own from My Requests → **+ Grievance**; it goes straight to HR and you follow its status there and under HR administration → Grievances (*With HR*, then *Resolved - actioned* or *Closed - no action*). Who can read it is printed on the form and above the case list, taken from the rules the server enforces: what was raised and how it was resolved — Admin and Super Admin only; a Store Manager sees that a case exists (number, employee, category, status) but not what it says; everyone else sees only their own cases, and reads what their own case says and its resolution (shown under it in My Requests). A Super Admin opens a case, writes the **Resolution Notes** (required) and chooses **Resolve - actioned** or **Close - no action**; **+ Grievance** on the case list raises one on an employee's behalf.

**Onboarding / Offboarding** lists each employee's checklist with its progress (*2 of 4*). **+ New checklist**: choose the type and employee (the code is suggested), type the steps or **Copy steps from** an earlier checklist of the same type, and save. **Open** a checklist to tick steps — each ticked step shows who ticked it and when — and add a step or a document link. When every step is ticked the checklist is *Completed*. Employees see their own checklist, read-only.

**Appraisals**: choose the **Appraisal cycle** (cycles and their KRAs are set up under **Manage cycles**). Every active employee you can see is listed as *Not started, Draft, Submitted for review, Reviewed* or *Returned for changes*. **Start** opens the appraisal with the cycle's KRAs filled in: enter the self ratings (1-5), then **Submit for review**. The reviewer — a Store Manager, Admin or Super Admin, never the person who submitted it — enters manager ratings (**Overall Rating** fills with the weighted average, which you can change) and comments, then **Approve review** or **Return for changes** with a reason; a returned appraisal is corrected and submitted again.

**Training**: choose the **Training program** (set up under **Manage programs**). Each employee shows *Not enrolled, Enrolled - due, Completed* or *Failed*. **Enrol** one, tick several and **Enrol selected**, or use **+ Enrol employee**. **Record result** sets Completed or Failed, the date and an optional score; someone who failed can be enrolled again.

**Running payroll** (HR administration → Payroll → **+ Run Payroll**): the period defaults to last month. Days marked **Absent** and approved **Unpaid** leave are loss of pay: the salary is paid for the remaining days of the month (a day with nothing recorded counts as worked). Each payslip in the list shows gross, the deductions (PF, ESI, PT, TDS, loan — hover for the amounts) and net pay. **Post to GL** posts one payslip. **Reconcile payroll** opens the **Payroll Reconciliation** report: for the pay periods you choose it sets each figure on the payslips beside what the ledger holds (gross on 5500, PF, ESI, PT, TDS, loan recovery, net pay on 2400), checks gross − deductions = net, then shows the salary paid out of 2400, how much of that payment is matched to the bank statement, what is still owed, and any payslips not yet posted. **View Details** on any line lists the payslips or GL lines behind it. Salary is paid with a Journal Voucher debiting 2400 Salary Payable and crediting the bank; match that bank line in Bank Reconciliation (§9B.3).

**Sales incentives** (when your admin has turned them on). Each salesman earns a share of the margin on their sales, accrued when the bill posts and taken back in the same split when the goods are returned (at the till or by a credit note on the return) or the bill is cancelled. **Run Payroll** adds what is owed to the salesman's payslip as *Incentive pay*; a return after payout is carried as a negative balance and comes off the next payslip. In the store app, **My earnings** shows only your own balance and entries.

**Tax, benefits, salary revisions and calibration** (HRM, payroll permission except Calibration). A CA must confirm every statutory figure (tax slabs, LWF, PF wage ceiling) before you rely on it or file a return.

- **Tax & Benefits** page: buttons open the lists for **Tax Slab**, **Tax Regime Rule**, **LWF Rate**, **Benefit Plan**, **Benefit Enrolment** and **Salary Revision**; below them download the **PF ECR** text file for a month, the **Form 24Q** CSV for a quarter (annexure I; annexure II with the full-year tax computation for Q4) and list the **arrears** of an approved salary revision.
- **Tax regime:** on the employee, pick **New** or **Old** (default New), the PAN and UAN, and for Old the declared deductions. When your admin turns on *Salary TDS by tax slab*, each payslip's TDS is the projected yearly tax divided by the months left in the year, and the payslip shows the regime.
- **Benefits:** create a **Benefit Plan** (type Earning, Deduction or Employer-paid; a fixed amount or a percent of basic), then a **Benefit Enrolment** with its start and end dates. Run Payroll puts each enrolled benefit on the payslip: earnings add to gross, deductions come off net, employer-paid shares cost the company only. All post with the payslip. The printed payslip lists each earning and deduction benefit and the **Labour Welfare Fund** as their own pay heads; employer-paid shares are not printed.
- **Salary revision:** **+ New** under Salary Revision with the employee, the new amounts, the effective date and the reason, then **Submit for Approval**. Once Approved, payroll uses the new amounts for every month from the effective date, whole months (no part-month split). Months already run are not changed or paid: **Arrears** lists what each would have been, for you to pay by a normal payslip adjustment.
- **Calibration** (HRM → Calibration): choose the cycle to see how many appraisals sit at each rating 1-5 by department, before and after calibration. For an approved appraisal enter the new rating and a reason, then **Calibrate**. Only HR or the cycle's owner can; the original rating stays and each change is on record.

**Expense claims** (HRM → Expenses → **+ New Expense Claim**):

1. The claim starts filled in with you as the employee, the store you are signed in at, and today's date. The date can be today or up to 90 days back.
2. Enter the **Amount (before GST)**, the **GST on the bill**, any **Advance already received** for this expense, the **Bill / receipt no.** and the **Purpose**. The line under the form shows what will be reimbursed: amount + GST − advance. GST cannot be more than the amount and the advance cannot be more than the bill. The same employee, category, date and amount twice is refused as a likely duplicate bill. Attach the receipt itself under **Receipt photo / PDF**: a photo or scan as JPEG, PNG or WebP, or a PDF, up to 5 MB each and 5 per claim. The claim saves first and the files upload straight after, so if one is refused, what you typed is kept; attach it again from **History**.
3. **Create Draft**, then **Submit for Approval**. The list's cards show how many claims, and how much, wait for approval, for finance verification, for payment and for correction.
4. **History** on a claim shows the steps it has passed (Draft → Pending Approval → Approved → Verified → Paid) and every approver comment — a rejection always carries its reason.
   - **Receipts.** Under the claim number the list shows a paperclip with the number of files, or **no receipt**. **History** lists the receipts: **View** opens a photo on the page and saves a PDF to your computer. While a claim is **Draft** or **Rejected**, whoever added a receipt can **Remove** it and add more (**Attach**). Once it is submitted the receipts are fixed; ask your Admin if a wrong file must come off. Anyone who can see the claim (you, your store's manager, Finance) can see its receipts; nobody else can. A file that is not really a picture or PDF (for example a document renamed to .jpg) is refused, and the same file cannot be attached twice to one claim.
5. A **Rejected** claim offers **Correct and resubmit**: the form opens with what you entered; fix it, save (it returns to Draft), and submit it again.
6. Finance clicks **Finance Verify**, then **Mark Paid**. Payment posts the expense and GST, pays out only what is still owed after the advance, and clears the advance from 1600 Staff Loans & Advances. A claim can be paid once.
   - **Limits (Ask 170.29).** A category with a limit (and a limit for your grade) is checked when you save. Either the claim is refused with the limit shown, or it goes up to a senior approver. Your administrator picks which.
   - **Advances (170.30).** Press **Ctrl+K**, type **Staff Advance**, then **+ New**: employee, date, amount, purpose, then **Pay advance**. Where your administrator has turned netting on, **Mark Paid** on your claim first clears what you still hold from advances and pays only the rest. The **Employee advance balance** report shows what is open per person.
   - **Travel requests (176.2).** Before a trip, press **Ctrl+K**, type **Travel Request**, then **+ New**: employee, purpose, destination, **From** and **To**, the **Estimated cost** and any **Advance needed** (not more than the cost). Save, then **Submit for Approval**; a Store Manager approves it. An approved request cannot be changed. The advance and the claims for the trip name it in **Travel request**; only the same employee's approved request can be named. **Reports → Expenses → Travel requests** (R5174) shows each trip's estimate, the advances paid, what was claimed and what is left.
   - **Corporate cards (176.3).** Finance keeps each company card under **Setup → Corporate Card**: a name, the **Card holder**, the **Last 4 digits** (never the full number) and the issuer. Statement lines go in **Card Statement Line** (Ctrl+K): type one, or **Import** the bank's statement as CSV with the columns *card*, *txn_date*, *description*, *amount*, *reference*. Each line is matched to the holder's claim of the same total (amount + GST) dated within 7 days, if there is exactly one. You can also type the **Claim** yourself; it must be the holder's, for the same total, submitted, and not on another line. **Reports → Expenses → Card lines not matched** (R5175) lists the rest, with the claims that could fit.

**Timesheets (Ask 170.31).** Finance -> Timesheet -> **+ New**: employee, project, date, hours (up to 24) and task, then **Submit for Approval**. Once approved the cost (hours x the employee's cost rate) is booked to the project and the entry can no longer be changed. A missing cost rate on the employee stops the submit.

**Project milestones and budget (Ask 170.32).** On **Project** set contract value, estimated cost and revenue method. Add **Project Milestone** rows (amount or percent of contract value, billable Yes or No) and one **Project Budget** with a line per cost type. Open a milestone and click **Complete milestone**: a billable one raises a Draft sales invoice (once, even if clicked again). **Project budget vs actual** shows each cost type against the ledger.

**Project revenue (Ask 170.33).** Where turned on, the month-end run books revenue earned against what was billed, as unbilled revenue or deferred revenue, once per project and month. Each month is listed under **Project Revenue Entry**.

**Service ticket clock (Ask 170.34).** Where turned on, an open service ticket shows **Respond by** and **Resolve by**. A ticket that passes either time is escalated once: the ticket shows **Escalated at** and **Escalated to**.

## 9E-2. People: positions, hiring, discipline, teams and calendars

1. **Positions.** Open **Position** and add a title, department and grade, and the position it reports to. A position cannot report to itself, directly or through others. On an **Employee**, pick the **Position** and the **Manager** (another employee). The older **Designation** and **Reporting Manager** boxes stay and are pickers too: designation from the positions' titles, reporting manager from the employees (57.26(g)).
2. **Hiring.** Add a **Job Opening** (position, number of openings, location). Add each **Applicant** and move the stage: Applied, Screening, Interview, Offer, Rejected. Only HR and admins see applicants. Add a **Job Offer** for the applicant, then **HR > People > Accept offer** with the offer id. This makes the Employee (status Inactive, to be activated at joining), marks the offer Accepted and the applicant Hired. An opening with every place filled refuses another acceptance.
3. **Login for a new joiner.** When every step of an onboarding checklist is ticked, open **HR > People > Login from onboarding**, type the checklist id and a role, and click **Create login**. The employee needs an email. No password is made or shown: the person sets their own with **Forgot password** on the sign-in page. If joiner approval is on, the login waits for approval.
4. **Exit clearance.** Administrators may turn on that an open exit checklist holds the final payslip (see the admin guide). The payslip then says the exit checklist is still open until every step is ticked.
5. **Disciplinary case.** Add the employee, category, description, the investigator and the other people named. Only HR, admins and the people named on the case can open it or its attachments. It goes to HR for approval like a grievance.
6. **Certificates.** On a **Training Record**, fill **Valid until** and the certificate number. **HR > People > Certificates expiring** lists what runs out in the next days you type. The **Certificate Expiry** report (Reports, HR) is the same list with the employee's name, department and training, marked *Expired* or *Expiring*, to filter and export. HR is also told once, ahead of expiry.
7. **My team.** When administrators turn the team setting on, a manager sees the leave, attendance and appraisals of their direct reports and approves their leave only. Anyone else's leave is refused.
8. **People analytics.** **HR > People > People analytics** shows headcount, joiners, leavers and attrition by department, location or position. A group smaller than the privacy minimum is shown inside one **Other** row. Without the all-stores scope you see your own store.
9. **Holiday calendar.** Add a **Holiday Calendar** for a location: the weekly off days (for example Saturday, Sunday) and the holiday dates. Leave counts only working days, and an absence on a holiday or weekly off is filed as that. A location with no calendar counts every day, as before.

## 9F. Manufacturing and supply planning

### 9F.1 Supply proposals — what to buy, move or make (Stage 73.2.18)

**Manufacturing → Supply Proposals.** The planner's list of what a location needs, and the place to turn it into orders.

1. Choose the **Location** and **Propose from**: **Reorder points** (every stock item below its reorder point: how fast it sells × lead time + safety stock — an item's own *Reorder Point Config* wins over the lead time and safety stock on the screen) or **MRP** (manufactured items below their reorder point, and the raw materials their BOM would be short of). Click **Generate proposals**. Running it again refreshes the open proposals instead of adding duplicates. An item's *Reorder Point Config* may also set **Min Order Qty**, **Max Stock Qty** and **Order Multiple**: the suggestion orders up to the maximum, rounds up to the multiple and is never below the minimum; empty means as before.
2. Each row shows the item, where the need came from, what is available (under it, short labels for stock already coming - *+N PO*, *+N transit*, *+N inward* - and *-N waiting* for back orders; a PO already covering the shortfall stops a new proposal), what is needed, the **suggested** quantity and an editable **Quantity**.
3. Tick the rows to supply together and choose how:
   - **Purchase order** — pick the **Vendor**; one Draft PO is created for all the ticked items, priced at their cost (moving average, else the item's standard cost — check the prices before submitting).
   - **Transfer from another location** — pick **From location**; one Draft stock transfer is created.
   - **Production order** — one Draft production order per ticked item, on its active BOM (an item without a BOM is refused).
   Click **Approve selected**. The message names the documents; open them on Purchase Orders, Inventory → Stock Movement or Manufacturing → Production Orders to review and submit as usual. Approve one location at a time.
4. Rows you will not act on: tick them and **Reject selected** with a reason (in **Note / reason**, or asked for).
5. **Show** switches between open, converted, rejected and all proposals. A converted proposal shows the document it became, who decided and when; that document shows the proposals it came from. A proposal is converted once — approving it again is refused.

Choose **Back orders** in **Propose from** to send the order lines still waiting for stock to buying: one proposal per item for what is not already in stock, inward, on a PO or in transit. Approve them as a Draft PO as usual; nothing is raised on its own. The list itself is **Back orders** under Reports (Procurement), by item and location with the oldest wait (Stage 146.4.3).

### 9F.2 BOMs and production orders (Stage 55.12)

**Manufacturing → Production Orders.**

**A new BOM** (**+ New BOM**): enter the **BOM Code** and the **Parent Item** — the finished item it makes. Then fill one row per component: pick the **Item**, enter the **Qty per finished unit** and, if some is lost in making, the **Expected scrap %** (10 means 10% more is issued). **+ Add component** adds a row and **Remove** takes one out; blank rows are skipped. The same item cannot be on two rows (put the total on one) and a BOM cannot use the item it makes. Sub-assemblies, by-products, alternate/effective-dated BOMs, QC requirement and standard cost are set afterwards under Setup → BOM.

**A new production order** (**+ New Production Order**): choose the **BOM** (each one shows its code and the name of the item it makes, 57.26(e)), the **Quantity** and the **Location** the materials are taken from. As soon as all three are filled, the **materials check** shows each component's quantity per unit, the total **Required**, what is **Available** at that location and any **Short** quantity. A short component means **Issue Material** will be refused until the stock arrives — transfer it in (§7) or receive a purchase. Components whose item type is backflushed (packaging) say *as output is reported*: they are taken as finished units are reported, so they never hold up the issue. Click **Create Order**; it is saved as Draft either way.

**The order list** shows each order's BOM and what it makes, **Made / ordered**, the location, status and **Next step**:

| Status | Next step | Who | Buttons |
|---|---|---|---|
| Draft | check materials, then issue them | Planner / store | **Materials** (opens the same check under the row), **Issue Material** |
| Material Issued / In Process | report output | Shop floor | **Complete (Receive FG)** for the rest in one go, **Report Partial** for a batch, **Confirm Operation**, **Scrap** (with a reason), **Rework** |
| Completed, no actual cost | record the actual cost | Planner | **Record Actual Cost** |
| Completed | done | — | — |

Material is issued once: issuing again is refused. **Report Partial** receives that many finished units and backflushes their packaging; the order stays open until everything is made, and reporting more than the order quantity is refused. For an item tracked by serial or batch, it and **Complete (Receive FG)** ask for the **piece codes** or **lot** of what was made, so each carries its own cost (§6.15). You can leave them blank. A code already in use is refused (`FIN-0911`), and an item that keeps neither can't take them (`FIN-0912`). Quality inspections for an order are on **Quality Inspections**; work sent out to a job worker is on **Job Work** (§9F.2a).

**The BOM changed after the material was issued.** The completion that would close the order (**Complete (Receive FG)**, or the **Report Partial** that finishes it) is stopped: *the BOM was changed after this production order's material was issued*. To finish on the changed BOM, the screen asks **Reason to accept it** - type why (for example *cloth substituted, agreed with planning*) and OK: the change is accepted and the completion goes ahead. Cancel leaves the order as it is. Accepting needs permission to change Production Orders, and a reason; it is recorded on the order and in the audit log with who, when, the reason and each component's quantity per unit before and after. It cannot be ticked on the order's record form or set by an import.

**What a finished unit is worth.** Finished goods come into stock at the cost of the material they used: their share of what **Issue Material** took out (at those items' average cost), plus the packaging backflushed for them. The last completion takes whatever is left. By-products come in at no cost. So a production run does not change the stock's total value, and **Inventory Valuation** still agrees with the Inventory Control account. **Record Actual Cost** feeds only the cost variance report; it does not change stock value.

**Confirming an operation with quantities (170.10).** **Confirm Operation** asks for the operation number, then the **Good quantity**, **Scrap quantity**, **Minutes taken** and an **Employee code** (blank = you). Leave the good quantity blank to confirm the whole operation with nothing recorded, as before. An operation can be confirmed in several goes; good plus scrap across them can never pass the order quantity (*That is more than the order quantity*), and the operation counts as confirmed once it reaches it. **Confirmations** opens the list under the order: operation, good, scrap, minutes, who and when.

**Scrap and rework (170.11).** Where the administrator has turned on *Scrap and rework move stock and cost*, **Scrap** takes the quantity of that item out of the order's location at its average cost and posts the loss to Production Scrap Loss. **Rework** opens a new Draft production order for the quantity, on the same BOM and location, with **Rework of** naming the original; you issue and finish it like any order. Sending more to rework than the order quantity is refused. With the setting off, both only write the order's log.

**By-products (170.12).** A BOM by-product line can carry a **Cost share %**. When output is received, that share of the cost goes to the by-product's average cost and the main product carries the rest. The shares must add up to 100% or less. No share: the by-product comes in at no cost, as before.

**Work in progress (170.13).** Where *Work-in-progress accounting* is on, **Issue Material** posts the material cost to Work in Progress instead of leaving it in stock value; each receipt of finished goods moves its cost back to Inventory; the completion that closes the order sends anything left in WIP to Production Variance. Orders issued before the setting was turned on are not affected.

> **Whole units.** Stock moves in whole units. If a component's quantity per unit times the order quantity (with scrap) is not a whole number — for example 2.2 × 4 = 8.8 — the check and Issue Material round up to the next whole unit (9), so the job is never short. A backflushed component is rounded up on the running total, so partial completions never take more in all than the whole order would.

### 9F.2a Job work (Stage 130.16)

**Manufacturing → Job Work** sends material to a job worker - for polishing or repair (the same item comes back), or to be made into something else (gold into rings) - and tracks every line until it is back.

1. **+ New Job Work Order**: the **Job worker** (a vendor), the **From store**, the **Due back** date and whether the goods are **Capital goods**. Add each line: the item sent, **Qty**, **Lot**, **Gross / Net / Fine weight**, **Purity**, and - when it comes back as something else - **Comes back as** (the output item) and **Output per unit** (how many come out of one unit sent; 0.2 = five units make one). Leave *Comes back as* blank when the same item comes back. **Save order** (it gets a JO number).
   For goods that go straight to the job worker, enter **Received by job worker on**: the statutory return date then counts from that day instead of the send day. Leave it empty otherwise.
2. **Send** (with the vehicle, LR / AWB and transporter) puts the order on a **delivery challan** (§5.8) and dispatches it: the goods stay yours, in the job worker's custody - out of available stock, not out of your stock value. If delivery challans need approval at your company, the order waits as *Challan Pending*; **Send** again once the challan is approved. **Print challan** prints it.
3. **Receive** what came back, line by line and as many times as it takes: **Accepted** and **Rejected** (in the units sent), **Scrap** and **Loss**, the weights back, and a reason for anything rejected, scrapped or lost. For the same item, accepted goes back to sellable stock and rejected to QC hold. For an item made into another, the material used leaves stock and the **Output made** comes in at the material's value (it fills itself from *Output per unit*; type the actual count if it differs) - rejected output goes to QC hold, never to sellable stock. An output tracked by serial or batch shows **Piece codes** or **Lot** under its line, so each piece carries its own cost (§6.15); you can leave them blank. Scrap and loss up to your company's allowed share are booked at once as job-work loss; more waits for approval (a write-off approver sees it in Approvals) and shows as *+N* beside the loss until then.
4. **Book bill** books the job worker's processing bill (bill number and amount) - once per bill number - to job-work conversion cost, spread over the lines by what was accepted.

Each order shows per line *Sent · accepted · rejected · loss · still out* and the net / fine weight still out, its due date (*overdue* when past) and its **statutory** return date (ITC-04: inputs within a year of sending, capital goods within three; ⚠ within 30 days). Overdue orders and orders near the statutory date are flagged once and reminded (*Job Work Overdue*, *Job Work Statutory Return Due* notifications). **Show** picks *To send*, *With job workers* and *Exceptions*. **Reports → Manufacturing**: **Job work outstanding (qty and weight)** and **Job work cost, yield and wastage** (yield against what the material should give, scrap / loss %, processing charge and cost per output).

An order made before this (one item out, one back) works as a one-line order.

### 9F.3 Quality inspections, certificates, non-conformance and maintenance (Stage 55.13)

**Manufacturing → Quality Inspections.** **+ New Inspection**:

1. **Inspecting**: *Goods received (GRN)*, *Production output* or *Goods going out (sales order)*, then the document. For goods received, the **Item** must be one received on that GRN; give the **Location** the stock is at and the **Quantity inspected**. For production output, item and location come from the order.
2. Optionally pick an **Inspection plan** — its parameters and specifications fill the readings table, and every one needs a reading. Without a plan, add your own readings (**+ Add reading**).
3. Mark each reading **Pass** or **Fail** and click **Record inspection**. One Fail fails the inspection.

What happens next is on the list (**Readings** shows what was measured):

- **Production output** → **Submit for approval**. The approver decides it under Approvals; the production order cannot be completed until it is approved, and an approved **Fail** blocks completion.
- **Goods received / going out** → **Decide**. A pass is **accepted** (no stock changes). A fail cannot be accepted: choose **Hold stock** (the quantity stays on hand but cannot be sold or issued until a hold release is approved), **Rework** (held the same way until reworked and released), or **Use as-is (concession)** — this needs a note, and someone other than the inspector must record it. A failed inspection raises a **non-conformance report**; its number appears on the row — click it to go there.

**Certificates.** **+ New Certificate** records a lot's test results (item, batch/lot, optional plan, each result Pass/Fail). A passed certificate can be **Released**; **Reject** moves every bin holding that lot to QC-Hold. A certificate records your own tests — it is not a regulatory certification.

**Non-conformance.** Each report shows what went wrong, where it was found, the item and lot. Work it through: **Investigate** (pick the root cause — reason codes of category *Quality*) → **Plan action** (the corrective action, the **owner** who does it and a due date; overdue ones are flagged) → **Close** (the evidence the action was done — for example a vendor letter, or the next lots passing). **+ New Report** raises one for anything not found by an inspection.

**Sampling by AQL (170.15).** When the inspection plan has an **AQL**, the sample is worked out for you:

1. In **+ New Inspection**, pick the **Inspection plan** and type the whole lot in **Quantity inspected**. A line under the form shows the sample size and the accept and reject numbers (for example lot 1,000 at AQL 1.0: sample 80, accept at 2, reject at 3).
2. Inspect that many units and type the count in **Defects found**.
3. Click **Record inspection**. Defects at or below the accept number record **Pass** (lot accepted); defects at or above the reject number record **Fail**, and the fail follows the same hold and non-conformance path as above.

A plan with no AQL works as before. **Reports → Quality → AQL Sample Plan** shows the plan for any lot size and AQL.

**Supplier actions (170.16).** **Manufacturing → Supplier Actions** lists the corrective action requests (SCARs) sent to vendors.

1. Click **+ New SCAR**. Pick the **Vendor**, type the **Non-conformance** number if there is one (the issue, item and lot are taken from it), the **Reply by** date and the **Issue**, then click **Open SCAR**. A non-conformance can have only one open SCAR.
2. When the vendor replies, click **Root cause**, then **Action**, then **Verify and close** on the row. Each step takes a few words, and they must go in that order. The last one closes the SCAR.

A SCAR past its reply-by date shows **late**.

**Instrument calibration (176.6).** **Setup → Instrument** lists your gauges, scales and meters: code, name, serial number, where it is kept and **Calibrate every (days)** (a whole number, 1 to 3650). Each calibration is a **Calibration Record** (Ctrl+K): the instrument, **Calibrated on** (not a future date), **Result** Pass or Fail, the certificate and who did it. Leave **Next due** alone: on a Pass it is the date plus the interval, on a Fail the same day (a failed instrument must be calibrated again). Set status **Recorded** and save; a recorded calibration cannot be changed. **Reports → Quality → Instruments due for calibration** (R7971) lists instruments never calibrated, overdue, failed, or due within 30 days; **Show All** lists every instrument in use.

**Maintenance.** **+ New Order**: the asset, the work, an optional date (with a date it is Scheduled at once; without, it waits in Draft — use **Schedule**) and a checklist, one task per line. Then **Start**, and **Complete**: tick every checklist task, add the parts used (recorded on the order — take them out of stock separately) and the hours, and write what was done. An order cannot be completed with a task unticked. **+ New Schedule** sets up recurring work (every N days, first due date, checklist); the system raises an order on each due date. Overdue open orders are flagged.

**Maintenance cost and meters (ask 170.8, 170.9).** On **Complete**, add the parts (item, quantity, **Store**), the **Hours**, the **Rate per hour** and the **Downtime** hours; they are always saved on the order. If the organisation turns on *Cost maintenance orders*, completing also takes the parts out of the chosen store and posts the cost; a short store or a missing **Store** stops the completion and nothing is saved. On **Fixed Assets**, a Capitalised asset has a **Reading** button: enter the meter value and unit. A reading below the last one is refused. A schedule can have **Every (meter)** as well as days; the order is raised at whichever comes first, and the meter count restarts at each order.

**Leases, CWIP, impairment and revaluation (ask 170.5-170.7).** On **Fixed Assets**, the **Lease** list has **New**: lessee or lessor, the asset, start, months, rent and rate. **Activate** books the right-of-use asset and liability (lessee) and shows the **Schedule**; **Post due** runs the months that have ended, once each. A Draft asset has **To CWIP**; vendor bill lines can name that asset (column **CWIP asset**) and the cost collects on it; **Capitalise** moves the total to the asset and starts depreciation from that date. A Capitalised asset has **Adjust**: **Impairment** (amount, reason) posts the loss; **Revaluation** (new value, reason) up goes to the revaluation reserve, down uses that reserve first and the rest is a loss. Depreciation then continues on the new value over the remaining life. The asset's **Lifecycle** table shows an **Adjusted** column for the year of each impairment or revaluation, and its closing value matches the books. **Dispose** of a revalued asset moves what is left of its reserve to retained earnings; the preview lists that entry too. Once an asset is capitalised, a bill line that names it, including one still waiting for approval, is refused.

### 9F.3a Lots held for inspection, and stock that has sat too long (148.33, 146.3.11)

Where your administrator has switched on **Inspect by rule**, some receipts are held. A goods receipt line of an item that needs a quality check (or from a new vendor, or a vendor whose recent lots were often rejected) is received into **QC hold**: it is in the store but cannot be sold. The receipt itself posts as always. The lot is listed on **Quality, Inspection lots** with its **sample size** (a share of the lot, at least one piece) and the reason it was held. The lot's vendor bill waits (`GOODSR-1330`) while the lot is still being inspected and while any failed unit is still held; see *Failed units* below, so a bill is never stuck for good.

Enter the results on the phone: **Store app, Inspect** (§10A.17). Each result line starts as **Fail**; change the pieces that passed to **Pass**. The lines must add up to the lot (`QUALIT-0600`). If a test has a limit and a reading is outside it, that line is a Fail whatever you chose. Pieces that passed move to available; pieces that failed stay in QC hold and cannot be sold. The lot is **Released**, **Part released** or **Rejected** (when the passed share is below the test's acceptance %). **Karat** and **Weight** you enter are saved on the batch. **Failed units** stay in QC hold and keep the vendor bill waiting until you deal with them on the same Inspect screen: **Scrap** takes them out of stock through a stock adjustment (choose a **Scrap reason** from your Inventory Adjustment reason codes; it posts its own journal) or **Release** puts them back on sale (give a note saying why). There is no quarantine location or return-to-vendor step for held units; a purchase return takes back only what the receipt booked as rejected. Where the item is marked **Warehouse may decide**, a lot can be **Waived** with a reason; a **QC mandatory** item cannot.

Where **Re-inspect stock by age** is on, **Manufacturing, Quality Inspections** shows a **Stock age inspections** section under the list. It lists the stores whose stock has reached the age a rule sets (for example polish on gold items after 90 days), filed 7 days ahead. Nothing is held for sale. Press **Decide** and choose the outcome: **Pass**; **Re-polish** (pick one of the job-work orders in the list); **Markdown** (flags it for a lower price); or **Scrap** (pick the items, quantities and a reason code; a stock adjustment is made). The age is taken from the item's receipt-date barcode where it has one, otherwise from the last receipt into the store; each row says which (*Receipt-date barcode* or *Last receipt*). The age is of the newest receipt, because stock is not tracked per receipt. **Reports, Inspections due by stock age** shows what is due. It also shows the **Markdown** flag; no other screen or price uses that flag yet.

## 10. Approvals

If your role can approve things (e.g. a manager approving a purchase order), you'll see an **Approvals** section listing anything waiting on you. Open an item, review it, and either approve or reject it (you can add a note explaining why). Once decided, it can't be silently changed — there's always a record of who approved what and when.

**My work.** The **Approvals** menu opens **My work**: one list of everything waiting for you, overdue first, then oldest:

- **Approvals** routed to your role (by the amount rules your administrator set) — including ones you submitted yourself, while **Who can approve their own documents** allows it (as shipped: anyone who can approve it; a new client starts at *Nobody*, 49.10.6b). When it does not, your own are left out, and trying one says *"You cannot approve or reject a document you submitted"* (APPROV-0840: another approver with the required role must decide it). Deciding your own above the amount your administrator set needs a comment of at least 10 characters saying why, or it says *"Write why you are deciding your own document"* (APPROV-0842);
- **Order desk** queues with orders in them (§9A.6), if you can read orders;
- **Catalog** tasks assigned to you, or to your role with nobody named yet (PIM → My Work).

Each row says why it is yours and since when; **Everything / Approvals / Order desk / Catalog** filter it. **Open** takes you where the work is done: the approvals table further down this page (Approve / Reject as before), OMS → Orders with that queue showing, or PIM → My Work. If one source cannot be loaded, the page says so and shows the rest.

**Purchase orders: who approves, and cover (148.39).** A purchase order goes to the role your administrator's **PO routing rules** name for its amount, the vendor's group and the item category; with no rule it goes by the amount bands as always. If it waits longer than its rule's number of days, the escalation role may decide it too. While an approver is away, a **delegate** (Setup, **Approval delegate**: the approver, who covers, from and to dates) sees and decides that approver's POs inside those dates - but never a PO they raised themselves. A rule's approver and escalation roles must be roles that exist (`PURCHA-5100` otherwise); in the rule list a role that no longer exists is marked, and the rule can be switched off.

**Cover and escalation for every approval (73.4.9).** A delegate covers all of the approver's approvals, not only purchase orders: inside the dates, **My work** lists each document routed to the approver's role, and **Approve / Reject** work as for the approver. You never decide one you raised or sent for approval, nor a Super Admin password reset. If your administrator has set a waiting time and an escalation role, a document that has waited longer also shows in that role's **My work**, and that role may decide it.

**Goods that came before the PO (148.38).** Where your administrator has switched on **Allow goods to be received before the PO**, a goods receipt can go in with no PO: leave the PO empty, choose the reason in **No PO: reason**, and type each line's item, quantity and price. Raise the PO afterwards and pick the receipt in **Received before PO: receipt**; it is approved the usual way, then the receipt takes its number and the bill matches. Until then the bill for those goods waits as Draft and cannot be paid. **Reports, Goods received before PO** lists the ones still waiting and the overdue ones.

**Approving a notification decision (Stage 49.12.5).** If an administrator named you the approver of an incident notification decision, open **Settings → Monitoring → Privacy approvals**. The list shows only decisions waiting for you: the incident, who may need telling, the decision (Notify or Do not notify), who decided it, the evidence reference and the reason. **Approve** closes it for good; **Send back** asks why (at least 5 characters). You cannot approve a decision you made yourself. If the decision changed after you opened the list, the screen asks you to reload and read it again.
