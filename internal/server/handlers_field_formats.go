package server

import (
	"encoding/json"
	"net/http"

	"custom_erp/engines"
)

// handleFieldFormats serves the field-format declarations (Stage 40.2).
//
// The frontend reads this once at boot to decide, per input, what to show as
// a placeholder, what hint to put under it, which keystrokes to refuse, and
// whether to upper-case as you type. Serving it rather than hardcoding the
// same table in app.js is the point: the server is what actually rejects a
// malformed value, so the guidance the user sees has to come from the same
// declarations, or the two drift and the form starts promising a format the
// server does not accept.
//
// Open to any authenticated role. There is nothing tenant-specific or
// sensitive here - it is a description of what a GSTIN looks like.
func handleFieldFormats(w http.ResponseWriter, r *http.Request) {
	// Cached hard: these change only when the binary does, and every screen
	// with a form wants them. The frontend still fetches once per boot; this
	// keeps a reload from paying for it again.
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_ = json.NewEncoder(w).Encode(engines.FieldFormatSpecs())
}
