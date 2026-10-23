package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strings"
)

// Stage 127.4 (user, 2026-10-21: "see if accounting and wms and other module
// needs print profile pbuild that also"; 74.3.4's profile part): a Print
// Profile says how a kind of document prints for every module - copy labels
// (one print run = one set), paper, default template, a footer line - for all
// stores or one. The most specific Active profile wins: this document + this
// store, this document, all documents + this store, all documents. With no
// profile a document prints exactly as it did.

// PrintProfile is the profile a print resolved to.
type PrintProfile struct {
	Code       string   `json:"code"`
	Name       string   `json:"name"`
	CopyLabels []string `json:"copy_labels,omitempty"`
	Paper      string   `json:"paper,omitempty"`
	TemplateID string   `json:"template_id,omitempty"`
	FooterText string   `json:"footer_text,omitempty"`
}

// ResolvePrintProfile picks the profile for one print. txn is the print
// transaction, doctype its doctype; a profile names either.
func ResolvePrintProfile(tenantID, txn, doctype, store string) *PrintProfile {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data FROM %s.documents WHERE doctype = 'PrintProfile' AND COALESCE(data->>'status', status) = 'Active' AND deleted_at IS NULL ORDER BY id`, schema))
	if err != nil {
		return nil
	}
	defer rows.Close()
	var best *PrintProfile
	bestScore := -1
	// Stage 130.16 (74.3.4): a profile may also name a legal entity and a
	// channel; profileScore ranks document, store, entity, channel.
	entity, channel := locationLegalEntity(tenantID, store), printChannel(txn)
	for rows.Next() {
		var id, raw string
		if rows.Scan(&id, &raw) != nil {
			continue
		}
		var data map[string]interface{}
		if json.Unmarshal([]byte(raw), &data) != nil {
			continue
		}
		score := profileScore(data, txn, doctype, store, entity, channel)
		if score < 0 {
			continue
		}
		if score > bestScore {
			bestScore = score
			best = &PrintProfile{Code: firstNonEmpty(strField(data, "code"), id), Name: strField(data, "name"),
				CopyLabels: splitCopyLabels(strField(data, "copy_labels")), Paper: strField(data, "paper"),
				TemplateID: strField(data, "template"), FooterText: strField(data, "footer_text")}
		}
	}
	return best
}

func splitCopyLabels(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// applyPrintProfile puts the resolved profile on the print data, where the
// page (trade-docs.js) reads it: fields.print_profile.
func (d *PrintDocument) applyPrintProfile(tenantID, txn string) {
	p := ResolvePrintProfile(tenantID, txn, d.Doctype, d.Store.Code)
	// Stage 130.16 (74.3.1): with no profile of its own, a delivery challan
	// prints Rule 55's three copies as one set.
	if p == nil && txn == PrintTxnDeliveryChallan {
		p = &PrintProfile{Code: "RULE55", Name: "Delivery challan copies", CopyLabels: []string{"Original for Consignee", "Duplicate for Transporter", "Triplicate for Consignor"}}
	}
	if p != nil {
		d.Fields["print_profile"] = p
		if p.FooterText != "" {
			d.Fields["profile_footer"] = p.FooterText
		}
	}
}

// printBrandLogo (Stage 130.16, 74.3.4) is the store's legal entity's
// approved logo - an image link or data URL - for {brand.logo}.
func printBrandLogo(tenantID, store string) string {
	entity := locationLegalEntity(tenantID, store)
	if entity == "" {
		return ""
	}
	data, _, err := fetchDocData(tenantID, "LegalEntity", entity)
	if err != nil || data == nil {
		return ""
	}
	return strings.TrimSpace(strField(data, "logo"))
}

// entityBankLine is the bank line an invoice prints: name, account, IFSC.
func entityBankLine(e map[string]interface{}) string {
	var parts []string
	if v := str(e, "bank_name"); v != "" {
		parts = append(parts, v)
	}
	if v := str(e, "bank_account"); v != "" {
		parts = append(parts, "A/c "+v)
	}
	if v := str(e, "bank_ifsc"); v != "" {
		parts = append(parts, "IFSC "+v)
	}
	return strings.Join(parts, " · ")
}
