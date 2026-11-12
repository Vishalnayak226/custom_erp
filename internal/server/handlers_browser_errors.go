package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"custom_erp/engines"
)

// 158.3: a screen that breaks only in a user's browser used to be learnt from
// the user. public/app.js now reports an uncaught error from one of the app's
// own scripts here (reportBrowserError: at most five a page, each once), and
// it is written to the caller's own system_error_logs as severity BROWSER, so
// the System Log shows it next to the server's faults. The handler stores
// only the bounded fields below - never the page URL's query - and
// LogSystemError redacts and bounds them again before they are written.

const (
	maxBrowserErrorBody    = 8 * 1024
	maxBrowserErrorMessage = 500
	maxBrowserErrorWhere   = 200
	maxBrowserErrorScreen  = 80
	maxBrowserErrorStack   = 4000
)

type browserErrorReport struct {
	Message string `json:"message"`
	Stack   string `json:"stack"`
	Where   string `json:"where"`  // script path:line:column
	Screen  string `json:"screen"` // the hash route, without its query
}

func handleBrowserError(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Resolved-Purpose") == "extension" {
		writeAPIErrorGeneric(w, r, http.StatusForbidden, "Extension tokens cannot report browser errors")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBrowserErrorBody+1))
	if err != nil || len(body) > maxBrowserErrorBody {
		writeAPIErrorGeneric(w, r, http.StatusRequestEntityTooLarge, "Browser error report is too large")
		return
	}
	var report browserErrorReport
	if err := json.Unmarshal(body, &report); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, "Browser error report is not valid JSON")
		return
	}
	message := boundBrowserField(report.Message, maxBrowserErrorMessage, true)
	if message == "" {
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, "Browser error report has no message")
		return
	}
	if where := boundBrowserField(report.Where, maxBrowserErrorWhere, true); where != "" {
		message += " at " + where
	}
	screen, _, _ := strings.Cut(report.Screen, "?")
	source := "browser"
	if screen = boundBrowserField(screen, maxBrowserErrorScreen, true); screen != "" {
		source += " " + screen
	}
	engines.LogSystemError(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Correlation-ID"),
		"BROWSER", source, message, boundBrowserField(report.Stack, maxBrowserErrorStack, false))
	w.WriteHeader(http.StatusNoContent)
}

// boundBrowserField trims a browser-supplied field to at most max bytes, cut
// on a character boundary; oneLine also folds it onto one line.
func boundBrowserField(value string, max int, oneLine bool) string {
	if oneLine {
		value = strings.Join(strings.Fields(value), " ")
	} else {
		value = strings.TrimSpace(value)
	}
	if len(value) <= max {
		return value
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut]
}
