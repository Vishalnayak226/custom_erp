package server

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"custom_erp/db"
	"custom_erp/engines"
)

// The generic metadata-driven document engine (GET/POST/PUT/DELETE
// /api/v1/doc/:doctype), permission checks, labels/sequence/prefix config,
// audit/system logs, the DocType Builder admin screens, and industry
// switching.

func handleGenericDoc(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	userID := r.Header.Get("Resolved-User-ID")
	location := r.Header.Get("Resolved-Location")

	// Resolve parameters using Go 1.22 enhanced routing Value methods
	doctype := r.PathValue("doctype")
	id := r.PathValue("id")

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Extension token handling (Stage 14.17-14.20): a token issued by
	// SignExtensionToken has no role (it's not a user session) - it carries
	// Resolved-Scope-Doctype instead, and is authorized here explicitly
	// rather than falling through to checkPermission below (which would
	// just deny it, correctly but with a generic and less useful error).
	// Read-only, and only for the exact doctype it was scoped to - a hired
	// 3rd-party developer's extension can look up what it needs to react to
	// a hook, never write, never see another doctype or tenant.
	if r.Header.Get("Resolved-Purpose") == "extension" {
		scopeDoctype := r.Header.Get("Resolved-Scope-Doctype")
		if r.Method != http.MethodGet || doctype != scopeDoctype {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("This token is scoped to read-only access on '%s'", scopeDoctype)})
			return
		}
		// Falls through to the normal GET handling below with an empty
		// role - the location filter's "role != HR/Admin" branch still
		// applies (an extension token is never location-exempt), and no
		// module-gate/RBAC bypass beyond the doctype-scope check above.
	} else {
		// 1. RBAC permissions verification (skipped for a scoped extension
		// token, which was already authorized above on narrower terms).
		action := ""
		switch r.Method {
		case http.MethodGet:
			action = "read"
		case http.MethodPost:
			action = "create"
			if id != "" {
				action = "update"
			}
		case http.MethodDelete:
			action = "delete"
		}
		allowed, permErr := checkPermission(tenantID, role, doctype, action)
		if permErr != nil {
			http.Error(w, permErr.Error(), http.StatusInternalServerError)
			return
		}
		if !allowed {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("You do not have permission to %s %s documents.", action, doctype)})
			return
		}
	}

	// 1b. Module-wise access control (Stage 14.1). {doctype} is a runtime
	// path param, so unlike the fixed module routes (moduleGate wraps those
	// at registration time) this has to resolve module_key per-request here.
	// A doctype with no module_key assigned (moduleKey == "") is treated as
	// ungated/core - matches this migration's additive, fail-open-for-
	// unmapped-doctypes design (existing doctypes keep working exactly as
	// before until explicitly mapped).
	if moduleKey, mErr := engines.ModuleForDoctype(tenantID, doctype); mErr == nil && moduleKey != "" {
		if enabled, _ := engines.IsModuleEnabled(tenantID, moduleKey); !enabled {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Module '%s' is disabled for this tenant", moduleKey)})
			return
		}
	}

	switch r.Method {
	case http.MethodGet:
		if id != "" {
			// Retrieve single document
			var dataStr string
			var status string
			err = db.DB.QueryRow(fmt.Sprintf(`
				SELECT data, status FROM %s.documents 
				WHERE doctype = $1 AND id = $2 AND deleted_at IS NULL`, schema), doctype, id).Scan(&dataStr, &status)
			if err == sql.ErrNoRows {
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "Document not found"})
				return
			} else if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			var dataMap map[string]interface{}
			_ = json.Unmarshal([]byte(dataStr), &dataMap)
			dataMap["id"] = id
			dataMap["status"] = status
			if dataMap, err = engines.FilterFieldsForRole(tenantID, role, doctype, dataMap); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			// Location Filter Validation (Object-Level Auth). Not every doctype
			// names this field "location" - FulfillmentTask uses "location_code" -
			// so check both rather than silently skipping the check (and letting
			// through a doc from another location) whenever a doctype uses the
			// other name.
			docLoc, hasLoc := dataMap["location"]
			if !hasLoc {
				docLoc, hasLoc = dataMap["location_code"]
			}
			if hasLoc && fmt.Sprintf("%v", docLoc) != location && role != "HR/Admin" {
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "This document does not belong to your assigned location."})
				return
			}

			_ = json.NewEncoder(w).Encode(dataMap)
		} else {
			// Retrieve multiple documents (support search, location filtering, and custom query filters)
			searchQuery := r.URL.Query().Get("q")
			query := fmt.Sprintf("SELECT id, data, status FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL", schema)
			var args []interface{}
			args = append(args, doctype)
			argIndex := 2

			// Location filtering: non-admins can only see records for their location.
			// COALESCE covers both field names in use across doctypes ("location"
			// vs FulfillmentTask's "location_code") - matches the single-doc GET
			// check above, which does the same for the same reason. The "IS NULL"
			// half matters just as much as the match itself: plenty of doctypes
			// (MarketplaceSettlement, LogisticsBooking) have no location concept
			// at all, and SQL's NULL = $x is never true - without this, every
			// non-admin would silently see zero rows of any location-less
			// doctype, not "all of them" (which is the correct behavior for a
			// doctype with nothing to scope by).
			if role != "HR/Admin" {
				query += fmt.Sprintf(" AND (COALESCE(data->>'location', data->>'location_code') = $%d OR COALESCE(data->>'location', data->>'location_code') IS NULL)", argIndex)
				args = append(args, location)
				argIndex++
			}

			// Dynamic search parameter filters check (WMS/OMS query filters)
			for key, vals := range r.URL.Query() {
				if key == "q" || key == "tenant_id" || key == "limit" || key == "offset" || len(vals) == 0 {
					continue
				}
				if !safeFilterKeyRe.MatchString(key) {
					w.WriteHeader(http.StatusBadRequest)
					_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Invalid filter parameter name: %q", key)})
					return
				}
				query += fmt.Sprintf(" AND data->>'%s' = $%d", key, argIndex)
				args = append(args, vals[0])
				argIndex++
			}

			// Pagination: bounds the response even when the caller doesn't ask for a
			// specific page, so this endpoint can never return an unbounded result set.
			// Note: when a search term (q) is active, the limit/offset bound the SQL-level
			// candidate set that gets fetched *before* the in-memory search filter below -
			// a search could miss a match sitting past the current page's window. Moving
			// search into SQL would remove that edge case but is a larger change than this
			// item calls for.
			limit := defaultListLimit
			if v := r.URL.Query().Get("limit"); v != "" {
				if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
					limit = parsed
				}
			}
			if limit > maxListLimit {
				limit = maxListLimit
			}
			offset := 0
			if v := r.URL.Query().Get("offset"); v != "" {
				if parsed, err := strconv.Atoi(v); err == nil && parsed >= 0 {
					offset = parsed
				}
			}
			query += fmt.Sprintf(" ORDER BY id LIMIT $%d OFFSET $%d", argIndex, argIndex+1)
			args = append(args, limit, offset)

			rows, err := db.DB.Query(query, args...)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			defer rows.Close()

			docs := []map[string]interface{}{}
			for rows.Next() {
				var docID string
				var dataStr string
				var status string
				if err := rows.Scan(&docID, &dataStr, &status); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}

				var dataMap map[string]interface{}
				_ = json.Unmarshal([]byte(dataStr), &dataMap)
				dataMap["id"] = docID
				dataMap["status"] = status
				if dataMap, err = engines.FilterFieldsForRole(tenantID, role, doctype, dataMap); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}

				// Local search match
				if searchQuery != "" {
					matched := false
					for _, val := range dataMap {
						if strings.Contains(strings.ToLower(fmt.Sprintf("%v", val)), strings.ToLower(searchQuery)) {
							matched = true
							break
						}
					}
					if !matched {
						continue
					}
				}

				docs = append(docs, dataMap)
			}
			_ = json.NewEncoder(w).Encode(docs)
		}

	case http.MethodPost:
		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "Invalid payload JSON", http.StatusBadRequest)
			return
		}
		if err := engines.RejectRestrictedFieldWrites(tenantID, role, doctype, payload); err != nil {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		// 2. Server-side metadata validation engine check
		err = engines.ValidateDocument(tenantID, doctype, payload)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		// Expense claim controls (Stage 13.13c, MB 16.2): date window and
		// duplicate-bill check, only on creation of a new claim - not on
		// later edits to an existing one.
		if doctype == "ExpenseClaim" && id == "" {
			if err := engines.ValidateExpenseClaimControls(tenantID, payload); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
		}

		// PIM variant uniqueness (Stage 15): unlike the ExpenseClaim check
		// above, this runs on create AND update - an edit can introduce a
		// duplicate variant combination just as easily as a create can.
		// effectiveID mirrors the docID resolution a few lines below (path
		// id on update; client-supplied payload["id"] on a create that sets
		// one explicitly, e.g. "id: code" - this codebase's own
		// convention; blank otherwise, which is fine since a fresh
		// server-generated UUID can never collide with a stored sibling).
		if doctype == "Item" {
			effectiveID := id
			if effectiveID == "" {
				if payloadID, exists := payload["id"]; exists && payloadID != nil {
					effectiveID = fmt.Sprintf("%v", payloadID)
				}
			}
			if err := engines.ValidateItemVariantUniqueness(tenantID, effectiveID, payload); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
		}

		// GST enforcement (Stage 17.5): every non-empty PO items line must
		// resolve to an Item with hsn_code/gst_rate set; the computed
		// breakdown is stored on the document itself (no GL posting here -
		// PO creation posts no GL entries in this system, GRN receipt does).
		if doctype == "PurchaseOrder" {
			breakdown, errGST := engines.ComputePurchaseOrderGST(tenantID, payload)
			if errGST != nil {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": errGST.Error()})
				return
			}
			payload["gst_breakdown"] = breakdown
		}

		// Location master validation (Stage 17.9): the doctypes/fields where
		// this codebase's existing free-text location columns are actually
		// operational (stock movement/procurement, built or touched this
		// session) - not a blanket retrofit of every doctype that happens to
		// have a location-shaped field, which would be a much larger and
		// riskier change than this stage's confirmed decision called for.
		locationFieldsByDoctype := map[string][]string{
			"PurchaseOrder": {"location", "target_warehouse"},
			"TransferOrder": {"from_warehouse", "to_warehouse"},
		}
		for _, field := range locationFieldsByDoctype[doctype] {
			locCode, _ := payload[field].(string)
			if locCode == "" {
				continue
			}
			if err := engines.ValidateLocationReference(tenantID, locCode); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("field %q: %v", field, err)})
				return
			}
		}

		// Setup Document ID and attributes
		docID := ""
		if id != "" {
			docID = id
		} else if payloadID, exists := payload["id"]; exists && payloadID != nil {
			docID = fmt.Sprintf("%v", payloadID)
		} else {
			docID = generateUUID()
		}

		// Re-approval-on-edit (Stage 13.8): capture the status this document
		// had *before* this write, so an edit to an already-Approved
		// approval-gated document can be forced back into the approval
		// queue after the upsert below, regardless of what status the
		// incoming payload itself claims.
		wasApproved := false
		if docID != "" {
			var priorStatus string
			if errPrior := db.DB.QueryRow(fmt.Sprintf(`SELECT status FROM %s.documents WHERE doctype = $1 AND id = $2`, schema), doctype, docID).Scan(&priorStatus); errPrior == nil {
				wasApproved = priorStatus == "Approved"
			}
		}

		payloadBytes, _ := json.Marshal(payload)
		statusVal := "Active"
		if s, exists := payload["status"]; exists && s != nil {
			statusVal = fmt.Sprintf("%v", s)
		}

		// Extension before_save hooks (Stage 14.17-14.20): synchronous, and
		// a failure blocks the save outright - a 3rd-party pricing/
		// validation hook that doesn't run must not let this proceed with
		// an unreviewed value. No-op (zero network calls) when no hook is
		// registered for this doctype, which is the overwhelmingly common
		// case for every tenant that hasn't set one up.
		if errHook := engines.InvokeBeforeSaveHooks(tenantID, doctype, docID, payload); errHook != nil {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": errHook.Error()})
			return
		}

		// Perform Upsert using parameterized parameters (SQL Injection Safe)
		query := fmt.Sprintf(`
			INSERT INTO %s.documents (id, doctype, data, status, created_by)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (id) DO UPDATE SET
				data = EXCLUDED.data,
				status = EXCLUDED.status,
				updated_at = CURRENT_TIMESTAMP`, schema)
		_, err = db.DB.Exec(query, docID, doctype, payloadBytes, statusVal, userID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Extension after_save hooks (Stage 14.17-14.20): fired async - the
		// save already committed, so a notification/sync hook's failure
		// can't roll it back and shouldn't slow down the response.
		engines.InvokeAfterSaveHooksAsync(tenantID, doctype, docID, payload)

		if wasApproved {
			if gated, errGate := engines.IsApprovalGated(tenantID, doctype); errGate == nil && gated {
				if errReset := engines.ResetToPendingOnEdit(tenantID, doctype, docID, userID, role, payload); errReset != nil {
					engines.LogSystemError(tenantID, r.Header.Get("Resolved-Correlation-ID"), "APPROVAL_RESET_FAILED", r.URL.Path, errReset.Error(), "")
				}
			}
		}

		// PIM Product Profile auto-create (Stage 15.2, V2 §6.1 step 2):
		// "PIM profile is auto-created with status PIM Draft." Create-only
		// (id == "" means this request hit the create route, not update) -
		// EnsurePIMProductProfile itself is also idempotent (ON CONFLICT DO
		// NOTHING), so this is belt-and-suspenders, not load-bearing.
		if doctype == "Item" && id == "" {
			if errProfile := engines.EnsurePIMProductProfile(tenantID, docID); errProfile != nil {
				engines.LogSystemError(tenantID, r.Header.Get("Resolved-Correlation-ID"), "PIM_PROFILE_CREATE_FAILED", r.URL.Path, errProfile.Error(), "")
			}
		}

		// HR Access Link Hook (Stage 13.13a, MB 16.3): an Employee's
		// active/inactive status controls their linked ERP user's ability
		// to log in.
		if doctype == "Employee" {
			empUserID, _ := payload["user_id"].(string)
			empStatus, _ := payload["status"].(string)
			if errSync := engines.SyncEmployeeAccessLink(tenantID, empUserID, empStatus); errSync != nil {
				engines.LogSystemError(tenantID, r.Header.Get("Resolved-Correlation-ID"), "ACCESS_LINK_SYNC_FAILED", r.URL.Path, errSync.Error(), "")
			}
		}

		// GRN Callback Hook: Automatically post received items to inventory ledger
		if doctype == "GRN" {
			locationCode, _ := payload["location"].(string)
			// GRN's own registered schema (db/migrations_phase3.sql) declares the mandatory
			// field as "received_items", a JSON-encoded string (same convention as BOM's
			// "components" field, engines/manufacturing.go fetchBOM) - not a raw "items"
			// array key, which was never part of GRN's declared schema and left this stock
			// posting silently unreachable for any caller filling in the actual mandatory field.
			var items []interface{}
			if receivedItemsStr, ok := payload["received_items"].(string); ok && receivedItemsStr != "" {
				_ = json.Unmarshal([]byte(receivedItemsStr), &items)
			}
			if locationCode != "" && len(items) > 0 {
				errLedger := engines.PostInventoryLedger(tenantID, locationCode, items)
				if errLedger != nil {
					log.Printf("Error posting GRN items to stock ledger: %v", errLedger)
				}
			}

			// Publish inventory transaction changed outbox event
			tx, errTx := db.DB.Begin()
			if errTx == nil {
				_ = db.SetSearchPath(tx, schema)
				_ = engines.PublishEvent(tx, schema, "inventory.stock_changed", map[string]interface{}{
					"grn_id":   docID,
					"location": locationCode,
				})
				_ = tx.Commit()
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "saved",
			"id":     docID,
		})

	case http.MethodDelete:
		if id == "" {
			http.Error(w, "Document ID is required", http.StatusBadRequest)
			return
		}

		var status, documentType string
		err = db.DB.QueryRow(fmt.Sprintf(`SELECT d.status, m.document_type FROM %s.documents d JOIN %s.doctype_meta m ON m.name = d.doctype WHERE d.id = $1 AND d.doctype = $2 AND d.deleted_at IS NULL`, schema, schema), id, doctype).Scan(&status, &documentType)
		if err == sql.ErrNoRows {
			http.Error(w, "Document not found or already deleted", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if documentType == "Transaction" && status == "Approved" {
			http.Error(w, "Approved transactional documents cannot be deleted", http.StatusBadRequest)
			return
		}
		_, err = db.DB.Exec(fmt.Sprintf("UPDATE %s.documents SET deleted_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE id = $1 AND doctype = $2 AND deleted_at IS NULL", schema), id, doctype)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		engines.LogAuditEvent(tenantID, userID, "SOFT_DELETE_"+doctype, "SUCCESS", "Document ID: "+id)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func checkPermission(tenantID string, role string, doctype string, action string) (bool, error) {
	if role == "HR/Admin" {
		return true, nil
	}

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false, err
	}

	column := ""
	switch action {
	case "read":
		column = "allow_read"
	case "create":
		column = "allow_create"
	case "update":
		column = "allow_update"
	case "delete":
		column = "allow_delete"
	default:
		return false, fmt.Errorf("invalid permission action: %s", action)
	}

	var allowed bool
	query := fmt.Sprintf("SELECT COALESCE(%s, false) FROM %s.role_permissions WHERE role = $1 AND doctype_name = $2", column, schema)
	err = db.DB.QueryRow(query, role, doctype).Scan(&allowed)
	if err == sql.ErrNoRows {
		// Default: deny if no mapping rule exists
		return false, nil
	}
	return allowed, err
}

func handleLabels(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")

	switch r.Method {
	case http.MethodGet:
		labels, err := engines.GetLabels(tenantID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(labels)

	case http.MethodPost:
		var req struct {
			OriginalText string `json:"original_text"`
			CustomText   string `json:"custom_text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid payload", http.StatusBadRequest)
			return
		}
		if req.OriginalText == "" || req.CustomText == "" {
			http.Error(w, "Fields original_text and custom_text are required", http.StatusBadRequest)
			return
		}

		err := engines.SaveLabel(tenantID, req.OriginalText, req.CustomText)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved"})

	case http.MethodDelete:
		orig := r.URL.Query().Get("original_text")
		if orig == "" {
			http.Error(w, "Query parameter original_text is required", http.StatusBadRequest)
			return
		}

		err := engines.DeleteLabel(tenantID, orig)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func handleSequence(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		DocType       string `json:"doc_type"`
		StoreCode     string `json:"store_code"`
		FinancialYear string `json:"financial_year"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}
	if req.DocType == "" || req.FinancialYear == "" {
		http.Error(w, "doc_type and financial_year are required", http.StatusBadRequest)
		return
	}

	code, err := engines.GenerateSequence(tenantID, req.DocType, req.StoreCode, req.FinancialYear)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	engines.LogAuditEvent(tenantID, "system", "GENERATE_SEQUENCE", "SUCCESS", fmt.Sprintf("Generated %s sequence code: %s", req.DocType, code))

	_ = json.NewEncoder(w).Encode(map[string]string{"code": code})
}

func handlePrefix(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	switch r.Method {
	case http.MethodGet:
		rows, err := db.DB.Query(fmt.Sprintf(`
			SELECT id, doc_type, prefix, separator, padding_width, reset_frequency, active_status 
			FROM %s.prefix_configs ORDER BY doc_type`, schema))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		type PrefixConfig struct {
			ID             string `json:"id"`
			DocType        string `json:"doc_type"`
			Prefix         string `json:"prefix"`
			Separator      string `json:"separator"`
			PaddingWidth   int    `json:"padding_width"`
			ResetFrequency string `json:"reset_frequency"`
			ActiveStatus   bool   `json:"active_status"`
		}

		configs := []PrefixConfig{}
		for rows.Next() {
			var c PrefixConfig
			err := rows.Scan(&c.ID, &c.DocType, &c.Prefix, &c.Separator, &c.PaddingWidth, &c.ResetFrequency, &c.ActiveStatus)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			configs = append(configs, c)
		}
		_ = json.NewEncoder(w).Encode(configs)

	case http.MethodPost:
		var req struct {
			DocType        string `json:"doc_type"`
			Prefix         string `json:"prefix"`
			Separator      string `json:"separator"`
			PaddingWidth   int    `json:"padding_width"`
			ResetFrequency string `json:"reset_frequency"`
			ActiveStatus   bool   `json:"active_status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid payload", http.StatusBadRequest)
			return
		}

		query := fmt.Sprintf(`
			INSERT INTO %s.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency, active_status) 
			VALUES ($1, $2, $3, $4, $5, $6) 
			ON CONFLICT (doc_type) DO UPDATE SET 
				prefix = EXCLUDED.prefix, 
				separator = EXCLUDED.separator, 
				padding_width = EXCLUDED.padding_width, 
				reset_frequency = EXCLUDED.reset_frequency, 
				active_status = EXCLUDED.active_status`, schema)
		_, err = db.DB.Exec(query, req.DocType, req.Prefix, req.Separator, req.PaddingWidth, req.ResetFrequency, req.ActiveStatus)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		engines.LogAuditEvent(tenantID, "admin", "UPDATE_PREFIX_CONFIG", "SUCCESS", fmt.Sprintf("Updated prefix config for doc_type: %s", req.DocType))
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved"})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func handleAuditLogs(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	rows, err := db.DB.Query(fmt.Sprintf("SELECT id, user_id, action, status, details, created_at FROM %s.audit_logs ORDER BY created_at DESC LIMIT 100", schema))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type AuditLog struct {
		ID        string `json:"id"`
		UserID    string `json:"user_id"`
		Action    string `json:"action"`
		Status    string `json:"status"`
		Details   string `json:"details"`
		CreatedAt string `json:"created_at"`
	}

	logs := []AuditLog{}
	for rows.Next() {
		var l AuditLog
		if err := rows.Scan(&l.ID, &l.UserID, &l.Action, &l.Status, &l.Details, &l.CreatedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		logs = append(logs, l)
	}

	_ = json.NewEncoder(w).Encode(logs)
}

func handleSystemLogs(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	rows, err := db.DB.Query(fmt.Sprintf("SELECT log_id, correlation_id, severity, module_source, error_message, stack_trace, created_at FROM %s.system_error_logs ORDER BY created_at DESC LIMIT 100", schema))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type SystemLog struct {
		LogID         string         `json:"log_id"`
		CorrelationID sql.NullString `json:"correlation_id"`
		Severity      string         `json:"severity"`
		ModuleSource  string         `json:"module_source"`
		ErrorMessage  string         `json:"error_message"`
		StackTrace    string         `json:"stack_trace"`
		CreatedAt     string         `json:"created_at"`
	}

	logs := []SystemLog{}
	for rows.Next() {
		var l SystemLog
		if err := rows.Scan(&l.LogID, &l.CorrelationID, &l.Severity, &l.ModuleSource, &l.ErrorMessage, &l.StackTrace, &l.CreatedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		logs = append(logs, l)
	}

	_ = json.NewEncoder(w).Encode(logs)
}

func handleDebugPanic(w http.ResponseWriter, r *http.Request) {
	panic("Deliberate testing panic: Dynamic recovery log engine operational!")
}

// handleReactivateMasterDocument is the only way to clear a soft-delete
// tombstone. Transactions remain immutable once deleted; masters can be
// restored by someone with their normal update permission.
func handleReactivateMasterDocument(w http.ResponseWriter, r *http.Request) {
	tenantID, role := r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Role")
	doctype, id := r.PathValue("doctype"), r.PathValue("id")
	allowed, err := checkPermission(tenantID, role, doctype, "update")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !allowed {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "You do not have permission to reactivate this document."})
		return
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var documentType string
	if err := db.DB.QueryRow(fmt.Sprintf("SELECT document_type FROM %s.doctype_meta WHERE name = $1", schema), doctype).Scan(&documentType); err != nil {
		http.Error(w, "Unknown document type", http.StatusNotFound)
		return
	}
	if documentType != "Master" {
		http.Error(w, "Only master documents can be reactivated", http.StatusBadRequest)
		return
	}
	result, err := db.DB.Exec(fmt.Sprintf("UPDATE %s.documents SET deleted_at = NULL, updated_at = CURRENT_TIMESTAMP WHERE id = $1 AND doctype = $2 AND deleted_at IS NOT NULL", schema), id, doctype)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		http.Error(w, "Deleted document not found", http.StatusNotFound)
		return
	}
	engines.LogAuditEvent(tenantID, r.Header.Get("Resolved-User-ID"), "REACTIVATE_"+doctype, "SUCCESS", "Document ID: "+id)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "reactivated"})
}

func handleGetDocTypeMeta(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	doctype := r.PathValue("doctype")

	fields, err := engines.GetDocTypeMeta(tenantID, doctype)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	fields, err = engines.FilterFieldMetaForRole(tenantID, role, doctype, fields)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(fields)
}

func handleGetDocTypes(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")

	list, err := engines.GetDocTypes(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(list)
}

func handleSaveDocType(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")

	var req struct {
		Name         string `json:"name"`
		Module       string `json:"module"`
		DocumentType string `json:"document_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	err := engines.SaveDocType(tenantID, req.Name, req.Module, req.DocumentType)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
}

func handleSaveFieldDefinition(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	doctype := r.PathValue("doctype")

	var req engines.FieldMeta
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	err := engines.SaveFieldDefinition(tenantID, doctype, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
}

func handleDeleteFieldDefinition(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	doctype := r.PathValue("doctype")
	id := r.PathValue("id")

	err := engines.DeleteFieldDefinition(tenantID, doctype, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}

func handleGetIndustries(w http.ResponseWriter, r *http.Request) {
	list := []map[string]string{
		{"code": "JEWELRY", "name": "Jewelry Industry"},
		{"code": "FOOD_BEV", "name": "Food and Beverage Industry"},
		{"code": "AUTO", "name": "Automobile Industry"},
		{"code": "CLOTHING", "name": "Clothing & Apparel Industry"},
	}
	_ = json.NewEncoder(w).Encode(list)
}

func handleSwitchIndustry(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")

	var req struct {
		IndustryCode string `json:"industry_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	profilePath := fmt.Sprintf("./public/profiles/%s.json", strings.ToLower(req.IndustryCode))
	err := engines.SwitchIndustryProfile(tenantID, profilePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to switch industry: %v", err), http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success", "message": "Industry configuration profile reloaded successfully"})
}
