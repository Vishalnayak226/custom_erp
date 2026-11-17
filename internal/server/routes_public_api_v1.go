package server

import (
	"net/http"

	"custom_erp/engines"
)

// Stage 38.1 + 38.8 - the public API route table.
//
// The table is declarative rather than a list of http.HandleFunc calls because
// two things need to read it: the registration loop below, and the OpenAPI
// generator (38.8). A spec generated from a separate hand-maintained list is a
// spec that is wrong the first time someone adds a route and forgets, so there
// is exactly one list and both consumers read it.
//
// Response carries a zero value of the type the handler encodes. The generator
// reflects over it for the schema, which means the published contract cannot
// drift from the Go type - renaming a json tag changes the spec in the same
// commit.

type publicAPIParam struct {
	Name        string
	In          string // "query" or "path"
	Required    bool
	Type        string // "string" or "integer"
	Description string
}

type publicAPIRoute struct {
	Method      string
	Path        string
	Scope       string
	ModuleKey   string // entitlement, independent of the credential's scope
	Summary     string
	Description string
	Params      []publicAPIParam
	Response    interface{}
	Handler     http.HandlerFunc
}

// publicAPIV1Routes is the complete public surface. A route absent from this
// slice is not reachable by any API credential.
func publicAPIV1Routes() []publicAPIRoute {
	return []publicAPIRoute{
		{
			Method: http.MethodGet, Path: "/api/public/v1/items", Scope: "items:read", ModuleKey: "master_data",
			Summary:     "List products",
			Description: "One page of sellable products. Cancelled and deleted products are excluded. Use updated_since to poll for changes rather than re-reading the whole catalog.",
			Params: []publicAPIParam{
				{Name: "limit", In: "query", Type: "integer", Description: "Page size, 1-200. Defaults to 50."},
				{Name: "offset", In: "query", Type: "integer", Description: "Rows to skip. Defaults to 0."},
				{Name: "updated_since", In: "query", Type: "string", Description: "RFC3339 UTC timestamp; returns only products changed at or after it."},
			},
			Response: engines.PublicPage{Data: []engines.PublicItem{}},
			Handler:  handlePublicListItems,
		},
		{
			Method: http.MethodGet, Path: "/api/public/v1/items/{code}", Scope: "items:read", ModuleKey: "master_data",
			Summary:     "Get one product",
			Description: "Curated product identity for a single item code.",
			Params: []publicAPIParam{
				{Name: "code", In: "path", Required: true, Type: "string", Description: "The item code."},
			},
			Response: engines.PublicItem{},
			Handler:  handlePublicGetItem,
		},
		{
			Method: http.MethodGet, Path: "/api/public/v1/inventory", Scope: "inventory:read", ModuleKey: "inventory",
			Summary:     "Read availability for one SKU",
			Description: "Available-to-sell quantity per location, computed with the same formula the order path enforces. A sku is required; there is no whole-catalog stock export on this surface.",
			Params: []publicAPIParam{
				{Name: "sku", In: "query", Required: true, Type: "string", Description: "The item code to look up."},
				{Name: "location", In: "query", Type: "string", Description: "Restrict to one location code. Omit for every location holding this SKU."},
			},
			Response: PublicInventoryResponse{Data: []engines.PublicInventoryLevel{}},
			Handler:  handlePublicInventory,
		},
		{
			Method: http.MethodGet, Path: "/api/public/v1/orders/{id}/status", Scope: "orders:read", ModuleKey: "oms",
			Summary:     "Track an order",
			Description: "Order status with per-line status and any shipments. The path segment accepts either this system's order id or the channel order id the order was imported with.",
			Params: []publicAPIParam{
				{Name: "id", In: "path", Required: true, Type: "string", Description: "Order id or channel order id."},
			},
			Response: engines.PublicOrderStatus{},
			Handler:  handlePublicOrderStatus,
		},
		{
			Method: http.MethodPost, Path: "/api/public/v1/notifications/{id}/delivery-status", Scope: "notifications:write", ModuleKey: "oms",
			Summary:     "Report a notification's delivery",
			Description: "The provider's delivery receipt. {id} is the delivery_id the notification webhook carried (also sent as the X-Notification-Delivery header). Body: {\"status\": \"delivered\" | \"failed\", \"provider_ref\": \"...\", \"detail\": \"...\"}. Delivered is final; repeating the same status is a no-op.",
			Params: []publicAPIParam{
				{Name: "id", In: "path", Required: true, Type: "string", Description: "The notification's delivery_id."},
			},
			Response: PublicNotificationDeliveryStatusResponse{},
			Handler:  handlePublicNotificationDeliveryStatus,
		},
		{
			Method: http.MethodGet, Path: "/api/public/v1/records/{doctype}", Scope: publicAPIDynamicScope, ModuleKey: "core",
			Summary:     "List records",
			Description: "Records of one document type the key may read. changed_since and cursor page through changes.",
			Params: []publicAPIParam{
				{Name: "doctype", In: "path", Required: true, Type: "string", Description: "The document type, such as PurchaseOrder."},
				{Name: "changed_since", In: "query", Type: "string", Description: "RFC3339 time; lists records changed at or after it, oldest change first."},
				{Name: "cursor", In: "query", Type: "string", Description: "The X-Next-Cursor of the previous page."},
				{Name: "limit", In: "query", Type: "integer", Description: "Page size."},
			},
			Response: map[string]interface{}{},
			Handler:  handlePublicRecords,
		},
		{
			Method: http.MethodPost, Path: "/api/public/v1/records/{doctype}", Scope: publicAPIDynamicScope, ModuleKey: "core",
			Summary:     "Create a record",
			Description: "Creates a record as the key owner. Idempotency-Key is required.",
			Params: []publicAPIParam{
				{Name: "doctype", In: "path", Required: true, Type: "string", Description: "The document type."},
			},
			Response: map[string]interface{}{},
			Handler:  handlePublicRecords,
		},
		{
			Method: http.MethodGet, Path: "/api/public/v1/records/{doctype}/schema", Scope: publicAPIDynamicScope, ModuleKey: "core",
			Summary:     "Describe a document type",
			Description: "Fields the key owner may see, with type, required and read-only.",
			Params: []publicAPIParam{
				{Name: "doctype", In: "path", Required: true, Type: "string", Description: "The document type."},
			},
			Response: map[string]interface{}{},
			Handler:  handlePublicRecordSchema,
		},
		{
			Method: http.MethodGet, Path: "/api/public/v1/records/{doctype}/{id}", Scope: publicAPIDynamicScope, ModuleKey: "core",
			Summary:     "Get a record",
			Description: "One record, as the key owner sees it.",
			Params: []publicAPIParam{
				{Name: "doctype", In: "path", Required: true, Type: "string", Description: "The document type."},
				{Name: "id", In: "path", Required: true, Type: "string", Description: "The record id."},
			},
			Response: map[string]interface{}{},
			Handler:  handlePublicRecords,
		},
		{
			Method: http.MethodPut, Path: "/api/public/v1/records/{doctype}/{id}", Scope: publicAPIDynamicScope, ModuleKey: "core",
			Summary:     "Change a record",
			Description: "Changes a record the owner may change. Idempotency-Key is required.",
			Params: []publicAPIParam{
				{Name: "doctype", In: "path", Required: true, Type: "string", Description: "The document type."},
				{Name: "id", In: "path", Required: true, Type: "string", Description: "The record id."},
			},
			Response: map[string]interface{}{},
			Handler:  handlePublicRecords,
		},
		{
			Method: http.MethodDelete, Path: "/api/public/v1/records/{doctype}/{id}", Scope: publicAPIDynamicScope, ModuleKey: "core",
			Summary:     "Delete a draft",
			Description: "Deletes a draft only; cancel anything else.",
			Params: []publicAPIParam{
				{Name: "doctype", In: "path", Required: true, Type: "string", Description: "The document type."},
				{Name: "id", In: "path", Required: true, Type: "string", Description: "The record id."},
			},
			Response: map[string]interface{}{},
			Handler:  handlePublicRecords,
		},
		{
			Method: http.MethodPost, Path: "/api/public/v1/records/{doctype}/{id}/submit", Scope: publicAPIDynamicScope, ModuleKey: "core",
			Summary:     "Submit a record for approval",
			Description: "Submit a record for approval. Idempotency-Key is required.",
			Params: []publicAPIParam{
				{Name: "doctype", In: "path", Required: true, Type: "string", Description: "The document type."},
				{Name: "id", In: "path", Required: true, Type: "string", Description: "The record id."},
			},
			Response: map[string]interface{}{},
			Handler:  handlePublicRecordAction("submit"),
		},
		{
			Method: http.MethodPost, Path: "/api/public/v1/records/{doctype}/{id}/cancel", Scope: publicAPIDynamicScope, ModuleKey: "core",
			Summary:     "Cancel a record",
			Description: "Cancel a record. Idempotency-Key is required.",
			Params: []publicAPIParam{
				{Name: "doctype", In: "path", Required: true, Type: "string", Description: "The document type."},
				{Name: "id", In: "path", Required: true, Type: "string", Description: "The record id."},
			},
			Response: map[string]interface{}{},
			Handler:  handlePublicRecordAction("cancel"),
		},
		{
			Method: http.MethodPost, Path: "/api/public/v1/records/{doctype}/{id}/approve", Scope: publicAPIDynamicScope, ModuleKey: "core",
			Summary:     "Approve a record (keys with can_approve only)",
			Description: "Approve a record (keys with can_approve only). Idempotency-Key is required.",
			Params: []publicAPIParam{
				{Name: "doctype", In: "path", Required: true, Type: "string", Description: "The document type."},
				{Name: "id", In: "path", Required: true, Type: "string", Description: "The record id."},
			},
			Response: map[string]interface{}{},
			Handler:  handlePublicRecordAction("approve"),
		},
		{
			Method: http.MethodPost, Path: "/api/public/v1/records/{doctype}/{id}/reject", Scope: publicAPIDynamicScope, ModuleKey: "core",
			Summary:     "Reject a record (keys with can_approve only)",
			Description: "Reject a record (keys with can_approve only). Idempotency-Key is required.",
			Params: []publicAPIParam{
				{Name: "doctype", In: "path", Required: true, Type: "string", Description: "The document type."},
				{Name: "id", In: "path", Required: true, Type: "string", Description: "The record id."},
			},
			Response: map[string]interface{}{},
			Handler:  handlePublicRecordAction("reject"),
		},
		{
			Method: http.MethodGet, Path: "/api/public/v1/actions", Scope: publicAPIKeySelfScope, ModuleKey: "core",
			Summary:     "List actions",
			Description: "The actions this key can run, each with its module, level and body.",
			Response:    map[string]interface{}{},
			Handler:     handlePublicActions,
		},
		{
			Method: http.MethodPost, Path: "/api/public/v1/actions/{module}/{action}", Scope: publicAPIDynamicScope, ModuleKey: "core",
			Summary:     "Run an action",
			Description: "Runs one listed action as the key owner. Idempotency-Key is required. The body is the action's own fields.",
			Params: []publicAPIParam{
				{Name: "module", In: "path", Required: true, Type: "string", Description: "The module, such as oms."},
				{Name: "action", In: "path", Required: true, Type: "string", Description: "The action, such as order-hold."},
			},
			Response: map[string]interface{}{},
			Handler:  handlePublicAction,
		},
		{
			Method: http.MethodGet, Path: "/api/public/v1/reports", Scope: publicAPIKeySelfScope, ModuleKey: "reports",
			Summary:     "List reports",
			Description: "The reports this key can run, with columns and filters.",
			Response:    map[string]interface{}{},
			Handler:     handlePublicReports,
		},
		{
			Method: http.MethodGet, Path: "/api/public/v1/reports/{id}", Scope: publicAPIDynamicScope, ModuleKey: "reports",
			Summary:     "Run a report",
			Description: "One report as json or csv, a page at a time. Any other query parameter is the report's own filter.",
			Params: []publicAPIParam{
				{Name: "id", In: "path", Required: true, Type: "string", Description: "The report id."},
				{Name: "format", In: "query", Type: "string", Description: "json (default) or csv."},
				{Name: "page", In: "query", Type: "integer", Description: "Page number from 1."},
				{Name: "page_size", In: "query", Type: "integer", Description: "Rows per page, up to 1000. Defaults to 200."},
			},
			Response: map[string]interface{}{},
			Handler:  handlePublicReportRun,
		},
		{
			Method: http.MethodGet, Path: "/api/public/v1/me", Scope: publicAPIKeySelfScope, ModuleKey: "core",
			Summary:     "This key",
			Description: "What this key is: owner, modules, limits left.",
			Response: map[string]interface{}{},
			Handler:  handlePublicMe,
		},
	}
}

// PublicInventoryResponse names the inventory endpoint's envelope so the
// generator has a type to reflect rather than an anonymous map.
type PublicInventoryResponse struct {
	Data  []engines.PublicInventoryLevel `json:"data"`
	Count int                            `json:"count"`
}

func registerPublicAPIV1Routes() {
	for _, route := range publicAPIV1Routes() {
		http.HandleFunc(route.Method+" "+route.Path, publicAPIMiddleware(route.Scope, moduleGate(route.ModuleKey, route.Handler)))
	}
	// Stage 38.2d: where an OAuth client exchanges its secret for a token.
	// Written out (= engines.OAuthTokenEndpointPath) so cmd/surfacescan lists
	// it; oauth_token_test.go reaches it through that constant, so the two
	// cannot drift apart unnoticed.
	http.HandleFunc("POST /api/public/v1/oauth/token", handleOAuthToken)
	// 205.4: a developer link opens once; the link is the proof, so no key yet.
	http.HandleFunc("POST /api/public/v1/claim/{token}", handlePublicClaim)
	// The screens post here with the token from the link (tenant is inside it).
	http.HandleFunc("POST /api/public/v1/developer-links/{token}/claim", handlePublicClaim)
	http.HandleFunc("GET /api/public/v1/docs/{token}", handlePublicShareDocs)
	// Anything else under the public prefix answers a JSON 404 rather than
	// falling through to the static file server, which would hand an API client
	// an HTML page. Go 1.22's mux prefers the more specific patterns above, so
	// this only catches genuinely unregistered paths. Deliberately outside
	// publicAPIMiddleware: an unknown path has no scope to check, and running
	// authentication for it would let a caller probe which paths exist by
	// timing the difference.
	http.HandleFunc(publicAPIPathPrefix, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		writeAPIErrorGeneric(w, r, http.StatusNotFound,
			"No such public API endpoint. See docs/api/public-api-v1.md for the endpoints available in v1.")
	})
}
