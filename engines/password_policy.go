package engines

import (
	_ "embed"
	"errors"
	"fmt"
	"strings"
)

// Stage 49.2.2: password baseline, enforced at every path that lets a human
// choose their own password - self-service change (handlers_profile.go),
// reset completion (password_reset.go) and admin account creation
// (handlers_admin_identity.go) - through this one shared function, the same
// choke-point convention this repo uses everywhere else (ValidateDocument,
// the writeAPIError family, etc.) rather than repeating the rule at each
// call site.
//
// commonPasswordsRaw is a small, locally-embedded, bounded list of the
// passwords that dominate every public breach-analysis writeup - not a live
// call to a breach-checking API (e.g. the HaveIBeenPwned k-anonymity range
// endpoint). That is a deliberate reading of "privacy-preserving bounded
// method": nothing about a candidate password ever leaves this process, at
// the cost of covering a curated few hundred entries rather than billions.
// It is not a claim of exhaustive breach-corpus coverage - see
// docs/security/risk_register.md if a stronger source is ever adopted.
//
//go:embed common_passwords.txt
var commonPasswordsRaw string

var commonPasswordSet = buildCommonPasswordSet()

func buildCommonPasswordSet() map[string]struct{} {
	set := make(map[string]struct{})
	for _, line := range strings.Split(commonPasswordsRaw, "\n") {
		p := strings.ToLower(strings.TrimSpace(line))
		if p == "" || strings.HasPrefix(p, "#") {
			continue
		}
		set[p] = struct{}{}
	}
	return set
}

// ValidatePasswordStrength enforces the 49.2.2 baseline. username may be
// empty (no caller passes empty today, but an unknown username should never
// panic here); a real value is compared case-insensitively so "Priya2026"
// for user "priya" is refused the same as the literal username. tenantID
// selects the tenant's configured minimum length
// ("security.password_min_length", Stage 30.7 settings-registry pattern).
func ValidatePasswordStrength(tenantID, password, username string) error {
	minLen := GetSettingInt(tenantID, "security.password_min_length")
	if minLen <= 0 {
		minLen = 12
	}
	if len(password) < minLen {
		return fmt.Errorf("password must be at least %d characters", minLen)
	}
	lower := strings.ToLower(strings.TrimSpace(password))
	if username != "" && lower == strings.ToLower(strings.TrimSpace(username)) {
		return errors.New("password must not be the same as the username")
	}
	if _, common := commonPasswordSet[lower]; common {
		return errors.New("password is one of the most commonly guessed/breached passwords - choose a less predictable one")
	}
	if isTrivialSequence(lower) {
		return errors.New("password is a simple repeated or sequential pattern - choose a less predictable one")
	}
	return nil
}

// isTrivialSequence catches the cheap patterns a fixed denylist alone would
// miss: every character the same ("aaaaaaaaaaaa") and a run of strictly
// ascending or descending byte values ("abcdefghijkl", "hgfedcba", a
// 12-digit countdown). Deliberately simple - this is a cheap backstop next
// to the denylist, not a full password-strength estimator.
func isTrivialSequence(s string) bool {
	if len(s) < 4 {
		return false
	}
	allSame, ascending, descending := true, true, true
	for i := 1; i < len(s); i++ {
		if s[i] != s[0] {
			allSame = false
		}
		if s[i] != s[i-1]+1 {
			ascending = false
		}
		if s[i] != s[i-1]-1 {
			descending = false
		}
	}
	return allSame || ascending || descending
}
