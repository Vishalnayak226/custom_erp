package engines

import (
	"fmt"
	"sort"
	"strings"
)

// 49.5.4 (b): one signed EDIT_<Doctype> audit row per record change, whichever
// way the change arrives - the record save (handlers_core_doc_engine.go), a
// CSV import (import.go) and the Setup Workbook (which applies through the
// record save). The abuse detections (security_detection_abuse.go) read
// these rows, so a vendor bank change made by import is seen like one made
// on screen. The row is signed by logAuditEvent like every audit row.

// ChangedFieldNames lists the fields whose value differs between before and
// after, sorted; id, version and updated_at never count, and a field dropped
// by the write counts unless it is the status.
func ChangedFieldNames(before, after map[string]interface{}) []string {
	var out []string
	seen := map[string]bool{}
	for k, v := range after {
		seen[k] = true
		if k == "id" || k == "version" || k == "updated_at" {
			continue
		}
		if fmt.Sprintf("%v", before[k]) != fmt.Sprintf("%v", v) {
			out = append(out, k)
		}
	}
	for k := range before {
		if !seen[k] && k != "id" && k != "version" && k != "status" {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// LogDocumentEdit writes the signed EDIT_<doctype> row for a change of an
// existing record and returns the changed field names (none: no row).
func LogDocumentEdit(tenantID, userID, doctype, docID string, before, after map[string]interface{}, reason string) []string {
	changed := ChangedFieldNames(before, after)
	if len(changed) == 0 {
		return nil
	}
	detail := fmt.Sprintf("Edited %s %s: %s", doctype, docID, strings.Join(changed, ", "))
	if r := strings.TrimSpace(reason); r != "" {
		detail += " - reason: " + r
	}
	LogAuditEvent(tenantID, userID, "EDIT_"+doctype, "SUCCESS", detail)
	return changed
}
