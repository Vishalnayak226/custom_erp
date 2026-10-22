package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"custom_erp/engines"
)

// Stage 101: the CRM platform's HTTP surface (/api/v1/crm/...). Engine logic
// lives in engines/crm_*.go; these handlers decode, check the role, call one
// engine function and write its answer. Who may do what follows the record
// types the work touches: reading and serving customers needs Customer read;
// building segments, templates, campaigns and journeys needs Campaign update;
// tickets need ServiceTicket; the pipeline needs SalesQuotation; senders are
// administrator-only (route level). Customer-facing pages (preferences,
// feedback) and provider callbacks are public routes guarded by their token
// or signature.

func crmActor(r *http.Request) string {
	return firstNonEmptyHeader(r, "Resolved-Username", "Resolved-User-ID")
}

func firstNonEmptyHeader(r *http.Request, names ...string) string {
	for _, n := range names {
		if v := strings.TrimSpace(r.Header.Get(n)); v != "" {
			return v
		}
	}
	return ""
}

func crmOK(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func crmFail(w http.ResponseWriter, r *http.Request, err error) {
	writeEngineError(w, r, err, http.StatusUnprocessableEntity)
}

func crmDecode(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(v); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "The request body is not valid JSON: "+err.Error())
		return false
	}
	return true
}

// crmAllow checks the role's permission on a record type and writes the
// refusal itself.
func crmAllow(w http.ResponseWriter, r *http.Request, doctype, action string) bool {
	ok, err := checkPermission(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Role"), doctype, action)
	if err != nil || !ok {
		writeAPIErrorGeneric(w, r, http.StatusForbidden, "Your role cannot do this here ("+doctype+" "+action+"). Ask an administrator for access.")
		return false
	}
	return true
}

func crmMethod(w http.ResponseWriter, r *http.Request, allowed ...string) bool {
	for _, m := range allowed {
		if r.Method == m {
			return true
		}
	}
	writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
	return false
}

func crmQueryMap(r *http.Request, keys ...string) map[string]string {
	m := map[string]string{}
	q := r.URL.Query()
	for _, k := range keys {
		if v := strings.TrimSpace(q.Get(k)); v != "" {
			m[k] = v
		}
	}
	return m
}

// --- customers -----------------------------------------------------------------

func handleCRMCustomers(w http.ResponseWriter, r *http.Request) {
	if !crmMethod(w, r, http.MethodGet) || !crmAllow(w, r, "Customer", "read") {
		return
	}
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	size, _ := strconv.Atoi(q.Get("page_size"))
	list, err := engines.SearchCRMCustomers(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Role"), q.Get("q"), q.Get("lifecycle"), page, size)
	if err != nil {
		crmFail(w, r, err)
		return
	}
	crmOK(w, list)
}

func handleCRMCustomer(w http.ResponseWriter, r *http.Request) {
	tenantID, role, id := r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Role"), r.PathValue("id")
	if !crmAllow(w, r, "Customer", "read") {
		return
	}
	switch r.PathValue("part") {
	case "":
		if !crmMethod(w, r, http.MethodGet) {
			return
		}
		page, err := engines.GetCRMCustomerPage(tenantID, role, id)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, page)
	case "timeline":
		if !crmMethod(w, r, http.MethodGet) {
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		var kinds []string
		if k := r.URL.Query().Get("kinds"); k != "" {
			kinds = strings.Split(k, ",")
		}
		t, err := engines.GetCRMTimeline(tenantID, id, r.URL.Query().Get("before"), limit, kinds)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, t)
	case "notes":
		if !crmMethod(w, r, http.MethodPost) {
			return
		}
		var req struct {
			Note string `json:"note"`
		}
		if !crmDecode(w, r, &req) {
			return
		}
		n, err := engines.AddCRMCustomerNote(tenantID, id, req.Note, crmActor(r))
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, n)
	case "consent":
		if r.Method == http.MethodGet {
			states, err := engines.CRMConsentFor(tenantID, id)
			if err != nil {
				crmFail(w, r, err)
				return
			}
			ledger, err := engines.CRMConsentLedger(tenantID, id)
			if err != nil {
				crmFail(w, r, err)
				return
			}
			crmOK(w, map[string]interface{}{"states": states, "ledger": ledger})
			return
		}
		if !crmMethod(w, r, http.MethodPost) {
			return
		}
		var in engines.CRMConsentInput
		if !crmDecode(w, r, &in) {
			return
		}
		in.CustomerID, in.RecordedBy = id, crmActor(r)
		if in.Source == "" {
			in.Source = "form"
		}
		if in.Source != "form" && in.Source != "till" && in.Source != "store_app" {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Staff record consent from the customer page, the till or the store app.")
			return
		}
		st, err := engines.RecordCRMConsent(tenantID, in)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, st)
	case "wishlist":
		if r.Method == http.MethodDelete {
			wid, _ := strconv.ParseInt(r.URL.Query().Get("wish_id"), 10, 64)
			if err := engines.RemoveCRMWish(tenantID, id, wid); err != nil {
				crmFail(w, r, err)
				return
			}
			crmOK(w, map[string]bool{"removed": true})
			return
		}
		if !crmMethod(w, r, http.MethodPost) {
			return
		}
		var req struct {
			ItemCode string `json:"item_code"`
			Note     string `json:"note"`
			Store    string `json:"store"`
		}
		if !crmDecode(w, r, &req) {
			return
		}
		wish, err := engines.AddCRMWish(tenantID, id, req.ItemCode, req.Note, crmFirst(req.Store, r.Header.Get("Resolved-Location")), crmActor(r))
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, wish)
	case "referral-code":
		if !crmMethod(w, r, http.MethodPost, http.MethodGet) {
			return
		}
		code, err := engines.CRMReferralCode(tenantID, id)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, map[string]string{"code": code})
	default:
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "Not found.")
	}
}

func crmFirst(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func handleCRMDuplicates(w http.ResponseWriter, r *http.Request) {
	if !crmMethod(w, r, http.MethodGet) || !crmAllow(w, r, "Customer", "update") {
		return
	}
	list, err := engines.ListCRMDuplicates(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Role"))
	if err != nil {
		crmFail(w, r, err)
		return
	}
	crmOK(w, map[string]interface{}{"pairs": list})
}

func handleCRMMerge(w http.ResponseWriter, r *http.Request) {
	if !crmMethod(w, r, http.MethodPost) || !crmAllow(w, r, "Customer", "update") {
		return
	}
	var req struct {
		KeepID  string `json:"keep_id"`
		MergeID string `json:"merge_id"`
		Reason  string `json:"reason"`
		Preview bool   `json:"preview"`
	}
	if !crmDecode(w, r, &req) {
		return
	}
	tenantID, role := r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Role")
	if req.Preview {
		p, err := engines.PreviewCRMMerge(tenantID, role, req.KeepID, req.MergeID)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, p)
		return
	}
	res, err := engines.RequestCRMMerge(tenantID, req.KeepID, req.MergeID, req.Reason, crmActor(r), role)
	if err != nil {
		crmFail(w, r, err)
		return
	}
	crmOK(w, res)
}

// handleCRMMeta is everything the CRM screens choose from: segment fields,
// events, template values, lifecycle states, reason codes, task kinds.
func handleCRMMeta(w http.ResponseWriter, r *http.Request) {
	if !crmMethod(w, r, http.MethodGet) {
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	th, _ := engines.GetCRMLifecycleThresholds(tenantID)
	crmOK(w, map[string]interface{}{
		"segment_fields": engines.CRMSegmentFields, "events": engines.CRMEventTypes, "template_variables": engines.CRMTemplateVariables,
		"lifecycle_states": engines.CRMLifecycleStates, "lifecycle": th, "reasons": engines.CRMReasonLabels, "task_kinds": engines.CRMTaskKinds,
		"lead_sources": engines.CRMLeadSources, "masked": !engines.RoleSeesContacts(tenantID, r.Header.Get("Resolved-Role")),
		"can_manage": func() bool {
			ok, _ := checkPermission(tenantID, r.Header.Get("Resolved-Role"), "Campaign", "update")
			return ok
		}(),
		"time_zone": engines.GetSettingString(tenantID, "crm.time_zone"),
	})
}

func handleCRMDashboard(w http.ResponseWriter, r *http.Request) {
	if !crmMethod(w, r, http.MethodGet) || !crmAllow(w, r, "Customer", "read") {
		return
	}
	d, err := engines.GetCRMDashboard(r.Header.Get("Resolved-Tenant-ID"))
	if err != nil {
		crmFail(w, r, err)
		return
	}
	crmOK(w, d)
}

// --- segments, lists, lifecycle ------------------------------------------------------

func handleCRMSegments(w http.ResponseWriter, r *http.Request) {
	tenantID, role := r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Role")
	id := r.PathValue("id")
	switch {
	case id == "" && r.Method == http.MethodGet:
		if !crmAllow(w, r, "Customer", "read") {
			return
		}
		list, err := engines.ListCRMSegments(tenantID)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, map[string]interface{}{"segments": list})
	case id == "count" || id == "explain":
		if !crmMethod(w, r, http.MethodPost) || !crmAllow(w, r, "Customer", "read") {
			return
		}
		var req struct {
			Definition engines.CRMSegmentGroup `json:"definition"`
			Channel    string                  `json:"channel"`
			CustomerID string                  `json:"customer_id"`
		}
		if !crmDecode(w, r, &req) {
			return
		}
		var out interface{}
		var err error
		if id == "count" {
			out, err = engines.CountCRMSegment(tenantID, role, req.Definition, req.Channel)
		} else {
			out, err = engines.ExplainCRMSegment(tenantID, req.Definition, req.CustomerID)
		}
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, out)
	case r.Method == http.MethodGet:
		if !crmAllow(w, r, "Customer", "read") {
			return
		}
		v, _ := strconv.Atoi(r.URL.Query().Get("version"))
		s, err := engines.GetCRMSegment(tenantID, id, v)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, s)
	case r.Method == http.MethodDelete:
		if !crmAllow(w, r, "Campaign", "update") {
			return
		}
		if err := engines.ArchiveCRMSegment(tenantID, id); err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, map[string]bool{"archived": true})
	case r.Method == http.MethodPost || r.Method == http.MethodPut:
		if !crmAllow(w, r, "Campaign", "update") {
			return
		}
		var req struct {
			Name        string                  `json:"name"`
			Description string                  `json:"description"`
			Definition  engines.CRMSegmentGroup `json:"definition"`
		}
		if !crmDecode(w, r, &req) {
			return
		}
		s, err := engines.SaveCRMSegment(tenantID, id, req.Name, req.Description, req.Definition, crmActor(r))
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, s)
	default:
		crmMethod(w, r)
	}
}

func handleCRMLists(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method == http.MethodGet {
		if !crmAllow(w, r, "Customer", "read") {
			return
		}
		list, err := engines.ListCRMStaticLists(tenantID)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, map[string]interface{}{"lists": list})
		return
	}
	if !crmMethod(w, r, http.MethodPost) || !crmAllow(w, r, "Campaign", "update") {
		return
	}
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Upload the list as a CSV file (form field 'file').")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Choose the CSV file.")
		return
	}
	defer file.Close()
	res, err := engines.UploadCRMStaticList(tenantID, r.FormValue("name"), r.FormValue("source"), r.FormValue("expires_on"), file, crmActor(r))
	if err != nil {
		crmFail(w, r, err)
		return
	}
	crmOK(w, res)
}

func handleCRMLifecycle(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method == http.MethodGet {
		counts, th, err := engines.GetCRMLifecycleCounts(tenantID)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, map[string]interface{}{"counts": counts, "thresholds": th})
		return
	}
	if !crmMethod(w, r, http.MethodPut) || !crmAllow(w, r, "Campaign", "update") {
		return
	}
	var th engines.CRMLifecycleThresholds
	if !crmDecode(w, r, &th) {
		return
	}
	out, err := engines.SaveCRMLifecycleThresholds(tenantID, th, crmActor(r))
	if err != nil {
		crmFail(w, r, err)
		return
	}
	crmOK(w, out)
}

// --- templates, senders ----------------------------------------------------------------

func handleCRMTemplates(w http.ResponseWriter, r *http.Request) {
	tenantID, id, part := r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"), r.PathValue("part")
	if !crmAllow(w, r, "Customer", "read") {
		return
	}
	switch {
	case id == "" && r.Method == http.MethodGet:
		list, err := engines.ListCRMTemplates(tenantID, r.URL.Query().Get("channel"))
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, map[string]interface{}{"templates": list})
	case part == "versions":
		list, err := engines.ListCRMTemplateVersions(tenantID, id)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, map[string]interface{}{"versions": list})
	case part == "preview":
		var req struct {
			Version    int    `json:"version"`
			CustomerID string `json:"customer_id"`
		}
		if r.Method == http.MethodPost && !crmDecode(w, r, &req) {
			return
		}
		p, err := engines.PreviewCRMTemplate(tenantID, id, req.Version, req.CustomerID)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, p)
	case part == "sync":
		if !crmMethod(w, r, http.MethodPost) || !crmAllow(w, r, "Campaign", "update") {
			return
		}
		t, err := engines.SyncCRMWhatsAppTemplate(tenantID, id)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, t)
	case r.Method == http.MethodGet:
		t, err := engines.GetCRMTemplate(tenantID, id)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, t)
	case r.Method == http.MethodPost || r.Method == http.MethodPut:
		if !crmAllow(w, r, "Campaign", "update") {
			return
		}
		var in engines.CRMTemplateInput
		if !crmDecode(w, r, &in) {
			return
		}
		t, err := engines.SaveCRMTemplate(tenantID, id, in, crmActor(r))
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, t)
	default:
		crmMethod(w, r)
	}
}

func handleCRMSenders(w http.ResponseWriter, r *http.Request) {
	tenantID, id := r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id")
	if r.Method == http.MethodGet {
		list, err := engines.ListCRMSenders(tenantID)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, map[string]interface{}{"senders": list})
		return
	}
	if !crmMethod(w, r, http.MethodPost, http.MethodPut) {
		return
	}
	var in engines.CRMSenderInput
	if !crmDecode(w, r, &in) {
		return
	}
	s, err := engines.SaveCRMSender(tenantID, id, in, crmActor(r))
	if err != nil {
		crmFail(w, r, err)
		return
	}
	crmOK(w, s)
}

// --- campaigns and messages -----------------------------------------------------------

func handleCRMCampaigns(w http.ResponseWriter, r *http.Request) {
	tenantID, role, id, part := r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Role"), r.PathValue("id"), r.PathValue("part")
	if !crmAllow(w, r, "Customer", "read") {
		return
	}
	actor := crmActor(r)
	switch {
	case id == "" && r.Method == http.MethodGet:
		list, err := engines.ListCRMCampaigns(tenantID)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, map[string]interface{}{"campaigns": list})
	case part == "preview":
		p, err := engines.PreviewCRMCampaign(tenantID, role, id)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, p)
	case part == "submit":
		if !crmMethod(w, r, http.MethodPost) || !crmAllow(w, r, "Campaign", "update") {
			return
		}
		c, err := engines.SubmitCRMCampaign(tenantID, id, actor, role)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, c)
	case part == "test":
		if !crmMethod(w, r, http.MethodPost) || !crmAllow(w, r, "Campaign", "update") {
			return
		}
		var req struct {
			CustomerID string `json:"customer_id"`
		}
		if !crmDecode(w, r, &req) {
			return
		}
		s, err := engines.SendCRMCampaignTest(tenantID, id, req.CustomerID, actor)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, s)
	case part != "":
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "Not found.")
	case r.Method == http.MethodGet:
		c, err := engines.GetCRMCampaign(tenantID, id)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, c)
	case r.Method == http.MethodPost || r.Method == http.MethodPut:
		if !crmAllow(w, r, "Campaign", "update") {
			return
		}
		var in engines.CRMCampaignInput
		if !crmDecode(w, r, &in) {
			return
		}
		c, err := engines.SaveCRMCampaign(tenantID, id, in, actor)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, c)
	default:
		crmMethod(w, r)
	}
}

func handleCRMCampaignRun(w http.ResponseWriter, r *http.Request) {
	tenantID, id, action := r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"), r.PathValue("action")
	if action == "results" {
		if !crmMethod(w, r, http.MethodGet) || !crmAllow(w, r, "Customer", "read") {
			return
		}
		res, err := engines.GetCRMCampaignResults(tenantID, id)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, res)
		return
	}
	if !crmMethod(w, r, http.MethodPost) || !crmAllow(w, r, "Campaign", "update") {
		return
	}
	run, err := engines.SetCRMCampaignRunState(tenantID, id, action, crmActor(r))
	if err != nil {
		crmFail(w, r, err)
		return
	}
	crmOK(w, run)
}

func handleCRMMessages(w http.ResponseWriter, r *http.Request) {
	tenantID, id := r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id")
	if !crmAllow(w, r, "Customer", "read") {
		return
	}
	switch {
	case id == "" && r.Method == http.MethodGet:
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		f := crmQueryMap(r, "status", "channel", "run_kind", "run_id", "customer_id", "reason_code", "history")
		list, err := engines.ListCRMSends(tenantID, f, limit)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		if engines.RoleSeesContacts(tenantID, r.Header.Get("Resolved-Role")) == false {
			for i := range list {
				list[i].Body = ""
			}
		}
		crmOK(w, map[string]interface{}{"messages": list})
	case id == "" && r.Method == http.MethodPost:
		var m engines.CRMStaffMessage
		if !crmDecode(w, r, &m) {
			return
		}
		s, err := engines.SendCRMStaffMessage(tenantID, crmActor(r), m)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, s)
	case r.PathValue("part") == "resolve":
		if !crmMethod(w, r, http.MethodPost) || !crmAllow(w, r, "Campaign", "update") {
			return
		}
		var req struct {
			Outcome string `json:"outcome"`
			Note    string `json:"note"`
		}
		if !crmDecode(w, r, &req) {
			return
		}
		if err := engines.ResolveCRMSend(tenantID, id, req.Outcome, req.Note, crmActor(r)); err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, map[string]bool{"settled": true})
	default:
		crmMethod(w, r)
	}
}

// --- journeys, feedback ---------------------------------------------------------------

func handleCRMJourneys(w http.ResponseWriter, r *http.Request) {
	tenantID, id, part := r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"), r.PathValue("part")
	if !crmAllow(w, r, "Customer", "read") {
		return
	}
	actor := crmActor(r)
	switch {
	case id == "" && r.Method == http.MethodGet:
		list, err := engines.ListCRMJourneys(tenantID)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, map[string]interface{}{"journeys": list})
	case id == "ready-made":
		if !crmMethod(w, r, http.MethodPost) || !crmAllow(w, r, "Campaign", "update") {
			return
		}
		list, err := engines.InstallCRMReadyJourneys(tenantID, actor)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, map[string]interface{}{"journeys": list})
	case part == "runs":
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		list, err := engines.ListCRMJourneyRuns(tenantID, id, r.URL.Query().Get("state"), limit)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, map[string]interface{}{"runs": list})
	case part == "publish":
		if !crmMethod(w, r, http.MethodPost) || !crmAllow(w, r, "Campaign", "update") {
			return
		}
		j, err := engines.PublishCRMJourney(tenantID, id, actor)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, j)
	case part == "switch":
		if !crmMethod(w, r, http.MethodPost) || !crmAllow(w, r, "Campaign", "update") {
			return
		}
		var req struct {
			Enabled  bool `json:"enabled"`
			StopLive bool `json:"stop_live"`
		}
		if !crmDecode(w, r, &req) {
			return
		}
		j, err := engines.SetCRMJourneyEnabled(tenantID, id, req.Enabled, req.StopLive, actor)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, j)
	case part != "":
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "Not found.")
	case r.Method == http.MethodGet:
		j, err := engines.GetCRMJourney(tenantID, id)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, j)
	case r.Method == http.MethodPost || r.Method == http.MethodPut:
		if !crmAllow(w, r, "Campaign", "update") {
			return
		}
		var req struct {
			Name        string                       `json:"name"`
			Description string                       `json:"description"`
			Definition  engines.CRMJourneyDefinition `json:"definition"`
		}
		if !crmDecode(w, r, &req) {
			return
		}
		j, err := engines.SaveCRMJourney(tenantID, id, req.Name, req.Description, req.Definition, actor)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, j)
	default:
		crmMethod(w, r)
	}
}

func handleCRMSurveys(w http.ResponseWriter, r *http.Request) {
	tenantID, id, part := r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"), r.PathValue("part")
	if !crmAllow(w, r, "Customer", "read") {
		return
	}
	switch {
	case id == "" && r.Method == http.MethodGet:
		list, err := engines.ListCRMSurveys(tenantID)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, map[string]interface{}{"surveys": list})
	case part == "summary":
		s, err := engines.GetCRMFeedbackSummary(tenantID, id, r.URL.Query().Get("from_date"), r.URL.Query().Get("to_date"))
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, s)
	case r.Method == http.MethodPost || r.Method == http.MethodPut:
		if !crmAllow(w, r, "Campaign", "update") {
			return
		}
		var in engines.CRMSurvey
		if !crmDecode(w, r, &in) {
			return
		}
		s, err := engines.SaveCRMSurvey(tenantID, id, in, crmActor(r))
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, s)
	default:
		crmMethod(w, r)
	}
}

// --- tickets and the inbox -----------------------------------------------------------

func handleCRMTickets(w http.ResponseWriter, r *http.Request) {
	tenantID, id, part := r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"), r.PathValue("part")
	actor := crmActor(r)
	switch {
	case id == "" && r.Method == http.MethodGet:
		if !crmAllow(w, r, "ServiceTicket", "read") {
			return
		}
		list, err := engines.ListCRMTickets(tenantID, crmQueryMap(r, "customer_id", "store", "state"))
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, map[string]interface{}{"tickets": list})
	case id == "" && r.Method == http.MethodPost:
		if !crmAllow(w, r, "ServiceTicket", "create") {
			return
		}
		var in engines.CRMTicketInput
		if !crmDecode(w, r, &in) {
			return
		}
		if in.Store == "" {
			in.Store = r.Header.Get("Resolved-Location")
		}
		tid, err := engines.CreateCRMTicket(tenantID, actor, in)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		t, err := engines.GetCRMTicket(tenantID, tid)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, t)
	case part == "notes":
		if !crmMethod(w, r, http.MethodPost) || !crmAllow(w, r, "ServiceTicket", "update") {
			return
		}
		var req struct {
			Note    string `json:"note"`
			Private bool   `json:"private"`
			Channel string `json:"channel"`
		}
		if !crmDecode(w, r, &req) {
			return
		}
		t, err := engines.AddCRMTicketNote(tenantID, id, req.Note, req.Private, req.Channel, actor)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, t)
	case part == "state":
		if !crmMethod(w, r, http.MethodPost) || !crmAllow(w, r, "ServiceTicket", "update") {
			return
		}
		var req struct {
			Action string `json:"action"`
			Value  string `json:"value"`
		}
		if !crmDecode(w, r, &req) {
			return
		}
		t, err := engines.SetCRMTicketState(tenantID, id, req.Action, req.Value, actor)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, t)
	case r.Method == http.MethodGet:
		if !crmAllow(w, r, "ServiceTicket", "read") {
			return
		}
		t, err := engines.GetCRMTicket(tenantID, id)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, t)
	default:
		crmMethod(w, r)
	}
}

func handleCRMConversations(w http.ResponseWriter, r *http.Request) {
	tenantID, role, id, part := r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Role"), r.PathValue("id"), r.PathValue("part")
	if !crmAllow(w, r, "Customer", "read") {
		return
	}
	switch {
	case id == "" && r.Method == http.MethodGet:
		list, err := engines.ListCRMConversations(tenantID, r.URL.Query().Get("status"), r.URL.Query().Get("assigned_to"))
		if err != nil {
			crmFail(w, r, err)
			return
		}
		if !engines.RoleSeesContacts(tenantID, role) {
			for i := range list {
				list[i].Phone = maskTail(list[i].Phone)
			}
		}
		crmOK(w, map[string]interface{}{"conversations": list})
	case part == "reply":
		if !crmMethod(w, r, http.MethodPost) {
			return
		}
		var req struct {
			Text       string `json:"text"`
			TemplateID string `json:"template_id"`
		}
		if !crmDecode(w, r, &req) {
			return
		}
		s, err := engines.ReplyCRMConversation(tenantID, id, req.Text, req.TemplateID, crmActor(r))
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, s)
	case part == "state":
		if !crmMethod(w, r, http.MethodPost) {
			return
		}
		var req struct {
			Action string `json:"action"`
			Value  string `json:"value"`
		}
		if !crmDecode(w, r, &req) {
			return
		}
		if err := engines.SetCRMConversation(tenantID, id, req.Action, req.Value, crmActor(r)); err != nil {
			crmFail(w, r, err)
			return
		}
		c, err := engines.GetCRMConversation(tenantID, role, id)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, c)
	case r.Method == http.MethodGet:
		c, err := engines.GetCRMConversation(tenantID, role, id)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, c)
	default:
		crmMethod(w, r)
	}
}

func maskTail(p string) string {
	if len(p) <= 4 {
		return "****"
	}
	return strings.Repeat("*", len(p)-4) + p[len(p)-4:]
}

// --- vouchers, referrals ---------------------------------------------------------------

func handleCRMVoucherHold(w http.ResponseWriter, r *http.Request) {
	if !crmMethod(w, r, http.MethodPost) || !crmAllow(w, r, "Voucher", "read") {
		return
	}
	var req struct {
		Code       string `json:"code"`
		Channel    string `json:"channel"`
		RefID      string `json:"ref_id"`
		CustomerID string `json:"customer_id"`
	}
	if !crmDecode(w, r, &req) {
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.PathValue("action") == "release" {
		if err := engines.ReleaseCRMVoucher(tenantID, req.Code, req.RefID); err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, map[string]bool{"released": true})
		return
	}
	res, err := engines.ReserveCRMVoucher(tenantID, req.Code, req.Channel, req.RefID, req.CustomerID)
	if err != nil {
		crmFail(w, r, err)
		return
	}
	crmOK(w, res)
}

func handleCRMReferrals(w http.ResponseWriter, r *http.Request) {
	tenantID, id := r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id")
	switch {
	case id == "" && r.Method == http.MethodGet:
		if !crmAllow(w, r, "Customer", "read") {
			return
		}
		list, err := engines.ListCRMReferrals(tenantID, r.URL.Query().Get("customer_id"))
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, map[string]interface{}{"referrals": list})
	case id == "" && r.Method == http.MethodPost:
		if !crmAllow(w, r, "Customer", "read") {
			return
		}
		var req struct {
			FriendID string `json:"friend_id"`
			Code     string `json:"code"`
		}
		if !crmDecode(w, r, &req) {
			return
		}
		ref, err := engines.RecordCRMReferral(tenantID, req.FriendID, req.Code, crmActor(r))
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, ref)
	case r.PathValue("part") == "decide":
		if !crmMethod(w, r, http.MethodPost) || !crmAllow(w, r, "Customer", "update") {
			return
		}
		var req struct {
			Approve bool `json:"approve"`
		}
		if !crmDecode(w, r, &req) {
			return
		}
		if err := engines.ApproveCRMReferral(tenantID, id, req.Approve, crmActor(r)); err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, map[string]bool{"decided": true})
	default:
		crmMethod(w, r)
	}
}

// --- clienteling -------------------------------------------------------------------------

func handleCRMTasks(w http.ResponseWriter, r *http.Request) {
	tenantID, id, action := r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"), r.PathValue("action")
	if !crmAllow(w, r, "Customer", "read") {
		return
	}
	actor := crmActor(r)
	switch {
	case id == "" && r.Method == http.MethodGet:
		f := crmQueryMap(r, "status", "store", "customer_id", "lead_id", "opportunity_id", "kind", "owner")
		if r.URL.Query().Get("mine") == "1" {
			f["mine"] = actor
			if f["store"] == "" {
				f["store"] = r.Header.Get("Resolved-Location")
			}
		}
		list, err := engines.ListCRMTasks(tenantID, f)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, map[string]interface{}{"tasks": list})
	case action != "":
		if !crmMethod(w, r, http.MethodPost) {
			return
		}
		var req struct {
			Outcome string `json:"outcome"`
		}
		if !crmDecode(w, r, &req) {
			return
		}
		t, err := engines.FinishCRMTask(tenantID, id, action, req.Outcome, actor)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, t)
	case r.Method == http.MethodGet:
		t, err := engines.GetCRMTask(tenantID, id)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, t)
	case r.Method == http.MethodPost || r.Method == http.MethodPut:
		var in engines.CRMTaskInput
		if !crmDecode(w, r, &in) {
			return
		}
		if in.Store == "" && id == "" {
			in.Store = r.Header.Get("Resolved-Location")
		}
		t, err := engines.SaveCRMTask(tenantID, id, in, actor)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, t)
	default:
		crmMethod(w, r)
	}
}

func handleCRMStoreSlots(w http.ResponseWriter, r *http.Request) {
	tenantID, store, day := r.Header.Get("Resolved-Tenant-ID"), r.PathValue("store"), r.PathValue("day")
	if !crmAllow(w, r, "Customer", "read") {
		return
	}
	if day != "" {
		slots, err := engines.ListCRMSlots(tenantID, store, day)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, map[string]interface{}{"slots": slots})
		return
	}
	if r.Method == http.MethodGet {
		s, err := engines.GetCRMStoreSlots(tenantID, store)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, s)
		return
	}
	if !crmMethod(w, r, http.MethodPut) || !crmAllow(w, r, "Campaign", "update") {
		return
	}
	var s engines.CRMStoreSlots
	if !crmDecode(w, r, &s) {
		return
	}
	s.Store = store
	out, err := engines.SaveCRMStoreSlots(tenantID, s, crmActor(r))
	if err != nil {
		crmFail(w, r, err)
		return
	}
	crmOK(w, out)
}

func handleCRMAppointments(w http.ResponseWriter, r *http.Request) {
	tenantID, id, action := r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"), r.PathValue("action")
	if !crmAllow(w, r, "Customer", "read") {
		return
	}
	switch {
	case id == "" && r.Method == http.MethodGet:
		q := r.URL.Query()
		list, err := engines.ListCRMAppointments(tenantID, q.Get("store"), q.Get("from_date"), q.Get("customer_id"))
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, map[string]interface{}{"appointments": list})
	case id == "" && r.Method == http.MethodPost:
		var in engines.CRMAppointmentInput
		if !crmDecode(w, r, &in) {
			return
		}
		if in.Store == "" {
			in.Store = r.Header.Get("Resolved-Location")
		}
		a, err := engines.BookCRMAppointment(tenantID, in, crmActor(r))
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, a)
	case action != "":
		if !crmMethod(w, r, http.MethodPost) {
			return
		}
		var req struct {
			Reason string `json:"reason"`
		}
		if !crmDecode(w, r, &req) {
			return
		}
		a, err := engines.SetCRMAppointmentStatus(tenantID, id, action, req.Reason, crmActor(r))
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, a)
	default:
		crmMethod(w, r)
	}
}

func handleCRMHolds(w http.ResponseWriter, r *http.Request) {
	tenantID, id, action := r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"), r.PathValue("action")
	if !crmAllow(w, r, "Customer", "read") {
		return
	}
	actor := crmActor(r)
	switch {
	case id == "" && r.Method == http.MethodGet:
		list, err := engines.ListCRMHolds(tenantID, crmQueryMap(r, "store", "customer_id", "status"))
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, map[string]interface{}{"holds": list})
	case id == "" && r.Method == http.MethodPost:
		var req struct {
			CustomerID string  `json:"customer_id"`
			ItemCode   string  `json:"item_code"`
			Store      string  `json:"store"`
			Qty        float64 `json:"qty"`
			HoldUntil  string  `json:"hold_until"`
			Note       string  `json:"note"`
		}
		if !crmDecode(w, r, &req) {
			return
		}
		h, err := engines.RequestCRMHold(tenantID, req.CustomerID, req.ItemCode, crmFirst(req.Store, r.Header.Get("Resolved-Location")), req.Qty, req.HoldUntil, req.Note, actor)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, h)
	case action == "decide":
		if !crmMethod(w, r, http.MethodPost) {
			return
		}
		var req struct {
			Confirm bool   `json:"confirm"`
			Reason  string `json:"reason"`
		}
		if !crmDecode(w, r, &req) {
			return
		}
		h, err := engines.DecideCRMHold(tenantID, id, req.Confirm, req.Reason, actor)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, h)
	case action == "ready":
		if !crmMethod(w, r, http.MethodPost) {
			return
		}
		var req struct {
			Channel    string `json:"channel"`
			TemplateID string `json:"template_id"`
		}
		if !crmDecode(w, r, &req) {
			return
		}
		s, err := engines.SendCRMHoldReady(tenantID, id, req.Channel, req.TemplateID, actor)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, s)
	default:
		crmMethod(w, r)
	}
}

// --- pipeline --------------------------------------------------------------------------------

func handleCRMPipelineConfig(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if !crmAllow(w, r, "SalesQuotation", "read") {
		return
	}
	switch r.PathValue("part") {
	case "stages":
		if r.Method == http.MethodGet {
			s, err := engines.ListCRMPipelineStages(tenantID)
			if err != nil {
				crmFail(w, r, err)
				return
			}
			crmOK(w, map[string]interface{}{"stages": s})
			return
		}
		if !crmMethod(w, r, http.MethodPut) || !crmAllow(w, r, "Campaign", "update") {
			return
		}
		var req struct {
			Stages []engines.CRMPipelineStage `json:"stages"`
		}
		if !crmDecode(w, r, &req) {
			return
		}
		s, err := engines.SaveCRMPipelineStages(tenantID, req.Stages)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, map[string]interface{}{"stages": s})
	case "lead-rules":
		if r.Method == http.MethodGet {
			s, err := engines.ListCRMLeadRules(tenantID)
			if err != nil {
				crmFail(w, r, err)
				return
			}
			crmOK(w, map[string]interface{}{"rules": s})
			return
		}
		if !crmMethod(w, r, http.MethodPut) || !crmAllow(w, r, "Campaign", "update") {
			return
		}
		var req struct {
			Rules []engines.CRMLeadRule `json:"rules"`
		}
		if !crmDecode(w, r, &req) {
			return
		}
		s, err := engines.SaveCRMLeadRules(tenantID, req.Rules)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, map[string]interface{}{"rules": s})
	case "forecast":
		if r.Method == http.MethodGet {
			rows, err := engines.GetCRMForecast(tenantID)
			if err != nil {
				crmFail(w, r, err)
				return
			}
			snaps, err := engines.ListCRMForecastSnapshots(tenantID)
			if err != nil {
				crmFail(w, r, err)
				return
			}
			crmOK(w, map[string]interface{}{"rows": rows, "snapshots": snaps})
			return
		}
		if !crmMethod(w, r, http.MethodPost) {
			return
		}
		var req struct {
			Label string `json:"label"`
		}
		if !crmDecode(w, r, &req) {
			return
		}
		s, err := engines.TakeCRMForecastSnapshot(tenantID, req.Label, crmActor(r))
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, s)
	default:
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "Not found.")
	}
}

func handleCRMLeads(w http.ResponseWriter, r *http.Request) {
	tenantID, id, part := r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"), r.PathValue("part")
	if !crmAllow(w, r, "SalesQuotation", "read") {
		return
	}
	actor := crmActor(r)
	switch {
	case id == "" && r.Method == http.MethodGet:
		f := crmQueryMap(r, "status", "owner", "unassigned")
		if r.URL.Query().Get("mine") == "1" {
			f["owner"] = actor
		}
		list, err := engines.ListCRMLeads(tenantID, f)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, map[string]interface{}{"leads": list})
	case part == "status":
		if !crmMethod(w, r, http.MethodPost) {
			return
		}
		var req struct {
			Status string `json:"status"`
			Reason string `json:"reason"`
		}
		if !crmDecode(w, r, &req) {
			return
		}
		l, err := engines.SetCRMLeadStatus(tenantID, id, req.Status, req.Reason, actor)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, l)
	case part == "convert":
		if !crmMethod(w, r, http.MethodPost) || !crmAllow(w, r, "SalesQuotation", "create") {
			return
		}
		var req struct {
			Title         string  `json:"title"`
			Value         float64 `json:"value"`
			ExpectedClose string  `json:"expected_close"`
		}
		if !crmDecode(w, r, &req) {
			return
		}
		l, err := engines.ConvertCRMLead(tenantID, id, req.Title, req.Value, req.ExpectedClose, actor)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, l)
	case part != "":
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "Not found.")
	case r.Method == http.MethodGet:
		l, err := engines.GetCRMLead(tenantID, id)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, l)
	case r.Method == http.MethodPost || r.Method == http.MethodPut:
		var in engines.CRMLead
		if !crmDecode(w, r, &in) {
			return
		}
		l, err := engines.SaveCRMLead(tenantID, id, in, actor)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, l)
	default:
		crmMethod(w, r)
	}
}

func handleCRMOpportunities(w http.ResponseWriter, r *http.Request) {
	tenantID, role, id, part := r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Role"), r.PathValue("id"), r.PathValue("part")
	if !crmAllow(w, r, "SalesQuotation", "read") {
		return
	}
	actor := crmActor(r)
	switch {
	case id == "" && r.Method == http.MethodGet:
		list, err := engines.ListCRMOpportunities(tenantID, crmQueryMap(r, "status", "owner", "customer_id", "stage_id"))
		if err != nil {
			crmFail(w, r, err)
			return
		}
		stages, err := engines.ListCRMPipelineStages(tenantID)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, map[string]interface{}{"opportunities": list, "stages": stages})
	case part == "move":
		if !crmMethod(w, r, http.MethodPost) {
			return
		}
		var req struct {
			StageID    string `json:"stage_id"`
			LostReason string `json:"lost_reason"`
		}
		if !crmDecode(w, r, &req) {
			return
		}
		o, err := engines.MoveCRMOpportunity(tenantID, id, req.StageID, req.LostReason, actor)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, o)
	case part == "quotation":
		if r.Method == http.MethodGet {
			o, err := engines.GetCRMOpportunity(tenantID, id)
			if err != nil {
				crmFail(w, r, err)
				return
			}
			if o.QuotationID == "" {
				crmOK(w, map[string]interface{}{"quotation": nil})
				return
			}
			d, lines, status, err := engines.GetCRMQuotation(tenantID, o.QuotationID)
			if err != nil {
				crmFail(w, r, err)
				return
			}
			crmOK(w, map[string]interface{}{"quotation": d, "lines": lines, "status": status})
			return
		}
		if !crmMethod(w, r, http.MethodPost) || !crmAllow(w, r, "SalesQuotation", "create") {
			return
		}
		var req struct {
			Lines      []engines.CRMQuotationLine `json:"lines"`
			ValidUntil string                     `json:"valid_until"`
			Terms      string                     `json:"terms"`
			Submit     bool                       `json:"submit"`
		}
		if !crmDecode(w, r, &req) {
			return
		}
		var o *engines.CRMOpportunity
		var err error
		if req.Submit && len(req.Lines) == 0 {
			o, err = engines.SubmitCRMQuotation(tenantID, id, actor, role)
		} else {
			o, err = engines.SaveCRMQuotation(tenantID, id, req.Lines, req.ValidUntil, req.Terms, actor)
			if err == nil && req.Submit {
				o, err = engines.SubmitCRMQuotation(tenantID, id, actor, role)
			}
		}
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, o)
	case part == "order":
		if !crmMethod(w, r, http.MethodPost) {
			return
		}
		o, err := engines.RetryCRMSalesOrder(tenantID, id, actor)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, o)
	case part != "":
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "Not found.")
	case r.Method == http.MethodGet:
		o, err := engines.GetCRMOpportunity(tenantID, id)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, o)
	case r.Method == http.MethodPost || r.Method == http.MethodPut:
		var in engines.CRMOpportunityInput
		if !crmDecode(w, r, &in) {
			return
		}
		o, err := engines.SaveCRMOpportunity(tenantID, id, in, actor)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, o)
	default:
		crmMethod(w, r)
	}
}

// --- public: the customer's pages and provider callbacks -------------------------------

func handleCRMPublicPreferences(w http.ResponseWriter, r *http.Request) {
	tenantID, token := r.Header.Get("Resolved-Tenant-ID"), r.URL.Query().Get("t")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		p, err := engines.GetCRMPreferences(tenantID, token)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, p)
		return
	}
	if !crmMethod(w, r, http.MethodPost) {
		return
	}
	var ch engines.CRMPreferenceChange
	if !crmDecode(w, r, &ch) {
		return
	}
	p, err := engines.SetCRMPreferences(tenantID, token, ch)
	if err != nil {
		crmFail(w, r, err)
		return
	}
	crmOK(w, p)
}

// handleCRMPublicUnsubscribe is RFC 8058 one-click unsubscribe: the mail
// client POSTs List-Unsubscribe=One-Click to the List-Unsubscribe address.
func handleCRMPublicUnsubscribe(w http.ResponseWriter, r *http.Request) {
	if !crmMethod(w, r, http.MethodPost) {
		return
	}
	if err := engines.CRMUnsubscribe(r.Header.Get("Resolved-Tenant-ID"), r.URL.Query().Get("t")); err != nil {
		crmFail(w, r, err)
		return
	}
	crmOK(w, map[string]bool{"unsubscribed": true})
}

func handleCRMPublicFeedback(w http.ResponseWriter, r *http.Request) {
	tenantID, token := r.Header.Get("Resolved-Tenant-ID"), r.URL.Query().Get("t")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		f, err := engines.GetCRMFeedbackForm(tenantID, token)
		if err != nil {
			crmFail(w, r, err)
			return
		}
		crmOK(w, f)
		return
	}
	if !crmMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Score   int    `json:"score"`
		Comment string `json:"comment"`
	}
	if !crmDecode(w, r, &req) {
		return
	}
	f, err := engines.SubmitCRMFeedback(tenantID, token, req.Score, req.Comment)
	if err != nil {
		crmFail(w, r, err)
		return
	}
	crmOK(w, f)
}

// handleCRMHookWhatsApp is WhatsApp's webhook: GET verifies the subscription,
// POST carries signed statuses, messages and template updates.
func handleCRMHookWhatsApp(w http.ResponseWriter, r *http.Request) {
	tenantID, key := r.Header.Get("Resolved-Tenant-ID"), r.URL.Query().Get("k")
	if r.Method == http.MethodGet {
		challenge, err := engines.VerifyCRMWhatsAppSubscription(tenantID, key, r.URL.Query())
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusForbidden, "Verification failed.")
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(challenge))
		return
	}
	if !crmMethod(w, r, http.MethodPost) {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, "Could not read the callback.")
		return
	}
	res, err := engines.HandleCRMWhatsAppWebhook(tenantID, key, body, r.Header.Get("X-Hub-Signature-256"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnauthorized, "Callback refused.")
		return
	}
	crmOK(w, res)
}

// handleCRMHook takes SMS, email and webhook delivery reports (and inbound
// SMS) as JSON, a form, or query parameters - whatever the provider sends.
func handleCRMHook(w http.ResponseWriter, r *http.Request) {
	tenantID, key := r.Header.Get("Resolved-Tenant-ID"), r.URL.Query().Get("k")
	kind := strings.TrimPrefix(r.URL.Path, "/api/v1/crm/hooks/")
	fields := map[string]string{}
	for k, v := range r.URL.Query() {
		if k != "k" && k != "tenant_id" && len(v) > 0 {
			fields[k] = v[0]
		}
	}
	if r.Method == http.MethodPost {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		ct := r.Header.Get("Content-Type")
		if strings.Contains(ct, "json") {
			var m map[string]interface{}
			if json.Unmarshal(body, &m) == nil {
				for k, v := range m {
					switch t := v.(type) {
					case string:
						fields[k] = t
					case float64:
						fields[k] = strconv.FormatFloat(t, 'f', -1, 64)
					case bool:
						fields[k] = strconv.FormatBool(t)
					}
				}
			}
		} else if vals, err := url.ParseQuery(string(body)); err == nil {
			for k, v := range vals {
				if len(v) > 0 {
					fields[k] = v[0]
				}
			}
		}
	}
	res, err := engines.HandleCRMProviderCallback(tenantID, kind, key, fields)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "Callback refused.")
		return
	}
	crmOK(w, res)
}
