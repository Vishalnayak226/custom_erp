package main

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Fixed scratch master data. Setup is idempotent: it creates what is missing
// and leaves what is there, so a second run reuses the same tenant.
const (
	skuMain   = "JL-I1"
	locStore  = "JL-S1"
	locWH     = "JL-W1"
	vendorID  = "JL-V1"
	userPass  = "JlScratch#2026pw"
	userPass2 = "JlScratch#2026pw2"
)

type roleUser struct{ name, role, loc string }

var roleUsers = []roleUser{
	{"jl-ap", "Accounts Payable", locWH},
	{"jl-sm", "Store Manager", locWH},
	{"jl-sms", "Store Manager", locStore},
	{"jl-cash", "Cashier", locStore},
	{"jl-acct", "Accountant", locStore},
	{"jl-wh", "Warehouse Manager", locWH},
}

func (r *Runner) quiet(who, method, path string, body interface{}, expect ...int) *Resp {
	resp, err := r.Cl.Do(who, method, path, body, CallOpts{Setup: true, NoRecord: true, Expect: expect})
	if err != nil {
		return &Resp{}
	}
	return resp
}

func (r *Runner) exists(doctype, field, id string) bool {
	resp := r.quiet("admin", "GET", fmt.Sprintf("/api/v1/doc/%s?%s=%s&limit=1", doctype, field, url.QueryEscape(id)), nil)
	return resp.Status == 200 && len(resp.Arr) > 0
}

func (r *Runner) ensureDoc(doctype, field, id string, body map[string]interface{}) error {
	if r.exists(doctype, field, id) {
		return nil
	}
	resp := r.quiet("admin", "POST", "/api/v1/doc/"+doctype, body)
	if resp.Status >= 300 {
		return fmt.Errorf("setup %s %s: %d %s", doctype, id, resp.Status, trunc(string(resp.Body), 200))
	}
	return nil
}

// gstinWithCheck completes a 14-character base with its checksum character.
func gstinWithCheck(base string) string {
	const chars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	sum := 0
	for i, c := range base {
		v := strings.IndexRune(chars, c)
		f := 1
		if i%2 == 1 {
			f = 2
		}
		p := v * f
		sum += p/36 + p%36
	}
	return base + string(chars[(36-sum%36)%36])
}

// Setup creates the scratch master data and signs the role users in.
func (r *Runner) Setup() error {
	steps := []struct {
		dt, field, id string
		body          map[string]interface{}
	}{
		{"Department", "code", "JL-OPS", map[string]interface{}{"code": "JL-OPS", "name": "JL Operations", "status": "Active"}},
		{"LegalEntity", "code", "JLLE", map[string]interface{}{"code": "JLLE", "name": "JL Legal Entity", "gstin": gstinWithCheck("29AABCA0001A1Z"), "status": "Active", "state": "Karnataka", "city": "Bengaluru", "pincode": "560001"}},
		{"Location", "code", locStore, map[string]interface{}{"code": locStore, "name": "JL Store 1", "type": "Store", "legal_entity": "JLLE", "status": "Active", "sellable": "Yes", "state": "Karnataka", "city": "Bengaluru", "pincode": "560001"}},
		{"Location", "code", locWH, map[string]interface{}{"code": locWH, "name": "JL Warehouse 1", "type": "Warehouse", "legal_entity": "JLLE", "status": "Active", "sellable": "Yes", "state": "Karnataka", "city": "Bengaluru", "pincode": "560001"}},
		{"Item", "code", skuMain, map[string]interface{}{"code": skuMain, "name": "JL item 1", "hsn_code": "6109", "gst_rate": 5, "tax_treatment": "Taxable", "sale_price": 500, "mrp": 500, "standard_cost": 200}},
		{"Vendor", "code", vendorID, map[string]interface{}{"code": vendorID, "name": "JL Vendor 1", "gstin": gstinWithCheck("29AABCV0001A1Z"), "state": "Karnataka", "status": "Active"}},
		{"Zone", "code", "JL-ZA", map[string]interface{}{"code": "JL-ZA", "location": locWH, "zone_type": "PickFace", "pick_sequence": 1, "putaway_sequence": 1, "status": "Active"}},
		{"Bin", "bin_code", "JL-R1", map[string]interface{}{"bin_code": "JL-R1", "location": locWH, "zone": "JL-ZA", "aisle": "01", "rack": "R1", "status": "Active"}},
		{"CourierServiceArea", "courier", "JLC", map[string]interface{}{"courier": "JLC", "pincode_prefix": "", "priority": 1, "status": "Active"}},
	}
	r.quiet("admin", "POST", "/api/v1/admin/role-templates/apply", map[string]interface{}{"roles": []string{"Warehouse Manager", "Accounts Payable", "Accounts Receivable", "Picker"}})
	for _, s := range steps {
		if err := r.ensureDoc(s.dt, s.field, s.id, s.body); err != nil {
			return err
		}
	}
	for _, u := range roleUsers {
		if err := r.ensureUser(u); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) ensureUser(u roleUser) error {
	s := &Session{Name: u.name, Role: u.role, Username: u.name, Password: userPass2}
	r.Cl.Sess[u.name] = s
	if err := r.Cl.Login(s); err == nil {
		return nil
	}
	resp := r.quiet("admin", "POST", "/api/v1/admin/users", map[string]string{"username": u.name, "password": userPass, "email": u.name + "@example.invalid", "role": u.role, "location_code": u.loc, "department": "JL-OPS"})
	if resp.Status >= 300 && !strings.Contains(string(resp.Body), "exist") && !strings.Contains(string(resp.Body), "already taken") {
		return fmt.Errorf("setup user %s: %d %s", u.name, resp.Status, trunc(string(resp.Body), 200))
	}
	first := &Session{Name: "__first", Username: u.name, Password: userPass}
	r.Cl.Sess["__first"] = first
	if err := r.Cl.Login(first); err != nil {
		return err
	}
	cp := r.quiet("__first", "POST", "/api/v1/me/change-password", map[string]string{"current_password": userPass, "new_password": userPass2})
	delete(r.Cl.Sess, "__first")
	if cp.Status != 200 {
		return fmt.Errorf("setup user %s change-password: %d %s", u.name, cp.Status, trunc(string(cp.Body), 200))
	}
	time.Sleep(300 * time.Millisecond)
	return r.Cl.Login(s)
}
