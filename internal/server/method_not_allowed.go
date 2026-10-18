package server

import (
	"net/http"
	"strings"
)

// apiMethodNotAllowed (73.C.8) answers an /api/ request whose address exists
// but not for its method with the API's error envelope (GLOBAL-0303) and an
// Allow header, instead of the router's plain-text "405 method not allowed",
// which no screen can read. It asks the router before serving - only a request
// that matches nothing is probed with the other methods - so no response
// writer is wrapped and streams are untouched. The static file server sits
// on the method-less "/" (routes.go), so an /api/ address with no route for
// a method lands there; that catch-all counts as no route.
func apiMethodNotAllowed(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if _, pattern := mux.Handler(r); pattern == "" || pattern == "/" {
				if allowed := otherMethodsForPath(mux, r); len(allowed) > 0 {
					w.Header().Set("Allow", strings.Join(allowed, ", "))
					writeAPIErrorDetail(w, r, "GLOBAL-0303", "", "This address does not accept "+safeMethodName(r.Method)+" requests.")
					return
				}
			}
		}
		mux.ServeHTTP(w, r)
	})
}

// otherMethodsForPath lists the methods the router does serve at r's path.
func otherMethodsForPath(mux *http.ServeMux, r *http.Request) []string {
	var allowed []string
	for _, m := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		if m == r.Method {
			continue
		}
		probe := r.Clone(r.Context())
		probe.Method = m
		if _, pattern := mux.Handler(probe); pattern != "" && pattern != "/" {
			allowed = append(allowed, m)
		}
	}
	return allowed
}
