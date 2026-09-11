package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"custom_erp/internal/docgen"
)

type CapabilityRegister struct {
	SchemaVersion      int             `json:"schema_version"`
	Status             string          `json:"status"`
	Owner              string          `json:"owner"`
	Release            string          `json:"release"`
	Configurations     []Configuration `json:"configurations"`
	Capabilities       []Capability    `json:"capabilities"`
	CommonRequirements []string        `json:"common_requirements,omitempty"`
}
type Configuration struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Industry   string `json:"industry"`
	Country    string `json:"country"`
	Deployment string `json:"deployment"`
	Devices    string `json:"devices"`
	OwnerModel string `json:"owner_model"`
	Limit      string `json:"limit"`
}
type Capability struct {
	ID              string   `json:"id"`
	Title           string   `json:"title"`
	Owner           string   `json:"owner"`
	Maturity        string   `json:"maturity"`
	Configurations  []string `json:"configurations"`
	Requirements    []string `json:"requirements"`
	BusinessNeeds   []string `json:"business_needs,omitempty"`
	Personas        []string `json:"personas,omitempty"`
	Processes       []string `json:"processes,omitempty"`
	Controls        []string `json:"controls,omitempty"`
	Design          []string `json:"design"`
	Tests           []string `json:"tests"`
	Help            []string `json:"help"`
	Stages          []string `json:"stages"`
	Limits          string   `json:"limits"`
	LastVerified    string   `json:"last_verified"`
	ReviewBy        string   `json:"review_by"`
	Approver        string   `json:"approver"`
	ReleaseEvidence []string `json:"release_evidence"`
}

func capabilityFiles(root string, now time.Time) (map[string][]byte, []Finding, error) {
	const source = "docs/product/capability-register.json"
	body, err := os.ReadFile(filepath.Join(root, source))
	if err != nil {
		return nil, nil, err
	}
	var register CapabilityRegister
	if err := json.Unmarshal(body, &register); err != nil {
		return nil, nil, err
	}
	findings := validateCapabilities(root, register, now)
	findings = append(findings, validateReverseTrace(root, register)...)
	var catalog, trace strings.Builder
	header := "<!-- GENERATED from docs/product/capability-register.json by cmd/doclint. DO NOT EDIT. -->\n\n"
	catalog.WriteString(header + "# Capability catalog\n\nSource status: **" + register.Status + "**. Owner: **" + register.Owner + "**. Version label: **" + register.Release + "**.\n\n" +
		"Maturity is scoped to the configurations below. Source and test-file links are implementation evidence, not proof that a deployed release passed. No Production or Certified claim is authorized without the signed evidence and approver required by the registry validator.\n\n" +
		"## Reference configurations\n\n| ID | Industry / country | Deployment / devices | Owner model | Limit |\n|---|---|---|---|---|\n")
	for _, c := range register.Configurations {
		fmt.Fprintf(&catalog, "| %s | %s / %s | %s / %s | %s | %s |\n", c.ID, c.Industry, c.Country, c.Deployment, c.Devices, c.OwnerModel, c.Limit)
	}
	catalog.WriteString("\n## Capability status\n\n| Capability | Maturity / configuration | Owner | Limits / remaining gates |\n|---|---|---|---|\n")
	trace.WriteString(header + "# Requirements and evidence traceability\n\nThis projection links requirement, design, implementation work, tests, help and release evidence. Missing signed release evidence is explicitly shown as pending. Approval remains with the accountable product, process, security and QA owners.\n\n")
	trace.WriteString("## Shared requirements\n\nThese requirements apply to every capability and configuration below: " + artifactLinks(register.CommonRequirements) + "\n\n")
	for _, c := range register.Capabilities {
		fmt.Fprintf(&catalog, "| %s — %s | %s / %s | %s | %s |\n", c.ID, c.Title, c.Maturity, strings.Join(c.Configurations, ", "), c.Owner, c.Limits)
		fmt.Fprintf(&trace, "## %s — %s\n\n- Requirements: %s\n- Design/control: %s\n- Automated tests: %s\n- User help: %s\n- Work items: %s\n- Verification/review: %s / %s\n- Signed release evidence: %s\n\n", c.ID, c.Title, artifactLinks(c.Requirements), artifactLinks(c.Design), artifactLinks(c.Tests), artifactLinks(c.Help), strings.Join(c.Stages, ", "), c.LastVerified, c.ReviewBy, artifactLinks(c.ReleaseEvidence))
		fmt.Fprintf(&trace, "- Business need: %s\n- Personas: %s\n- Processes: %s\n- Security/data/operations/legal control: %s\n\n", artifactLinks(c.BusinessNeeds), artifactLinks(c.Personas), artifactLinks(c.Processes), artifactLinks(c.Controls))
	}
	reverse := map[string][]string{}
	for _, c := range register.Capabilities {
		for _, group := range [][]string{register.CommonRequirements, c.BusinessNeeds, c.Requirements, c.Controls, c.Personas, c.Processes} {
			for _, ref := range group {
				if !contains(reverse[ref], c.ID) {
					reverse[ref] = append(reverse[ref], c.ID)
				}
			}
		}
	}
	keys := make([]string, 0, len(reverse))
	for key := range reverse {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	trace.WriteString("## Reverse lookup\n\n| Requirement / need / process / persona | Capabilities |\n|---|---|\n")
	for _, key := range keys {
		fmt.Fprintf(&trace, "| %s | %s |\n", artifactLinks([]string{key}), strings.Join(reverse[key], ", "))
	}
	return map[string][]byte{"docs/generated/capability-catalog.md": []byte(catalog.String()), "docs/generated/requirements-traceability.md": []byte(trace.String())}, findings, nil
}
func artifactLinks(paths []string) string {
	if len(paths) == 0 {
		return "pending"
	}
	links := make([]string, 0, len(paths))
	for _, path := range paths {
		links = append(links, "["+path+"](../../"+path+")")
	}
	return strings.Join(links, ", ")
}
func validateCapabilities(root string, register CapabilityRegister, now time.Time) []Finding {
	findings := []Finding{}
	add := func(id, message string) {
		findings = append(findings, Finding{"docs/product/capability-register.json", "capability-evidence", id + ": " + message})
	}
	if (register.SchemaVersion != 1 && register.SchemaVersion != 2) || register.Owner == "" || register.Status == "" {
		add("register", "schema, owner and status are required")
	}
	configs := map[string]bool{}
	for _, c := range register.Configurations {
		if c.ID == "" || configs[c.ID] {
			add(c.ID, "duplicate or empty configuration ID")
		}
		configs[c.ID] = true
	}
	ids := map[string]bool{}
	for _, c := range register.Capabilities {
		if c.ID == "" || ids[c.ID] {
			add(c.ID, "duplicate or empty capability ID")
		}
		ids[c.ID] = true
		if register.SchemaVersion == 2 && (len(c.BusinessNeeds) == 0 || len(c.Personas) == 0 || len(c.Processes) == 0 || len(c.Controls) == 0 || len(register.CommonRequirements) == 0) {
			add(c.ID, "business, persona, process, control and shared requirement links are required")
		}
		if !contains([]string{"Experimental", "Preview", "Production", "Certified"}, c.Maturity) {
			add(c.ID, "invalid maturity")
		}
		if c.Owner == "" || len(c.Configurations) == 0 || len(c.Requirements) == 0 || len(c.Stages) == 0 {
			add(c.ID, "owner, configuration, requirements and work item are required")
		}
		for _, config := range c.Configurations {
			if !configs[config] {
				add(c.ID, "unknown configuration "+config)
			}
		}
		for label, value := range map[string]string{"last_verified": c.LastVerified, "review_by": c.ReviewBy} {
			if _, err := time.Parse("2006-01-02", value); err != nil {
				add(c.ID, "invalid "+label)
			}
		}
		if c.ReviewBy < now.Format("2006-01-02") {
			add(c.ID, "review expired")
		}
		if c.Maturity == "Production" || c.Maturity == "Certified" {
			if c.Approver == "" || len(c.ReleaseEvidence) == 0 || len(c.Design) == 0 || len(c.Tests) == 0 || len(c.Help) == 0 {
				add(c.ID, "Production/Certified requires approver and requirement/design/test/help/release evidence")
			}
			if register.Status != "approved" && register.Status != "active" {
				add(c.ID, "draft register cannot authorize Production/Certified")
			}
			covered := map[string]bool{}
			for _, ref := range c.ReleaseEvidence {
				path, _, _ := strings.Cut(ref, "#")
				full, err := docgen.Path(root, path)
				if err != nil {
					continue
				}
				body, err := os.ReadFile(full)
				if err != nil {
					continue
				}
				meta, _ := frontmatter(strings.ReplaceAll(string(body), "\r\n", "\n"))
				valid := meta["type"] == "record" && meta["result"] == "accepted" && meta["release"] == register.Release && meta["approved_by"] == c.Approver
				for _, key := range []string{"doc_id", "configuration", "approved_on", "approval_reference", "artifact_sha256"} {
					valid = valid && meta[key] != ""
				}
				if _, err := time.Parse("2006-01-02", meta["approved_on"]); err != nil {
					valid = false
				}
				if !valid {
					add(c.ID, "release evidence must be an accepted scoped record with matching release/approver and approval/artifact references")
					continue
				}
				covered[meta["configuration"]] = true
				if c.Maturity == "Certified" && meta["certification_reference"] == "" {
					add(c.ID, "Certified requires a qualified certification reference")
				}
			}
			for _, config := range c.Configurations {
				if !covered[config] {
					add(c.ID, "no accepted release evidence for "+config)
				}
			}
		}
		for _, group := range [][]string{c.Requirements, c.Design, c.Tests, c.Help, c.ReleaseEvidence, c.BusinessNeeds, c.Personas, c.Processes, c.Controls, register.CommonRequirements} {
			for _, ref := range group {
				path, fragment, _ := strings.Cut(ref, "#")
				full, err := docgen.Path(root, path)
				if err != nil {
					add(c.ID, "unsafe evidence path")
					continue
				}
				body, err := os.ReadFile(full)
				if err != nil {
					add(c.ID, "missing artifact "+path)
					continue
				}
				if fragment != "" && strings.HasSuffix(path, ".md") {
					_, content := frontmatter(strings.ReplaceAll(string(body), "\r\n", "\n"))
					if !headingIDs(stripCode(content), strings.HasPrefix(path, "docs/kb/"))[fragment] {
						add(c.ID, "missing requirement/evidence heading "+ref)
					}
				}
			}
		}
	}
	return findings
}

// Scan the normative requirement suite in reverse as well as checking the
// forward links, so adding a requirement cannot silently omit it from scope.
func validateReverseTrace(root string, register CapabilityRegister) []Finding {
	if register.SchemaVersion < 2 {
		return nil
	}
	linked := map[string]bool{}
	for _, c := range register.Capabilities {
		for _, group := range [][]string{register.CommonRequirements, c.BusinessNeeds, c.Requirements, c.Controls} {
			for _, ref := range group {
				linked[ref] = true
			}
		}
	}
	pattern := regexp.MustCompile(`(?m)^#{2,6}\s+((?:BR|FR|NFR|SEC|DATA|OPS)-[A-Z0-9-]+)\s*$`)
	var findings []Finding
	err := filepath.WalkDir(filepath.Join(root, "docs/requirements"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		meta, body := frontmatter(strings.ReplaceAll(string(raw), "\r\n", "\n"))
		if meta["type"] != "normative" {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		for _, match := range pattern.FindAllStringSubmatch(stripCode(body), -1) {
			ref := rel + "#" + strings.ToLower(match[1])
			if !linked[ref] {
				findings = append(findings, Finding{rel, "capability-evidence", "requirement has no reverse capability trace: " + match[1]})
			}
		}
		return nil
	})
	if err != nil {
		findings = append(findings, Finding{"docs/requirements", "capability-evidence", "cannot inventory requirement suite"})
	}
	return findings
}
