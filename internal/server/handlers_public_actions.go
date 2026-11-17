package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"

	"custom_erp/engines"
)

// 205.14: the Actions API. A reviewed list of the screens' own action handlers,
// each re-entered as the key owner (asKeyOwner), so the screen's rules - stage
// gates, stock, approvals, the owner's role and store - hold unchanged. Only a
// handler that was read and checked is listed. Money and period actions stay
// off the list on purpose: a payment, a posting, a period close or a tax filing
// is made on its screen by a person, and the generic records routes already
// cover submit, cancel and approve with their approval rules.

type publicAction struct {
	Module  string
	Action  string
	Level   string // read | write
	Summary string
	Body    string // what the body carries, in one line
	Pattern string // the route's entry in route_capabilities.go
	Handler http.HandlerFunc
	HasID   bool // the body's "id" is the record the action acts on
}

// publicActions is the whole list. A test walks it: every entry names a known
// module and level, its pattern holds a capability entry, and its path is unique.
var publicActions = []publicAction{
	{Module: "oms", Action: "order-hold", Level: "write", Summary: "Put an order on hold",
		Body: `{"id": "<order id>", "reason_code": "...", "owner": "..."}`, Pattern: "POST /api/v1/orders/{id}/hold", Handler: handlePlaceOrderHold, HasID: true},
	{Module: "oms", Action: "order-release-hold", Level: "write", Summary: "Release an order from hold",
		Body: `{"id": "<order id>"}`, Pattern: "POST /api/v1/orders/{id}/release-hold", Handler: handleReleaseOrderHold, HasID: true},
	{Module: "oms", Action: "order-cancel", Level: "write", Summary: "Cancel an order, if its stage allows",
		Body: `{"id": "<order id>", "reason_code": "..."}`, Pattern: "POST /api/v1/orders/{id}/cancel", Handler: handleCancelOrder, HasID: true},
	{Module: "oms", Action: "order-release-to-fulfillment", Level: "write", Summary: "Release a reserved order to the warehouse",
		Body: `{"id": "<order id>"}`, Pattern: "POST /api/v1/orders/{id}/release-to-fulfillment", Handler: handleReleaseOrderToFulfillment, HasID: true},
	{Module: "inventory", Action: "piece-hold-place", Level: "write", Summary: "Hold one serial-numbered piece",
		Body: `{"sku": "...", "serial_no": "...", "customer_id": "...", "until": "YYYY-MM-DD", "note": "..."}`, Pattern: "POST /api/v1/inventory/piece-holds", Handler: handlePlacePieceHold},
	{Module: "inventory", Action: "piece-hold-release", Level: "write", Summary: "Release a held piece",
		Body: `{"sku": "...", "serial_no": "...", "reason": "..."}`, Pattern: "POST /api/v1/inventory/piece-holds/release", Handler: handleReleasePieceHold},
}

func findPublicAction(module, action string) *publicAction {
	for i := range publicActions {
		if publicActions[i].Module == module && publicActions[i].Action == action {
			return &publicActions[i]
		}
	}
	return nil
}

// publicAPIModuleCall names the module a call reaches when its path names no
// document type: an action's module, a report's module. The middleware judges it
// with the key's rules before the handler runs, so a refusal is logged as one.
func publicAPIModuleCall(r *http.Request) (module string, write bool) {
	path := r.URL.Path
	switch {
	case strings.HasPrefix(path, "/api/public/v1/actions/") && r.Method == http.MethodPost:
		if a := findPublicAction(r.PathValue("module"), r.PathValue("action")); a != nil {
			return a.Module, a.Level == "write"
		}
	case strings.HasPrefix(path, "/api/public/v1/reports/") && r.Method == http.MethodGet:
		if def, ok := engines.GetReportDefinition(r.PathValue("id")); ok {
			return def.ModuleKey, false
		}
	}
	return "", false
}

// handlePublicActions lists the actions this key can run.
func handlePublicActions(w http.ResponseWriter, r *http.Request) {
	call := publicAPICallFrom(r)
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	w.Header().Set("Content-Type", "application/json")
	out := []map[string]interface{}{}
	if call != nil {
		for _, a := range publicActions {
			if refusal, err := engines.CheckAPIKeyModule(tenantID, call.Credential, call.Access, a.Module, a.Level == "write"); err != nil || refusal != nil {
				continue
			}
			out = append(out, map[string]interface{}{
				"module": a.Module, "action": a.Action, "level": a.Level, "summary": a.Summary, "body": a.Body,
				"method": "POST", "path": "/api/public/v1/actions/" + a.Module + "/" + a.Action,
			})
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": out})
}

// handlePublicAction runs one listed action as the key owner.
func handlePublicAction(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	a := findPublicAction(r.PathValue("module"), r.PathValue("action"))
	if a == nil {
		writeAPIErrorDetail(w, r, engines.APIKeyActionUnknown, "", "No action "+r.PathValue("module")+"/"+r.PathValue("action")+". See GET /api/public/v1/actions.")
		return
	}
	owner, ok := asKeyOwner(w, r, a.Pattern)
	if !ok {
		return
	}
	if r.Body != nil {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			writeAPIErrorDetail(w, r, engines.APIKeyInvalidRequest, "", "The body could not be read.")
			return
		}
		owner.Body = io.NopCloser(bytes.NewReader(raw))
		if a.HasID {
			var probe struct {
				ID string `json:"id"`
			}
			_ = json.Unmarshal(raw, &probe)
			if strings.TrimSpace(probe.ID) == "" {
				writeAPIErrorDetail(w, r, engines.APIKeyInvalidRequest, "", `The body needs "id".`)
				return
			}
			owner.SetPathValue("id", probe.ID)
		}
	} else if a.HasID {
		writeAPIErrorDetail(w, r, engines.APIKeyInvalidRequest, "", `The body needs "id".`)
		return
	}
	// The action changes the record's status here, not through apiMiddleware,
	// so its webhook event is sent here the same way (ask 226).
	if a.HasID {
		tenantID, id := r.Header.Get("Resolved-Tenant-ID"), owner.PathValue("id")
		if engines.DocumentEventsWanted(tenantID) {
			if doctype, prior := engines.DocumentByID(tenantID, id); doctype != "" {
				defer publishStatusMove(tenantID, doctype, id, prior)
			}
		}
	}
	a.Handler(w, owner)
}

// publicActionKeys lists "<module>/<action>" in a stable order, for the spec.
func publicActionKeys() []string {
	out := make([]string, 0, len(publicActions))
	for _, a := range publicActions {
		out = append(out, a.Module+"/"+a.Action)
	}
	sort.Strings(out)
	return out
}
