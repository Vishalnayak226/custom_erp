package releasegate

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// RecordSchema is bumped when Record changes shape.
const RecordSchema = 1

// Record is the release-candidate evidence record: one file that says what
// the candidate is and what was checked, bound to the exact commit and tree.
type Record struct {
	SchemaVersion int    `json:"schema_version"`
	GeneratedAt   string `json:"generated_at"`
	Version       string `json:"version,omitempty"`
	BaseCommit    string `json:"base_commit"` // the previous release: HEAD before this release commits
	Tree          string `json:"tree"`        // the exact tree G11 will commit
	Verdict       string `json:"verdict"`     // pass or fail
	Warnings      int    `json:"warnings"`

	Migrations    []Migration       `json:"migrations"`
	MigrationsSHA string            `json:"migrations_set_sha256,omitempty"`
	Artifacts     []Artifact        `json:"artifacts"`
	SBOM          SBOMRef           `json:"sbom"`
	Config        ConfigRef         `json:"config_contract"`
	Dependencies  DepsRef           `json:"dependencies"`
	Surface       []string          `json:"surface_high_risk,omitempty"`
	Results       map[string]string `json:"results"`
	Exceptions    []ExceptionView   `json:"exceptions"`
	Checks        []Check           `json:"checks"`
}

// Artifact is a hashed build output.
type Artifact struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size_bytes"`
}

// SBOMRef points at the manifest cmd/releasemanifest's library wrote.
type SBOMRef struct {
	File         string `json:"file,omitempty"`
	SHA256       string `json:"sha256,omitempty"`
	Dependencies int    `json:"dependencies"`
	Error        string `json:"error,omitempty"`
}

// ConfigRef is the config contract: the environment names the build reads.
type ConfigRef struct {
	EnvNames []string `json:"env_names"`
	SHA256   string   `json:"sha256"`
	Added    []string `json:"added_since_base,omitempty"`
	Removed  []string `json:"removed_since_base,omitempty"`
}

// DepsRef is the dependency and licence record.
type DepsRef struct {
	ByType     map[string]int `json:"by_type"`
	Licenses   map[string]int `json:"licenses"`
	NoLicense  []string       `json:"without_license,omitempty"`
	LedgerFile string         `json:"ledger"`
}

// Input is everything Evaluate needs; collect.go fills it from the tree.
type Input struct {
	Now        time.Time
	Version    string
	BaseCommit string
	Tree       string

	Migrations    []Migration
	MigrationsSHA string
	MigrationNote string
	Artifacts     []Artifact
	SBOM          SBOMRef
	Config        ConfigRef
	Deps          DepsRef
	Surface       Surface
	Results       map[string]string
	Vuln          Vuln

	Policy      Policy
	Exceptions  *ExceptionFile // nil: the register file is missing
	PublicKey   ed25519.PublicKey
	Evidence    map[string]*Evidence
	IsAncestor  func(string) bool
	LimitsExist bool
}

// Evaluate runs every rule and builds the record.
func Evaluate(in Input) Record {
	today := time.Date(in.Now.UTC().Year(), in.Now.UTC().Month(), in.Now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	rec := Record{
		SchemaVersion: RecordSchema, GeneratedAt: in.Now.UTC().Format(time.RFC3339), Version: in.Version,
		BaseCommit: in.BaseCommit, Tree: in.Tree, Migrations: in.Migrations, MigrationsSHA: in.MigrationsSHA,
		Artifacts: in.Artifacts, SBOM: in.SBOM, Config: in.Config, Dependencies: in.Deps, Results: in.Results,
	}
	if rec.Artifacts == nil {
		rec.Artifacts = []Artifact{}
	}
	add := func(c Check) { rec.Checks = append(rec.Checks, c) }

	add(CheckCommit(in.BaseCommit, in.Tree))
	add(CheckMigrations(in.Migrations, in.MigrationNote))

	// artifact
	if len(in.Artifacts) == 0 {
		add(Check{ID: "artifact", Requirement: "artifact checksum", Gate: "D3 (Deploy)", Status: Deferred,
			Detail: "the linux binary is built and signed by Deploy (D3, sha256 checked on the server); pass -artifact name=path to record one here"})
	} else {
		names := make([]string, len(in.Artifacts))
		for i, a := range in.Artifacts {
			names[i] = a.Name + "=" + short(a.SHA256)
		}
		add(Check{ID: "artifact", Requirement: "artifact checksum", Status: Pass, Detail: strings.Join(names, ", ")})
	}

	// sbom / provenance / dependency + licence record
	sbom := Check{ID: "sbom", Requirement: "SBOM and provenance", Status: Pass,
		Detail: fmt.Sprintf("%s (%d dependencies), sha256 %s", in.SBOM.File, in.SBOM.Dependencies, short(in.SBOM.SHA256))}
	if in.SBOM.Error != "" {
		sbom.Status, sbom.Detail = Fail, in.SBOM.Error
	}
	add(sbom)
	lic := Check{ID: "dependency_licenses", Requirement: "dependency and licence record", Status: Pass,
		Detail: fmt.Sprintf("%d licence kinds across the ledger (go.mod, go.sum, CI tools, vendored JS)", len(in.Deps.Licenses))}
	if len(in.Deps.NoLicense) > 0 {
		lic.Status = Fail
		lic.Detail = fmt.Sprintf("%d ledger entr%s with no licence: %s", len(in.Deps.NoLicense), map[bool]string{true: "y", false: "ies"}[len(in.Deps.NoLicense) == 1], strings.Join(firstN(in.Deps.NoLicense, 5), ", "))
	}
	add(lic)

	// config contract
	cfg := Check{ID: "config_contract", Requirement: "config contract", Status: Pass,
		Detail: fmt.Sprintf("%d environment names read by the build, sha256 %s", len(in.Config.EnvNames), short(in.Config.SHA256))}
	if len(in.Config.Added) > 0 {
		cfg.Detail += "; NEW since the previous release: " + strings.Join(in.Config.Added, ", ") + " (Deploy D1 checks secret keys on the host)"
	}
	if len(in.Config.EnvNames) == 0 {
		cfg.Status, cfg.Detail = Fail, "no environment names found - the scan itself is broken"
	}
	add(cfg)

	add(CheckSurface(in.Surface, in.Policy.SurfaceHighRisk))
	rec.Surface = firstN(in.Surface.High, 30)
	add(CheckResults(in.Results))
	add(CheckVuln(in.Vuln, in.Policy.Govulncheck))

	// exceptions
	var valid map[string]Exception
	if in.Exceptions == nil {
		add(Check{ID: "exceptions", Requirement: "zero P0 / zero unresolved P1", Status: Fail,
			Detail: "release/exceptions.json is missing - it is the register of open defects; an empty register is {\"schema_version\":1,\"exceptions\":[]}"})
	} else {
		r := CheckExceptions(*in.Exceptions, in.PublicKey, today)
		add(r.Check)
		valid = r.Valid
		rec.Exceptions = r.Views
	}
	if rec.Exceptions == nil {
		rec.Exceptions = []ExceptionView{}
	}

	// run evidence
	for _, k := range EvidenceKinds {
		covered := false
		for _, e := range valid {
			for _, cv := range e.Covers {
				if cv == k.Kind {
					covered = true
				}
			}
		}
		anc := in.IsAncestor
		if anc == nil {
			anc = func(string) bool { return false }
		}
		add(CheckEvidence(k.Kind, in.Evidence[k.Kind], in.Policy.Evidence[k.Kind], today, anc, covered))
	}
	add(CheckLimits(in.LimitsExist, in.Policy.SupportedLimits))

	rec.Verdict = "pass"
	for _, c := range rec.Checks {
		switch c.Status {
		case Fail:
			rec.Verdict = "fail"
		case Warn, NotRun:
			rec.Warnings++
		}
	}
	return rec
}

// Failures lists the failed checks as one line each.
func (r Record) Failures() []string {
	var out []string
	for _, c := range r.Checks {
		if c.Status == Fail {
			out = append(out, c.ID+": "+c.Detail)
		}
	}
	return out
}

// JSON encodes the record deterministically.
func (r Record) JSON() ([]byte, error) {
	if r.Config.EnvNames == nil {
		r.Config.EnvNames = []string{}
	}
	sort.Strings(r.Config.EnvNames)
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Markdown is the short human form of the record.
func (r Record) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Release-candidate record\n\n")
	fmt.Fprintf(&b, "- Verdict: **%s** (%d warning/not-run)\n- Version: %s\n- Base commit (previous release): `%s`\n- Tree (what G11 commits): `%s`\n- Generated: %s\n- Migrations: %d, set sha256 `%s`\n- SBOM: `%s` (%d dependencies)\n- Config contract: %d names, sha256 `%s`\n\n",
		strings.ToUpper(r.Verdict), r.Warnings, r.Version, r.BaseCommit, r.Tree, r.GeneratedAt, len(r.Migrations), short(r.MigrationsSHA), r.SBOM.File, r.SBOM.Dependencies, len(r.Config.EnvNames), short(r.Config.SHA256))
	b.WriteString("| Check | Requirement | Status | Detail |\n|---|---|---|---|\n")
	for _, c := range r.Checks {
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", c.ID, c.Requirement, c.Status, strings.ReplaceAll(c.Detail, "|", "/"))
	}
	if len(r.Exceptions) > 0 {
		b.WriteString("\nExceptions register:\n\n")
		for _, e := range r.Exceptions {
			fmt.Fprintf(&b, "- %s %s %q until %s: %s\n", e.ID, e.Severity, e.Title, e.Expires, e.Verdict)
		}
	}
	if len(r.Surface) > 0 {
		b.WriteString("\nHigh-risk surface changes:\n\n")
		for _, l := range r.Surface {
			fmt.Fprintf(&b, "- %s\n", l)
		}
	}
	return b.String()
}
