package server

import (
	"net/http"
	"regexp"
	"strings"

	"custom_erp/engines"
)

// Ask 237.7: the allocation sheet for a posted goods receipt. Stock Movement
// › Allocation › From a goods receipt downloads it: each SKU the receipt put
// into stock, with its accepted qty, and a column per store headed by its
// location code. The client types the qty for each store and uploads it on
// the same screen; RunStoreAllocation reads the wide sheet and makes one
// Draft transfer per store from the receipt's location (X-Allocation-From).

var allocFileUnsafe = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

func handleTransferAllocationGRNSheet(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	allowed, err := checkPermission(tenantID, r.Header.Get("Resolved-Role"), "TransferOrder", "create")
	if err != nil || !allowed {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	grn := strings.TrimSpace(r.URL.Query().Get("grn"))
	if grn == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'grn' is required")
		return
	}
	sheet, err := engines.GetGRNAllocationSheet(tenantID, grn)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	book, err := allocGRNBook(sheet)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	w.Header().Set("X-Allocation-From", sheet.Location)
	swWriteXLSX(w, "allocation-"+strings.Trim(allocFileUnsafe.ReplaceAllString(sheet.Code, "-"), "-")+".xlsx", book)
}

// allocGRNBook is the sheet: header, a "#" hint row (each store's name), then
// one row per SKU with the store columns blank.
func allocGRNBook(s *engines.GRNAllocationSheet) ([]byte, error) {
	cols := []swCol{
		{Head: "Combination ID*", Hint: "From " + s.Code + " at " + s.Location, Width: 22},
		{Head: "Design No", Width: 16},
		{Head: "Item Name", Width: 28},
		{Head: "Received Qty", Hint: "Accepted", Width: 14},
	}
	for _, st := range s.Stores {
		cols = append(cols, swCol{Head: st.Code, Hint: st.Name})
	}
	var data [][]engines.XLSXCell
	for _, l := range s.Lines {
		data = append(data, []engines.XLSXCell{swT(l.SKU), swT(l.Design), swT(l.Name), swNum(float64(l.Qty))})
	}
	sheet := swTableSheet(cols, data)
	sheet.Name = "Allocation"
	return engines.WriteXLSXBook([]engines.XLSXSheet{sheet})
}
