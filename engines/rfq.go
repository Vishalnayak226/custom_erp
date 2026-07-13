package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
)

// GetVendorQuotesForRFQ lists every quote submitted against one RFQ, for
// side-by-side comparison.
func GetVendorQuotesForRFQ(tenantID, rfqID string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, data FROM %s.documents
		WHERE doctype = 'VendorQuote' AND data->>'rfq_id' = $1
		ORDER BY (data->>'quoted_price')::numeric ASC`, schema), rfqID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var id, dataStr string
		if err := rows.Scan(&id, &dataStr); err != nil {
			return nil, err
		}
		var data map[string]interface{}
		_ = json.Unmarshal([]byte(dataStr), &data)
		data["id"] = id
		results = append(results, data)
	}
	return results, nil
}

// SelectWinningQuote marks one VendorQuote as the winner for its RFQ: the
// chosen quote becomes Selected, every other quote against the same RFQ
// becomes Rejected, and the RFQ itself closes. All three writes happen in
// one transaction so a partial selection (e.g. a winner marked but the RFQ
// left open) can't occur.
func SelectWinningQuote(tenantID, rfqID, quoteID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var winnerDataStr string
	var winnerRfqID string
	err = tx.QueryRow(fmt.Sprintf(`SELECT data, data->>'rfq_id' FROM %s.documents WHERE doctype = 'VendorQuote' AND id = $1`, schema), quoteID).Scan(&winnerDataStr, &winnerRfqID)
	if err != nil {
		return fmt.Errorf("quote not found: %v", err)
	}
	if winnerRfqID != rfqID {
		return fmt.Errorf("quote %s does not belong to RFQ %s", quoteID, rfqID)
	}

	var winnerData map[string]interface{}
	_ = json.Unmarshal([]byte(winnerDataStr), &winnerData)
	winnerData["status"] = "Selected"
	winnerMarshaled, _ := json.Marshal(winnerData)
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = 'Selected', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'VendorQuote' AND id = $2`, schema), winnerMarshaled, quoteID); err != nil {
		return err
	}

	rows, err := tx.Query(fmt.Sprintf(`SELECT id, data FROM %s.documents WHERE doctype = 'VendorQuote' AND data->>'rfq_id' = $1 AND id != $2`, schema), rfqID, quoteID)
	if err != nil {
		return err
	}
	type otherQuote struct {
		id   string
		data map[string]interface{}
	}
	var others []otherQuote
	for rows.Next() {
		var id, dataStr string
		if err := rows.Scan(&id, &dataStr); err != nil {
			rows.Close()
			return err
		}
		var data map[string]interface{}
		_ = json.Unmarshal([]byte(dataStr), &data)
		others = append(others, otherQuote{id: id, data: data})
	}
	rows.Close()

	for _, o := range others {
		o.data["status"] = "Rejected"
		marshaled, _ := json.Marshal(o.data)
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = 'Rejected', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'VendorQuote' AND id = $2`, schema), marshaled, o.id); err != nil {
			return err
		}
	}

	var rfqDataStr string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'RFQ' AND id = $1`, schema), rfqID).Scan(&rfqDataStr); err != nil {
		return fmt.Errorf("RFQ not found: %v", err)
	}
	var rfqData map[string]interface{}
	_ = json.Unmarshal([]byte(rfqDataStr), &rfqData)
	rfqData["status"] = "Closed"
	rfqMarshaled, _ := json.Marshal(rfqData)
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = 'Closed', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'RFQ' AND id = $2`, schema), rfqMarshaled, rfqID); err != nil {
		return err
	}

	return tx.Commit()
}
