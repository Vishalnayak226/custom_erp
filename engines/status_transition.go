package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
	"strings"
)

// Per-doctype status-transition map (Stage 29.8).
//
// This is the remaining half of ERP_LOOPHOLES_ANALYSIS.md's Medium #9. The
// security-relevant half was closed earlier (handlers_core_doc_engine.go's
// GLOBAL-0019 guard stops a bare doc write claiming Approved/Rejected on an
// approval-gated doctype). What was still open was a general map for the
// doctypes the approval engine does not own: masters' Active/Inactive and the
// richer lifecycles on GRN, VendorInvoice, ProductionOrder and friends.
//
// Design, per the user's 2026-07-29 decisions:
//
//   - OPT-IN STRICT. A transition with no configured rule is allowed unless
//     its doctype is flagged strict_status_transitions. That is what makes
//     this safe to ship: engine-driven flows and every un-flagged doctype
//     behave exactly as before, and a doctype is tightened only once its
//     matrix has been seeded and tested. A globally fail-closed map would
//     have had to be complete on day one or it breaks production.
//   - Reuses StatusTransitionRule (Stage 26.12.9) rather than adding a second
//     mechanism. Same master, same admin screen; it just governs every doctype
//     now instead of only order cancellation.
//
// Scope note: this governs writes coming through the generic document API,
// which is where a user can hand-craft a status. Engines that move a document
// through its own lifecycle in SQL are not routed through here - deliberately,
// since they are the code that defines the legal path rather than a caller
// asserting one.

// legacyOMSEntities are the four non-doctype entity names StatusTransitionRule
// carried before Stage 29.8 widened it. engines/orders.go still reads
// entity='Order' rows for its cancellation matrix, so these stay valid values
// even though no doctype by that name exists.
var legacyOMSEntities = map[string]bool{
	"Order":            true,
	"OrderLine":        true,
	"FulfillmentOrder": true,
	"Shipment":         true,
}

// statusTransitionRule is one row of the map, as stored in the generic
// documents table under doctype='StatusTransitionRule'.
type statusTransitionRule struct {
	allowed            bool
	requiresReasonCode bool
	found              bool
}

// lookupStatusTransitionRule finds the Active rule governing
// entity/from->to, if one exists.
func lookupStatusTransitionRule(schema, entity, fromStatus, toStatus string) (statusTransitionRule, error) {
	var allowed, requiresReason string
	err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT COALESCE(data->>'allowed', 'Yes'), COALESCE(data->>'requires_reason_code', 'No')
		   FROM %s.documents
		  WHERE doctype = 'StatusTransitionRule' AND status = 'Active' AND deleted_at IS NULL
		    AND data->>'entity' = $1 AND data->>'from_status' = $2 AND data->>'to_status' = $3
		  LIMIT 1`, schema), entity, fromStatus, toStatus).Scan(&allowed, &requiresReason)
	if err == sql.ErrNoRows {
		return statusTransitionRule{}, nil
	} else if err != nil {
		return statusTransitionRule{}, err
	}
	return statusTransitionRule{
		allowed:            allowed == "Yes",
		requiresReasonCode: requiresReason == "Yes",
		found:              true,
	}, nil
}

// isStrictStatusDoctype reports whether this doctype has opted in to
// deny-unless-listed enforcement.
func isStrictStatusDoctype(schema, doctype string) (bool, error) {
	var strict bool
	err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT strict_status_transitions FROM %s.doctype_meta WHERE name = $1`, schema), doctype).Scan(&strict)
	if err == sql.ErrNoRows {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return strict, nil
}

// ValidateStatusTransition enforces the map for one generic-doc write.
// priorStatus is the document's status before this write ("" on a create);
// newStatus is what this write will actually store - callers must pass the
// same value the handler is about to persist, not the raw payload field, so
// the check and the write can never disagree.
//
// Returns nil (allow) when: this is a create, the status isn't changing, an
// explicit rule permits it, or no rule exists and the doctype isn't strict.
func ValidateStatusTransition(tenantID, doctype, priorStatus, newStatus string, payload map[string]interface{}) error {
	// A create has no prior state to transition from - ValidateDocument's
	// Select-options check already constrains which statuses are even legal
	// values, and the approval-gate guard covers claiming Approved outright.
	if priorStatus == "" || newStatus == "" || priorStatus == newStatus {
		return nil
	}

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}

	rule, err := lookupStatusTransitionRule(schema, doctype, priorStatus, newStatus)
	if err != nil {
		return err
	}

	if rule.found {
		if !rule.allowed {
			return &ValidationError{
				Code:    "GLOBAL-0019",
				Message: fmt.Sprintf("%s cannot move from '%s' to '%s'", doctype, priorStatus, newStatus),
			}
		}
		if rule.requiresReasonCode && !hasReasonCode(payload) {
			return &ValidationError{
				Code:    "GLOBAL-0019",
				Message: fmt.Sprintf("moving %s from '%s' to '%s' requires a reason_code", doctype, priorStatus, newStatus),
			}
		}
		return nil
	}

	// No rule. Fail open unless this doctype opted in to strict.
	strict, err := isStrictStatusDoctype(schema, doctype)
	if err != nil {
		return err
	}
	if !strict {
		return nil
	}

	// Escape hatch for documents already sitting in a status the doctype
	// doesn't even declare. Found while testing against a clone of the dev
	// database: a real SalesInvoice was in status "Active", which is not in
	// its option set (Draft,Approved,Paid,Cancelled) - legacy debris from
	// before that doctype had a lifecycle. Strict mode would have frozen it
	// permanently, because no rule can exist for a from_status the schema
	// doesn't know about, and there is no legal move out. Letting a write
	// leave an unrecognised status is strictly safer than trapping the row:
	// the destination is still constrained by ValidateDocument's own
	// Select-options check, so this can only move debris back into a
	// declared status, never into a made-up one.
	known, err := isDeclaredStatusOption(schema, doctype, priorStatus)
	if err != nil {
		return err
	}
	if !known {
		return nil
	}

	allowedTargets, err := allowedTransitionsFrom(schema, doctype, priorStatus)
	if err != nil {
		return err
	}
	msg := fmt.Sprintf("%s cannot move from '%s' to '%s'", doctype, priorStatus, newStatus)
	if len(allowedTargets) > 0 {
		msg += fmt.Sprintf(" - allowed from '%s': %s", priorStatus, strings.Join(allowedTargets, ", "))
	} else {
		msg += fmt.Sprintf(" - '%s' is a terminal status", priorStatus)
	}
	return &ValidationError{Code: "GLOBAL-0019", Message: msg}
}

// isDeclaredStatusOption reports whether status is one of the values the
// doctype's own status field declares. A doctype with no Select-constrained
// status field (free-text or absent) reports false for everything, which
// correctly makes strict mode inert for it rather than blocking writes it has
// no vocabulary to reason about.
func isDeclaredStatusOption(schema, doctype, status string) (bool, error) {
	var options sql.NullString
	err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT options FROM %s.doctype_fields
		  WHERE doctype_name = $1 AND fieldname = 'status' AND fieldtype = 'Select'`, schema),
		doctype).Scan(&options)
	if err == sql.ErrNoRows {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if !options.Valid || options.String == "" {
		return false, nil
	}
	for _, opt := range strings.Split(options.String, ",") {
		if strings.TrimSpace(opt) == status {
			return true, nil
		}
	}
	return false, nil
}

// allowedTransitionsFrom lists the legal destinations from a status, so a
// rejection can tell the caller what they *can* do instead of only what they
// can't - the difference between a usable error and a dead end.
func allowedTransitionsFrom(schema, entity, fromStatus string) ([]string, error) {
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT data->>'to_status' FROM %s.documents
		  WHERE doctype = 'StatusTransitionRule' AND status = 'Active' AND deleted_at IS NULL
		    AND data->>'entity' = $1 AND data->>'from_status' = $2
		    AND COALESCE(data->>'allowed', 'Yes') = 'Yes'
		  ORDER BY data->>'to_status'`, schema), entity, fromStatus)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var to string
		if err := rows.Scan(&to); err != nil {
			return nil, err
		}
		out = append(out, to)
	}
	return out, rows.Err()
}

// resolveWrittenStatus derives the status this write will actually persist.
//
// It mirrors handlers_core_doc_engine.go's own derivation exactly - including
// its default of "Active" when the payload carries no status - and that is the
// point: a transition check that evaluated a different value than the one
// about to be stored would either block a legal write or wave through an
// illegal one. If that default ever changes, this must change with it.
//
// In practice the default rarely fires for a strict doctype: ValidateDocument
// runs earlier in the same request and rejects a missing mandatory field, and
// status is mandatory on every doctype seeded strict in Stage 29.8. It is
// mirrored anyway rather than assumed away.
func resolveWrittenStatus(payload map[string]interface{}) string {
	if s, exists := payload["status"]; exists && s != nil {
		return fmt.Sprintf("%v", s)
	}
	return "Active"
}

// hasReasonCode reports whether the payload carries a non-empty reason code.
// Accepts either spelling this repo already uses across its own screens
// (fulfillment_pickpack.go's short-pick uses reason_code, several UI payloads
// send "reason").
func hasReasonCode(payload map[string]interface{}) bool {
	for _, key := range []string{"reason_code", "reason"} {
		if v, ok := payload[key]; ok && v != nil {
			if s := strings.TrimSpace(fmt.Sprintf("%v", v)); s != "" {
				return true
			}
		}
	}
	return false
}

// validateStatusTransitionRule guards the StatusTransitionRule master itself.
// Now that entity is free text (Stage 29.8 widened it from a 4-value Select so
// new doctypes need no migration), a typo'd entity would otherwise create a
// rule that silently governs nothing - which is worse than a rejection,
// because in strict mode the admin would believe a transition was permitted
// while every attempt kept failing.
func validateStatusTransitionRule(tenantID string, payload map[string]interface{}) error {
	entity := strings.TrimSpace(fmt.Sprintf("%v", payload["entity"]))
	if entity == "" || entity == "<nil>" {
		return &ValidationError{Code: "GLOBAL-0019", Message: "entity is required"}
	}
	if legacyOMSEntities[entity] {
		return nil
	}

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var exists bool
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT EXISTS(SELECT 1 FROM %s.doctype_meta WHERE name = $1)`, schema), entity).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return &ValidationError{
			Code:    "GLOBAL-0019",
			Message: fmt.Sprintf("'%s' is not a known doctype or OMS entity - a rule naming it would govern nothing", entity),
		}
	}

	from := strings.TrimSpace(fmt.Sprintf("%v", payload["from_status"]))
	to := strings.TrimSpace(fmt.Sprintf("%v", payload["to_status"]))
	if from != "" && from == to {
		return &ValidationError{Code: "GLOBAL-0019", Message: "from_status and to_status must differ"}
	}
	return nil
}
