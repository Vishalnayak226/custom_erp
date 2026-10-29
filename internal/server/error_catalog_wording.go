package server

// Stage 47.14.4 - floor wording for spreadsheet catalog rows.
//
// The historical spreadsheet (error_catalog_generated.go) gave whole groups of
// floor errors one shared action - eleven stock refusals all said "Select
// valid stock/location or request approval.", seven till refusals "Correct
// details or request approval." - so a cashier told the price is above MRP
// and one told the cash limit is reached read the same instruction, and
// neither learned what to do. A few headlines also packed alternatives into
// slashes ("item/design/combination"), which reads badly and cannot be
// translated word for word.
//
// These rows replace the wording only - code, status, severity and logging
// stay the spreadsheet's. Rules every row here follows (and
// TestStage47144FloorErrorsSayWhatToDo enforces for every floor code):
// a plain verb first, one action per error, no slashes, no system words, the
// person who can actually do it named when it is not the reader.
//
// When the spreadsheet itself carries the new wording, remove the row here in
// the same change (same rule as error_catalog_extensions.go).
var catalogWording = []struct{ Code, UserMessage, UserAction string }{
	// Goods receipt.
	{Code: "GOODSR-0089", UserAction: "Lower the accepted quantity to no more than was received, then save again."},
	{Code: "GOODSR-0090", UserAction: "Choose a rejection reason on each line with a rejected quantity, then save again."},
	{Code: "GOODSR-0091", UserAction: "Try again. If it fails again, tell support and quote the reference shown."},
	{Code: "GOODSR-0095", UserAction: "Finish and post the goods receipt first, then post the vendor invoice."},
	{Code: "GRN-0253", UserAction: "Cancel the documents made from this receipt first (its vendor invoice or stock movement), then cancel the receipt."},
	{Code: "GRN-0254", UserAction: "Generate the barcodes for the accepted quantity, then post again."},
	// Stock.
	{Code: "INV-0256", UserMessage: "The stock that expires first, or the oldest stock, must be issued first.", UserAction: "Pick the batch the screen suggests."},
	{Code: "INVENT-0101", UserAction: "Lower the quantity to what is available, or choose a location that has the stock."},
	{Code: "INVENT-0102", UserAction: "Scan the barcode of a unit that is in stock here."},
	{Code: "INVENT-0103", UserAction: "Scan the barcode on another unit. If this unit is in your hand, give it to a supervisor to check."},
	{Code: "INVENT-0104", UserAction: "Use stock that is not blocked, or ask a supervisor to release the block first."},
	{Code: "INVENT-0105", UserMessage: "Stock of this item at this location cannot go below zero.", UserAction: "Lower the quantity, or receive or move the stock in first."},
	{Code: "INVENT-0106", UserAction: "Pick a batch that has not expired, and set the expired stock aside."},
	{Code: "INVENT-0107", UserMessage: "This transaction needs a storage location or bin.", UserAction: "Choose the bin, then save again."},
	{Code: "INVENT-0108", UserMessage: "A stock count is already running for this item at this location.", UserAction: "Wait for the count to finish, or ask the person running it to close it."},
	{Code: "INVENT-0109", UserAction: "Choose a reason for the adjustment, then post again."},
	{Code: "INVENT-0110", UserMessage: "This adjustment is larger than allowed without approval.", UserAction: "Submit it for approval. It posts once a manager approves it."},
	{Code: "INVENT-0114", UserAction: "Use stock that is not reserved, or ask a supervisor to release the reservation."},
	{Code: "INVENT-0115", UserMessage: "The scanned barcode is for a different item.", UserAction: "Scan the barcode of the item on the line, or change the line to the scanned item."},
	{Code: "STOCKT-0111", UserAction: "Choose a different location to send the stock to."},
	{Code: "STOCKT-0112", UserAction: "Lower the quantity to what the sending location has."},
	{Code: "STOCKT-0113", UserAction: "Receive the transfer at the receiving store first, then close it."},
	// Devices and printing.
	{Code: "MOBILE-0175", UserAction: "Ask an administrator to register this device, then sign in again."},
	{Code: "MOBILE-0176", UserAction: "Check each record the sync names, correct it, then sync again."},
	{Code: "MOBILE-0177", UserMessage: "The scanned code could not be read as a barcode or QR code.", UserAction: "Scan again, or type the code in."},
	{Code: "DEVICE-0298", UserAction: "Pick another printer, or ask an administrator to set one up for this location."},
	{Code: "DEVICE-0299", UserAction: "Check the printer is on, connected and loaded, then print again."},
	{Code: "DEVICE-0300", UserAction: "Ask an administrator to set up the barcode template."},
	// The till.
	{Code: "POSOFF-0237", UserMessage: "This till has no POS profile yet.", UserAction: "Ask an administrator to set up the POS profile for this store and till, then open the till again."},
	{Code: "POSOFF-0238", UserAction: "Open the till session and enter the opening cash, then bill."},
	{Code: "POSOFF-0240", UserAction: "Count the cash again. If the difference stands, enter a reason and ask a manager to approve it."},
	{Code: "POSOFF-0241", UserAction: "Open the bill's conflict, choose which version to keep, then sync again."},
	{Code: "POSOFF-0243", UserMessage: "This till has no card machine linked to it.", UserAction: "Take the payment another way, and ask an administrator to link the card machine in POS settings."},
	{Code: "POSOFF-0244", UserAction: "Hold the bill and check the payment on the card machine or UPI app before finishing it."},
	{Code: "SALESP-0122", UserAction: "Lower the price to the MRP or below."},
	{Code: "SALESP-0123", UserAction: "Lower the discount, or ask a manager to approve it."},
	{Code: "SALESP-0124", UserAction: "Remove the coupon, or check its dates and conditions."},
	{Code: "SALESP-0125", UserAction: "Change the payments so they add up to the bill total."},
	{Code: "SALESP-0126", UserMessage: "A card or UPI payment needs its reference number.", UserAction: "Type the reference from the card slip or the UPI app."},
	{Code: "SALESP-0127", UserAction: "Take part of the payment by card or UPI so the cash stays under the limit."},
	{Code: "SALESP-0128", UserAction: "Take a return against the bill instead."},
	// Returns.
	{Code: "SALESR-0129", UserAction: "Tell the customer the return period has ended, or ask a manager whether an exception can be made."},
	{Code: "SALESR-0130", UserAction: "Lower the return quantity to no more than was sold, less anything already returned."},
	{Code: "SALESR-0131", UserAction: "Scan or type the original bill number. Without the bill, a manager handles the return."},
	{Code: "SALESR-0132", UserAction: "Lower the refund to the approved return value."},
	// Dispatch.
	{Code: "WMSLOG-0137", UserAction: "Choose the courier for the shipment, then dispatch."},
	{Code: "WMSLOG-0138", UserAction: "Read the courier's reason below, correct the shipment details, then try again."},
	{Code: "WMSLOG-0139", UserAction: "Try again in a few minutes. If it keeps failing, tell your administrator the courier link may be down."},
}

func init() {
	for _, w := range catalogWording {
		entry, ok := errorCatalog[w.Code]
		if !ok {
			panic("error catalog wording for a code that does not exist: " + w.Code)
		}
		if w.UserMessage != "" {
			entry.UserMessage = w.UserMessage
		}
		if w.UserAction != "" {
			entry.UserAction = w.UserAction
		}
		errorCatalog[w.Code] = entry
	}
}
