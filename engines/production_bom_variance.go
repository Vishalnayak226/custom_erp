package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/lib/pq"
)

// Stage 49.5.6c: accepting a changed BOM is an action, not a field. MFG-0276
// stops a production order from finishing when its BOM changed after the
// material was issued; bom_variance_acknowledged lifts that stop. It used to
// be an ordinary key - any Production Order save could set (or clear) it, with
// no reason and no audit row. Now only AcknowledgeBOMVariance writes it: it
// needs the Production Order update permission (the handler) and a reason, and
// records who, when, why and what changed - on the order (bom_variance_ack)
// and in the audit log.
//
// The same shape as the action-only statuses (Stage 132.17): a plain save may
// send the keys back unchanged, never set, change or clear them, and an
// import cannot either (the generic save asks CheckActionOnlyFields; the CSV
// import screenActionOnlyImportFields).

// Error codes (internal/server/error_catalog_extensions.go).
const (
	ActionOnlyFieldCode   = "MFG-4962" // a save or import wrote an action's key
	BOMVarianceReasonCode = "MFG-4963" // no reason given
	BOMVarianceNoneCode   = "MFG-4964" // nothing to acknowledge
)

// actionOnlyFieldRule is a doctype's keys that only one action writes.
type actionOnlyFieldRule struct {
	Fields []string
	Action string
}

var actionOnlyFields = map[string]actionOnlyFieldRule{
	"ProductionOrder": {Fields: []string{"bom_variance_acknowledged", "bom_variance_ack"}, Action: "Accept BOM change"},
}

// CheckActionOnlyFields refuses a save that sets, changes or clears a key
// only the document's own action writes. docID is the record being updated
// ("" for a new one).
func CheckActionOnlyFields(tenantID, doctype, docID string, payload map[string]interface{}) error {
	rule, ok := actionOnlyFields[doctype]
	if !ok {
		return nil
	}
	var stored map[string]interface{}
	if strings.TrimSpace(docID) != "" {
		stored, _, _ = fetchDocData(tenantID, doctype, docID)
	}
	return actionOnlyFieldChange(doctype, rule, payload, stored)
}

func actionOnlyFieldChange(doctype string, rule actionOnlyFieldRule, sent, stored map[string]interface{}) error {
	for _, k := range rule.Fields {
		v, ok := sent[k]
		if !ok || sameActionOnlyValue(v, stored[k]) {
			continue
		}
		return &ValidationError{Code: ActionOnlyFieldCode, SubFor: k, Message: fmt.Sprintf(
			"%s is set only by the %s's %s action, not by a save or an import", k, doctype, rule.Action)}
	}
	return nil
}

// sameActionOnlyValue compares two values as they may arrive - a JSON bool or
// object, or the text a CSV cell carries - with blank and false alike.
func sameActionOnlyValue(a, b interface{}) bool {
	a, b = actionOnlyNormal(a), actionOnlyNormal(b)
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	ja, errA := json.Marshal(a)
	jb, errB := json.Marshal(b)
	return errA == nil && errB == nil && string(ja) == string(jb)
}

func actionOnlyNormal(v interface{}) interface{} {
	if s, ok := v.(string); ok {
		s = strings.TrimSpace(s)
		switch strings.ToLower(s) {
		case "", "false", "0", "no":
			return nil
		case "true", "1", "yes":
			return true
		}
		var parsed interface{}
		if json.Unmarshal([]byte(s), &parsed) != nil {
			return s
		}
		v = parsed
	}
	switch t := v.(type) {
	case bool:
		if !t {
			return nil
		}
	case map[string]interface{}:
		if len(t) == 0 {
			return nil
		}
	}
	return v
}

// screenActionOnlyImportFields refuses an import row that sets, changes or
// clears an action's key, and keeps the stored value on every other row - an
// import replaces a record's data, which would otherwise wipe it.
func screenActionOnlyImportFields(tx *sql.Tx, doctype string, writes []importWrite, result *ImportResult) []importWrite {
	rule, ok := actionOnlyFields[doctype]
	if !ok || len(writes) == 0 {
		return writes
	}
	ids := make([]string, len(writes))
	for i, w := range writes {
		ids[i] = w.id
	}
	stored := map[string]map[string]interface{}{}
	rows, err := tx.Query(`SELECT id, data FROM documents WHERE doctype = $1 AND id = ANY($2::text[])`, doctype, pq.Array(ids))
	if err == nil {
		for rows.Next() {
			var id, raw string
			if err = rows.Scan(&id, &raw); err != nil {
				break
			}
			d := map[string]interface{}{}
			_ = json.Unmarshal([]byte(raw), &d)
			stored[id] = d
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
	}
	if err != nil {
		for _, w := range writes {
			recordImportError(result, w.rowNumber, fmt.Sprintf("could not read the records this import names: %v", err))
		}
		return nil
	}
	kept := writes[:0]
	for _, w := range writes {
		row := map[string]interface{}{}
		if json.Unmarshal([]byte(w.data), &row) != nil {
			kept = append(kept, w)
			continue
		}
		prior := stored[w.id]
		if err := actionOnlyFieldChange(doctype, rule, row, prior); err != nil {
			recordImportError(result, w.rowNumber, err.Error())
			continue
		}
		for _, k := range rule.Fields {
			if v, ok := prior[k]; ok {
				row[k] = v
			} else {
				delete(row, k)
			}
		}
		if b, err := json.Marshal(row); err == nil {
			w.data = string(b)
		}
		kept = append(kept, w)
	}
	return kept
}

// BOMVarianceLine is one component whose quantity per finished unit differs
// between the material issued and the BOM now. Issued 0 = added since; Now 0
// = removed since.
type BOMVarianceLine struct {
	Sku    string  `json:"sku"`
	Issued float64 `json:"issued_per_unit"`
	Now    float64 `json:"now_per_unit"`
}

// BOMVarianceAck is what AcknowledgeBOMVariance records on the order.
type BOMVarianceAck struct {
	By       string            `json:"by"`
	At       string            `json:"at"`
	Reason   string            `json:"reason"`
	Variance []BOMVarianceLine `json:"variance"`
}

// productionBOMVariance reports whether the order's BOM differs from the one
// its material was issued against - the comparison finishProductionQty's
// MFG-0276 check makes - and how, per component.
func productionBOMVariance(tenantID string, data map[string]interface{}) (bool, []BOMVarianceLine, error) {
	snap, _ := data["bom_snapshot"].(string)
	if snap == "" {
		return false, nil, nil
	}
	bomID, _ := data["bom_id"].(string)
	fresh, err := explodeBOMComponents(tenantID, bomID, 1.0, map[string]bool{}, 0)
	if err != nil {
		return false, nil, err
	}
	freshBytes, err := json.Marshal(fresh)
	if err != nil {
		return false, nil, err
	}
	if string(freshBytes) == snap {
		return false, nil, nil
	}
	var issued []bomComponent
	_ = json.Unmarshal([]byte(snap), &issued)
	was, now := map[string]float64{}, map[string]float64{}
	var order []string
	for _, c := range issued {
		if _, seen := was[c.Sku]; !seen {
			order = append(order, c.Sku)
		}
		was[c.Sku] += c.Qty
	}
	for _, c := range fresh {
		if _, seen := was[c.Sku]; !seen {
			if _, dup := now[c.Sku]; !dup {
				order = append(order, c.Sku)
			}
		}
		now[c.Sku] += c.Qty
	}
	var lines []BOMVarianceLine
	for _, sku := range order {
		if round4(was[sku]) != round4(now[sku]) {
			lines = append(lines, BOMVarianceLine{Sku: sku, Issued: round4(was[sku]), Now: round4(now[sku])})
		}
	}
	return true, lines, nil
}

func round4(v float64) float64 { return math.Round(v*10000) / 10000 }

// AcknowledgeBOMVariance accepts that a production order's BOM changed after
// its material was issued (MFG-0276), so it can be finished. The caller has
// checked the Production Order update permission; a reason is required, and
// there must be a change to accept. Only the two acknowledgement keys are
// written (a merge), so a completion running alongside loses nothing.
func AcknowledgeBOMVariance(tenantID, orderID, actorUserID, reason string) (*BOMVarianceAck, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, &ValidationError{Code: BOMVarianceReasonCode, SubFor: "reason", Message: "give the reason the changed BOM is accepted for this order"}
	}
	data, status, err := fetchProductionOrder(tenantID, orderID)
	if err != nil {
		return nil, fmt.Errorf("production order not found: %v", err)
	}
	if status == "Completed" || status == "Cancelled" {
		return nil, &ValidationError{Code: BOMVarianceNoneCode, Message: fmt.Sprintf("production order %s is %s - there is nothing left to finish", orderID, status)}
	}
	changed, lines, err := productionBOMVariance(tenantID, data)
	if err != nil {
		return nil, err
	}
	if !changed {
		return nil, &ValidationError{Code: BOMVarianceNoneCode, Message: fmt.Sprintf("production order %s has no BOM change to accept - its BOM is as it was when the material was issued", orderID)}
	}
	ack := &BOMVarianceAck{By: actorUserID, At: time.Now().UTC().Format(time.RFC3339), Reason: reason, Variance: lines}
	if ack.Variance == nil {
		ack.Variance = []BOMVarianceLine{}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if err := mergeDocData(schema, "ProductionOrder", orderID, map[string]interface{}{
		"bom_variance_acknowledged": true, "bom_variance_ack": ack,
	}); err != nil {
		return nil, err
	}
	LogAuditEvent(tenantID, actorUserID, "PRODUCTION_ORDER_BOM_VARIANCE_ACK", "SUCCESS", fmt.Sprintf(
		"BOM change accepted for production order %s (BOM %s). Reason: %s. Change per unit: %s",
		orderID, strField(data, "bom_id"), reason, describeBOMVariance(lines)))
	return ack, nil
}

func describeBOMVariance(lines []BOMVarianceLine) string {
	if len(lines) == 0 {
		return "same quantities, components restructured"
	}
	parts := make([]string, 0, len(lines))
	for _, l := range lines {
		switch {
		case l.Issued == 0:
			parts = append(parts, fmt.Sprintf("%s added (%g)", l.Sku, l.Now))
		case l.Now == 0:
			parts = append(parts, fmt.Sprintf("%s removed (was %g)", l.Sku, l.Issued))
		default:
			parts = append(parts, fmt.Sprintf("%s %g -> %g", l.Sku, l.Issued, l.Now))
		}
	}
	return strings.Join(parts, "; ")
}
