package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"
)

// Stage 35.8 (Settlement/payment reconciliation, the "UniReco" gap).
// MarketplaceSettlementLine rows arrive via the existing generic
// POST /api/v1/import/{doctype} + BulkImportCSV path (engines/import.go,
// db/migrations_stage35_8_settlement_reconciliation.sql registers the
// doctype) - one row per channel order in a marketplace's settlement/payout
// file. ReconcileMarketplaceSettlements below is the matching engine: it
// resolves each line to an internal order via the existing
// channel_order_mapping table (Stage 35.1), compares the marketplace's
// reported gross sale against what was actually invoiced
// (CreateSalesInvoiceFromOrder/pack_invoice.go), and either auto-posts a
// clean match or holds a Variance for a human to dispute or write off.
//
// GL split, once a line clears (auto-matched or written off): debit
// Cash/Bank (1100) for the net payout actually received, debit Marketplace
// Commission Expense (5200), debit Marketplace Shipping & Other Fees (5210),
// debit TDS/TCS Receivable (1310/1320) for the two tax credits Indian
// marketplaces deduct at source, and credit Accounts Receivable (1300) for
// the invoiced value the settlement is clearing - the same 1300 a
// SalesInvoice debits in PostSalesInvoice. Any gap between what was invoiced
// and what the marketplace actually reported as gross lands on Settlement
// Variance Written Off (5270), on whichever side keeps the entry balanced.

// settlementLineRow is one MarketplaceSettlementLine as scanned off
// data->>'...' for the reconcile pass - all money fields may have arrived as
// a CSV-imported JSON string (numFromInterface, engines/wms.go, already
// handles that) so this struct only exists post-parse.
type settlementLineRow struct {
	ID                string
	Channel           string
	ChannelOrderID    string
	SettlementBatchID string
	SettlementDate    string
	GrossAmount       float64
	Commission        float64
	ShippingFee       float64
	OtherFee          float64
	TDS               float64
	TCS               float64
	NetPayout         float64
}

// SettlementReconcileResult summarizes one ReconcileMarketplaceSettlements
// pass, mirroring BankReconcileResult's (engines/bank_reconciliation.go) own
// "report what matched, leave the rest visible" shape.
type SettlementReconcileResult struct {
	Scanned    int `json:"scanned"`
	Matched    int `json:"matched"`
	Variance   int `json:"variance"`
	Invalid    int `json:"invalid"`
	Unresolved int `json:"unresolved"`
}

// round2 is engines/gst.go's own rounding helper, reused as-is.

// resolveOrderIDFromChannelMapping reads the same channel_order_mapping
// table Stage 35.1's ImportChannelSalesOrder already populates - a
// settlement line whose channel_order_id has no mapping row yet (the
// settlement file arrived before, or independently of, order intake) is
// left Unmatched rather than guessed at.
func resolveOrderIDFromChannelMapping(schema, channel, channelOrderID string) (string, bool, error) {
	var orderID string
	err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT order_id FROM %s.channel_order_mapping WHERE channel = $1 AND channel_order_id = $2`, schema),
		channel, channelOrderID).Scan(&orderID)
	if err == sql.ErrNoRows {
		return "", false, nil
	} else if err != nil {
		return "", false, err
	}
	return orderID, true, nil
}

// resolveOrderExpectedAmount is "what we actually billed for this order" -
// the value a settlement is reconciled against. It prefers the sum of the
// order's own (non-Cancelled) SalesInvoice total_amount, since 35.4.2 already
// invoices at the package's own locked-in unit_price; an order shipped
// before any invoice exists yet (or one whose invoice flow was skipped)
// falls back to SUM(unit_price * qty) over its SalesOrderLine rows.
func resolveOrderExpectedAmount(schema, orderID string) (float64, bool, error) {
	var invoiceTotal sql.NullFloat64
	err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT SUM((data->>'total_amount')::numeric) FROM %s.documents
		 WHERE doctype = 'SalesInvoice' AND data->>'sales_order_id' = $1 AND status != 'Cancelled' AND deleted_at IS NULL`, schema),
		orderID).Scan(&invoiceTotal)
	if err != nil {
		return 0, false, err
	}
	if invoiceTotal.Valid && invoiceTotal.Float64 > 0 {
		return invoiceTotal.Float64, true, nil
	}
	var lineTotal sql.NullFloat64
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT SUM(COALESCE((data->>'unit_price')::numeric, 0) * COALESCE((data->>'qty')::numeric, 0)) FROM %s.documents
		 WHERE doctype = 'SalesOrderLine' AND data->>'order_id' = $1 AND deleted_at IS NULL`, schema),
		orderID).Scan(&lineTotal)
	if err != nil {
		return 0, false, err
	}
	if !lineTotal.Valid || lineTotal.Float64 <= 0 {
		return 0, false, nil
	}
	return lineTotal.Float64, true, nil
}

// postSettlementLineGL writes the balanced GL split described in this file's
// header comment, and is the only place either the auto-match path or the
// write-off path posts money - so both stay identical in accounting shape.
// postingKey is stable per line, so a re-run (or a caller retrying a dropped
// response) can never double-post (PostDoubleEntry's own 24.5 idempotency).
func postSettlementLineGL(tenantID, lineID string, gross, commission, shippingFee, otherFee, tds, tcs, expected, variance float64) error {
	debits := map[string]int64{}
	credits := map[string]int64{}
	netPayout := gross - commission - shippingFee - otherFee - tds - tcs
	add := func(m map[string]int64, code string, amt float64) {
		p := RupeesToPaise(amt)
		if p == 0 {
			return
		}
		m[code] += p
	}
	add(debits, "1100", netPayout)
	add(debits, "5200", commission)
	add(debits, "5210", shippingFee+otherFee)
	add(debits, "1310", tds)
	add(debits, "1320", tcs)
	if variance > 0 {
		add(debits, "5270", variance)
	} else if variance < 0 {
		add(credits, "5270", -variance)
	}
	add(credits, "1300", expected)
	if len(debits) == 0 && len(credits) == 0 {
		return nil
	}
	return PostDoubleEntry(tenantID, "MarketplaceSettlementLine", lineID, debits, credits, "",
		fmt.Sprintf("MarketplaceSettlementLine:%s:POST", lineID))
}

// applySettlementLineOutcome patches one line's data in place, used by every
// terminal transition below (Matched/Variance/Invalid/WrittenOff) so the
// jsonb shape stays identical regardless of which path set it.
func applySettlementLineOutcome(schema, lineID string, patch map[string]interface{}) error {
	marshaled, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	status, _ := patch["match_status"].(string)
	_, err = db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = data || $1::jsonb, status = COALESCE(NULLIF($2, ''), status), updated_at = CURRENT_TIMESTAMP
		 WHERE id = $3 AND doctype = 'MarketplaceSettlementLine'`, schema),
		marshaled, status, lineID)
	return err
}

// ReconcileMarketplaceSettlements scans every Unmatched line: validates its
// own arithmetic (gross minus every reported deduction must equal net
// payout - a bad settlement file shouldn't silently post), resolves it to an
// order, compares the marketplace's reported gross against what was actually
// invoiced, and either auto-posts a within-tolerance match or holds a
// Variance for RaiseSettlementDispute/WriteOffSettlementVariance. Safe to
// call repeatedly (e.g. on a schedule) - it only ever touches rows still
// Unmatched.
func ReconcileMarketplaceSettlements(tenantID, userID string) (*SettlementReconcileResult, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tolerance := GetSettingFloat(tenantID, "oms.settlement_variance_tolerance")
	if tolerance <= 0 {
		tolerance = 2.0
	}

	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT id, COALESCE(data->>'channel',''), COALESCE(data->>'channel_order_id',''),
		        COALESCE(data->>'gross_amount','0'), COALESCE(data->>'commission','0'),
		        COALESCE(data->>'shipping_fee','0'), COALESCE(data->>'other_fee','0'),
		        COALESCE(data->>'tds','0'), COALESCE(data->>'tcs','0'), COALESCE(data->>'net_payout','0')
		 FROM %s.documents
		 WHERE doctype = 'MarketplaceSettlementLine' AND deleted_at IS NULL
		   AND COALESCE(data->>'match_status', 'Unmatched') = 'Unmatched'`, schema))
	if err != nil {
		return nil, err
	}
	var lines []settlementLineRow
	for rows.Next() {
		var l settlementLineRow
		var gross, commission, shippingFee, otherFee, tds, tcs, netPayout string
		if err := rows.Scan(&l.ID, &l.Channel, &l.ChannelOrderID, &gross, &commission, &shippingFee, &otherFee, &tds, &tcs, &netPayout); err != nil {
			rows.Close()
			return nil, err
		}
		l.GrossAmount = numFromInterface(gross)
		l.Commission = numFromInterface(commission)
		l.ShippingFee = numFromInterface(shippingFee)
		l.OtherFee = numFromInterface(otherFee)
		l.TDS = numFromInterface(tds)
		l.TCS = numFromInterface(tcs)
		l.NetPayout = numFromInterface(netPayout)
		lines = append(lines, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	result := &SettlementReconcileResult{}
	for _, l := range lines {
		result.Scanned++
		derivedNet := round2(l.GrossAmount - l.Commission - l.ShippingFee - l.OtherFee - l.TDS - l.TCS)
		if math.Abs(derivedNet-round2(l.NetPayout)) > 0.01 {
			if err := applySettlementLineOutcome(schema, l.ID, map[string]interface{}{
				"match_status": "Invalid",
				"match_note":   fmt.Sprintf("gross (%.2f) minus reported deductions = %.2f, does not match reported net payout %.2f", l.GrossAmount, derivedNet, l.NetPayout),
			}); err != nil {
				return result, err
			}
			result.Invalid++
			continue
		}

		orderID, found, err := resolveOrderIDFromChannelMapping(schema, l.Channel, l.ChannelOrderID)
		if err != nil {
			return result, err
		}
		if !found {
			result.Unresolved++
			continue
		}

		expected, hasExpected, err := resolveOrderExpectedAmount(schema, orderID)
		if err != nil {
			return result, err
		}
		if !hasExpected {
			result.Unresolved++
			continue
		}

		variance := round2(expected - l.GrossAmount)
		patch := map[string]interface{}{
			"order_id":        orderID,
			"expected_amount": expected,
			"variance_amount": variance,
		}
		if math.Abs(variance) <= tolerance {
			if err := postSettlementLineGL(tenantID, l.ID, l.GrossAmount, l.Commission, l.ShippingFee, l.OtherFee, l.TDS, l.TCS, expected, variance); err != nil {
				return result, err
			}
			patch["match_status"] = "Matched"
			patch["posted_at"] = time.Now().UTC().Format(time.RFC3339)
			result.Matched++
		} else {
			patch["match_status"] = "Variance"
			result.Variance++
		}
		if err := applySettlementLineOutcome(schema, l.ID, patch); err != nil {
			return result, err
		}
	}
	LogAuditEvent(tenantID, userID, "SETTLEMENT_RECONCILE", "SUCCESS",
		fmt.Sprintf("scanned=%d matched=%d variance=%d invalid=%d unresolved=%d", result.Scanned, result.Matched, result.Variance, result.Invalid, result.Unresolved))
	return result, nil
}

func fetchSettlementLine(tenantID, lineID string) (schema string, data map[string]interface{}, err error) {
	schema, err = db.GetTenantSchema(tenantID)
	if err != nil {
		return "", nil, err
	}
	var dataBytes []byte
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT data FROM %s.documents WHERE doctype = 'MarketplaceSettlementLine' AND id = $1 AND deleted_at IS NULL`, schema),
		lineID).Scan(&dataBytes)
	if err != nil {
		return "", nil, fmt.Errorf("settlement line %s not found: %v", lineID, err)
	}
	if err := json.Unmarshal(dataBytes, &data); err != nil {
		return "", nil, err
	}
	return schema, data, nil
}

// RaiseSettlementDispute moves a held Variance line to Disputed, the
// operator's signal that they're chasing the marketplace for a correction
// rather than accepting the gap yet. Requires the same mandatory,
// category-matched ReasonCode convention as returns.go's own
// RejectReturnRequest ("Settlement" category, config-driven - no schema
// change to ReasonCode itself).
func RaiseSettlementDispute(tenantID, lineID, reasonCode, note, userID string) error {
	schema, data, err := fetchSettlementLine(tenantID, lineID)
	if err != nil {
		return err
	}
	if data["match_status"] != "Variance" {
		return fmt.Errorf("settlement line %s is not a held Variance (currently %v)", lineID, data["match_status"])
	}
	if err := requireActiveReasonCode(tenantID, reasonCode, "Settlement"); err != nil {
		return err
	}
	return applySettlementLineOutcome(schema, lineID, map[string]interface{}{
		"match_status":   "Disputed",
		"dispute_reason": reasonCode,
		"dispute_note":   note,
		"disputed_by":    userID,
		"disputed_at":    time.Now().UTC().Format(time.RFC3339),
	})
}

// ResolveSettlementDispute records the marketplace's corrected gross amount
// once a dispute is settled with them, and re-runs the same tolerance check
// ReconcileMarketplaceSettlements uses: within tolerance posts and closes as
// Matched, otherwise the line stays Disputed with its variance updated so it
// can be raised again or written off.
func ResolveSettlementDispute(tenantID, lineID string, correctedGrossAmount float64, userID string) error {
	if correctedGrossAmount < 0 {
		return errors.New("corrected gross amount must not be negative")
	}
	schema, data, err := fetchSettlementLine(tenantID, lineID)
	if err != nil {
		return err
	}
	if data["match_status"] != "Disputed" {
		return fmt.Errorf("settlement line %s is not Disputed (currently %v)", lineID, data["match_status"])
	}
	commission := numFromInterface(data["commission"])
	shippingFee := numFromInterface(data["shipping_fee"])
	otherFee := numFromInterface(data["other_fee"])
	tds := numFromInterface(data["tds"])
	tcs := numFromInterface(data["tcs"])
	expected := numFromInterface(data["expected_amount"])
	newNetPayout := round2(correctedGrossAmount - commission - shippingFee - otherFee - tds - tcs)
	variance := round2(expected - correctedGrossAmount)

	patch := map[string]interface{}{
		"gross_amount":    correctedGrossAmount,
		"net_payout":      newNetPayout,
		"variance_amount": variance,
		"resolved_by":     userID,
		"resolved_at":     time.Now().UTC().Format(time.RFC3339),
	}
	tolerance := GetSettingFloat(tenantID, "oms.settlement_variance_tolerance")
	if tolerance <= 0 {
		tolerance = 2.0
	}
	if math.Abs(variance) <= tolerance {
		if err := postSettlementLineGL(tenantID, lineID, correctedGrossAmount, commission, shippingFee, otherFee, tds, tcs, expected, variance); err != nil {
			return err
		}
		patch["match_status"] = "Matched"
		patch["posted_at"] = time.Now().UTC().Format(time.RFC3339)
	}
	return applySettlementLineOutcome(schema, lineID, patch)
}

// WriteOffSettlementVariance closes a Variance or Disputed line without
// further chasing the marketplace: posts the same GL split
// ReconcileMarketplaceSettlements would have, letting Settlement Variance
// Written Off (5270) absorb the gap between what was invoiced and what the
// marketplace actually reported/paid.
func WriteOffSettlementVariance(tenantID, lineID, reasonCode, userID string) error {
	schema, data, err := fetchSettlementLine(tenantID, lineID)
	if err != nil {
		return err
	}
	status, _ := data["match_status"].(string)
	if status != "Variance" && status != "Disputed" {
		return fmt.Errorf("settlement line %s cannot be written off from status %q", lineID, status)
	}
	if err := requireActiveReasonCode(tenantID, reasonCode, "Settlement"); err != nil {
		return err
	}
	gross := numFromInterface(data["gross_amount"])
	commission := numFromInterface(data["commission"])
	shippingFee := numFromInterface(data["shipping_fee"])
	otherFee := numFromInterface(data["other_fee"])
	tds := numFromInterface(data["tds"])
	tcs := numFromInterface(data["tcs"])
	expected := numFromInterface(data["expected_amount"])
	variance := numFromInterface(data["variance_amount"])

	if err := postSettlementLineGL(tenantID, lineID, gross, commission, shippingFee, otherFee, tds, tcs, expected, variance); err != nil {
		return err
	}
	return applySettlementLineOutcome(schema, lineID, map[string]interface{}{
		"match_status":     "WrittenOff",
		"write_off_reason": reasonCode,
		"write_off_by":     userID,
		"posted_at":        time.Now().UTC().Format(time.RFC3339),
	})
}

func init() {
	RegisterReport(ReportDefinition{
		ID: "oms-unsettled-settlements", Label: "Unsettled Marketplace Orders", Category: "OMS",
		Columns: []ReportColumn{
			{Key: "order_id", Label: "Order"}, {Key: "channel", Label: "Channel"},
			{Key: "status", Label: "Order Status"}, {Key: "days_since_ship", Label: "Days Since Ship"},
			{Key: "overdue", Label: "Overdue"},
		},
		Run: getUnsettledMarketplaceOrdersReport,
	})
	RegisterReport(ReportDefinition{
		ID: "oms-settlement-variance", Label: "Settlement Variance Queue", Category: "OMS",
		Columns: []ReportColumn{
			{Key: "line_id", Label: "Settlement Line"}, {Key: "channel", Label: "Channel"},
			{Key: "channel_order_id", Label: "Channel Order ID"}, {Key: "order_id", Label: "Order"},
			{Key: "settlement_batch_id", Label: "Batch"}, {Key: "gross_amount", Label: "Gross Amount", Sensitive: true},
			{Key: "expected_amount", Label: "Expected (Invoiced)", Sensitive: true},
			{Key: "variance_amount", Label: "Variance", Sensitive: true}, {Key: "match_status", Label: "Status"},
			{Key: "settlement_date", Label: "Settlement Date"},
		},
		Run: getSettlementVarianceReport,
	})
}

// getUnsettledMarketplaceOrdersReport flags a Shipped/Delivered channel
// order with no cleared (Matched/WrittenOff) settlement line yet - real
// UniReco's core "money owed but not yet accounted for" view. Age is
// measured off updated_at (the order's last status change), the same
// documented proxy getCourierPerformanceReport already uses for turnaround -
// no separate per-status-change timestamp history exists in this schema.
func getUnsettledMarketplaceOrdersReport(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	thresholdDays := GetSettingInt(tenantID, "oms.settlement_aging_days")
	if thresholdDays <= 0 {
		thresholdDays = 7
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT so.id, COALESCE(so.data->>'channel',''), so.status, so.updated_at
		FROM %s.documents so
		WHERE so.doctype = 'SalesOrder' AND so.deleted_at IS NULL AND so.status IN ('Shipped','Delivered')
		  AND COALESCE(so.data->>'channel','') <> ''
		  AND NOT EXISTS (
		    SELECT 1 FROM %s.documents sl WHERE sl.doctype = 'MarketplaceSettlementLine' AND sl.deleted_at IS NULL
		      AND sl.data->>'order_id' = so.id AND sl.data->>'match_status' IN ('Matched','WrittenOff')
		  )
		ORDER BY so.updated_at ASC`, schema, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	now := time.Now()
	var results []map[string]interface{}
	for rows.Next() {
		var orderID, channel, status string
		var updatedAt time.Time
		if err := rows.Scan(&orderID, &channel, &status, &updatedAt); err != nil {
			return nil, err
		}
		days := int(now.Sub(updatedAt).Hours() / 24)
		results = append(results, map[string]interface{}{
			"order_id": orderID, "channel": channel, "status": status,
			"days_since_ship": days, "overdue": days > thresholdDays,
		})
	}
	if results == nil {
		results = []map[string]interface{}{}
	}
	return results, rows.Err()
}

// getSettlementVarianceReport lists every MarketplaceSettlementLine still
// awaiting a human decision (Variance or Disputed) - the queue
// RaiseSettlementDispute/ResolveSettlementDispute/WriteOffSettlementVariance
// act on.
func getSettlementVarianceReport(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(data->>'channel',''), COALESCE(data->>'channel_order_id',''),
		       COALESCE(data->>'order_id',''), COALESCE(data->>'settlement_batch_id',''),
		       COALESCE((data->>'gross_amount')::numeric,0), COALESCE((data->>'expected_amount')::numeric,0),
		       COALESCE((data->>'variance_amount')::numeric,0), data->>'match_status', COALESCE(data->>'settlement_date','')
		FROM %s.documents
		WHERE doctype = 'MarketplaceSettlementLine' AND deleted_at IS NULL
		  AND data->>'match_status' IN ('Variance','Disputed')
		ORDER BY (data->>'variance_amount')::numeric DESC`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []map[string]interface{}
	for rows.Next() {
		var id, channel, channelOrderID, orderID, batchID, matchStatus, settlementDate string
		var gross, expected, variance float64
		if err := rows.Scan(&id, &channel, &channelOrderID, &orderID, &batchID, &gross, &expected, &variance, &matchStatus, &settlementDate); err != nil {
			return nil, err
		}
		results = append(results, map[string]interface{}{
			"line_id": id, "channel": channel, "channel_order_id": channelOrderID, "order_id": orderID,
			"settlement_batch_id": batchID, "gross_amount": gross, "expected_amount": expected,
			"variance_amount": variance, "match_status": matchStatus, "settlement_date": settlementDate,
		})
	}
	if results == nil {
		results = []map[string]interface{}{}
	}
	return results, rows.Err()
}
