package server

import "strings"

// Stage 140.14.4: GLOBAL-0002 ("Invalid format") is the catch-all code for a
// 4xx refusal with no code of its own, and its catalogue hint ("Correct the
// field format.") is wrong advice for a refusal that is not about a format.
// The hint follows the kind of the message, decided here once for the two
// writers every refusal goes through (writeAPIErrorGeneric and
// writeAPIErrorDetail). A message of no known kind keeps the catalogue hint.
func refusalHint(code, text, catalogue string) string {
	if code != "GLOBAL-0002" {
		return catalogue
	}
	t := strings.ToLower(text)
	switch {
	case strings.Contains(t, "packed") && (strings.Contains(t, "cancel") || strings.Contains(t, "changed")):
		return "Let it ship and raise a return, or mark the shipment RTO if it has not left."
	case strings.Contains(t, "cannot be deleted") || strings.Contains(t, "can't be deleted") || strings.Contains(t, "cannot delete") || strings.Contains(t, "not be deleted"):
		return "Cancel or reverse it instead of deleting it."
	case strings.Contains(t, "return location") || strings.Contains(t, "return_location") || strings.Contains(t, "auto-routed"):
		return "Choose the return location."
	case strings.Contains(t, "already exists") || strings.Contains(t, "already used") || strings.Contains(t, "already in use") || strings.Contains(t, "already registered"):
		return "Open the existing record, or use a different value."
	}
	return catalogue
}
