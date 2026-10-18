package engines

// Stage 55.20: the report catalog organised by the business question each
// report answers, in plain words. Keyed by report ID; ListReportDefinitions
// copies Question/Description onto the catalog entries, and the Report
// Catalog's search matches them. A report with no entry here still appears,
// under its category, exactly as before.

type reportQuestion struct {
	Question    string
	Description string
}

var reportQuestions = map[string]reportQuestion{
	// Money in, money out, and whether the books agree.
	"profit-and-loss":         {"Did we make money in this period?", "Revenue less expenses by account for the dates chosen; each line drills to its GL postings."},
	"balance-sheet":           {"What do we own and owe on a date?", "Assets, liabilities and equity as at the date chosen; each line drills to its GL postings."},
	"cash-flow-statement":     {"Where did the cash come from and go?", "Cash in and out on 1100 for the dates chosen, by activity."},
	"cash-book":               {"What went through the cash account?", "Every 1100 Cash/Bank posting with a running balance."},
	"bank-book":               {"What went through this bank account?", "Every posting on the bank account's GL code with a running balance."},
	"gl-drilldown":            {"What makes up this account's balance?", "Every posting on one account, oldest first, with a running balance."},
	"gl-journal":              {"Who posted what, and from which document?", "Every GL line in the period with the user, job or integration that posted it."},
	"gl-posting-review":       {"Which people and jobs posted to the ledger?", "GL lines grouped by poster for the period; drills to that poster's lines."},
	"statutory-gl-export":     {"What does the auditor need from the ledger?", "The full GL for a closed period, with poster, for export."},
	"tax-ledger":              {"What GST did we post, line by line?", "Every posting on the GST accounts, optionally for a date range."},
	"gst-return-summary":      {"What GST do we owe for this return period?", "Output GST, taxable and non-taxable value and input credit for the dates chosen; control totals tie the tax lines to the ledger."},
	"receivables-ageing":      {"Who owes us, and for how long?", "Open customer invoices by age from their due date; control totals tie them to 1300 Accounts Receivable."},
	"payables-ageing":         {"What do we owe suppliers for goods received, and for how long?", "Goods received and not yet paid, per receipt, by age from the bill's due date (else the receipt date); part-payments are taken off. Control totals tie it to 2100 GRN Suspense."},
	"po-commitments":          {"What have we committed to buy, and how old is it?", "Approved purchase orders not yet closed, by age. These are commitments, not ledger liabilities - the control totals say so."},
	"customer-ledger":         {"What has this customer been invoiced?", "Customer invoices with status and amount."},
	"vendor-ledger":           {"What have we ordered from this vendor?", "Purchase orders by vendor with status and amount."},
	"dunning-queue":           {"Which overdue customers need chasing?", "Overdue invoices with their dunning stage."},
	"cash-flow-forecast":      {"What cash is expected in and out?", "Expected receipts and payments by due date."},
	"budget-variance":         {"Are we within budget?", "Budget against actual by account for the period."},
	"pos-sale-reconciliation": {"Did every till sale post correctly?", "Each POS sale against the tender, revenue, tax and stock it posted."},
	"stored-value-liability":  {"What do we owe on gift cards?", "Outstanding gift card balances against the liability account."},
	"payroll-reconciliation":  {"Does payroll agree from gross pay to the bank and the ledger?", "Payslip gross, deductions and net against the GL, salary paid, and payments confirmed on the bank statement."},

	// Selling.
	"sales-register":          {"What did we sell at the till?", "Paid and settled POS sales with their total; control totals tie them to revenue and output GST."},
	"customer-lifetime-value": {"Which customers are worth the most?", "Spend and visits per customer over their history."},
	"rfm-segmentation":        {"Which customers are loyal, lapsing or new?", "Recency, frequency and spend scores per customer."},
	"points-liability":        {"What do we owe in loyalty points?", "Unredeemed points per customer at their redemption value."},
	"loyalty-summary":         {"How are points being earned and used?", "Earned, redeemed and balance per customer."},
	"campaign-roi":            {"Did this campaign pay off?", "Campaign spend against the sales it drove."},

	// Stock.
	"current-stock":            {"How much do we have, and where?", "On hand, available, committed and reserved per item and location."},
	"inventory-valuation":      {"What is our stock worth?", "On-hand quantity at moving average cost; control totals tie it to 1200 Inventory Control."},
	"non-valuated-stock":       {"Which stock has no cost on record?", "Items with stock but no unit cost - valued at zero until costed."},
	"stock-ledger":             {"What moved in and out of stock?", "Every stock movement with its source document."},
	"stock-ageing":             {"How old is the stock on the shelf?", "Stock by receipt batch and age."},
	"reorder-suggestions":      {"What should we reorder?", "Items under their reorder level with the suggested quantity."},
	"exception-negative-stock": {"Where has stock gone negative?", "Item and location pairs below zero."},
	"batch-ageing":             {"Where is this batch, and how old is it?", "Batch lookup with its age and remaining quantity."},

	// Buying.
	"grn-register":   {"What have we received?", "Goods receipts with their purchase order and lines."},
	"rfq-comparison": {"Which vendor quoted best?", "Quotes side by side for one request for quotation."},

	// People.
	"attendance-summary": {"Who was present, absent or on leave?", "Attendance days per employee for the dates chosen."},

	// Assets.
	"asset-register": {"What fixed assets do we hold, and what are they worth?", "Each asset with cost, accumulated depreciation and book value."},

	// Orders and fulfilment.
	"order-aging":               {"Which orders are waiting too long?", "Open orders by age and status."},
	"sla-breach":                {"Which orders missed their promise?", "Orders past their dispatch or delivery SLA."},
	"oms-unsettled-settlements": {"Which marketplace orders are not yet paid out?", "Delivered marketplace orders with no settlement line."},
	"oms-settlement-variance":   {"Where did a marketplace pay us a different amount?", "Settlement lines held as variance for dispute or write-off."},
	"return-aging":              {"Which returns are waiting to be processed?", "Open returns by age and status."},
}
