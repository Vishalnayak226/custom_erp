package server

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"custom_erp/db"
	"custom_erp/engines"
)

// Review fixes for 148.37: an assistant never acts as a full-access user, an
// order status shows only the user's stores' lines and parcels, a draft is
// bounded, and a tenant clone / reset knows the link table.

func TestStage14837FullAccessUserCannotBeAssistant(t *testing.T) {
	f := newAIFixture(t)
	f.moduleOn(true)
	for _, role := range []string{engines.RoleSuperAdmin, engines.RoleAdmin} {
		id, _ := f.user(role, "")
		rec := f.do(http.MethodPost, "/api/v1/admin/ai-access/clients", f.admin,
			fmt.Sprintf(`{"name":"x","acting_user":%q,"scopes":["ai:read"]}`, id), nil)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("issue for %s: %d %s", role, rec.Code, rec.Body.String())
		}
	}
	// A user promoted after the client was made stops working on the next call.
	id, _ := f.user(engines.RoleStoreManager, f.locA)
	_, token := f.issue(id, "ai:read")
	if rec := f.do(http.MethodGet, f.stockPath(f.locA), token, "", nil); rec.Code != 200 {
		t.Fatalf("before promotion: %d", rec.Code)
	}
	engines.ResolveLiveUserState("default", id)
	if _, err := db.DB.Exec(`UPDATE tenant_default.users SET role = $2 WHERE id = $1`, id, engines.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	rec := f.do(http.MethodGet, f.stockPath(f.locA), token, "", nil)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "SEC-0309") {
		t.Fatalf("a promoted user's assistant: %d %s", rec.Code, rec.Body.String())
	}
}

func TestStage14837OrderStatusHidesOtherStoresLinesAndParcels(t *testing.T) {
	f := newAIFixture(t)
	f.moduleOn(true)
	userID, _ := f.user(engines.RoleStoreManager, f.locA)
	_, token := f.issue(userID, "ai:read")
	skuA, skuB := "H37LA"+f.suffix, "H37LB"+f.suffix
	id := engines.NewDocID("SO")
	creator := `(SELECT id FROM tenant_default.users ORDER BY id LIMIT 1)`
	ins := func(docID, doctype, data, status string) {
		t.Helper()
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1,$2,$3,$4,`+creator+`)`, docID, doctype, data, status); err != nil {
			t.Fatal(err)
		}
	}
	ins(id, "SalesOrder", `{"order_status":"Reserved"}`, "Reserved")
	ins(id+"-L1", "SalesOrderLine", fmt.Sprintf(`{"order_id":%q,"sku":%q,"qty":1,"location_code":%q}`, id, skuA, f.locA), "Reserved")
	ins(id+"-L2", "SalesOrderLine", fmt.Sprintf(`{"order_id":%q,"sku":%q,"qty":1,"location_code":%q}`, id, skuB, f.locB), "Reserved")
	ins(id+"-B1", "LogisticsBooking", fmt.Sprintf(`{"order_id":%q,"tracking_number":"TRKA%s","location_code":%q}`, id, f.suffix, f.locA), "Booked")
	ins(id+"-B2", "LogisticsBooking", fmt.Sprintf(`{"order_id":%q,"tracking_number":"TRKB%s","location_code":%q}`, id, f.suffix, f.locB), "Booked")
	f.cleanup = append(f.cleanup, func() {
		db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1 OR data->>'order_id' = $1`, id)
	})
	rec := f.do(http.MethodGet, "/api/public/v1/ai/orders/"+id+"/status", token, "", nil)
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, skuA) || !strings.Contains(body, "TRKA"+f.suffix) {
		t.Fatalf("own lines and parcel: %d %s", rec.Code, body)
	}
	if strings.Contains(body, skuB) || strings.Contains(body, "TRKB"+f.suffix) {
		t.Fatalf("another store's line or parcel leaked: %s", body)
	}
}

func TestStage14837DraftLimits(t *testing.T) {
	f := newAIFixture(t)
	f.moduleOn(true)
	userID, _ := f.user(engines.RoleStoreManager, f.locA)
	client, _ := f.issue(userID, "ai:read", "ai:draft")
	actor, err := engines.ResolveAIActor("default", client.ClientID)
	if err != nil {
		t.Fatal(err)
	}
	line := []engines.AIDraftLine{{SKU: f.sku, Qty: 1, UnitPrice: 10}}
	made := []string{}
	t.Cleanup(func() {
		for _, id := range made {
			db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, id)
		}
	})
	if _, err := engines.CreateAssistantDraft(actor, engines.AIDraftInput{CustomerName: strings.Repeat("a", 201), Lines: line}); err == nil {
		t.Fatal("a 201-letter customer name was accepted")
	}
	if _, err := engines.CreateAssistantDraft(actor, engines.AIDraftInput{ShippingAddress: strings.Repeat("a", 501), Lines: line}); err == nil {
		t.Fatal("a 501-letter address was accepted")
	}
	// An item that is not active cannot be drafted.
	if _, err := db.DB.Exec(`UPDATE tenant_default.documents SET status = 'Inactive' WHERE id = $1`, "ITEM-"+f.sku); err != nil {
		t.Fatal(err)
	}
	if _, err := engines.CreateAssistantDraft(actor, engines.AIDraftInput{Lines: line}); err == nil {
		t.Fatal("an inactive item was accepted")
	}
	db.DB.Exec(`UPDATE tenant_default.documents SET status = 'Active' WHERE id = $1`, "ITEM-"+f.sku)
	for i := 0; i < 20; i++ {
		id, err := engines.CreateAssistantDraft(actor, engines.AIDraftInput{CustomerName: "c", Lines: line})
		if err != nil {
			t.Fatalf("draft %d: %v", i+1, err)
		}
		made = append(made, id)
	}
	if id, err := engines.CreateAssistantDraft(actor, engines.AIDraftInput{CustomerName: "c", Lines: line}); err == nil {
		made = append(made, id)
		t.Fatal("a 21st open draft was accepted")
	}
}

// The people's side of a draft: a list of the assistant drafts at the user's
// stores, which is where Submit lives. Off with the module; a draft at another
// store is not listed.
func TestStage14837AssistantDraftListShowsOnlyTheUsersStores(t *testing.T) {
	f := newAIFixture(t)
	f.moduleOn(true)
	userA, tokA := f.user(engines.RoleStoreManager, f.locA)
	_, tokB := f.user(engines.RoleStoreManager, f.locB)
	client, _ := f.issue(userA, "ai:read", "ai:draft")
	actor, err := engines.ResolveAIActor("default", client.ClientID)
	if err != nil {
		t.Fatal(err)
	}
	id, err := engines.CreateAssistantDraft(actor, engines.AIDraftInput{CustomerName: "Asha", Lines: []engines.AIDraftLine{{SKU: f.sku, Qty: 2, UnitPrice: 10}}})
	if err != nil {
		t.Fatal(err)
	}
	f.cleanup = append(f.cleanup, func() { db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, id) })
	rec := f.do(http.MethodGet, "/api/v1/orders/assistant-drafts", tokA, "", nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), id) || !strings.Contains(rec.Body.String(), "Asha") {
		t.Fatalf("own store's list: %d %s", rec.Code, rec.Body.String())
	}
	rec = f.do(http.MethodGet, "/api/v1/orders/assistant-drafts", tokB, "", nil)
	if rec.Code != 200 || strings.Contains(rec.Body.String(), id) {
		t.Fatalf("another store sees the draft: %d %s", rec.Code, rec.Body.String())
	}
	f.moduleOn(false)
	if rec := f.do(http.MethodGet, "/api/v1/orders/assistant-drafts", tokA, "", nil); rec.Code != http.StatusForbidden {
		t.Fatalf("list while off: %d", rec.Code)
	}
}
