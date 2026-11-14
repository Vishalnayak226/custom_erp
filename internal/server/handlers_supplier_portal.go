package server

import (
	"custom_erp/engines"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// Stage 168.4: the supplier portal. Every handler refuses extension tokens,
// accepts only the Supplier role and speaks for the one vendor the login is
// linked to. A record of another vendor answers exactly like a missing one.
func supplierPortalVendor(w http.ResponseWriter, r *http.Request) (string, string, *engines.SupplierVendor, bool) {
	if refuseExtensionToken(w, r) {
		return "", "", nil, false
	}
	tenantID, role := r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Role")
	userID := r.Header.Get("Resolved-User-ID")
	if !engines.IsSupplierRole(role) {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return "", "", nil, false
	}
	v, err := engines.ResolveSupplierVendor(tenantID, userID)
	if err != nil {
		if errors.Is(err, engines.ErrSupplierNotLinked) {
			writeAPIErrorGeneric(w, r, http.StatusForbidden, err.Error())
		} else {
			writeEngineError(w, r, err, http.StatusInternalServerError)
		}
		return "", "", nil, false
	}
	return tenantID, userID, v, true
}

func supplierPortalReply(w http.ResponseWriter, r *http.Request, out interface{}, err error) {
	if errors.Is(err, engines.ErrSupplierNotFound) {
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "Not found")
		return
	}
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(out)
}

func supplierPortalList(w http.ResponseWriter, r *http.Request,
	list func(string, *engines.SupplierVendor) ([]map[string]interface{}, error),
	one func(string, *engines.SupplierVendor, string) (map[string]interface{}, error)) {
	tenantID, _, v, ok := supplierPortalVendor(w, r)
	if !ok {
		return
	}
	if id := strings.TrimSpace(r.URL.Query().Get("id")); id != "" && one != nil {
		out, err := one(tenantID, v, id)
		supplierPortalReply(w, r, out, err)
		return
	}
	out, err := list(tenantID, v)
	supplierPortalReply(w, r, map[string]interface{}{"vendor": v.Name, "rows": out}, err)
}

func handleSupplierPortalOrders(w http.ResponseWriter, r *http.Request) {
	supplierPortalList(w, r, engines.SupplierOrders, engines.SupplierOrder)
}

func handleSupplierPortalASNs(w http.ResponseWriter, r *http.Request) {
	supplierPortalList(w, r, engines.SupplierASNs, engines.SupplierASN)
}

func handleSupplierPortalBills(w http.ResponseWriter, r *http.Request) {
	supplierPortalList(w, r, engines.SupplierBills, engines.SupplierBill)
}

func handleSupplierPortalPayments(w http.ResponseWriter, r *http.Request) {
	supplierPortalList(w, r, engines.SupplierPayments, nil)
}

func handleSupplierPortalSendASN(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, v, ok := supplierPortalVendor(w, r)
	if !ok {
		return
	}
	var in engines.SupplierASNInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Send the order and its lines")
		return
	}
	out, err := engines.SendSupplierASN(tenantID, userID, v, in)
	if errors.Is(err, engines.ErrSupplierNotFound) {
		supplierPortalReply(w, r, nil, err)
		return
	}
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(out)
}
