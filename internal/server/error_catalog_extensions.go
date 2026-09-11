package server

// Source-owned additions to the historical spreadsheet catalog. Keep these
// outside its generated file so rebuilding that file cannot erase live codes.
// When a code enters the spreadsheet, remove its extension in the same change.
func init() {
	for _, entry := range []CatalogEntry{
		{Code: "GOODSR-0096", MessageID: "EXT-GOODSR-0096", Module: "Goods Receipt", Scenario: "Damaged quantity requires a reason", MessageType: "Error", UserMessage: "Enter a damage reason for each damaged receipt line.", UserAction: "Record the observed damage and submit the receipt again.", Severity: "High", Blocking: true, DisplayStyle: "Field inline", HTTPStatus: 422, LogRequired: true, Priority: "P1", RequirementLevel: "Mature ERP"},
		{Code: "GOODSR-0097", MessageID: "EXT-GOODSR-0097", Module: "Goods Receipt", Scenario: "Rejected and damaged quantities exceed received quantity", MessageType: "Error", UserMessage: "Rejected plus damaged quantity cannot exceed received quantity.", UserAction: "Check the received, rejected and damaged counts; correct the line before submitting.", Severity: "High", Blocking: true, DisplayStyle: "Field inline", HTTPStatus: 422, LogRequired: true, Priority: "P1", RequirementLevel: "Mature ERP"},
	} {
		if _, exists := errorCatalog[entry.Code]; exists {
			panic("duplicate error catalog extension: " + entry.Code)
		}
		errorCatalog[entry.Code] = entry
	}
}
