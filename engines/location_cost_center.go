package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// A cost center for every location (Stage 93).
//
// The user, 2026-10-18: "Can it create auto cost center once I create a
// store. A std cost center name should create automatically. It should be
// editable. Not this lengthy name." Decided the same day: every location type
// (Store, Warehouse, HO), existing locations linked or given one on release,
// and postings tagged with their store's cost center in the same pass.
//
// Location.cost_center is a plain Link. A new location saved with it blank
// is linked to an Active cost center of the same name that no other location
// uses, else one is created: name = the location's name, code from the
// CostCenter master series (CC00001, Stage 76.5). Both stay editable - the
// name on Setup -> Cost Center, the link on the location. 93.6: the field is
// never blank - an edit that clears it gets one the same way, and the form
// offers "New: <name>" as its blank choice.

// PrepareLocationCostCenter fills a new location's blank cost_center before
// it is written, so the location is saved once with the link in place. If
// the save is then refused, the cost center it created is left unlinked and
// the next attempt with the same name links it instead of making another.
func PrepareLocationCostCenter(tenantID, locationID, userID string, payload map[string]interface{}) error {
	if strings.TrimSpace(strField(payload, "cost_center")) != "" {
		return nil
	}
	id, err := linkOrCreateLocationCostCenter(tenantID, locationID, locationCostCenterName(payload), userID)
	if err != nil {
		return err
	}
	payload["cost_center"] = id
	return nil
}

// EnsureLocationCostCenter gives a saved location with no cost center one,
// and returns it. A location that already has one is left as it is.
func EnsureLocationCostCenter(tenantID, locationID, userID string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	var raw string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'Location' AND id = $1 AND deleted_at IS NULL`, schema),
		locationID).Scan(&raw); err != nil {
		return "", err
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return "", err
	}
	if current := strings.TrimSpace(strField(data, "cost_center")); current != "" {
		return current, nil
	}
	id, err := linkOrCreateLocationCostCenter(tenantID, locationID, locationCostCenterName(data), userID)
	if err != nil {
		return "", err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents
		SET data = data || jsonb_build_object('cost_center', $2::text), version = version + 1, updated_at = CURRENT_TIMESTAMP
		WHERE doctype = 'Location' AND id = $1`, schema), locationID, id)
	return id, err
}

// BackfillLocationCostCenters gives every location with no cost center one
// (93.2). Idempotent: a location it has handled carries one and is skipped.
func BackfillLocationCostCenters(tenantID string) (int, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id FROM %s.documents
		WHERE doctype = 'Location' AND deleted_at IS NULL AND COALESCE(data->>'cost_center', '') = ''
		ORDER BY created_at, id`, schema))
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	done := 0
	for _, id := range ids {
		if _, err := EnsureLocationCostCenter(tenantID, id, "system"); err != nil {
			return done, fmt.Errorf("location %s: %v", id, err)
		}
		done++
	}
	return done, nil
}

// BackfillLocationCostCentersAllTenants runs the backfill for every client,
// from `erp-server -migrate` after the schema sync. Returns the locations
// handled per tenant id.
func BackfillLocationCostCentersAllTenants() (map[string]int, error) {
	rows, err := db.DB.Query(`SELECT MIN(tenant_id) FROM public.tenants GROUP BY schema_name ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	var tenants []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			rows.Close()
			return nil, err
		}
		tenants = append(tenants, t)
	}
	rows.Close()
	out := map[string]int{}
	for _, t := range tenants {
		n, err := BackfillLocationCostCenters(t)
		if n > 0 {
			out[t] = n
		}
		if err != nil {
			return out, fmt.Errorf("tenant %s: %v", t, err)
		}
	}
	return out, nil
}

func locationCostCenterName(data map[string]interface{}) string {
	if name := strings.TrimSpace(strField(data, "name")); name != "" {
		return name
	}
	return strings.TrimSpace(strField(data, "code"))
}

// linkOrCreateLocationCostCenter returns an Active cost center named like the
// location that no other location uses (an existing "JP Nagar" is linked, not
// duplicated), else a new one.
func linkOrCreateLocationCostCenter(tenantID, locationID, name, userID string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("the location has no name to give its cost center")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	var existing string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT cc.id FROM %[1]s.documents cc
		WHERE cc.doctype = 'CostCenter' AND cc.deleted_at IS NULL AND cc.status = 'Active'
		  AND lower(trim(cc.data->>'name')) = lower($1)
		  AND NOT EXISTS (SELECT 1 FROM %[1]s.documents l
		                   WHERE l.doctype = 'Location' AND l.deleted_at IS NULL
		                     AND l.data->>'cost_center' = cc.id AND l.id <> $2)
		ORDER BY cc.created_at, cc.id LIMIT 1`, schema), name, locationID).Scan(&existing)
	if err == nil {
		return existing, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}

	payload := map[string]interface{}{"name": name, "status": "Active"}
	if err := PrepareMasterNumber(tenantID, "", "CostCenter", true, payload); err != nil {
		return "", err
	}
	id := strField(payload, "id")
	if id == "" {
		return "", fmt.Errorf("could not number the new cost center")
	}
	if err := ValidateDocument(tenantID, "CostCenter", payload); err != nil {
		ReleaseMasterNumber(tenantID, "CostCenter", id)
		return "", err
	}
	if userID == "" {
		userID = "system"
	}
	raw, _ := json.Marshal(payload)
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'CostCenter', $2, 'Active', $3)`, schema),
		id, raw, userID); err != nil {
		ReleaseMasterNumber(tenantID, "CostCenter", id)
		return "", err
	}
	LogAuditEvent(tenantID, userID, "COST_CENTER_AUTO_CREATED", "Success",
		fmt.Sprintf("Cost center %s (%s) created for location %s", id, name, locationID))
	return id, nil
}

// locationRefKeys are the keys a posted document names its location under.
var locationRefKeys = []string{"location", "location_code", "return_location", "store"}

// locationParentKeys lead from a document with no location of its own to the
// one that has it: a credit note to its invoice or cart, a vendor invoice /
// note / landed cost to its GRN, a refund to its return, a payslip to the
// employee.
var locationParentKeys = []string{"sales_invoice_id", "reference_cart", "return_request_id", "grn_id", "grn_reference", "employee_id"}

func coalesceKeys(alias string, keys []string) string {
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("NULLIF(%s.data->>'%s', '')", alias, k)
	}
	return "COALESCE(" + strings.Join(parts, ", ") + ")"
}

// postingDimensions are what a posting with none of its own takes from the
// document it posts (93.3, 93.7, 93.10).
type postingDimensions struct {
	CostCenter, Entity, Department string
}

// postingDimensionsTx reads them in one query, inside the posting's own
// transaction:
//   - cost center and legal entity: the source document's location's (93.3,
//     93.7), through one parent where the document names no location;
//   - department (93.10): the document's own, else its parent's (a payslip's
//     employee), else the department of the user who created it - except a
//     journal voucher, which is booked by accounts for anyone and stays as
//     picked.
//
// Only an Active record is taken; anything missing or Inactive gives "" - an
// untagged posting, never a refused one.
func postingDimensionsTx(tx *sql.Tx, schema, docID string) postingDimensions {
	var out postingDimensions
	if docID == "" {
		return out
	}
	creatorDept := "NULL"
	if userDepartmentReady(tx, schema) {
		creatorDept = fmt.Sprintf(`CASE WHEN src.doctype <> 'JournalVoucher' THEN (SELECT u.department FROM %s.users u WHERE u.id = src.created_by) END`, schema)
	}
	err := tx.QueryRow(fmt.Sprintf(`
		WITH src AS (
		  SELECT s.doctype, s.created_by, %[2]s AS ref, %[3]s AS parent, NULLIF(s.data->>'department', '') AS dept
		    FROM %[1]s.documents s WHERE s.id = $1 LIMIT 1
		), par AS (
		  SELECT %[4]s AS ref, NULLIF(p.data->>'department', '') AS dept
		    FROM %[1]s.documents p, src WHERE p.id = src.parent LIMIT 1
		), ref AS (
		  SELECT COALESCE(src.ref, (SELECT ref FROM par)) AS v FROM src
		), loc AS (
		  SELECT l.data, 0 AS pref FROM %[1]s.documents l, ref
		   WHERE l.id = ref.v AND l.doctype = 'Location' AND l.deleted_at IS NULL
		  UNION ALL
		  SELECT l.data, 1 FROM %[1]s.documents l, ref
		   WHERE l.doctype = 'Location' AND l.data->>'code' = ref.v AND l.deleted_at IS NULL
		), loc1 AS (
		  SELECT data FROM loc ORDER BY pref LIMIT 1
		), dept AS (
		  SELECT src.dept AS v, 0 AS pref FROM src
		  UNION ALL SELECT par.dept, 1 FROM par
		  UNION ALL SELECT %[5]s, 2 FROM src
		)
		SELECT
		  COALESCE((SELECT c.id FROM loc1 JOIN %[1]s.documents c ON c.id = loc1.data->>'cost_center'
		             AND c.doctype = 'CostCenter' AND c.status = 'Active' AND c.deleted_at IS NULL LIMIT 1), ''),
		  COALESCE((SELECT e.id FROM loc1 JOIN %[1]s.documents e ON e.id = loc1.data->>'legal_entity'
		             AND e.doctype = 'LegalEntity' AND e.status = 'Active' AND e.deleted_at IS NULL LIMIT 1), ''),
		  COALESCE((SELECT d.id FROM dept JOIN %[1]s.documents d ON d.id = dept.v
		             AND d.doctype = 'Department' AND d.status = 'Active' AND d.deleted_at IS NULL
		            ORDER BY dept.pref LIMIT 1), '')`,
		schema, coalesceKeys("s", locationRefKeys), coalesceKeys("s", locationParentKeys), coalesceKeys("p", locationRefKeys), creatorDept),
		docID).Scan(&out.CostCenter, &out.Entity, &out.Department)
	if err != nil {
		return postingDimensions{}
	}
	return out
}

type userDepartmentCheck struct {
	ready     bool
	checkedAt time.Time
}

var userDepartmentColumns sync.Map // schema -> userDepartmentCheck

// userDepartmentReady: users.department (93.9) exists in this schema. Asked
// before the dimension query names it, so a lagging migration can never abort
// the posting's transaction - the same guard as glAttributionReady.
func userDepartmentReady(tx *sql.Tx, schema string) bool {
	if v, ok := userDepartmentColumns.Load(schema); ok {
		c := v.(userDepartmentCheck)
		if c.ready || time.Since(c.checkedAt) < time.Minute {
			return c.ready
		}
	}
	var ready bool
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = 'users' AND column_name = 'department')`, schema).Scan(&ready); err != nil {
		return false
	}
	userDepartmentColumns.Store(schema, userDepartmentCheck{ready: ready, checkedAt: time.Now()})
	return ready
}
