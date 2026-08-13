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
			Method: http.MethodGet, Path: "/api/public/v1/items", Scope: "items:read",
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
			Method: http.MethodGet, Path: "/api/public/v1/items/{code}", Scope: "items:read",
			Summary:     "Get one product",
			Description: "Curated product identity for a single item code.",
			Params: []publicAPIParam{
				{Name: "code", In: "path", Required: true, Type: "string", Description: "The item code."},
			},
			Response: engines.PublicItem{},
			Handler:  handlePublicGetItem,
		},
		{
			Method: http.MethodGet, Path: "/api/public/v1/inventory", Scope: "inventory:read",
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
			Method: http.MethodGet, Path: "/api/public/v1/orders/{id}/status", Scope: "orders:read",
			Summary:     "Track an order",
			Description: "Order status with per-line status and any shipments. The path segment accepts either this system's order id or the channel order id the order was imported with.",
			Params: []publicAPIParam{
				{Name: "id", In: "path", Required: true, Type: "string", Description: "Order id or channel order id."},
			},
			Response: engines.PublicOrderStatus{},
			Handler:  handlePublicOrderStatus,
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
		http.HandleFunc(route.Method+" "+route.Path, publicAPIMiddleware(route.Scope, route.Handler))
	}
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
			"No such public API endpoint. See docs/specs/public_api_v1.md for the endpoints available in v1.")
	})
}
