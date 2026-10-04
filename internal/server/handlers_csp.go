package server

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
)

const maxCSPReportBytes = 16 * 1024

type cspViolation struct {
	ViolatedDirective  string `json:"violated-directive"`
	EffectiveDirective string `json:"effectiveDirective"`
	BlockedURI         string `json:"blocked-uri"`
	LineNumber         int    `json:"line-number"`
	ModernLineNumber   int    `json:"lineNumber"`
}

// handleCSPReport accepts legacy report-uri payloads and the body shape used
// by the Reporting API. It deliberately drops document/source URLs (which can
// contain ERP record ids or query tokens) and logs only bounded directive,
// blocked-resource class and source line metadata.
func handleCSPReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxCSPReportBytes+1))
	if err != nil || len(body) > maxCSPReportBytes {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	violations := decodeCSPViolations(body)
	for _, violation := range violations {
		directive := safeCSPToken(violation.ViolatedDirective)
		if directive == "" {
			directive = safeCSPToken(violation.EffectiveDirective)
		}
		if directive == "" {
			continue
		}
		line := violation.LineNumber
		if line == 0 {
			line = violation.ModernLineNumber
		}
		log.Printf("[CSP-REPORT] directive=%s blocked=%s line=%d", directive, cspBlockedClass(violation.BlockedURI), line)
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeCSPViolations(body []byte) []cspViolation {
	var legacy struct {
		Report cspViolation `json:"csp-report"`
	}
	if err := json.Unmarshal(body, &legacy); err == nil && (legacy.Report.ViolatedDirective != "" || legacy.Report.EffectiveDirective != "") {
		return []cspViolation{legacy.Report}
	}
	var modern []struct {
		Body cspViolation `json:"body"`
	}
	if err := json.Unmarshal(body, &modern); err != nil {
		return nil
	}
	out := make([]cspViolation, 0, len(modern))
	for _, item := range modern {
		out = append(out, item.Body)
	}
	return out
}

func safeCSPToken(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) > 80 {
		value = value[:80]
	}
	var out strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == ' ' {
			out.WriteRune(r)
		}
	}
	return strings.TrimSpace(out.String())
}

func cspBlockedClass(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch {
	case value == "":
		return "unknown"
	case value == "inline":
		return "inline"
	case value == "eval":
		return "eval"
	case value == "self" || strings.HasPrefix(value, "'self'"):
		return "self"
	case strings.HasPrefix(value, "data:"):
		return "data"
	case strings.HasPrefix(value, "blob:"):
		return "blob"
	default:
		return "external"
	}
}
