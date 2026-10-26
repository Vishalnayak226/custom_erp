package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// AssetRegisterEntry is one asset with its depreciation and net block added
// on top of the stored fields. Stage 55.16 (Batch D): depreciation is now
// posted monthly (engines/asset_depreciation.go), so AccumulatedDepreciation
// is what the books hold - the asset's 1410 credits - and
// CalculatedDepreciation is what the schedule says should be posted through
// the last month that has ended; a gap between the two is a lag the next run
// clears.
type AssetRegisterEntry struct {
	ID                      string  `json:"id"`
	Code                    string  `json:"code"`
	Category                string  `json:"category"`
	Cost                    float64 `json:"cost"`
	Location                string  `json:"location"`
	Custodian               string  `json:"custodian"`
	UsefulLifeYears         int     `json:"useful_life_years"`
	CapitalisationDate      string  `json:"capitalisation_date"`
	Status                  string  `json:"status"`
	ItemCode                string  `json:"item_code,omitempty"`
	SourceGRN               string  `json:"source_grn,omitempty"`
	AccumulatedDepreciation float64 `json:"accumulated_depreciation"`
	CalculatedDepreciation  float64 `json:"calculated_depreciation"`
	NetBlock                float64 `json:"net_block"`
}

// GetAssetRegister lists every Asset document with its posted and calculated
// depreciation.
func GetAssetRegister(tenantID string) ([]AssetRegisterEntry, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	postedPaise, err := postedAccumulatedDepreciation(schema)
	if err != nil {
		return nil, err
	}
	now := time.Now()
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
		if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
			log.Printf("[ASSETS] corrupt Asset %s: %v", id, err)
			continue
		}

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
		entry.ItemCode, _ = data["item_code"].(string)
		entry.SourceGRN, _ = data["source_grn"].(string)
		data["status"] = status
		cost := assetCostPaise(data)
		entry.Cost = PaiseToRupees(cost)
		usefulLife := 0
		if v, ok := data["useful_life_years"].(float64); ok {
			usefulLife = int(v)
		}
		entry.UsefulLifeYears = usefulLife

		switch status {
		case "Capitalised":
			if plan, ok := assetDepreciationPlan(data, ""); ok {
				var due int64
				for i := 1; i <= plan.monthsDue(now); i++ {
					due += plan.amount(i)
				}
				entry.CalculatedDepreciation = PaiseToRupees(due)
			}
			entry.AccumulatedDepreciation = PaiseToRupees(postedPaise[id])
			entry.NetBlock = PaiseToRupees(cost - postedPaise[id])
		case "Disposed":
			// Written off - DisposeAsset already posted the GL entry for
			// whatever the net book value was at disposal time; the
			// register should now show it has no remaining book value,
			// not its original cost.
			entry.AccumulatedDepreciation = entry.Cost
			entry.CalculatedDepreciation = entry.Cost
			entry.NetBlock = 0
		default: // Draft
			entry.NetBlock = entry.Cost
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
	return CapitalizeAssetWithLife(tenantID, assetID, 0)
}

// CapitalizeAssetWithLife is CapitalizeAsset with a useful life supplied at
// capitalisation (Stage 57.8): an asset raised from a GRN arrives without
// one, and the Fixed Assets screen asks for it on Capitalise. A positive
// usefulLifeYears is stored on the asset; zero keeps what the asset has.
func CapitalizeAssetWithLife(tenantID, assetID string, usefulLifeYears float64) error {
	data, status, err := fetchAssetData(tenantID, assetID)
	if err != nil {
		return fmt.Errorf("asset not found: %v", err)
	}
	if status != "Draft" {
		return fmt.Errorf("only a Draft asset can be capitalised (current status: %s)", status)
	}
	cost := assetCostPaise(data) // a Draft: the cost to the paisa
	if cost <= 0 {
		return fmt.Errorf("asset cost must be a positive number to capitalise")
	}
	data["capitalised_paise"] = cost
	// ASSET-0271: useful_life_years drives the depreciation plan - without
	// it, depreciation silently computes as zero forever (see
	// assetDepreciationPlan's own months <= 0 short-circuit) rather than
	// erroring where the missing input actually is.
	if usefulLifeYears > 0 {
		data["useful_life_years"] = usefulLifeYears
	}
	usefulLife, _ := data["useful_life_years"].(float64)
	if usefulLife <= 0 {
		return &ValidationError{Code: "ASSET-0271", Message: "useful_life_years must be a positive number to capitalise - depreciation cannot be calculated without it"}
	}
	if _, ok := data["capitalisation_date"].(string); !ok || data["capitalisation_date"] == "" {
		data["capitalisation_date"] = BusinessToday()
	}

	if err := PostDoubleEntry(tenantID, "Asset", assetID, map[string]int64{"1400": cost}, map[string]int64{"2100": cost}, "", fmt.Sprintf("Asset:%s:CAPITALIZE", assetID)); err != nil {
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
		return &ValidationError{Code: "ASSET-0270", Message: fmt.Sprintf("only a Capitalised asset can be transferred (current status: %s)", status)}
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

// DisposeAsset disposes of a Capitalised asset with no proceeds, kept for
// existing callers. Stage 55.16 (Batch D): it is DisposeAssetWithProceeds
// with zero proceeds - the depreciation due is posted first, and the entry
// clears 1400 and 1410 with the remaining book value as the loss. (Before
// this, depreciation was never posted and only the calculated net block was
// written off to 5300; assets disposed that way keep that entry.)
func DisposeAsset(tenantID, assetID, disposalType string) error {
	_, err := DisposeAssetWithProceeds(tenantID, assetID, disposalType, 0, "")
	return err
}
