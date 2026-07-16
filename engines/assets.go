package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"time"
)

// AssetRegisterEntry is one asset with its calculated depreciation/net
// block added on top of the stored fields - MB 16.1 asks for these to be
// "calculated and reportable", not stored (a stored figure would drift out
// of date the moment time passes without a matching write).
type AssetRegisterEntry struct {
	ID                      string `json:"id"`
	Code                    string `json:"code"`
	Category                string `json:"category"`
	Cost                    int    `json:"cost"`
	Location                string `json:"location"`
	Custodian               string `json:"custodian"`
	UsefulLifeYears         int    `json:"useful_life_years"`
	CapitalisationDate      string `json:"capitalisation_date"`
	Status                  string `json:"status"`
	AccumulatedDepreciation int    `json:"accumulated_depreciation"`
	NetBlock                int    `json:"net_block"`
}

// calculateDepreciation applies straight-line depreciation: cost spread
// evenly across useful_life_years, capped so accumulated depreciation
// never exceeds cost (an asset is never worth less than zero on the books
// under this method) and never applies before capitalisation.
func calculateDepreciation(cost, usefulLifeYears int, capitalisationDate string) (accumulated, netBlock int) {
	if usefulLifeYears <= 0 || capitalisationDate == "" {
		return 0, cost
	}
	capDate, err := time.Parse("2006-01-02", capitalisationDate)
	if err != nil {
		return 0, cost
	}
	yearsElapsed := time.Since(capDate).Hours() / 24 / 365.25
	if yearsElapsed < 0 {
		yearsElapsed = 0
	}
	annualDepreciation := float64(cost) / float64(usefulLifeYears)
	accumulated = int(annualDepreciation * yearsElapsed)
	if accumulated > cost {
		accumulated = cost
	}
	return accumulated, cost - accumulated
}

// GetAssetRegister lists every Asset document with depreciation calculated
// as of now.
func GetAssetRegister(tenantID string) ([]AssetRegisterEntry, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data, status FROM %s.documents WHERE doctype = 'Asset' ORDER BY id`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []AssetRegisterEntry
	for rows.Next() {
		var id, dataStr, status string
		if err := rows.Scan(&id, &dataStr, &status); err != nil {
			return nil, err
		}
		var data map[string]interface{}
		_ = json.Unmarshal([]byte(dataStr), &data)

		entry := AssetRegisterEntry{ID: id, Status: status}
		if v, ok := data["code"].(string); ok {
			entry.Code = v
		}
		if v, ok := data["category"].(string); ok {
			entry.Category = v
		}
		if v, ok := data["location"].(string); ok {
			entry.Location = v
		}
		if v, ok := data["custodian"].(string); ok {
			entry.Custodian = v
		}
		if v, ok := data["capitalisation_date"].(string); ok {
			entry.CapitalisationDate = v
		}
		cost := 0
		if v, ok := data["cost"].(float64); ok {
			cost = int(v)
		}
		entry.Cost = cost
		usefulLife := 0
		if v, ok := data["useful_life_years"].(float64); ok {
			usefulLife = int(v)
		}
		entry.UsefulLifeYears = usefulLife

		switch status {
		case "Capitalised":
			entry.AccumulatedDepreciation, entry.NetBlock = calculateDepreciation(cost, usefulLife, entry.CapitalisationDate)
		case "Disposed":
			// Written off - DisposeAsset already posted the GL entry for
			// whatever the net book value was at disposal time; the
			// register should now show it has no remaining book value,
			// not its original cost.
			entry.AccumulatedDepreciation = cost
			entry.NetBlock = 0
		default: // Draft
			entry.NetBlock = cost
		}
		results = append(results, entry)
	}
	return results, nil
}

func fetchAssetData(tenantID, assetID string) (data map[string]interface{}, status string, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, "", err
	}
	var dataStr string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = 'Asset' AND id = $1`, schema), assetID).Scan(&dataStr, &status)
	if err != nil {
		return nil, "", err
	}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return nil, "", err
	}
	return data, status, nil
}

func saveAssetData(tenantID, assetID, newStatus string, data map[string]interface{}) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	data["status"] = newStatus
	marshaled, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'Asset' AND id = $3`, schema), marshaled, newStatus, assetID)
	return err
}

// CapitalizeAsset moves an asset from Draft to Capitalised: sets the
// capitalisation date (today, if not already set - MB 16.1 explicitly notes
// this may differ from the invoice date) and posts the acquisition GL
// entry (Debit Fixed Assets, Credit GRN Suspense - the same "goods/asset
// received, payment tracked elsewhere" liability account GRNs already use).
func CapitalizeAsset(tenantID, assetID string) error {
	data, status, err := fetchAssetData(tenantID, assetID)
	if err != nil {
		return fmt.Errorf("asset not found: %v", err)
	}
	if status != "Draft" {
		return fmt.Errorf("only a Draft asset can be capitalised (current status: %s)", status)
	}
	cost := 0
	if v, ok := data["cost"].(float64); ok {
		cost = int(v)
	}
	if cost <= 0 {
		return fmt.Errorf("asset cost must be a positive number to capitalise")
	}
	if _, ok := data["capitalisation_date"].(string); !ok || data["capitalisation_date"] == "" {
		data["capitalisation_date"] = time.Now().Format("2006-01-02")
	}

	if err := PostDoubleEntry(tenantID, "Asset", assetID, map[string]int{"1400": cost}, map[string]int{"2100": cost}); err != nil {
		return fmt.Errorf("failed to post capitalisation GL entry: %v", err)
	}
	return saveAssetData(tenantID, assetID, "Capitalised", data)
}

// TransferAsset moves a Capitalised asset between locations/custodians and
// logs it to the audit trail (MB 16.1's "Transfer" requirement).
func TransferAsset(tenantID, assetID, newLocation, newCustodian, actorUsername string) error {
	data, status, err := fetchAssetData(tenantID, assetID)
	if err != nil {
		return fmt.Errorf("asset not found: %v", err)
	}
	if status != "Capitalised" {
		return fmt.Errorf("only a Capitalised asset can be transferred (current status: %s)", status)
	}
	oldLocation, _ := data["location"].(string)
	oldCustodian, _ := data["custodian"].(string)
	data["location"] = newLocation
	data["custodian"] = newCustodian

	if err := saveAssetData(tenantID, assetID, status, data); err != nil {
		return err
	}
	LogAuditEvent(tenantID, actorUsername, "ASSET_TRANSFER", "SUCCESS",
		fmt.Sprintf("Asset %s transferred: location %s -> %s, custodian %s -> %s", assetID, oldLocation, newLocation, oldCustodian, newCustodian))
	return nil
}

// DisposeAsset writes off a Capitalised asset (sale/scrap/write-off) and
// posts the disposal GL entry. Simplified deliberately: this treats the
// full net book value at disposal time as a loss (Debit Loss on Disposal,
// Credit Fixed Assets), not netting against a sale/disposal proceeds
// figure - MB 16.1 doesn't specify a proceeds field, and inventing one
// would be scope beyond what's asked for here.
func DisposeAsset(tenantID, assetID, disposalType string) error {
	data, status, err := fetchAssetData(tenantID, assetID)
	if err != nil {
		return fmt.Errorf("asset not found: %v", err)
	}
	if status != "Capitalised" {
		return fmt.Errorf("only a Capitalised asset can be disposed (current status: %s)", status)
	}

	cost := 0
	if v, ok := data["cost"].(float64); ok {
		cost = int(v)
	}
	usefulLife := 0
	if v, ok := data["useful_life_years"].(float64); ok {
		usefulLife = int(v)
	}
	capDate, _ := data["capitalisation_date"].(string)
	_, netBlock := calculateDepreciation(cost, usefulLife, capDate)

	if netBlock > 0 {
		if err := PostDoubleEntry(tenantID, "Asset-Disposal", assetID, map[string]int{"5300": netBlock}, map[string]int{"1400": netBlock}); err != nil {
			return fmt.Errorf("failed to post disposal GL entry: %v", err)
		}
	}

	data["disposal_type"] = disposalType
	return saveAssetData(tenantID, assetID, "Disposed", data)
}
