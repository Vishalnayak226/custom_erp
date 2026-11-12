package engines

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"
	"unicode"

	"custom_erp/db"
)

// Stage 148.18: the vendor's signed link to one purchase order (decision D6: a
// signed link, no vendor accounts). Off by default (procurement.vendor_link).
//
// The link is stateless-signed: client + link id + PO + expiry under the same
// key ring the session tokens use, so a reminder can rebuild it and nothing
// secret is stored. A row per link holds the PO, the expiry and the revoke.
// Through it the vendor sees only that PO's lines, proposes a quantity / date
// per line, and uploads an invoice file. A proposal waits for the buyer (or, for
// a vendor marked trusted, applies at once); an invoice waits for review and
// never posts.

const SettingVendorLink = "procurement.vendor_link"

const (
	vendorLinkDays        = 14
	vendorReminderGapDays = 7
	vendorFileMaxBytes    = 2 << 20
	vendorFilesPerLink    = 5
	vendorProposalMaxRows = 100
	vendorPendingCap      = 200
)

// VendorLinkOn reports whether the vendor link is switched on for a client.
func VendorLinkOn(tenantID string) bool { return GetSettingBool(tenantID, SettingVendorLink) }

func vlErr(code, format string, a ...interface{}) error {
	return &ValidationError{Code: code, Message: fmt.Sprintf(format, a...)}
}

func vlBadLink() error {
	return vlErr("PURCHA-5061", "this link is not valid or has expired")
}

// vlPOOpen: a PO the vendor can still talk about.
func vlPOOpen(status string) bool { return status == "Approved" || status == "Pending Approval" }

func vlFmtTime(t time.Time) string {
	return t.In(BusinessLocation()).Format("2 Jan 2006 15:04")
}

// --- the signed token ----------------------------------------------------------

type vlLink struct {
	id, poID, vendorID string
	expires            time.Time
}

func vlSign(secret []byte, tenantID string, l vlLink) string {
	m := hmac.New(sha256.New, secret)
	fmt.Fprintf(m, "vpl1|%s|%s|%s|%d", tenantID, l.id, l.poID, l.expires.Unix())
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func vlToken(tenantID string, l vlLink) string {
	return l.id + "." + vlSign(jwtSigningKey.secret, tenantID, l)
}

func vlSigOK(tenantID string, l vlLink, sig string) bool {
	for _, k := range jwtKeys {
		if len(k.secret) == 0 {
			continue
		}
		if hmac.Equal([]byte(vlSign(k.secret, tenantID, l)), []byte(sig)) {
			return true
		}
	}
	return false
}

// vlResolve finds the live link a token stands for. Every refusal is the same
// error: nothing says whether the link existed, expired or was revoked.
func vlResolve(tenantID, token string) (string, *vlLink, error) {
	if !VendorLinkOn(tenantID) {
		return "", nil, vlBadLink()
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", nil, vlBadLink()
	}
	id, sig, ok := strings.Cut(strings.TrimSpace(token), ".")
	if !ok || len(id) != 16 || len(sig) < 20 || len(sig) > 64 {
		return "", nil, vlBadLink()
	}
	if _, err := hex.DecodeString(id); err != nil {
		return "", nil, vlBadLink()
	}
	l := vlLink{id: id}
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT po_id, vendor_id, expires_at FROM %s.vendor_po_links
		WHERE id = $1 AND revoked_at IS NULL AND expires_at > now()`, schema), id).Scan(&l.poID, &l.vendorID, &l.expires)
	if err != nil || !vlSigOK(tenantID, l, sig) {
		return "", nil, vlBadLink()
	}
	return schema, &l, nil
}

// --- the PO behind a link ------------------------------------------------------

func vlLoadPO(schema, poID string) (map[string]interface{}, string, error) {
	var raw, status string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents
		WHERE doctype = 'PurchaseOrder' AND id = $1 AND deleted_at IS NULL`, schema), poID).Scan(&raw, &status); err != nil {
		return nil, "", vlErr("PURCHA-5062", "that purchase order is not open")
	}
	var d map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return nil, "", vlErr("PURCHA-5062", "that purchase order is not open")
	}
	return d, status, nil
}

func vlVendorID(po map[string]interface{}) string {
	return firstNonEmpty(strField(po, "vendor"), strField(po, "vendor_id"))
}

func vlVendor(tenantID, vendorID string) map[string]interface{} {
	if vendorID == "" {
		return map[string]interface{}{}
	}
	d, _, err := fetchDocData(tenantID, "Vendor", vendorID)
	if err != nil || d == nil {
		return map[string]interface{}{}
	}
	return d
}

func vlLineUnit(tenantID string, l map[string]interface{}) string {
	if u := firstNonEmpty(strField(l, "uom"), strField(l, "unit")); u != "" {
		return u
	}
	if it, err := ResolveItemBySKU(tenantID, strField(l, "sku")); err == nil {
		if u := firstNonEmpty(strField(it.Data, "uom"), strField(it.Data, "unit")); u != "" {
			return u
		}
	}
	return "Nos"
}

func vlLineDue(l map[string]interface{}, po map[string]interface{}) string {
	var inst []POInstalment
	if raw, ok := l["instalments"]; ok {
		b, _ := json.Marshal(raw)
		_ = json.Unmarshal(b, &inst)
	}
	sort.SliceStable(inst, func(a, b int) bool { return inst[a].Date < inst[b].Date })
	if len(inst) > 0 {
		return inst[0].Date
	}
	return strField(po, "expected_delivery_date")
}

func vlNewID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// --- making and revoking a link ------------------------------------------------

type VendorLinkInfo struct {
	Token   string `json:"token"`
	Path    string `json:"path"`
	URL     string `json:"url,omitempty"`
	Expires string `json:"expires"`
}

func vlInfo(tenantID, schema string, l vlLink) *VendorLinkInfo {
	tok := vlToken(tenantID, l)
	p := "/p/vendor.html?t=" + tok
	if tenantID != "" && tenantID != "default" {
		p += "&tenant_id=" + url.QueryEscape(tenantID)
	}
	return &VendorLinkInfo{Token: tok, Path: p, URL: crmPublicLink(schema, "/p/vendor.html", tok),
		Expires: l.expires.In(BusinessLocation()).Format("2 Jan 2006")}
}

// CreateVendorPOLink makes a fresh 14-day link for an approved PO and revokes
// the one it replaces.
func CreateVendorPOLink(tenantID, userID, poID string) (*VendorLinkInfo, error) {
	if !VendorLinkOn(tenantID) {
		return nil, vlErr("PURCHA-5060", "the vendor link is not switched on")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	po, status, err := vlLoadPO(schema, poID)
	if err != nil || status != "Approved" {
		return nil, vlErr("PURCHA-5062", "only an approved purchase order can be shared with the vendor")
	}
	l, err := vlInsertLink(schema, userID, poID, vlVendorID(po))
	if err != nil {
		return nil, err
	}
	LogAuditEvent(tenantID, userID, "VENDOR_LINK_CREATED", "SUCCESS", "Vendor link made for purchase order "+firstNonEmpty(strField(po, "code"), poID))
	return vlInfo(tenantID, schema, *l), nil
}

func vlInsertLink(schema, userID, poID, vendorID string) (*vlLink, error) {
	id, err := vlNewID()
	if err != nil {
		return nil, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.vendor_po_links SET revoked_at = now(), revoked_by = $2
		WHERE po_id = $1 AND revoked_at IS NULL`, schema), poID, userID); err != nil {
		return nil, err
	}
	l := &vlLink{id: id, poID: poID, vendorID: vendorID}
	if err := tx.QueryRow(fmt.Sprintf(`INSERT INTO %s.vendor_po_links (id, po_id, vendor_id, created_by, expires_at)
		VALUES ($1, $2, $3, $4, now() + make_interval(days => %d)) RETURNING expires_at`, schema, vendorLinkDays),
		id, poID, vendorID, userID).Scan(&l.expires); err != nil {
		return nil, err
	}
	return l, tx.Commit()
}

// RevokeVendorPOLink ends the PO's live link at once.
func RevokeVendorPOLink(tenantID, userID, poID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	res, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.vendor_po_links SET revoked_at = now(), revoked_by = $2
		WHERE po_id = $1 AND revoked_at IS NULL AND expires_at > now()`, schema), poID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return vlErr("PURCHA-5068", "this purchase order has no live link")
	}
	LogAuditEvent(tenantID, userID, "VENDOR_LINK_REVOKED", "SUCCESS", "Vendor link revoked for purchase order "+poID)
	return nil
}

// --- what the vendor sees ------------------------------------------------------

type VendorLinkLine struct {
	No   int     `json:"no"`
	Sku  string  `json:"sku"`
	Item string  `json:"item"`
	Qty  float64 `json:"qty"`
	Unit string  `json:"unit"`
	Due  string  `json:"due"`
}

type VendorLinkView struct {
	Vendor  string           `json:"vendor"`
	PO      string           `json:"po"`
	Expires string           `json:"expires"`
	Lines   []VendorLinkLine `json:"lines"`
	Waiting int              `json:"waiting"`
	Files   int              `json:"files"`
}

// VendorLinkPublicView is the whole page's data. It names the vendor, the PO
// number and the lines - no ids, prices, totals or other orders.
func VendorLinkPublicView(tenantID, token string) (*VendorLinkView, error) {
	schema, l, err := vlResolve(tenantID, token)
	if err != nil {
		return nil, err
	}
	po, status, err := vlLoadPO(schema, l.poID)
	if err != nil || !vlPOOpen(status) {
		return nil, vlErr("PURCHA-5062", "that purchase order is not open")
	}
	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.vendor_po_links SET last_opened_at = now(), open_count = open_count + 1 WHERE id = $1`, schema), l.id)
	lines, err := parsePOLineMaps(strField(po, "items"))
	if err != nil {
		return nil, vlErr("PURCHA-5062", "that purchase order is not open")
	}
	v := &VendorLinkView{Vendor: strField(vlVendor(tenantID, l.vendorID), "name"),
		PO: firstNonEmpty(strField(po, "po_number"), strField(po, "code"), l.poID), Lines: []VendorLinkLine{},
		Expires: l.expires.In(BusinessLocation()).Format("2 Jan 2006")}
	names := map[string]string{}
	for i, ln := range lines {
		sku := strField(ln, "sku")
		if _, ok := names[sku]; !ok {
			names[sku] = ""
			if it, err := ResolveItemBySKU(tenantID, sku); err == nil {
				names[sku] = strField(it.Data, "name")
			}
		}
		v.Lines = append(v.Lines, VendorLinkLine{No: i + 1, Sku: sku, Item: names[sku], Qty: poLineDueQty(ln),
			Unit: vlLineUnit(tenantID, ln), Due: vlLineDue(ln, po)})
	}
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s.vendor_po_proposals WHERE link_id = $1 AND status = 'Pending'`, schema), l.id).Scan(&v.Waiting)
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s.vendor_po_files WHERE link_id = $1`, schema), l.id).Scan(&v.Files)
	return v, nil
}

// --- proposals -----------------------------------------------------------------

type VendorProposalLine struct {
	LineNo int      `json:"line"`
	Qty    *float64 `json:"qty,omitempty"`
	Date   string   `json:"date,omitempty"`
	Unit   string   `json:"unit,omitempty"`
	Note   string   `json:"note,omitempty"`
}

type VendorProposalResult struct {
	Pending int `json:"pending"`
	Applied int `json:"applied"`
}

func vlClean(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, strings.TrimSpace(s))
	if r := []rune(s); len(r) > max {
		s = string(r[:max])
	}
	return strings.TrimSpace(s)
}

func vlValidProposal(lines []map[string]interface{}, in []VendorProposalLine) error {
	if len(in) == 0 || len(in) > vendorProposalMaxRows {
		return vlErr("PURCHA-5063", "send at least one line to change")
	}
	today := BusinessToday()
	last := time.Now().In(BusinessLocation()).AddDate(3, 0, 0).Format("2006-01-02")
	for _, p := range in {
		if p.LineNo < 1 || p.LineNo > len(lines) {
			return vlErr("PURCHA-5063", "there is no line %d on this order", p.LineNo)
		}
		if p.Qty == nil && strings.TrimSpace(p.Date) == "" {
			return vlErr("PURCHA-5063", "line %d: give a quantity or a date", p.LineNo)
		}
		if p.Qty != nil && (math.IsNaN(*p.Qty) || *p.Qty <= 0 || *p.Qty > 1e7) {
			return vlErr("PURCHA-5063", "line %d: the quantity must be above zero", p.LineNo)
		}
		if d := strings.TrimSpace(p.Date); d != "" {
			if _, err := time.Parse("2006-01-02", d); err != nil {
				return vlErr("PURCHA-5063", "line %d: the date must be YYYY-MM-DD", p.LineNo)
			}
			if d < today {
				return vlErr("PURCHA-5063", "line %d: the date cannot be in the past", p.LineNo)
			}
			if d > last {
				return vlErr("PURCHA-5063", "line %d: that date is too far away", p.LineNo)
			}
		}
	}
	return nil
}

type vlProposal struct {
	id         int64
	linkID     string
	poID       string
	lineNo     int
	qty        *float64
	date, unit string
	note       string
}

// SubmitVendorProposal records what the vendor proposes. Nothing on the order
// moves until the buyer applies it - except for a vendor marked trusted, whose
// proposal in a known unit applies at once.
func SubmitVendorProposal(tenantID, token string, in []VendorProposalLine) (*VendorProposalResult, error) {
	schema, l, err := vlResolve(tenantID, token)
	if err != nil {
		return nil, err
	}
	po, status, err := vlLoadPO(schema, l.poID)
	if err != nil || !vlPOOpen(status) {
		return nil, vlErr("PURCHA-5062", "that purchase order is not open")
	}
	lines, err := parsePOLineMaps(strField(po, "items"))
	if err != nil {
		return nil, vlErr("PURCHA-5062", "that purchase order is not open")
	}
	if err := vlValidProposal(lines, in); err != nil {
		return nil, err
	}
	var pend int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s.vendor_po_proposals WHERE link_id = $1 AND status = 'Pending'`, schema), l.id).Scan(&pend)
	if pend+len(in) > vendorPendingCap {
		return nil, vlErr("PURCHA-5063", "too many changes are waiting - the buyer has to deal with them first")
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var made []vlProposal
	for _, p := range in {
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.vendor_po_proposals SET status = 'Replaced', decided_at = now()
			WHERE link_id = $1 AND line_no = $2 AND status = 'Pending'`, schema), l.id, p.LineNo); err != nil {
			return nil, err
		}
		q := vlProposal{linkID: l.id, poID: l.poID, lineNo: p.LineNo, qty: p.Qty, date: strings.TrimSpace(p.Date),
			unit: vlClean(p.Unit, 40), note: vlClean(p.Note, 200)}
		if q.qty != nil {
			r := math.Round(*q.qty*1000) / 1000
			q.qty = &r
		}
		if err := tx.QueryRow(fmt.Sprintf(`INSERT INTO %s.vendor_po_proposals (link_id, po_id, line_no, qty, promised_date, vendor_unit, note)
			VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`, schema), q.linkID, q.poID, q.lineNo, q.qty, q.date, q.unit, q.note).Scan(&q.id); err != nil {
			return nil, err
		}
		made = append(made, q)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	LogAuditEvent(tenantID, "vendor-link", "VENDOR_LINK_PROPOSAL", "SUCCESS",
		fmt.Sprintf("Vendor proposed changes to %d line(s) of purchase order %s", len(made), firstNonEmpty(strField(po, "code"), l.poID)))
	res := &VendorProposalResult{}
	trusted := strings.EqualFold(strField(vlVendor(tenantID, l.vendorID), "vendor_link_trusted"), "Yes")
	for _, q := range made {
		// Trust covers a date or a cut. More units than the line holds always wait for the buyer.
		raises := q.qty != nil && *q.qty > numFromInterface(lines[q.lineNo-1]["qty"])
		if trusted && !raises && (q.unit == "" || vlUnitKnown(schema, l.vendorID, q.unit, vlLineUnit(tenantID, lines[q.lineNo-1]))) {
			if err := vlApply(tenantID, schema, "vendor link (trusted)", "System", q, true, false); err == nil {
				res.Applied++
				continue
			}
		}
		res.Pending++
	}
	return res, nil
}

// --- unit names ----------------------------------------------------------------

func vlUnitKey(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func vlUnitKnown(schema, vendorID, vendorUnit, lineUnit string) bool {
	if vlUnitKey(vendorUnit) == "" || vlUnitKey(vendorUnit) == vlUnitKey(lineUnit) {
		return true
	}
	var n int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s.vendor_unit_names WHERE vendor_id = $1 AND vendor_unit = $2`, schema),
		vendorID, vlUnitKey(vendorUnit)).Scan(&n)
	return n > 0
}

func vlRememberUnit(schema, userID, vendorID, vendorUnit, itemUnit string) {
	if vendorID == "" || vlUnitKey(vendorUnit) == "" {
		return
	}
	_, _ = db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.vendor_unit_names (vendor_id, vendor_unit, item_unit, created_by)
		VALUES ($1, $2, $3, $4) ON CONFLICT (vendor_id, vendor_unit) DO UPDATE SET item_unit = EXCLUDED.item_unit`, schema),
		vendorID, vlUnitKey(vendorUnit), itemUnit, userID)
}

// --- applying a proposal -------------------------------------------------------

func vlLoadProposal(schema string, id int64) (vlProposal, string, error) {
	p := vlProposal{id: id}
	var status string
	var q sql.NullFloat64
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT link_id, po_id, line_no, qty, promised_date, vendor_unit, note, status
		FROM %s.vendor_po_proposals WHERE id = $1`, schema), id).Scan(&p.linkID, &p.poID, &p.lineNo, &q, &p.date, &p.unit, &p.note, &status)
	if err != nil {
		return p, "", vlErr("PURCHA-5064", "that change was not found")
	}
	if q.Valid {
		v := q.Float64
		p.qty = &v
	}
	return p, status, nil
}

// ApplyVendorProposal is the buyer's "apply": it changes the PO line. A quantity
// is an amendment of the order (reason, totals, approval again); a date sets
// the line's one instalment. Either way the vendor's confirmation re-opens.
// rememberUnit keeps the vendor's unit name for this vendor.
func ApplyVendorProposal(tenantID, userID, role string, id int64, rememberUnit bool) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	p, _, err := vlLoadProposal(schema, id)
	if err != nil {
		return err
	}
	return vlApply(tenantID, schema, userID, role, p, false, rememberUnit)
}

func vlApply(tenantID, schema, actor, role string, p vlProposal, auto, rememberUnit bool) error {
	res, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.vendor_po_proposals SET status = 'Applying' WHERE id = $1 AND status = 'Pending'`, schema), p.id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return vlErr("PURCHA-5064", "that change was already dealt with")
	}
	fail := func(e error) error {
		_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.vendor_po_proposals SET status = 'Pending' WHERE id = $1 AND status = 'Applying'`, schema), p.id)
		return e
	}
	po, status, err := vlLoadPO(schema, p.poID)
	if err != nil {
		return fail(err)
	}
	if status == "Closed" || status == "Cancelled" {
		return fail(vlErr("PURCHA-5065", "the purchase order is %s", strings.ToLower(status)))
	}
	lines, err := parsePOLineMaps(strField(po, "items"))
	if err != nil || p.lineNo < 1 || p.lineNo > len(lines) {
		return fail(vlErr("PURCHA-5065", "that line is no longer on the order"))
	}
	ln := lines[p.lineNo-1]
	if b, _ := ln["stopped"].(bool); b {
		return fail(vlErr("PURCHA-5065", "line %d is stopped", p.lineNo))
	}
	var inst []POInstalment
	if raw, ok := ln["instalments"]; ok {
		b, _ := json.Marshal(raw)
		_ = json.Unmarshal(b, &inst)
	}
	if len(inst) > 1 {
		return fail(vlErr("PURCHA-5065", "line %d has several instalments - change it on the order", p.lineNo))
	}
	if p.qty != nil && math.Abs(*p.qty-numFromInterface(ln["qty"])) > 1e-9 {
		if _, taken, e := poLineOpenQty(tenantID, p.poID, lines); e == nil && *p.qty < taken[p.lineNo-1]-1e-9 {
			return fail(vlErr("PURCHA-5065", "line %d already has more than that received", p.lineNo))
		}
		if err := vlAmendQty(tenantID, actor, role, p.poID, p.lineNo, *p.qty, p.vendorNote(auto)); err != nil {
			return fail(err)
		}
	}
	if p.date != "" {
		err := editPOLines(tenantID, p.poID, func(ls []map[string]interface{}) error {
			l, err := poLineAt(ls, p.lineNo)
			if err != nil {
				return err
			}
			q := poLineDueQty(l)
			if v := numFromInterface(l["qty"]); v > 0 && l["free_qty"] == nil {
				q = v
			}
			l["instalments"] = []POInstalment{{Qty: q, Date: p.date}}
			reopenConfirmation(l)
			return nil
		})
		if err != nil {
			return fail(err)
		}
	}
	if p.unit != "" && rememberUnit {
		vlRememberUnit(schema, actor, vlVendorID(po), p.unit, vlLineUnit(tenantID, ln))
	}
	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.vendor_po_proposals SET status = 'Applied', auto_applied = $2, decided_by = $3, decided_at = now()
		WHERE id = $1`, schema), p.id, auto, actor)
	LogAuditEvent(tenantID, actor, "VENDOR_LINK_APPLIED", "SUCCESS",
		fmt.Sprintf("Applied the vendor's change to line %d of purchase order %s", p.lineNo, firstNonEmpty(strField(po, "code"), p.poID)))
	return nil
}

func (p vlProposal) vendorNote(auto bool) string {
	who := "the vendor's proposal"
	if auto {
		who = "the trusted vendor's proposal"
	}
	return fmt.Sprintf("Quantity of line %d changed by %s", p.lineNo, who)
}

// vlAmendQty changes one line's quantity the way a saved amendment does: the
// order's own edit rules, the totals, the edit audit and, for an approved order
// under an approval rule, back to approval.
func vlAmendQty(tenantID, actor, role, poID string, lineNo int, qty float64, reason string) error {
	prior, status, err := fetchDocData(tenantID, "PurchaseOrder", poID)
	if err != nil {
		return err
	}
	payload := map[string]interface{}{}
	for k, v := range prior {
		payload[k] = v
	}
	lines, err := parsePOLineMaps(strField(prior, "items"))
	if err != nil {
		return err
	}
	l, err := poLineAt(lines, lineNo)
	if err != nil {
		return err
	}
	var inst []POInstalment
	if raw, ok := l["instalments"]; ok {
		b, _ := json.Marshal(raw)
		_ = json.Unmarshal(b, &inst)
	}
	if len(inst) > 1 {
		return vlErr("PURCHA-5065", "line %d has several instalments - change it on the order", lineNo)
	}
	l["qty"] = qty
	if len(inst) == 1 {
		inst[0].Qty = qty
		l["instalments"] = inst
	}
	reopenConfirmation(l)
	items, _ := json.Marshal(lines)
	payload["items"] = string(items)
	payload["amendment_reason"] = reason
	payload["status"] = status
	if err := ValidateTransactionalRules(tenantID, "PurchaseOrder", poID, status, prior, payload); err != nil {
		return err
	}
	if err := ApplyPurchaseOrderTotals(tenantID, payload); err != nil {
		return err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(payload)
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, updated_at = CURRENT_TIMESTAMP
		WHERE doctype = 'PurchaseOrder' AND id = $2`, schema), raw, poID); err != nil {
		return err
	}
	LogDocumentEdit(tenantID, actor, "PurchaseOrder", poID, prior, payload, reason)
	if status == "Approved" {
		if gated, e := IsApprovalGated(tenantID, "PurchaseOrder"); e == nil && gated {
			if e := ResetToPendingOnEdit(tenantID, "PurchaseOrder", poID, actor, role, payload); e != nil {
				log.Printf("[VENDOR-LINK] re-approval after amendment of %s: %v", poID, e)
			}
		}
	}
	return nil
}

// RejectVendorProposal turns a proposal down; the order is left alone.
func RejectVendorProposal(tenantID, userID string, id int64, note string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	res, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.vendor_po_proposals SET status = 'Rejected', decided_by = $2, decided_at = now(), decision_note = $3
		WHERE id = $1 AND status = 'Pending'`, schema), id, userID, vlClean(note, 300))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return vlErr("PURCHA-5064", "that change was already dealt with")
	}
	LogAuditEvent(tenantID, userID, "VENDOR_LINK_REJECTED", "SUCCESS", fmt.Sprintf("Rejected the vendor's change %d", id))
	return nil
}

// --- invoice files -------------------------------------------------------------

var vlFileTypes = map[string]struct {
	ctype string
	magic []byte
}{
	".pdf":  {"application/pdf", []byte("%PDF-")},
	".png":  {"image/png", []byte("\x89PNG\r\n\x1a\n")},
	".jpg":  {"image/jpeg", []byte("\xff\xd8\xff")},
	".jpeg": {"image/jpeg", []byte("\xff\xd8\xff")},
}

// SubmitVendorInvoice keeps an uploaded invoice for the buyer to look at. It
// never creates a bill. The type is checked by name and by the file's own first
// bytes, the size is capped, and a link takes only a few files.
func SubmitVendorInvoice(tenantID, token, fileName string, data []byte, invoiceNo, invoiceDate string, amount *float64) error {
	schema, l, err := vlResolve(tenantID, token)
	if err != nil {
		return err
	}
	if _, status, e := vlLoadPO(schema, l.poID); e != nil || !vlPOOpen(status) {
		return vlErr("PURCHA-5062", "that purchase order is not open")
	}
	name := path.Base(strings.ReplaceAll(strings.TrimSpace(fileName), "\\", "/"))
	name = vlClean(name, 120)
	ext := strings.ToLower(path.Ext(name))
	ft, ok := vlFileTypes[ext]
	switch {
	case name == "" || name == "." || name == "/":
		return vlErr("PURCHA-5066", "the file needs a name")
	case !ok:
		return vlErr("PURCHA-5066", "send a PDF, PNG or JPG file")
	case len(data) == 0:
		return vlErr("PURCHA-5066", "the file is empty")
	case len(data) > vendorFileMaxBytes:
		return vlErr("PURCHA-5066", "the file is larger than 2 MB")
	case !bytes.HasPrefix(data, ft.magic):
		return vlErr("PURCHA-5066", "the file is not really a %s", strings.ToUpper(strings.TrimPrefix(ext, ".")))
	}
	invoiceNo = vlClean(invoiceNo, 60)
	if d := strings.TrimSpace(invoiceDate); d != "" {
		if _, err := time.Parse("2006-01-02", d); err != nil {
			return vlErr("PURCHA-5066", "the invoice date must be YYYY-MM-DD")
		}
	}
	if amount != nil && (math.IsNaN(*amount) || *amount < 0 || *amount > 1e10) {
		return vlErr("PURCHA-5066", "the invoice amount is not valid")
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// One link's uploads queue on its row, so the cap holds when they arrive together.
	var held int
	if err := tx.QueryRow(fmt.Sprintf(`SELECT 1 FROM %s.vendor_po_links WHERE id = $1 FOR UPDATE`, schema), l.id).Scan(&held); err != nil {
		return err
	}
	var n int
	if err := tx.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s.vendor_po_files WHERE link_id = $1`, schema), l.id).Scan(&n); err != nil {
		return err
	}
	if n >= vendorFilesPerLink {
		return vlErr("PURCHA-5066", "this link already has %d files", vendorFilesPerLink)
	}
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.vendor_po_files (link_id, po_id, file_name, content_type, size_bytes, data, invoice_no, invoice_date, amount)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, schema), l.id, l.poID, name, ft.ctype, len(data), data, invoiceNo, strings.TrimSpace(invoiceDate), amount); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, "vendor-link", "VENDOR_LINK_INVOICE", "SUCCESS", fmt.Sprintf("Vendor sent an invoice file for purchase order %s", l.poID))
	return nil
}

// VendorLinkFile returns one uploaded file to staff.
func VendorLinkFile(tenantID string, id int64) (name, ctype string, data []byte, err error) {
	schema, e := db.GetTenantSchema(tenantID)
	if e != nil {
		return "", "", nil, e
	}
	if e := db.DB.QueryRow(fmt.Sprintf(`SELECT file_name, content_type, data FROM %s.vendor_po_files WHERE id = $1`, schema), id).Scan(&name, &ctype, &data); e != nil {
		return "", "", nil, vlErr("PURCHA-5067", "that file was not found")
	}
	return name, ctype, data, nil
}

// ReviewVendorFile marks an uploaded invoice Reviewed or Rejected. Neither posts.
func ReviewVendorFile(tenantID, userID string, id int64, status, note string) error {
	if status != "Reviewed" && status != "Rejected" {
		return vlErr("PURCHA-5067", "a file is marked Reviewed or Rejected")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	res, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.vendor_po_files SET status = $2, reviewed_by = $3, reviewed_at = now(), review_note = $4
		WHERE id = $1`, schema), id, status, userID, vlClean(note, 300))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return vlErr("PURCHA-5067", "that file was not found")
	}
	LogAuditEvent(tenantID, userID, "VENDOR_LINK_FILE_REVIEW", "SUCCESS", fmt.Sprintf("Marked the vendor's file %d %s", id, status))
	return nil
}

// --- what the buyer sees -------------------------------------------------------

type VendorProposalRow struct {
	ID         int64    `json:"id"`
	LineNo     int      `json:"line"`
	Item       string   `json:"item"`
	CurQty     float64  `json:"cur_qty"`
	CurDue     string   `json:"cur_due"`
	Qty        *float64 `json:"qty,omitempty"`
	Date       string   `json:"date,omitempty"`
	VendorUnit string   `json:"vendor_unit,omitempty"`
	UnitKnown  bool     `json:"unit_known"`
	Note       string   `json:"note,omitempty"`
	Status     string   `json:"status"`
	Auto       bool     `json:"auto"`
	At         string   `json:"at"`
	DecidedBy  string   `json:"decided_by,omitempty"`
}

type VendorFileRow struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	Size        int      `json:"size"`
	InvoiceNo   string   `json:"invoice_no,omitempty"`
	InvoiceDate string   `json:"invoice_date,omitempty"`
	Amount      *float64 `json:"amount,omitempty"`
	Status      string   `json:"status"`
	At          string   `json:"at"`
}

type VendorLinkAdmin struct {
	Enabled   bool                `json:"enabled"`
	Trusted   bool                `json:"trusted"`
	Link      *VendorLinkInfo     `json:"link,omitempty"`
	Opened    int                 `json:"opened"`
	LastOpen  string              `json:"last_open,omitempty"`
	Proposals []VendorProposalRow `json:"proposals"`
	Files     []VendorFileRow     `json:"files"`
}

func VendorLinkAdminView(tenantID, poID string) (*VendorLinkAdmin, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	po, _, err := vlLoadPO(schema, poID)
	if err != nil {
		return nil, err
	}
	out := &VendorLinkAdmin{Enabled: VendorLinkOn(tenantID), Proposals: []VendorProposalRow{}, Files: []VendorFileRow{}}
	vid := vlVendorID(po)
	out.Trusted = strings.EqualFold(strField(vlVendor(tenantID, vid), "vendor_link_trusted"), "Yes")
	l := vlLink{poID: poID, vendorID: vid}
	var opened sql.NullTime
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT id, expires_at, open_count, last_opened_at FROM %s.vendor_po_links
		WHERE po_id = $1 AND revoked_at IS NULL AND expires_at > now() ORDER BY created_at DESC LIMIT 1`, schema), poID).Scan(&l.id, &l.expires, &out.Opened, &opened)
	if err == nil {
		out.Link = vlInfo(tenantID, schema, l)
		if opened.Valid {
			out.LastOpen = vlFmtTime(opened.Time)
		}
	}
	lines, _ := parsePOLineMaps(strField(po, "items"))
	rs, err := db.DB.Query(fmt.Sprintf(`SELECT id, line_no, qty, promised_date, vendor_unit, note, status, auto_applied, created_at, decided_by
		FROM %s.vendor_po_proposals WHERE po_id = $1 AND status <> 'Replaced' AND status <> 'Applying' ORDER BY id DESC LIMIT 200`, schema), poID)
	if err != nil {
		return nil, err
	}
	for rs.Next() {
		var r VendorProposalRow
		var q sql.NullFloat64
		var at time.Time
		if rs.Scan(&r.ID, &r.LineNo, &q, &r.Date, &r.VendorUnit, &r.Note, &r.Status, &r.Auto, &at, &r.DecidedBy) != nil {
			continue
		}
		if q.Valid {
			v := q.Float64
			r.Qty = &v
		}
		r.At = vlFmtTime(at)
		if r.LineNo >= 1 && r.LineNo <= len(lines) {
			ln := lines[r.LineNo-1]
			r.CurQty, r.CurDue = poLineDueQty(ln), vlLineDue(ln, po)
			if it, e := ResolveItemBySKU(tenantID, strField(ln, "sku")); e == nil {
				r.Item = strField(it.Data, "name")
			}
			r.UnitKnown = vlUnitKnown(schema, vid, r.VendorUnit, vlLineUnit(tenantID, ln))
		}
		out.Proposals = append(out.Proposals, r)
	}
	rs.Close()
	fs, err := db.DB.Query(fmt.Sprintf(`SELECT id, file_name, size_bytes, invoice_no, invoice_date, amount, status, created_at
		FROM %s.vendor_po_files WHERE po_id = $1 ORDER BY id DESC LIMIT 50`, schema), poID)
	if err != nil {
		return nil, err
	}
	for fs.Next() {
		var f VendorFileRow
		var a sql.NullFloat64
		var at time.Time
		if fs.Scan(&f.ID, &f.Name, &f.Size, &f.InvoiceNo, &f.InvoiceDate, &a, &f.Status, &at) != nil {
			continue
		}
		if a.Valid {
			v := a.Float64
			f.Amount = &v
		}
		f.At = vlFmtTime(at)
		out.Files = append(out.Files, f)
	}
	fs.Close()
	return out, nil
}

// --- reminders -----------------------------------------------------------------

// RunVendorLinkReminders mails each vendor whose approved order has lines past
// their date, once a week per order, with the link in the mail. Only with the
// setting on; through the outbox like every other mail.
func RunVendorLinkReminders(tenantID, schema string) error {
	if !GetSettingBoolForSchema(schema, SettingVendorLink) {
		return nil
	}
	rows, err := POOverdueLines(tenantID, "")
	if err != nil || len(rows) == 0 {
		return err
	}
	var order []string
	byPO := map[string][]map[string]interface{}{}
	for _, r := range rows {
		id, _ := r["po_id"].(string)
		if id == "" {
			continue
		}
		if _, ok := byPO[id]; !ok {
			order = append(order, id)
		}
		byPO[id] = append(byPO[id], r)
	}
	for _, poID := range order {
		var recent int
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s.vendor_po_links WHERE po_id = $1
			AND last_reminded_at > now() - make_interval(days => %d)`, schema, vendorReminderGapDays), poID).Scan(&recent)
		if recent > 0 {
			continue
		}
		po, status, e := vlLoadPO(schema, poID)
		if e != nil || status != "Approved" {
			continue
		}
		to := strings.TrimSpace(strField(vlVendor(tenantID, vlVendorID(po)), "contact_email"))
		if to == "" {
			continue
		}
		l, e := vlLiveLink(schema, poID, vlVendorID(po))
		if e != nil {
			log.Printf("[VENDOR-LINK] reminder link for %s: %v", poID, e)
			continue
		}
		link := crmPublicLink(schema, "/p/vendor.html", vlToken(tenantID, *l))
		if link == "" {
			continue
		}
		num := firstNonEmpty(strField(po, "po_number"), strField(po, "code"), poID)
		var b strings.Builder
		fmt.Fprintf(&b, "Hello,\n\nThese lines on purchase order %s are past their date:\n\n", num)
		for _, r := range byPO[poID] {
			fmt.Fprintf(&b, "- %v: %v to come, due %v (%v days late)\n", firstNonEmpty(fmt.Sprint(r["name"]), fmt.Sprint(r["sku"])), r["pending"], r["due"], r["days_late"])
		}
		fmt.Fprintf(&b, "\nPlease confirm, or tell us the date you can deliver, here:\n%s\n\nThe link works until %s.\n",
			link, l.expires.In(BusinessLocation()).Format("2 Jan 2006"))
		tx, e := db.DB.Begin()
		if e != nil {
			return e
		}
		if e = PublishEvent(tx, schema, "email.send", map[string]interface{}{"to": to,
			"subject": "Purchase order " + num + ": lines past their date", "body": b.String()}); e == nil {
			_, e = tx.Exec(fmt.Sprintf(`UPDATE %s.vendor_po_links SET last_reminded_at = now() WHERE id = $1`, schema), l.id)
		}
		if e != nil {
			tx.Rollback()
			log.Printf("[VENDOR-LINK] reminder for %s: %v", poID, e)
			continue
		}
		if e = tx.Commit(); e != nil {
			return e
		}
	}
	return nil
}

// vlLiveLink returns the PO's live link while it has days to run, else a new one.
func vlLiveLink(schema, poID, vendorID string) (*vlLink, error) {
	l := vlLink{poID: poID, vendorID: vendorID}
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT id, expires_at FROM %s.vendor_po_links
		WHERE po_id = $1 AND revoked_at IS NULL AND expires_at > now() + interval '3 days' ORDER BY created_at DESC LIMIT 1`, schema), poID).Scan(&l.id, &l.expires)
	if err == nil {
		return &l, nil
	}
	return vlInsertLink(schema, "system", poID, vendorID)
}

// VendorLinkPOData returns a PO's data for a staff location-scope check. kind
// is "po" (id is the order), "proposal" or "file" (the id of one of its rows).
func VendorLinkPOData(tenantID, kind, id string) (map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	poID := id
	switch kind {
	case "proposal", "file":
		table := map[string]string{"proposal": "vendor_po_proposals", "file": "vendor_po_files"}[kind]
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT po_id FROM %s.%s WHERE id = $1::bigint`, schema, table), id).Scan(&poID); err != nil {
			return nil, vlErr("PURCHA-5064", "that change was not found")
		}
	}
	po, _, err := vlLoadPO(schema, poID)
	return po, err
}
