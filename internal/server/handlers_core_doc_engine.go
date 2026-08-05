package server

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"custom_erp/db"
	"custom_erp/engines"
)

// The generic metadata-driven document engine (GET/POST/PUT/DELETE
// /api/v1/doc/:doctype), permission checks, labels/sequence/prefix config,
// audit/system logs, the Database Schema Design admin screens (DocType
// Builder internally), and industry switching.

// validResetFrequencies (24.21) is the exact 3-value set
// engines/numbering.go's sequence generator understands - anything else
// would otherwise be accepted at save time and only surface as a problem
// later, at sequence-generation time.
var validResetFrequencies = map[string]bool{
	"ANNUAL":  true,
	"MONTHLY": true,
	"NEVER":   true,
}

// validIndustryCodes (24.3) is shared between handleGetIndustries (the
// list a client can pick from) and handleSwitchIndustry (which must reject
// anything outside that same list before it touches the filesystem).
var validIndustryCodes = map[string]bool{
	"JEWELRY":       true,
	"FOOD_BEV":      true,
	"AUTO":          true,
	"CLOTHING":      true,
	"PHARMA":        true,
	"METAL":         true,
	"CONSTRUCTION":  true,
	"MEDICAL":       true,
	"SEMICONDUCTOR": true,
	"AGRICULTURE":   true,
}

// supplierRole is the limited-role login an outside supplier signs in as
// (Stage 26.4.10). Named here rather than repeated as a string literal
// because every one of the row-scoping checks below keys off it, and a typo
// in any one of them would silently disable that check.
const supplierRole = "Supplier"

// supplierScopeField is the field a scoped row is matched on - the Vendor
// code the submission belongs to.
const supplierScopeField = "supplier_code"

func handleGenericDoc(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	userID := r.Header.Get("Resolved-User-ID")
	location := r.Header.Get("Resolved-Location")

	// Resolve parameters using Go 1.22 enhanced routing Value methods
	doctype := r.PathValue("doctype")
	id := r.PathValue("id")

	// 26.4.10: a Supplier login is an OUTSIDE party. Doctype-level RBAC alone
	// would let every supplier read every other supplier's submissions, so
	// this role additionally gets row-level scoping to its own Vendor,
	// enforced at this one choke point every document read and write already
	// passes through rather than in each branch below. Resolved once here;
	// supplierCode stays "" for every internal role, which is what makes all
	// three checks below no-ops for everyone else.
	supplierCode := ""
	if role == supplierRole {
		var supErr error
		supplierCode, supErr = engines.SupplierCodeForUser(tenantID, userID)
		if supErr != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to resolve the supplier for this account")
			return
		}
		// A Supplier account with no Vendor linked can reach nothing at all.
		// Failing closed matters more here than a helpful error: an unscoped
		// supplier session is precisely the cross-tenant-style leak this
		// scoping exists to prevent.
		if supplierCode == "" {
			writeAPIErrorGeneric(w, r, http.StatusForbidden, "This supplier account is not linked to a vendor yet. Ask your contact at the company to finish setting it up.")
			return
		}
	}

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
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
			writeAPIErrorGeneric(w, r, http.StatusForbidden, fmt.Sprintf("This token is scoped to read-only access on '%s'", scopeDoctype))
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
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, permErr.Error())
			return
		}
		if !allowed {
			writeAPIError(w, r, "GLOBAL-0011", "")
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
			writeAPIError(w, r, "SAAS-0191", "")
			return
		}
	}

	switch r.Method {
	case http.MethodGet:
		if id != "" {
			// Retrieve single document
			var dataStr string
			var status string
			var version int
			err = db.DB.QueryRow(fmt.Sprintf(`
				SELECT data, status, version FROM %s.documents
				WHERE doctype = $1 AND id = $2 AND deleted_at IS NULL`, schema), doctype, id).Scan(&dataStr, &status, &version)
			if err == sql.ErrNoRows {
				writeAPIError(w, r, "GLOBAL-0004", "")
				return
			} else if err != nil {
				writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
				return
			}

			var dataMap map[string]interface{}
			if err := json.Unmarshal([]byte(dataStr), &dataMap); err != nil {
				// 24.18: a nil dataMap from a failed unmarshal would
				// otherwise panic on the assignment just below
				// ("assignment to entry in nil map").
				engines.LogSystemError(tenantID, r.Header.Get("Resolved-Correlation-ID"), "ERROR", r.URL.Path, fmt.Sprintf("corrupt stored data for %s %s: %v", doctype, id, err), "")
				writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Stored document data is corrupt")
				return
			}
			dataMap["id"] = id
			dataMap["status"] = status
			// 24.10: surfaced so a caller can round-trip it back as
			// expected_version on the next update - see the POST branch below.
			dataMap["version"] = version
			if dataMap, err = engines.FilterFieldsForRole(tenantID, role, doctype, dataMap); err != nil {
				writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
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
				writeAPIError(w, r, "GLOBAL-0011", "")
				return
			}

			// 26.4.10: the object-level half of supplier scoping. A row that
			// carries a supplier_code must match this session's; a row with
			// none (an Item, say) is not supplier-scoped and stays readable,
			// mirroring exactly how the location check above treats a doctype
			// with no location field.
			if supplierCode != "" {
				if docSup, ok := dataMap[supplierScopeField]; ok && fmt.Sprintf("%v", docSup) != supplierCode {
					writeAPIError(w, r, "GLOBAL-0011", "")
					return
				}
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

			// 26.4.10: the list half of supplier scoping, same shape and same
			// IS NULL reasoning as the location filter directly above - a
			// doctype with no supplier_code (Item) stays fully listable, a
			// doctype that has one is narrowed to this supplier's own rows.
			if supplierCode != "" {
				query += fmt.Sprintf(" AND (data->>'%s' = $%d OR data->>'%s' IS NULL)", supplierScopeField, argIndex, supplierScopeField)
				args = append(args, supplierCode)
				argIndex++
			}

			// Dynamic search parameter filters check (WMS/OMS query filters)
			for key, vals := range r.URL.Query() {
				if key == "q" || key == "tenant_id" || key == "limit" || key == "offset" || len(vals) == 0 {
					continue
				}
				if !safeFilterKeyRe.MatchString(key) {
					writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, fmt.Sprintf("Invalid filter parameter name: %q", key))
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
			limit := defaultListLimitFor(tenantID)
			if v := r.URL.Query().Get("limit"); v != "" {
				if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
					limit = parsed
				}
			}
			if maxLimit := maxListLimitFor(tenantID); limit > maxLimit {
				limit = maxLimit
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
				writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
				return
			}
			defer rows.Close()

			docs := []map[string]interface{}{}
			for rows.Next() {
				var docID string
				var dataStr string
				var status string
				if err := rows.Scan(&docID, &dataStr, &status); err != nil {
					writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
					return
				}

				var dataMap map[string]interface{}
				if err := json.Unmarshal([]byte(dataStr), &dataMap); err != nil {
					// 24.18: skip this one corrupt row rather than panicking
					// on the nil-map assignment below and failing the whole
					// list for every other, valid document.
					engines.LogSystemError(tenantID, r.Header.Get("Resolved-Correlation-ID"), "ERROR", r.URL.Path, fmt.Sprintf("corrupt stored data for %s %s: %v", doctype, docID, err), "")
					continue
				}
				dataMap["id"] = docID
				dataMap["status"] = status
				if dataMap, err = engines.FilterFieldsForRole(tenantID, role, doctype, dataMap); err != nil {
					writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
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
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload JSON")
			return
		}
		if err := engines.RejectRestrictedFieldWrites(tenantID, role, doctype, payload); err != nil {
			writeAPIErrorGeneric(w, r, http.StatusForbidden, err.Error())
			return
		}

		// 26.4.10: the write half of supplier scoping.
		//
		// On create, supplier_code is OVERWRITTEN from the session rather than
		// validated against it - a supplier never gets to choose whose name a
		// submission is filed under, even by accident. On update, the row's
		// stored supplier_code is what is checked: the payload's copy is
		// attacker-controlled, so trusting it would let a supplier re-target
		// someone else's record simply by posting the right id.
		if supplierCode != "" {
			if id == "" {
				payload[supplierScopeField] = supplierCode
			} else {
				var ownerCode string
				ownErr := db.DB.QueryRow(fmt.Sprintf(
					`SELECT COALESCE(data->>'%s', '') FROM %s.documents WHERE id = $1 AND doctype = $2 AND deleted_at IS NULL`,
					supplierScopeField, schema), id, doctype).Scan(&ownerCode)
				if ownErr != nil || (ownerCode != "" && ownerCode != supplierCode) {
					writeAPIError(w, r, "GLOBAL-0011", "")
					return
				}
				payload[supplierScopeField] = supplierCode
			}
		}

		// Purchase Requisitions are numbered by the server from Prefix Configs
		// (the PR series), not by a value typed into the generic form. Saving a
		// requirement also learns its description into the reusable master used
		// by the form's next type-ahead search. This is deliberately before
		// metadata validation because code is a mandatory field that is supplied
		// here for a new record.
		if doctype == "PurchaseRequisition" {
			if err := engines.PreparePurchaseRequisition(tenantID, location, id == "", payload); err != nil {
				writeEngineError(w, r, err, http.StatusUnprocessableEntity)
				return
			}
		}

		// Server-generated document numbers (Stage 30.6). Same contract as
		// PreparePurchaseRequisition just above, for the rest of the doctypes
		// whose create screens used to ask a human to type the number: the
		// number is drawn from the tenant's Prefix Configs series under a row
		// lock, never from the request. A no-op for any doctype without a
		// registered series, and for every update (id != "").
		//
		// Ordered before ValidateDocument for the same reason PR's is - the
		// fields it fills are mandatory, so they must be populated before the
		// mandatory-field check runs.
		if err := engines.PrepareDocumentNumber(tenantID, location, doctype, id == "", payload); err != nil {
			writeEngineError(w, r, err, http.StatusUnprocessableEntity)
			return
		}

		// Fill the derived half of any duplicate mandatory field pair
		// (Stage 30.5.6 - PurchaseOrder's vendor/vendor_id). Same position and
		// same reason as the numbering call above: both halves are mandatory,
		// so the copy has to happen before ValidateDocument runs. Applies to
		// updates as well as creates, since an edit that changes the vendor
		// must move both keys together or the two would drift apart.
		engines.PrepareMirroredFields(doctype, payload)

		// A GRN's receiving location defaults from its PO's target warehouse
		// when the caller didn't supply one (Stage 30.2.1). Before metadata
		// validation for the same reason PreparePurchaseRequisition is: the
		// field it fills in is mandatory, so the default has to be in place
		// before the mandatory check runs.
		if doctype == "GRN" {
			if err := engines.PrepareGRNReceipt(tenantID, payload); err != nil {
				writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to resolve the receiving location from the purchase order")
				return
			}
		}

		// 2. Server-side metadata validation engine check
		err = engines.ValidateDocument(tenantID, doctype, payload)
		if err != nil {
			// Stage 25: ValidateDocument attaches a precise catalog code for
			// its known scenarios (mandatory/format/select/link, shared
			// across every doctype); anything else (metadata lookup
			// failure, etc.) falls back to generic, same as before.
			if verr, ok := err.(*engines.ValidationError); ok && verr.Code != "" {
				writeAPIErrorDetail(w, r, verr.Code, verr.SubFor, verr.Message)
			} else {
				writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
			}
			return
		}
		if doctype == "PurchaseRequisition" {
			description, _ := payload["description"].(string)
			if err := engines.EnsurePurchaseRequisitionDescription(tenantID, description); err != nil {
				writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
				return
			}
		}

		// ReportColumnProfile (Stage 28.3): a Universal (shared) column profile
		// can only be created or edited by privileged roles - Personal profiles
		// are unrestricted. Enforced here at the generic-doc choke point so the
		// client-side gate can't be bypassed by a hand-crafted request.
		if doctype == "ReportColumnProfile" {
			if scope, _ := payload["scope"].(string); scope == "Universal" && role != "HR/Admin" && role != "Store Manager" {
				writeAPIErrorGeneric(w, r, http.StatusForbidden, "Only HR/Admin or Store Manager can create or edit a Universal column profile")
				return
			}
		}

		// Expense claim controls (Stage 13.13c, MB 16.2): date window and
		// duplicate-bill check, only on creation of a new claim - not on
		// later edits to an existing one.
		if doctype == "ExpenseClaim" && id == "" {
			if err := engines.ValidateExpenseClaimControls(tenantID, payload); err != nil {
				writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
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
				writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
				return
			}
			if err := engines.ValidateMasterDataRules(tenantID, effectiveID, doctype, payload); err != nil {
				if verr, ok := err.(*engines.ValidationError); ok && verr.Code != "" {
					writeAPIErrorDetail(w, r, verr.Code, verr.SubFor, verr.Message)
				} else {
					writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
				}
				return
			}
		}

		// Master Data field-format rules (Stage 25 Batch 2): Vendor/Customer
		// don't need the id-resolution dance above (their checks are
		// format-only, no cross-row query), so they're not folded into the
		// Item block.
		if doctype == "Vendor" || doctype == "Customer" {
			if err := engines.ValidateMasterDataRules(tenantID, id, doctype, payload); err != nil {
				if verr, ok := err.(*engines.ValidationError); ok && verr.Code != "" {
					writeAPIErrorDetail(w, r, verr.Code, verr.SubFor, verr.Message)
				} else {
					writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
				}
				return
			}
		}

		// Duplicate-content detection (Stage 26.4.2): ProductContent's own
		// "<item>::<language>" composite id (Stage 15) is always client-set in
		// the payload on its upsert-style save, never present in the URL path -
		// same effectiveID resolution the Item block above uses, needed here
		// too so the duplicate-title query can exclude the document's own row.
		if doctype == "ProductContent" {
			effectiveID := id
			if effectiveID == "" {
				if payloadID, exists := payload["id"]; exists && payloadID != nil {
					effectiveID = fmt.Sprintf("%v", payloadID)
				}
			}
			if err := engines.ValidateMasterDataRules(tenantID, effectiveID, doctype, payload); err != nil {
				if verr, ok := err.(*engines.ValidationError); ok && verr.Code != "" {
					writeAPIErrorDetail(w, r, verr.Code, verr.SubFor, verr.Message)
				} else {
					writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
				}
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
				writeEngineError(w, r, errGST, http.StatusUnprocessableEntity)
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
		// Widened 24.37/18.2: Stage 18.1's Attendance/Asset/ExpenseClaim/
		// ProductionOrder "Location" typeahead fields were never wired into
		// this same choke point - every one of their existing live values
		// already matches a real Location id (verified against the dev DB
		// before enabling this), so no backfill was needed here, unlike the
		// Vendor/Item Link conversions added alongside this.
		locationFieldsByDoctype := map[string][]string{
			"PurchaseOrder":   {"location", "target_warehouse"},
			"TransferOrder":   {"from_warehouse", "to_warehouse"},
			"Attendance":      {"location"},
			"Asset":           {"location"},
			"ExpenseClaim":    {"location"},
			"ProductionOrder": {"location"},
		}
		for _, field := range locationFieldsByDoctype[doctype] {
			locCode, _ := payload[field].(string)
			if locCode == "" {
				continue
			}
			if err := engines.ValidateLocationReference(tenantID, locCode); err != nil {
				writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, fmt.Sprintf("field %q: %v", field, err))
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
		// incoming payload itself claims. Stage 25 Batch 3 reuses this same
		// pre-write lookup (now also capturing the prior data, not just
		// status) for PurchaseOrder/ProductionOrder's own edit-gate checks
		// just below, rather than a second query.
		wasApproved := false
		var priorStatus string
		var priorData map[string]interface{}
		if docID != "" {
			var priorDataStr string
			if errPrior := db.DB.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = $1 AND id = $2`, schema), doctype, docID).Scan(&priorDataStr, &priorStatus); errPrior == nil {
				wasApproved = priorStatus == "Approved"
				// 24.33: a failure here must not silently fall through with
				// priorData == nil - every edit-gate check downstream
				// (validatePurchaseOrderEditRules etc.) treats priorData ==
				// nil as "this is a create, nothing to gate", so swallowing
				// the error would let an invalid status transition through
				// on a row whose stored JSON happens to be corrupted.
				if err := json.Unmarshal([]byte(priorDataStr), &priorData); err != nil {
					writeAPIErrorGeneric(w, r, http.StatusInternalServerError, fmt.Sprintf("stored document %s data is corrupted: %v", docID, err))
					return
				}
			}
		}

		// Stage 25 Batch 3: GRN/PurchaseOrder/TransferOrder/ProductionOrder/
		// Employee/Leave transactional checks - same choke point as Batch
		// 2's ValidateMasterDataRules above, just for this stage's modules.
		if err := engines.ValidateTransactionalRules(tenantID, doctype, docID, priorStatus, priorData, payload); err != nil {
			if verr, ok := err.(*engines.ValidationError); ok && verr.Code != "" {
				writeAPIErrorDetail(w, r, verr.Code, verr.SubFor, verr.Message)
			} else {
				writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
			}
			return
		}

		// Optimistic locking (24.10): a caller can optionally include
		// expected_version (popped out before marshaling - not document
		// business data) to assert which version it last read. Omitted
		// (every existing caller today) preserves the exact prior
		// last-write-wins behavior; supplying it turns a stale concurrent
		// edit into a clear conflict instead of a silent overwrite.
		var expectedVersion *int
		if v, exists := payload["expected_version"]; exists {
			if n, ok := v.(float64); ok {
				iv := int(n)
				expectedVersion = &iv
			}
			delete(payload, "expected_version")
		}

		payloadBytes, _ := json.Marshal(payload)
		statusVal := "Active"
		if s, exists := payload["status"]; exists && s != nil {
			statusVal = fmt.Sprintf("%v", s)
		}

		// GLOBAL-0019 (ERP_LOOPHOLES_ANALYSIS.md Medium #9, "No Validation on
		// Document Status Transitions"): a plain create/update on an
		// approval-gated doctype must never be able to claim Approved/
		// Rejected directly - those states may only be reached through
		// SubmitForApproval/DecideApproval's own maker-checker/role/location
		// checks. This does NOT need a per-doctype valid-transition map (the
		// broader, genuinely open half of that finding, left for a future
		// business-rules decision) - approval-gated doctypes already have a
		// fully-defined state machine owned by the approval engine itself,
		// so "don't let a bare doc write short-circuit it" is enforceable
		// today with zero new business input. statusVal != priorStatus is
		// the guard: an edit that merely round-trips an already-Approved
		// document's unchanged status (the common "GET included status,
		// PUT sent the whole object back" pattern) still falls through here
		// exactly as before - the existing wasApproved/ResetToPendingOnEdit
		// logic below still forces it back to Pending Approval regardless.
		if (statusVal == "Approved" || statusVal == "Rejected") && statusVal != priorStatus {
			if gated, errGate := engines.IsApprovalGated(tenantID, doctype); errGate == nil && gated {
				writeAPIError(w, r, "GLOBAL-0019", "")
				return
			}
		}

		// Extension before_save hooks (Stage 14.17-14.20): synchronous, and
		// a failure blocks the save outright - a 3rd-party pricing/
		// validation hook that doesn't run must not let this proceed with
		// an unreviewed value. No-op (zero network calls) when no hook is
		// registered for this doctype, which is the overwhelmingly common
		// case for every tenant that hasn't set one up.
		if errHook := engines.InvokeBeforeSaveHooks(tenantID, doctype, docID, payload); errHook != nil {
			writeEngineError(w, r, errHook, http.StatusBadGateway)
			return
		}

		// Perform Upsert using parameterized parameters (SQL Injection Safe).
		// version = version + 1 on every successful write; the ON CONFLICT
		// DO UPDATE's WHERE clause (24.10) is what makes the write
		// conditional - it only ever evaluates on an actual conflict (an
		// update to a pre-existing row), never on a fresh create, and only
		// blocks the write when the caller supplied an expected_version that
		// no longer matches. NULL (no expected_version supplied) always
		// passes, so this is a no-op for every caller that doesn't opt in.
		query := fmt.Sprintf(`
			INSERT INTO %s.documents (id, doctype, data, status, created_by, version)
			VALUES ($1, $2, $3, $4, $5, 1)
			ON CONFLICT (id) DO UPDATE SET
				data = EXCLUDED.data,
				status = EXCLUDED.status,
				updated_at = CURRENT_TIMESTAMP,
				version = %s.documents.version + 1
			WHERE ($6::int IS NULL OR %s.documents.version = $6)`, schema, schema, schema)
		result, err := db.DB.Exec(query, docID, doctype, payloadBytes, statusVal, userID, expectedVersion)
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		if rowsAffected, _ := result.RowsAffected(); rowsAffected == 0 && expectedVersion != nil {
			writeAPIErrorGeneric(w, r, http.StatusConflict, "This document was modified by someone else since you last loaded it - please refresh and try again")
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

		// Attendance location-mismatch check (HR-0268, Stage 25 Batch 3): a
		// Warning catalog entry with Blocking:false - it logs/audits, it
		// never rejects the save (unlike the checks routed through
		// ValidateTransactionalRules above, which are all Blocking:true).
		if doctype == "Attendance" {
			if mismatched, msg := engines.CheckAttendanceLocationMismatch(tenantID, payload); mismatched {
				logForEntry(r, errorCatalog["HR-0268"], msg)
			}
		}

		// GRN Callback Hook: Automatically post received items to inventory ledger.
		// Stage 26.3.1: gated to id == "" (create-only), same convention as the
		// Item/PIM-profile create-only check above - this previously ran on
		// EVERY save including edits, so re-saving an existing GRN with its
		// same received_items (e.g. a future Approve/Cancel status-transition
		// action) would silently double-post the same qty into
		// inventory_availability a second time. GRN had no UI at all before
		// this stage's Workbench screen, so the bug was real but dormant
		// (unreachable) until now - closing it here rather than shipping a
		// screen that could newly trigger it.
		if doctype == "GRN" && id == "" {
			locationCode, _ := payload["location"].(string)
			// GRN's own registered schema (db/migrations_phase3.sql) declares the mandatory
			// field as "received_items", a JSON-encoded string (same convention as BOM's
			// "components" field, engines/manufacturing.go fetchBOM) - not a raw "items"
			// array key, which was never part of GRN's declared schema and left this stock
			// posting silently unreachable for any caller filling in the actual mandatory field.
			var items []interface{}
			if receivedItemsStr, ok := payload["received_items"].(string); ok && receivedItemsStr != "" {
				if err := json.Unmarshal([]byte(receivedItemsStr), &items); err != nil {
					// 24.18: a malformed received_items previously failed
					// silently (items stayed nil, so the len(items) > 0
					// check below just skipped posting) - the GRN would
					// have saved successfully with zero stock effect and no
					// indication anything was wrong. Reject it instead.
					writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, fmt.Sprintf("received_items is not valid JSON: %v", err))
					return
				}
			}
			if locationCode != "" && len(items) > 0 {
				// Stage 26.5.2: QC-aware posting - accepted qty still lands
				// in `available` exactly as PostInventoryLedger always did;
				// rejected/damaged qty now routes to the qc_hold/damaged
				// buckets instead of being silently posted as available
				// alongside it (see engines/wms_receiving.go).
				_, errLedger := engines.PostGRNReceiptWithQC(tenantID, locationCode, items, userID, docID)
				if errLedger != nil {
					// Stage 30.2.1: this used to be a bare log.Printf, so a
					// failed stock post returned HTTP 200 "saved" - the
					// receipt counted against the PO (closing it to further
					// GRNs, PURCHA-0084) while no stock ever moved, and the
					// only trace was a line in the server log nobody reads.
					//
					// The document row is already committed by this point and
					// can't be rolled back into the same transaction, so the
					// receipt is reversed the way the domain already expresses
					// it - status Cancelled, which is a declared GRN status
					// and is exactly what fetchGRNReceivedQuantities excludes,
					// so the PO stays open for a real receipt. Then the caller
					// is told, instead of being congratulated.
					engines.LogSystemError(tenantID, r.Header.Get("Resolved-Correlation-ID"), "ERROR", r.URL.Path,
						fmt.Sprintf("GRN %s: stock posting failed, receipt cancelled: %v", docID, errLedger), "")
					if _, errCancel := db.DB.Exec(fmt.Sprintf(
						`UPDATE %s.documents SET status = 'Cancelled', data = jsonb_set(data::jsonb, '{posting_error}', to_jsonb($1::text), true), updated_at = CURRENT_TIMESTAMP WHERE doctype = 'GRN' AND id = $2`, schema),
						errLedger.Error(), docID); errCancel != nil {
						// Reversal itself failed - now the row really is
						// inconsistent, so say so loudly rather than reporting
						// the friendlier "nothing was posted" message below.
						engines.LogSystemError(tenantID, r.Header.Get("Resolved-Correlation-ID"), "CRITICAL", r.URL.Path,
							fmt.Sprintf("GRN %s: could not cancel after a failed stock post - the receipt counts against its PO but no stock moved: %v", docID, errCancel), "")
						writeAPIErrorGeneric(w, r, http.StatusInternalServerError, fmt.Sprintf("Goods receipt %s could not post to stock and could not be reversed automatically - cancel it manually before receiving again.", docID))
						return
					}
					writeAPIErrorGeneric(w, r, http.StatusInternalServerError, fmt.Sprintf("Goods receipt %s could not be posted to stock at %s, so it was cancelled and no stock was added. Check the location is active, then post the receipt again.", docID, locationCode))
					return
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
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Document ID is required")
			return
		}

		var status, documentType string
		err = db.DB.QueryRow(fmt.Sprintf(`SELECT d.status, m.document_type FROM %s.documents d JOIN %s.doctype_meta m ON m.name = d.doctype WHERE d.id = $1 AND d.doctype = $2 AND d.deleted_at IS NULL`, schema, schema), id, doctype).Scan(&status, &documentType)
		if err == sql.ErrNoRows {
			writeAPIError(w, r, "GLOBAL-0004", "")
			return
		}
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		if documentType == "Transaction" && status == "Approved" {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Approved transactional documents cannot be deleted")
			return
		}
		_, err = db.DB.Exec(fmt.Sprintf("UPDATE %s.documents SET deleted_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE id = $1 AND doctype = $2 AND deleted_at IS NULL", schema), id, doctype)
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
			return
		}

		engines.LogAuditEvent(tenantID, userID, "SOFT_DELETE_"+doctype, "SUCCESS", "Document ID: "+id)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})

	default:
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed on this endpoint")
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
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		_ = json.NewEncoder(w).Encode(labels)

	case http.MethodPost:
		if !requireHRAdmin(w, r, r.Header.Get("Resolved-Role")) {
			return
		}
		var req struct {
			OriginalText string `json:"original_text"`
			CustomText   string `json:"custom_text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
			return
		}
		if req.OriginalText == "" || req.CustomText == "" {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields original_text and custom_text are required")
			return
		}

		err := engines.SaveLabel(tenantID, req.OriginalText, req.CustomText)
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		// META-0202 (Stage 25.5): non-blocking - the save above already
		// committed, this only logs/audits a same-target-text collision so
		// an admin renaming two different concepts to the same word finds
		// out, without being stopped from doing it.
		if conflict, msg := engines.CheckLabelReplacementConflict(tenantID, req.OriginalText, req.CustomText); conflict {
			logForEntry(r, errorCatalog["META-0202"], msg)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved"})

	case http.MethodDelete:
		if !requireHRAdmin(w, r, r.Header.Get("Resolved-Role")) {
			return
		}
		orig := r.URL.Query().Get("original_text")
		if orig == "" {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Query parameter original_text is required")
			return
		}

		err := engines.DeleteLabel(tenantID, orig)
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})

	default:
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed on this endpoint")
	}
}

// handleSequence mints numbering-sequence codes - HR/Admin-only (24.2):
// this is global config generation, not a per-record action, so the same
// gate as handleLabels/handlePrefix's write paths applies to the whole
// handler rather than just a subset of methods.
func handleSequence(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed on this endpoint")
		return
	}
	if !requireHRAdmin(w, r, r.Header.Get("Resolved-Role")) {
		return
	}

	var req struct {
		DocType       string `json:"doc_type"`
		StoreCode     string `json:"store_code"`
		FinancialYear string `json:"financial_year"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
		return
	}
	if req.DocType == "" || req.FinancialYear == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "doc_type and financial_year are required")
		return
	}

	code, err := engines.GenerateSequence(tenantID, req.DocType, req.StoreCode, req.FinancialYear)
	if err != nil {
		// ADMINC-0030 (Stage 25.5): an inactive numbering config is a
		// client-correctable config problem, not a server crash - this was
		// a blanket 500 for every GenerateSequence error before the engine
		// started returning a precise *ValidationError for that case.
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}

	engines.LogAuditEvent(tenantID, "system", "GENERATE_SEQUENCE", "SUCCESS", fmt.Sprintf("Generated %s sequence code: %s", req.DocType, code))

	_ = json.NewEncoder(w).Encode(map[string]string{"code": code})
}

func handlePrefix(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	switch r.Method {
	case http.MethodGet:
		rows, err := db.DB.Query(fmt.Sprintf(`
			SELECT id, doc_type, prefix, separator, padding_width, reset_frequency, active_status, COALESCE(include_store, TRUE)
			FROM %s.prefix_configs ORDER BY doc_type`, schema))
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
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
			IncludeStore   bool   `json:"include_store"`
		}

		configs := []PrefixConfig{}
		for rows.Next() {
			var c PrefixConfig
			err := rows.Scan(&c.ID, &c.DocType, &c.Prefix, &c.Separator, &c.PaddingWidth, &c.ResetFrequency, &c.ActiveStatus, &c.IncludeStore)
			if err != nil {
				writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
				return
			}
			configs = append(configs, c)
		}
		_ = json.NewEncoder(w).Encode(configs)

	case http.MethodPost:
		if !requireHRAdmin(w, r, r.Header.Get("Resolved-Role")) {
			return
		}
		// IncludeStore is a pointer so an omitted field keeps the stored value
		// rather than silently reading as Go's false - this endpoint is a full
		// upsert, and a caller that predates the field would otherwise turn
		// the store segment off on every save it makes.
		var req struct {
			DocType        string `json:"doc_type"`
			Prefix         string `json:"prefix"`
			Separator      string `json:"separator"`
			PaddingWidth   int    `json:"padding_width"`
			ResetFrequency string `json:"reset_frequency"`
			ActiveStatus   bool   `json:"active_status"`
			IncludeStore   *bool  `json:"include_store"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
			return
		}
		if !validResetFrequencies[req.ResetFrequency] {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "reset_frequency must be one of ANNUAL, MONTHLY, NEVER")
			return
		}
		includeStore := true
		if req.IncludeStore != nil {
			includeStore = *req.IncludeStore
		}

		query := fmt.Sprintf(`
			INSERT INTO %s.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency, active_status, include_store)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (doc_type) DO UPDATE SET
				prefix = EXCLUDED.prefix,
				separator = EXCLUDED.separator,
				padding_width = EXCLUDED.padding_width,
				reset_frequency = EXCLUDED.reset_frequency,
				active_status = EXCLUDED.active_status,
				include_store = COALESCE($8, %s.prefix_configs.include_store)`, schema, schema)
		_, err = db.DB.Exec(query, req.DocType, req.Prefix, req.Separator, req.PaddingWidth, req.ResetFrequency, req.ActiveStatus, includeStore, req.IncludeStore)
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
			return
		}

		engines.LogAuditEvent(tenantID, "admin", "UPDATE_PREFIX_CONFIG", "SUCCESS", fmt.Sprintf("Updated prefix config for doc_type: %s", req.DocType))
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved"})

	default:
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed on this endpoint")
	}
}

func handleAuditLogs(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed on this endpoint")
		return
	}

	// 24.20: same limit/offset query-param pattern the generic doc-list
	// endpoint already supports, preserving the old hardcoded-100 default
	// for a caller that passes neither, capped at the same max.
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if maxLimit := maxListLimitFor(tenantID); limit > maxLimit {
		limit = maxLimit
	}
	offset := 0
	if v := r.URL.Query().Get("offset"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	rows, err := db.DB.Query(fmt.Sprintf("SELECT id, user_id, action, status, details, created_at FROM %s.audit_logs ORDER BY created_at DESC LIMIT $1 OFFSET $2", schema), limit, offset)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
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
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
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
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed on this endpoint")
		return
	}

	rows, err := db.DB.Query(fmt.Sprintf("SELECT log_id, correlation_id, severity, module_source, error_message, stack_trace, created_at FROM %s.system_error_logs ORDER BY created_at DESC LIMIT 100", schema))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
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
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
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
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if !allowed {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	var documentType string
	if err := db.DB.QueryRow(fmt.Sprintf("SELECT document_type FROM %s.doctype_meta WHERE name = $1", schema), doctype).Scan(&documentType); err != nil {
		// META-0196 (Stage 25.5): "DocType not registered" - unlike the
		// document-lookup-by-id sites elsewhere in this file (a real
		// GLOBAL-0004 "record not found"), this query is keyed purely on
		// the doctype name itself, so ErrNoRows here specifically means
		// the doctype has no doctype_meta row at all.
		writeAPIError(w, r, "META-0196", "")
		return
	}
	if documentType != "Master" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Only master documents can be reactivated")
		return
	}
	result, err := db.DB.Exec(fmt.Sprintf("UPDATE %s.documents SET deleted_at = NULL, updated_at = CURRENT_TIMESTAMP WHERE id = $1 AND doctype = $2 AND deleted_at IS NOT NULL", schema), id, doctype)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		writeAPIError(w, r, "GLOBAL-0004", "")
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
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	fields, err = engines.FilterFieldMetaForRole(tenantID, role, doctype, fields)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(fields)
}

func handleGetDocTypes(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")

	list, err := engines.GetDocTypes(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
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
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
		return
	}

	err := engines.SaveDocType(tenantID, req.Name, req.Module, req.DocumentType)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
}

func handleSaveFieldDefinition(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	doctype := r.PathValue("doctype")

	var req engines.FieldMeta
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
		return
	}

	err := engines.SaveFieldDefinition(tenantID, doctype, req)
	if err != nil {
		// META-0197/META-0201 (Stage 25.5): SaveFieldDefinition now rejects
		// an unsupported fieldtype or a fieldname collision as a precise
		// *ValidationError instead of ever reaching the DB - any other
		// failure (a genuine DB error) still falls back to 500 unchanged.
		writeEngineError(w, r, err, http.StatusInternalServerError)
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
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
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
		{"code": "PHARMA", "name": "Pharmaceuticals & Biotechnology"},
		{"code": "METAL", "name": "Metal & Steel Fabrication"},
		{"code": "CONSTRUCTION", "name": "Construction & Contracting"},
		{"code": "MEDICAL", "name": "Medical Devices"},
		{"code": "SEMICONDUCTOR", "name": "Semiconductors"},
		{"code": "AGRICULTURE", "name": "Agriculture & Perishable Goods"},
	}
	_ = json.NewEncoder(w).Encode(list)
}

func handleSwitchIndustry(w http.ResponseWriter, r *http.Request) {
	if !requireHRAdmin(w, r, r.Header.Get("Resolved-Role")) {
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")

	var req struct {
		IndustryCode string `json:"industry_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request body")
		return
	}
	if !validIndustryCodes[strings.ToUpper(req.IndustryCode)] {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Unknown industry_code")
		return
	}

	profilePath := fmt.Sprintf("./public/profiles/%s.json", strings.ToLower(req.IndustryCode))
	err := engines.SwitchIndustryProfile(tenantID, profilePath)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, fmt.Sprintf("Failed to switch industry: %v", err))
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success", "message": "Industry configuration profile reloaded successfully"})
}
