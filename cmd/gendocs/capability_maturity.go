package main

import (
	"fmt"
	"strings"

	"custom_erp/engines"
)

// Stage 47.17.5: docs/product/capability-maturity.md is a projection of
// engines.CapabilityMaturityRegistry() - the level each capability (module ×
// configuration) is at, the evidence, the limits and any certification
// expiry. Hand edits are lost on the next run; change the registry.
const capabilityMaturityPath = "docs/product/capability-maturity.md"

func capabilityMaturityFiles(stamp string) map[string][]byte {
	return map[string][]byte{capabilityMaturityPath: []byte(capabilityMaturityDoc(stamp))}
}

func capabilityMaturityDoc(stamp string) string {
	var b strings.Builder
	b.WriteString("---\n" +
		"doc_id: DOC-PRODUCT-CAPABILITY-MATURITY\n" +
		"title: Capability maturity\n" +
		"type: generated\n" +
		"status: active\n" +
		"owner: product-owner\n" +
		"authority: projection\n" +
		"confidentiality: internal\n" +
		"last_verified: " + stamp + "\n" +
		"---\n\n")
	b.WriteString(generatedHeader("Capability maturity", stamp,
		"`engines.CapabilityMaturityRegistry()` (`engines/capability_maturity.go`)",
		"`pwsh docs/update-docs.ps1 -Group Content`"))
	b.WriteString("Maturity is per configuration, not per module: the online till and the offline till are both the `sales` module and are not equally proven. " +
		"**Experimental** - synthetic data only. **Preview** - built and tested, known limits, pilot use. " +
		"**Production** - journey, control, documentation, restore and UAT evidence, with a live run on a client on record. " +
		"**Certified** - external validation with an expiry (none yet). " +
		"A module with no entry, a Certified entry past its expiry, or a Production entry without dated evidence fails `TestStage47175EveryModuleHasAMaturityEntry`. " +
		"The Control Panel carries the same list (`capability_maturity`).\n\n")

	entries := engines.CapabilityMaturityRegistry()
	counts := map[string]int{}
	for _, e := range entries {
		counts[e.Level]++
	}
	b.WriteString("| Level | Capabilities |\n|---|---|\n")
	for _, l := range engines.MaturityLevels {
		fmt.Fprintf(&b, "| %s | %d |\n", l, counts[l])
	}
	b.WriteString("\n")
	for i := len(engines.MaturityLevels) - 1; i >= 0; i-- {
		level := engines.MaturityLevels[i]
		if counts[level] == 0 {
			continue
		}
		fmt.Fprintf(&b, "## %s\n\n| Capability | Module | Evidence | Limits | Verified |\n|---|---|---|---|---|\n", level)
		for _, e := range entries {
			if e.Level != level {
				continue
			}
			verified := e.VerifiedOn
			if verified == "" {
				verified = "-"
			}
			if e.CertifiedUntil != "" {
				verified += " (certified until " + e.CertifiedUntil + ")"
			}
			limits := e.Limits
			if limits == "" {
				limits = "-"
			}
			fmt.Fprintf(&b, "| %s (`%s`) | `%s` | %s | %s | %s |\n", mdCell(e.Title), e.Key, e.Module, mdCell(strings.Join(e.Evidence, "; ")), mdCell(limits), verified)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// mdCell keeps a value on one table row.
func mdCell(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "|", "\\|"), "\n", " ")
}
