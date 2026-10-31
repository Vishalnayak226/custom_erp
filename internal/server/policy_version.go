package server

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"

	"custom_erp/engines"
)

// Stage 49.11.2: the capability / policy version on each security event.
//
// The route-level authorisation policy is compiled into the binary: the
// capability each route pattern needs (routeCapabilities), which roles hold a
// restricted capability (capabilityRoleAllowlist), the platform-only ones
// (devOnlyCapabilities) and every role template's capabilities and module
// grants. policyVersion fingerprints all of it, so two events carrying the
// same fingerprint were decided by the same rules, and an investigator can
// tell when the rules themselves changed. A client's own permission edits are
// data, not this policy - each is its own ROLE_PERMISSIONS audit row.

var policyVersionOnce struct {
	sync.Once
	v string
}

// policyVersion is "p" and the first 12 hex digits of the SHA-256 of the
// compiled authorisation policy, computed once.
func policyVersion() string {
	policyVersionOnce.Do(func() { policyVersionOnce.v = computePolicyVersion() })
	return policyVersionOnce.v
}

func computePolicyVersion() string {
	var lines []string
	for pattern, c := range routeCapabilities {
		lines = append(lines, fmt.Sprintf("route|%s|%v|%s", pattern, c.Level, c.Capability))
	}
	for capability, roles := range capabilityRoleAllowlist {
		r := append([]string(nil), roles...)
		sort.Strings(r)
		lines = append(lines, "allow|"+capability+"|"+strings.Join(r, ","))
	}
	for capability, on := range devOnlyCapabilities {
		if on {
			lines = append(lines, "platform|"+capability)
		}
	}
	for _, t := range engines.RoleTemplates() {
		caps := append([]string(nil), t.Capabilities...)
		sort.Strings(caps)
		lines = append(lines, "template|"+t.Name+"|"+strings.Join(caps, ","))
		for module, level := range t.Modules {
			lines = append(lines, fmt.Sprintf("grant|%s|%s|%s", t.Name, module, level))
		}
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return "p" + hex.EncodeToString(sum[:])[:12]
}
