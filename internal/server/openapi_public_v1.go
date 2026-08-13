package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"time"
)

// Stage 38.8 - OpenAPI generated from the route table, not written by hand.
//
// The generator reads publicAPIV1Routes() and reflects over each route's
// declared response type. That is the whole point: a hand-written spec is
// correct exactly once, and every later route or renamed field silently makes
// it a lie. Here, renaming a json tag changes the published contract in the
// same commit, and adding a route to the table adds it to the spec.
//
// OpenAPI 3.0.3 rather than 3.1: 3.0 is what every current client generator and
// documentation renderer accepts without argument, and this spec uses nothing
// 3.1 would express better.
//
// Stdlib only - encoding/json plus reflect. No spec library is vendored, in
// keeping with the repo's standing no-new-dependency rule.

const openAPIVersion = "3.0.3"

type openAPIGenerator struct {
	components map[string]interface{}
}

// schemaFor produces a JSON Schema for one Go type. v may be an invalid
// reflect.Value; it is consulted only to resolve interface-typed fields, where
// the declared type says nothing and the example value says everything (the
// paged envelope's Data field is exactly this case).
func (g *openAPIGenerator) schemaFor(t reflect.Type, v reflect.Value) map[string]interface{} {
	if t == reflect.TypeOf(time.Time{}) {
		return map[string]interface{}{"type": "string", "format": "date-time"}
	}
	switch t.Kind() {
	case reflect.Ptr:
		schema := g.schemaFor(t.Elem(), reflect.Value{})
		schema["nullable"] = true
		return schema
	case reflect.Interface:
		if v.IsValid() && !v.IsNil() {
			return g.schemaFor(v.Elem().Type(), v.Elem())
		}
		// A genuinely open field. Saying so is more honest than inventing a
		// shape the endpoint does not promise.
		return map[string]interface{}{"description": "Endpoint-specific payload."}
	case reflect.Struct:
		return g.structSchema(t, v)
	case reflect.Slice, reflect.Array:
		var elem reflect.Value
		if v.IsValid() && v.Len() > 0 {
			elem = v.Index(0)
		}
		return map[string]interface{}{"type": "array", "items": g.schemaFor(t.Elem(), elem)}
	case reflect.Map:
		return map[string]interface{}{"type": "object", "additionalProperties": g.schemaFor(t.Elem(), reflect.Value{})}
	case reflect.String:
		return map[string]interface{}{"type": "string"}
	case reflect.Bool:
		return map[string]interface{}{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]interface{}{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]interface{}{"type": "number"}
	}
	return map[string]interface{}{}
}

// structSchema registers a named struct once under components/schemas and
// returns a $ref to it, so a type used by three endpoints is documented once
// rather than pasted three times.
func (g *openAPIGenerator) structSchema(t reflect.Type, v reflect.Value) map[string]interface{} {
	name := t.Name()
	// A type whose shape depends on the example value - the paged envelope's
	// interface-typed Data field is the case that matters - must NOT be cached
	// under its Go type name. Two endpoints returning PublicPage carry
	// different Data, and a single named component could only describe one of
	// them, silently mis-documenting the other. Those are inlined per endpoint.
	anonymous := name == "" || typeHasDynamicField(t, map[reflect.Type]bool{})
	if !anonymous {
		if _, exists := g.components[name]; exists {
			return map[string]interface{}{"$ref": "#/components/schemas/" + name}
		}
		// Reserved before recursing so a self-referential type cannot loop.
		g.components[name] = map[string]interface{}{"type": "object"}
	}

	properties := map[string]interface{}{}
	required := []string{}
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" {
			continue // unexported
		}
		tag := field.Tag.Get("json")
		if tag == "-" {
			continue
		}
		parts := strings.Split(tag, ",")
		fieldName := parts[0]
		if fieldName == "" {
			fieldName = field.Name
		}
		optional := false
		for _, option := range parts[1:] {
			if option == "omitempty" {
				optional = true
			}
		}
		var fieldValue reflect.Value
		if v.IsValid() && v.Kind() == reflect.Struct {
			fieldValue = v.Field(i)
		}
		properties[fieldName] = g.schemaFor(field.Type, fieldValue)
		if !optional {
			required = append(required, fieldName)
		}
	}
	sort.Strings(required)
	schema := map[string]interface{}{"type": "object", "properties": properties}
	if len(required) > 0 {
		schema["required"] = required
	}
	if anonymous {
		return schema
	}
	g.components[name] = schema
	return map[string]interface{}{"$ref": "#/components/schemas/" + name}
}

// typeHasDynamicField reports whether any field, at any depth, is an interface
// whose schema can only come from an example value.
func typeHasDynamicField(t reflect.Type, seen map[reflect.Type]bool) bool {
	for t.Kind() == reflect.Ptr || t.Kind() == reflect.Slice || t.Kind() == reflect.Array || t.Kind() == reflect.Map {
		t = t.Elem()
	}
	if t.Kind() == reflect.Interface {
		return true
	}
	if t.Kind() != reflect.Struct || seen[t] {
		return false
	}
	seen[t] = true
	for i := 0; i < t.NumField(); i++ {
		if t.Field(i).PkgPath != "" {
			continue
		}
		if typeHasDynamicField(t.Field(i).Type, seen) {
			return true
		}
	}
	return false
}

// PublicAPIOpenAPISpec renders the whole public surface as an OpenAPI document.
// Exported so cmd/gendocs can write it to disk alongside the other generated
// references, and so the admin endpoint can serve the same bytes.
func PublicAPIOpenAPISpec() ([]byte, error) {
	g := &openAPIGenerator{components: map[string]interface{}{}}

	// The error envelope every failure shares, described once.
	g.components["Error"] = map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"error":          map[string]interface{}{"type": "string", "description": "Human-readable message."},
			"code":           map[string]interface{}{"type": "string", "description": "Stable platform error code, for example GLOBAL-0004."},
			"correlation_id": map[string]interface{}{"type": "string", "description": "Echoes the X-Correlation-ID response header. Quote it in a support request."},
			"retryable":      map[string]interface{}{"type": "boolean", "description": "Whether retrying the identical request could succeed."},
		},
		"required": []string{"error", "code"},
	}
	errorResponse := func(description string) map[string]interface{} {
		return map[string]interface{}{
			"description": description,
			"content": map[string]interface{}{
				"application/json": map[string]interface{}{
					"schema": map[string]interface{}{"$ref": "#/components/schemas/Error"},
				},
			},
		}
	}

	paths := map[string]interface{}{}
	for _, route := range publicAPIV1Routes() {
		parameters := []interface{}{}
		for _, param := range route.Params {
			entry := map[string]interface{}{
				"name": param.Name, "in": param.In, "required": param.Required || param.In == "path",
				"description": param.Description,
				"schema":      map[string]interface{}{"type": param.Type},
			}
			parameters = append(parameters, entry)
		}
		parameters = append(parameters, map[string]interface{}{"$ref": "#/components/parameters/TenantHeader"})

		operation := map[string]interface{}{
			"summary":     route.Summary,
			"description": route.Description + "\n\nRequired scope: `" + route.Scope + "`.",
			"operationId": openAPIOperationID(route.Method, route.Path),
			"tags":        []string{openAPITagFor(route.Path)},
			"parameters":  parameters,
			"responses": map[string]interface{}{
				"200": map[string]interface{}{
					"description": "Success.",
					"content": map[string]interface{}{
						"application/json": map[string]interface{}{
							"schema": g.schemaFor(reflect.TypeOf(route.Response), reflect.ValueOf(route.Response)),
						},
					},
				},
				"401": errorResponse("The API credential is missing, malformed, revoked or expired. The response never says which."),
				"403": errorResponse("The credential authenticated but does not hold the scope this endpoint requires."),
				"404": errorResponse("No such resource."),
				"422": errorResponse("A parameter was present but not valid."),
				"429": errorResponse("Per-minute rate limit or daily quota exhausted. Retry-After says how long to wait."),
			},
		}
		if _, exists := paths[route.Path]; !exists {
			paths[route.Path] = map[string]interface{}{}
		}
		paths[route.Path].(map[string]interface{})[strings.ToLower(route.Method)] = operation
	}

	spec := map[string]interface{}{
		"openapi": openAPIVersion,
		"info": map[string]interface{}{
			"title":   "ERP Public API",
			"version": "1.0.0",
			"description": "The curated public integration surface. Only the endpoints listed here are reachable with an API " +
				"credential; the application's internal API is not part of this contract.\n\n" +
				"Every request carries `Authorization: Bearer <api key>` and `X-Tenant-ID`. Mutating endpoints additionally " +
				"require an `Idempotency-Key` header, so a retry after a timeout cannot be mistaken for a second request.\n\n" +
				"Responses carry `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-Quota-Limit` and `X-Quota-Remaining` so a " +
				"client can pace itself without waiting to be rejected.\n\n" +
				"Compatibility rules for v1 are in docs/specs/public_api_v1.md.",
		},
		"servers": []interface{}{
			map[string]interface{}{"url": "/", "description": "This deployment."},
		},
		"security": []interface{}{
			map[string]interface{}{"ApiKeyAuth": []string{}},
		},
		"tags": []interface{}{
			map[string]interface{}{"name": "Items", "description": "Curated product identity."},
			map[string]interface{}{"name": "Inventory", "description": "Availability reads."},
			map[string]interface{}{"name": "Orders", "description": "Order tracking."},
		},
		"paths": paths,
		"components": map[string]interface{}{
			"schemas": g.components,
			"parameters": map[string]interface{}{
				"TenantHeader": map[string]interface{}{
					"name": "X-Tenant-ID", "in": "header", "required": true,
					"description": "The tenant the credential belongs to.",
					"schema":      map[string]interface{}{"type": "string"},
				},
			},
			"securitySchemes": map[string]interface{}{
				"ApiKeyAuth": map[string]interface{}{
					"type": "http", "scheme": "bearer",
					"description": "An opaque `erp_v1_...` API credential issued by a Super Admin. Never a user session token.",
				},
			},
		},
	}
	return json.MarshalIndent(spec, "", "  ")
}

func openAPITagFor(path string) string {
	trimmed := strings.TrimPrefix(path, publicAPIPathPrefix)
	segment := strings.SplitN(trimmed, "/", 2)[0]
	if segment == "" {
		return "General"
	}
	return strings.ToUpper(segment[:1]) + segment[1:]
}

// openAPIOperationID builds a stable, readable id from the method and path -
// stable because client generators name their generated methods after it, so a
// churning id is a breaking change for every generated client.
func openAPIOperationID(method, path string) string {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(path, publicAPIPathPrefix), "/"), "/")
	id := strings.ToLower(method)
	for _, part := range parts {
		if part == "" {
			continue
		}
		part = strings.Trim(part, "{}")
		id += strings.ToUpper(part[:1]) + part[1:]
	}
	return id
}

// handlePublicAPIOpenAPISpec serves the generated document to a Super Admin
// session. It stays on the private admin surface: an integrator receives the
// spec from their onboarding docs, and an unauthenticated endpoint that
// enumerates every route is a gift to anyone probing the deployment.
func handlePublicAPIOpenAPISpec(w http.ResponseWriter, r *http.Request) {
	if !requireHRAdmin(w, r, r.Header.Get("Resolved-Role")) {
		return
	}
	spec, err := PublicAPIOpenAPISpec()
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, fmt.Sprintf("Could not render the OpenAPI document: %v", err))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(spec)
}
