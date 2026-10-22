package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Stage 101.1.3 - duplicates and merge, extending 26.7.9's MergeCustomers.
// One phone is one customer (85 D1), so duplicates are the same person under
// a second number: the same email, the same name and birthday, the same
// CleverTap identity. A merge shows what moves before anyone confirms it,
// goes through the approval engine as a CustomerMerge record, and moves the
// CRM's own rows with the bills and points; the survivor gets at most one
// copy of any message still waiting to go.

const crmMergeDoctype = "CustomerMerge"

// CRMDuplicatePair is two customers who look like one person.
type CRMDuplicatePair struct {
	A       CRMCustomerRow `json:"a"`
	B       CRMCustomerRow `json:"b"`
	Reasons []string       `json:"reasons"`
}

// ListCRMDuplicates finds possible duplicates, up to 200 pairs.
func ListCRMDuplicates(tenantID, role string) ([]CRMDuplicatePair, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		WITH live AS (
		  SELECT id, data, created_at FROM %s.documents
		   WHERE doctype = 'Customer' AND deleted_at IS NULL AND status <> 'Merged'
		     AND COALESCE(data->>'walk_in', '') <> 'Yes' AND COALESCE(data->>'anonymized_at', '') = ''
		)
		SELECT a.id, b.id,
		       (LOWER(COALESCE(a.data->>'email', '')) <> '' AND LOWER(a.data->>'email') = LOWER(b.data->>'email')) AS same_email,
		       (LOWER(TRIM(COALESCE(a.data->>'name', ''))) <> '' AND LOWER(TRIM(a.data->>'name')) = LOWER(TRIM(b.data->>'name'))
		        AND COALESCE(a.data->>'date_of_birth', '') <> '' AND a.data->>'date_of_birth' = b.data->>'date_of_birth') AS same_name_dob,
		       (COALESCE(a.data->>'clevertap_id', '') <> '' AND a.data->>'clevertap_id' = b.data->>'clevertap_id') AS same_clevertap
		  FROM live a JOIN live b ON a.id < b.id
		   AND ((LOWER(COALESCE(a.data->>'email', '')) <> '' AND LOWER(a.data->>'email') = LOWER(b.data->>'email'))
		     OR (LOWER(TRIM(COALESCE(a.data->>'name', ''))) <> '' AND LOWER(TRIM(a.data->>'name')) = LOWER(TRIM(b.data->>'name'))
		         AND COALESCE(a.data->>'date_of_birth', '') <> '' AND a.data->>'date_of_birth' = b.data->>'date_of_birth')
		     OR (COALESCE(a.data->>'clevertap_id', '') <> '' AND a.data->>'clevertap_id' = b.data->>'clevertap_id'))
		 ORDER BY a.created_at DESC LIMIT 200`, schema))
	if err != nil {
		return nil, err
	}
	type pair struct {
		a, b               string
		email, nameDOB, ct bool
	}
	var pairs []pair
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.a, &p.b, &p.email, &p.nameDOB, &p.ct); err != nil {
			rows.Close()
			return nil, err
		}
		pairs = append(pairs, p)
	}
	rows.Close()
	out := []CRMDuplicatePair{}
	masked := !RoleSeesContacts(tenantID, role)
	for _, p := range pairs {
		a, errA := crmCustomerRow(schema, p.a, masked)
		b, errB := crmCustomerRow(schema, p.b, masked)
		if errA != nil || errB != nil {
			continue
		}
		var reasons []string
		if p.email {
			reasons = append(reasons, "Same email")
		}
		if p.nameDOB {
			reasons = append(reasons, "Same name and birthday")
		}
		if p.ct {
			reasons = append(reasons, "Same CleverTap identity")
		}
		out = append(out, CRMDuplicatePair{A: a, B: b, Reasons: reasons})
	}
	return out, nil
}

func crmCustomerRow(schema, id string, masked bool) (CRMCustomerRow, error) {
	c, err := crmLoadCustomer(schema, id)
	if err != nil {
		return CRMCustomerRow{}, err
	}
	r := CRMCustomerRow{ID: c.ID, Code: strField(c.Data, "code"), Name: c.Name(), Phone: strField(c.Data, "phone"), Email: strField(c.Data, "email"),
		City: strField(c.Data, "city"), Tier: strField(c.Data, "loyalty_tier"), Tags: crmSplitTags(strField(c.Data, "tags"))}
	if f, err := crmCustomerFacts(schema, c.ID); err == nil {
		r.Visits, r.Spend, r.Lifecycle = f.Visits, round2(f.Spend), f.Lifecycle
		if f.LastAt.Valid {
			r.LastBill = f.LastAt.Time.Format("2006-01-02")
		}
	}
	if masked {
		r.Phone, r.Email = crmMaskPhone(r.Phone), crmMaskEmail(r.Email)
	}
	return r, nil
}

// CRMMergePreview says what a merge would move onto the record kept.
type CRMMergePreview struct {
	Keep            CRMCustomerRow `json:"keep"`
	Merge           CRMCustomerRow `json:"merge"`
	Bills           int            `json:"bills"`
	Invoices        int            `json:"invoices"`
	Vouchers        int            `json:"vouchers"`
	Points          int            `json:"points"`
	ConsentRows     int            `json:"consent_rows"`
	LiveJourneys    int            `json:"live_journeys"`
	JourneysEnded   int            `json:"journeys_ended"`
	QueuedMessages  int            `json:"queued_messages"`
	MessagesDropped int            `json:"messages_dropped"`
	Tickets         int            `json:"tickets"`
	Notes           int            `json:"notes"`
	Tasks           int            `json:"tasks"`
	Appointments    int            `json:"appointments"`
}

// PreviewCRMMerge counts what moves, without changing anything.
func PreviewCRMMerge(tenantID, role, keepID, mergeID string) (*CRMMergePreview, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	keep, merge, err := crmMergePair(schema, keepID, mergeID)
	if err != nil {
		return nil, err
	}
	masked := !RoleSeesContacts(tenantID, role)
	p := &CRMMergePreview{}
	if p.Keep, err = crmCustomerRow(schema, keep.ID, masked); err != nil {
		return nil, err
	}
	if p.Merge, err = crmCustomerRow(schema, merge.ID, masked); err != nil {
		return nil, err
	}
	counts := []struct {
		dst *int
		q   string
	}{
		{&p.Bills, `SELECT COUNT(*) FROM %[1]s.documents WHERE doctype = 'POSCart' AND data->>'customer_id' = $1`},
		{&p.Invoices, `SELECT COUNT(*) FROM %[1]s.documents WHERE doctype = 'SalesInvoice' AND data->>'customer' = $1`},
		{&p.Vouchers, `SELECT COUNT(*) FROM %[1]s.documents WHERE doctype = 'Voucher' AND data->>'customer_id' = $1`},
		{&p.Points, `SELECT COALESCE(SUM(CASE WHEN transaction_type = 'Earn' THEN points ELSE -points END), 0) FROM %[1]s.loyalty_point_ledger WHERE customer_id = $1 AND cancelled_at IS NULL`},
		{&p.ConsentRows, `SELECT COUNT(*) FROM %[1]s.crm_consent_ledger WHERE customer_id = $1`},
		{&p.LiveJourneys, `SELECT COUNT(*) FROM %[1]s.crm_journey_runs WHERE customer_id = $1 AND state IN ('Active', 'Waiting')`},
		{&p.QueuedMessages, `SELECT COUNT(*) FROM %[1]s.crm_sends WHERE customer_id = $1 AND status IN ('Queued', 'Deferred')`},
		{&p.Tickets, `SELECT COUNT(*) FROM %[1]s.documents WHERE doctype = 'ServiceTicket' AND data->>'customer' = $1`},
		{&p.Notes, `SELECT COUNT(*) FROM %[1]s.crm_customer_notes WHERE customer_id = $1`},
		{&p.Tasks, `SELECT COUNT(*) FROM %[1]s.crm_tasks WHERE customer_id = $1`},
		{&p.Appointments, `SELECT COUNT(*) FROM %[1]s.crm_appointments WHERE customer_id = $1`},
	}
	for _, c := range counts {
		if err := db.DB.QueryRow(fmt.Sprintf(c.q, schema), merge.ID).Scan(c.dst); err != nil {
			return nil, err
		}
	}
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %[1]s.crm_journey_runs m WHERE m.customer_id = $2 AND m.state IN ('Active', 'Waiting')
		AND EXISTS (SELECT 1 FROM %[1]s.crm_journey_runs k WHERE k.customer_id = $1 AND k.journey_id = m.journey_id AND k.cart_key = m.cart_key AND k.state IN ('Active', 'Waiting'))`, schema),
		keep.ID, merge.ID).Scan(&p.JourneysEnded)
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %[1]s.crm_sends m WHERE m.customer_id = $2 AND m.status IN ('Queued', 'Deferred')
		AND EXISTS (SELECT 1 FROM %[1]s.crm_sends k WHERE k.customer_id = $1 AND k.run_kind = m.run_kind AND k.run_id = m.run_id AND k.channel = m.channel)`, schema),
		keep.ID, merge.ID).Scan(&p.MessagesDropped)
	return p, nil
}

func crmMergePair(schema, keepID, mergeID string) (crmCustomer, crmCustomer, error) {
	keepID, mergeID = strings.TrimSpace(keepID), strings.TrimSpace(mergeID)
	if keepID == "" || mergeID == "" {
		return crmCustomer{}, crmCustomer{}, crmErr(CRMCodeMerge, "choose the customer to keep and the one to merge into it")
	}
	keep, err := crmLoadCustomer(schema, keepID)
	if errors.Is(err, ErrCRMNotFound) {
		return keep, crmCustomer{}, crmNotFound("customer", keepID)
	}
	if err != nil {
		return keep, crmCustomer{}, err
	}
	merge, err := crmLoadCustomer(schema, mergeID)
	if errors.Is(err, ErrCRMNotFound) {
		return keep, merge, crmNotFound("customer", mergeID)
	}
	if err != nil {
		return keep, merge, err
	}
	if keep.ID == merge.ID {
		return keep, merge, crmErr(CRMCodeMerge, "choose two different customers")
	}
	if keep.Status == "Merged" || merge.Status == "Merged" {
		return keep, merge, crmErr(CRMCodeMerge, "one of these customers is already merged into another")
	}
	if crmCustomerErased(keep) || crmCustomerErased(merge) {
		return keep, merge, crmErr(CRMCodeMerge, "an erased customer cannot be merged")
	}
	return keep, merge, nil
}

// CRMMergeRequest is the result of asking for a merge.
type CRMMergeRequest struct {
	ID      string           `json:"id"`
	Status  string           `json:"status"`
	Preview *CRMMergePreview `json:"preview"`
}

// RequestCRMMerge records a CustomerMerge. With an approval rule for
// CustomerMerge (the migration adds Store Manager where none exists) it waits
// in Approvals; with none, it is done at once (the 73.2.11 precedent).
func RequestCRMMerge(tenantID, keepID, mergeID, reason, actor, role string) (*CRMMergeRequest, error) {
	preview, err := PreviewCRMMerge(tenantID, role, keepID, mergeID)
	if err != nil {
		return nil, err
	}
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var pending string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = $1 AND status IN ('Draft', 'Pending Approval') AND deleted_at IS NULL
		AND ((data->>'keep_id' = $2 AND data->>'merge_id' = $3) OR (data->>'keep_id' = $3 AND data->>'merge_id' = $2)) LIMIT 1`, schema),
		crmMergeDoctype, preview.Keep.ID, preview.Merge.ID).Scan(&pending)
	if err == nil {
		return nil, crmErr(CRMCodeMerge, "a merge of these two is already waiting for approval (%s)", pending)
	}
	id := NewDocID("CMG")
	previewJSON, _ := json.Marshal(preview)
	data := crmJSON(map[string]interface{}{"code": id, "keep_id": preview.Keep.ID, "merge_id": preview.Merge.ID,
		"reason": strings.TrimSpace(reason), "preview": string(previewJSON), "status": "Draft",
		"title": fmt.Sprintf("Merge %s into %s", preview.Merge.Name, preview.Keep.Name)})
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Draft', $4)`, schema),
		id, crmMergeDoctype, data, actor); err != nil {
		return nil, err
	}
	approver, err := RequiredApproverRoleForAmount(tenantID, crmMergeDoctype, 0)
	if err != nil {
		return nil, err
	}
	if approver != "" {
		if err := SubmitForApproval(tenantID, crmMergeDoctype, id, actor, role); err != nil {
			return nil, err
		}
		return &CRMMergeRequest{ID: id, Status: "Pending Approval", Preview: preview}, nil
	}
	if err := crmExecuteMerge(tenantID, id, actor); err != nil {
		return nil, err
	}
	return &CRMMergeRequest{ID: id, Status: "Merged", Preview: preview}, nil
}

func init() {
	RegisterApprovalDecisionHook(crmMergeDoctype, false, func(tenantID, docID, decision, actor string) {
		if decision != "Approved" {
			return
		}
		if err := crmExecuteMerge(tenantID, docID, actor); err != nil {
			LogSystemError(tenantID, "", "ERROR", "crm_merge", fmt.Sprintf("approved merge %s failed: %v", docID, err), "")
		}
	})
}

// crmExecuteMerge carries out an approved (or rule-free) CustomerMerge once.
func crmExecuteMerge(tenantID, mergeDocID, actor string) error {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return err
	}
	var raw []byte
	var status string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE id = $1 AND doctype = $2`, schema), mergeDocID, crmMergeDoctype).Scan(&raw, &status); err != nil {
		return err
	}
	if status == "Merged" {
		return nil
	}
	var d map[string]interface{}
	_ = json.Unmarshal(raw, &d)
	if err := MergeCustomers(tenantID, strField(d, "keep_id"), strField(d, "merge_id"), actor); err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'Merged', data = data || '{"status":"Merged"}'::jsonb, updated_at = CURRENT_TIMESTAMP WHERE id = $1`, schema), mergeDocID)
	return err
}

// crmMergeRowsTx moves the CRM's own rows from the duplicate onto the record
// kept, inside MergeCustomers' transaction. A live journey or a waiting
// message the kept record already has once is ended on the duplicate rather
// than moved, so the survivor never gets two copies.
func crmMergeRowsTx(tx *sql.Tx, schema, keepID, mergeID string) error {
	if to, err := crmTableExistsTx(tx, schema, "crm_sends"); err != nil || !to {
		return err
	}
	steps := []string{
		// waiting messages: drop the duplicate copy, move the rest
		`UPDATE %[1]s.crm_sends m SET status = 'Cancelled', reason_code = 'MERGED_DUPLICATE', reason_detail = 'The same message was waiting for the customer kept in a merge',
		   decided_at = NOW(), final_at = NOW()
		 WHERE m.customer_id = $2 AND m.status IN ('Queued', 'Deferred')
		   AND EXISTS (SELECT 1 FROM %[1]s.crm_sends k WHERE k.customer_id = $1 AND k.run_kind = m.run_kind AND k.run_id = m.run_id AND k.channel = m.channel)`,
		`UPDATE %[1]s.crm_sends m SET customer_id = $1
		 WHERE m.customer_id = $2
		   AND NOT EXISTS (SELECT 1 FROM %[1]s.crm_sends k WHERE k.customer_id = $1 AND k.run_kind = m.run_kind AND k.run_id = m.run_id AND k.channel = m.channel)`,
		// live journeys: end the duplicate's where the kept record runs the same one
		`UPDATE %[1]s.crm_journey_runs m SET state = 'Exited', exit_reason = 'merged', ended_at = NOW(), updated_at = NOW()
		 WHERE m.customer_id = $2 AND m.state IN ('Active', 'Waiting')
		   AND EXISTS (SELECT 1 FROM %[1]s.crm_journey_runs k WHERE k.customer_id = $1 AND k.journey_id = m.journey_id AND k.cart_key = m.cart_key AND k.state IN ('Active', 'Waiting'))`,
		`UPDATE %[1]s.crm_journey_runs SET customer_id = $1 WHERE customer_id = $2`,
		`UPDATE %[1]s.crm_consent_ledger SET customer_id = $1 WHERE customer_id = $2`,
		`UPDATE %[1]s.crm_customer_notes SET customer_id = $1 WHERE customer_id = $2`,
		`UPDATE %[1]s.crm_tasks SET customer_id = $1 WHERE customer_id = $2`,
		`UPDATE %[1]s.crm_appointments SET customer_id = $1 WHERE customer_id = $2`,
		`UPDATE %[1]s.crm_feedback SET customer_id = $1 WHERE customer_id = $2`,
		`UPDATE %[1]s.crm_hold_requests SET customer_id = $1 WHERE customer_id = $2`,
		`UPDATE %[1]s.crm_conversations SET customer_id = $1 WHERE customer_id = $2`,
		`UPDATE %[1]s.crm_static_list_members SET customer_id = $1 WHERE customer_id = $2`,
		`UPDATE %[1]s.crm_pref_tokens SET customer_id = $1 WHERE customer_id = $2`,
		`UPDATE %[1]s.crm_events SET customer_id = $1 WHERE customer_id = $2`,
		`UPDATE %[1]s.crm_wishlist m SET removed_at = NOW() WHERE m.customer_id = $2 AND m.removed_at IS NULL
		   AND EXISTS (SELECT 1 FROM %[1]s.crm_wishlist k WHERE k.customer_id = $1 AND k.item_code = m.item_code AND k.removed_at IS NULL)`,
		`UPDATE %[1]s.crm_wishlist SET customer_id = $1 WHERE customer_id = $2`,
		`DELETE FROM %[1]s.crm_referral_codes m WHERE m.customer_id = $2 AND EXISTS (SELECT 1 FROM %[1]s.crm_referral_codes k WHERE k.customer_id = $1)`,
		`UPDATE %[1]s.crm_referral_codes SET customer_id = $1 WHERE customer_id = $2`,
		`UPDATE %[1]s.crm_referrals SET referrer_id = $1 WHERE referrer_id = $2`,
		`UPDATE %[1]s.crm_referrals m SET status = 'Cancelled', flag_reason = 'Merged into the referrer or an existing friend'
		 WHERE m.friend_id = $2 AND (m.referrer_id = $1 OR EXISTS (SELECT 1 FROM %[1]s.crm_referrals k WHERE k.friend_id = $1))`,
		`UPDATE %[1]s.crm_referrals SET friend_id = $1 WHERE friend_id = $2 AND status <> 'Cancelled'`,
		`UPDATE %[1]s.crm_opportunities SET customer_id = $1 WHERE customer_id = $2`,
		`UPDATE %[1]s.documents SET data = jsonb_set(data, '{customer}', to_jsonb($1::text)), updated_at = CURRENT_TIMESTAMP
		 WHERE doctype IN ('ServiceTicket', 'SalesQuotation') AND data->>'customer' = $2`,
	}
	for _, q := range steps {
		if _, err := tx.Exec(fmt.Sprintf(q, schema), keepID, mergeID); err != nil {
			return err
		}
	}
	return crmSyncConsentTicksTx(tx, schema, keepID, "merge")
}

func crmTableExistsTx(tx *sql.Tx, schema, table string) (bool, error) {
	var ok bool
	err := tx.QueryRow(`SELECT to_regclass($1) IS NOT NULL`, schema+"."+table).Scan(&ok)
	return ok, err
}
