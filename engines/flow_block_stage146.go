package engines

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Stage 146.3.14: a bin or zone can be closed to receipts only (drain it while
// picks continue) or to picks only (freeze picks while receipts continue).
// The block lives on the Bin / Zone record (flow_block, flow_block_reason) and
// the server keeps who and when (flow_block_by, flow_block_at). No block means
// today's behaviour. A bin is blocked when it or its zone is.

const (
	FlowBlockNoReceipts = "No receipts"
	FlowBlockNoPicks    = "No picks"
)

// StampFlowBlock keeps who and when a block was set on a Bin or Zone. An
// unchanged block keeps its stamp, a new or changed one is stamped with actor,
// a lifted one loses it.
func StampFlowBlock(doctype string, prior, payload map[string]interface{}, actor string) {
	if doctype != "Bin" && doctype != "Zone" {
		return
	}
	kind := strField(payload, "flow_block")
	if kind == "" {
		delete(payload, "flow_block_by")
		delete(payload, "flow_block_at")
		return
	}
	if prior != nil && strField(prior, "flow_block") == kind && strField(prior, "flow_block_at") != "" {
		payload["flow_block_by"] = prior["flow_block_by"]
		payload["flow_block_at"] = prior["flow_block_at"]
		return
	}
	payload["flow_block_by"] = actor
	payload["flow_block_at"] = time.Now().UTC().Format(time.RFC3339)
}

// validateFlowBlockFields refuses an unknown block kind or a block with no reason.
func validateFlowBlockFields(payload map[string]interface{}) error {
	kind := strField(payload, "flow_block")
	if kind == "" {
		return nil
	}
	if kind != FlowBlockNoReceipts && kind != FlowBlockNoPicks {
		return &ValidationError{Code: "WMS-1312", SubFor: "Bin", Message: fmt.Sprintf("block %q is not one of %q, %q", kind, FlowBlockNoReceipts, FlowBlockNoPicks)}
	}
	if strField(payload, "flow_block_reason") == "" {
		return &ValidationError{Code: "WMS-1312", SubFor: "Bin", Message: "say why the bin or zone is blocked"}
	}
	return nil
}

// binFlowBlock returns the block in force on a bin ("" none), whether it comes
// from the bin or its zone, and the reason.
func binFlowBlock(q interface {
	QueryRow(query string, args ...interface{}) *sql.Row
}, schema, binCode string) (kind, reason, from string) {
	var bk, br, zk, zr, zone string
	_ = q.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(b.data->>'flow_block', ''), COALESCE(b.data->>'flow_block_reason', ''), COALESCE(b.data->>'zone', ''),
		       COALESCE(z.data->>'flow_block', ''), COALESCE(z.data->>'flow_block_reason', '')
		FROM %[1]s.documents b
		LEFT JOIN %[1]s.documents z ON z.doctype = 'Zone' AND z.deleted_at IS NULL AND z.data->>'code' = b.data->>'zone'
		WHERE b.doctype = 'Bin' AND b.deleted_at IS NULL AND b.data->>'bin_code' = $1 LIMIT 1`, schema), binCode).Scan(&bk, &br, &zone, &zk, &zr)
	if bk != "" {
		return bk, br, "bin " + binCode
	}
	if zk != "" {
		return zk, zr, "zone " + zone
	}
	return "", "", ""
}

// enforceReceiptsBlock refuses stock into a bin (or zone) closed to receipts.
func enforceReceiptsBlock(tx *sql.Tx, schema, binCode string) error {
	kind, reason, from := binFlowBlock(tx, schema, binCode)
	if kind != FlowBlockNoReceipts {
		return nil
	}
	return &ValidationError{Code: "WMS-1313", SubFor: "Bin", Message: fmt.Sprintf("%s is closed to receipts - %s", from, strings.TrimSpace(reason))}
}

// pickBlockSQL is the condition that leaves out bins closed to picks (their
// own or their zone's). binAlias is the Bin document alias in the query.
func pickBlockSQL(schema, binAlias string) string {
	return fmt.Sprintf(`AND COALESCE(%[2]s.data->>'flow_block', '') <> 'No picks'
		  AND NOT EXISTS (SELECT 1 FROM %[1]s.documents fbz WHERE fbz.doctype = 'Zone' AND fbz.deleted_at IS NULL
		        AND fbz.data->>'code' = %[2]s.data->>'zone' AND fbz.data->>'flow_block' = 'No picks')`, schema, binAlias)
}

// AuditFlowBlock writes an audit row when a Bin or Zone block is set, changed
// or lifted by this save (who, which, what kind and why).
func AuditFlowBlock(tenantID, doctype string, prior, payload map[string]interface{}, actor string) {
	if doctype != "Bin" && doctype != "Zone" {
		return
	}
	was, now := strField(prior, "flow_block"), strField(payload, "flow_block")
	if was == now {
		return
	}
	name := strField(payload, "bin_code")
	if doctype == "Zone" {
		name = strField(payload, "code")
	}
	detail := fmt.Sprintf("%s %s: block %q -> %q", doctype, name, was, now)
	if now != "" {
		detail += " - " + strField(payload, "flow_block_reason")
	}
	LogAuditEvent(tenantID, actor, "WMS_FLOW_BLOCK", "SUCCESS", detail)
}
