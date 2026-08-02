// Command gendocs generates the three reference appendices in docs/guides/
// that must never be hand-written, because a hand-written copy of a list the
// code owns drifts the moment anyone touches the code - which is exactly the
// failure Stage 30.3 spent a whole pass correcting.
//
//	ERROR_CODES.md       - from internal/server's error catalog
//	REPORT_CATALOG.md    - from engines' report registry
//	PERMISSION_MATRIX.md - from the live role_permissions table
//
// Run it with:
//
//	go run ./cmd/gendocs                      # the two that need no database
//	go run ./cmd/gendocs -db "postgres://..."  # all three
//
// PERMISSION_MATRIX.md is skipped with a warning rather than failing the run
// if no database is reachable - the other two are the ones that go stale.
//
// Windows note: Controlled Folder Access refuses writes under Documents\ from
// an unrecognised binary and reports it as "the system cannot find the file
// specified". Same failure and workaround as cmd/brainmap - generate into
// %TEMP% with -out and copy in with PowerShell.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"custom_erp/db"
	"custom_erp/engines"
	"custom_erp/internal/server"
)

func main() {
	out := flag.String("out", filepath.Join("docs", "guides"), "directory to write the generated docs into")
	connStr := flag.String("db", "", "database connection string for PERMISSION_MATRIX.md (skipped if empty)")
	flag.Parse()

	stamp := time.Now().Format("2006-01-02")

	writeOut(filepath.Join(*out, "ERROR_CODES.md"), errorCodesDoc(stamp))
	writeOut(filepath.Join(*out, "REPORT_CATALOG.md"), reportCatalogDoc(stamp))

	if *connStr == "" {
		fmt.Println("  [skip] PERMISSION_MATRIX.md - pass -db to generate it")
		return
	}
	body, err := permissionMatrixDoc(stamp, *connStr)
	if err != nil {
		fmt.Printf("  [skip] PERMISSION_MATRIX.md - %v\n", err)
		return
	}
	writeOut(filepath.Join(*out, "PERMISSION_MATRIX.md"), body)
}

func writeOut(path, body string) {
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		fmt.Printf("  [fail] %s: %v\n", path, err)
		if strings.Contains(err.Error(), "cannot find the file specified") {
			fmt.Println("         On Windows this is usually Controlled Folder Access blocking an")
			fmt.Println("         unrecognised binary from writing under Documents\\, not a missing")
			fmt.Println("         directory. Generate into a TEMP directory with -out and copy in")
			fmt.Println("         with PowerShell, as docs/guides/update-guides.ps1 does.")
		}
		os.Exit(1)
	}
	fmt.Printf("  [ok]   %s (%d bytes)\n", path, len(body))
}

// generatedHeader is the same warning on all three files. Anyone editing one
// by hand loses the edit on the next run, so say so at the top.
func generatedHeader(title, stamp, source, regen string) string {
	return fmt.Sprintf("# %s\n\n"+
		"<!-- GENERATED FILE - DO NOT EDIT BY HAND.\n"+
		"     Source: %s\n"+
		"     Regenerate: %s -->\n\n"+
		"> **Generated %s.** This page is produced from %s, so it cannot drift from\n"+
		"> the running system. Hand edits are lost on the next run - change the source instead.\n\n",
		title, source, regen, stamp, source)
}

func errorCodesDoc(stamp string) string {
	var b strings.Builder
	b.WriteString(generatedHeader(
		"Error Code Reference",
		stamp,
		"`internal/server`'s error catalog (`error_catalog_generated.go`)",
		"`go run ./cmd/gendocs`"))

	entries := server.CatalogEntries()
	b.WriteString(fmt.Sprintf("Every error dialog in the app shows a code like `GLOBAL-0001`. Look it up here.\n\n"+
		"There are **%d** codes. Each row says what the user is shown, what to do about\n"+
		"it, and how serious it is.\n\n"+
		"**How to read an error dialog** - it has up to three lines: the *headline* (the\n"+
		"catalog's User Message, below), the *detail* (the specific field or value that\n"+
		"failed, written by the engine that rejected it), and the *action* (the User\n"+
		"Action column, below). The detail line is usually the one that tells you what to\n"+
		"fix. See [USER_GUIDE](USER_GUIDE.md) §12.\n\n", len(entries)))

	byModule := map[string][]server.CatalogEntry{}
	for _, e := range entries {
		mod := e.Module
		if mod == "" {
			mod = "Uncategorised"
		}
		byModule[mod] = append(byModule[mod], e)
	}
	modules := sortedKeys(byModule)

	b.WriteString("## Contents\n\n")
	for _, m := range modules {
		b.WriteString(fmt.Sprintf("- [%s](#%s) - %d codes\n", m, anchor(m), len(byModule[m])))
	}
	b.WriteString("\n---\n")

	for _, m := range modules {
		rows := byModule[m]
		sort.Slice(rows, func(i, j int) bool { return rows[i].Code < rows[j].Code })
		b.WriteString(fmt.Sprintf("\n## %s\n\n", m))
		b.WriteString("| Code | When it happens | What you see | What to do | Severity | HTTP |\n")
		b.WriteString("|---|---|---|---|---|---|\n")
		for _, e := range rows {
			b.WriteString(fmt.Sprintf("| `%s` | %s | %s | %s | %s | %d |\n",
				e.Code, cell(e.Scenario), cell(e.UserMessage), cell(e.UserAction), cell(e.Severity), e.HTTPStatus))
		}
	}
	b.WriteString("\n---\n\n*A code with an HTTP 5xx status is an internal failure. The detail line is " +
		"deliberately withheld from the response in that case - give your administrator the " +
		"correlation ID shown in the dialog instead.*\n")
	return b.String()
}

func reportCatalogDoc(stamp string) string {
	var b strings.Builder
	b.WriteString(generatedHeader(
		"Report Catalog",
		stamp,
		"`engines`' report registry (`report_definitions.go`)",
		"`go run ./cmd/gendocs`"))

	defs := engines.ListReportDefinitions()
	byCat := map[string][]engines.ReportDefinition{}
	for _, d := range defs {
		cat := d.Category
		if cat == "" {
			cat = "Uncategorised"
		}
		byCat[cat] = append(byCat[cat], d)
	}
	cats := sortedKeys(byCat)

	b.WriteString(fmt.Sprintf("Every report reachable from **Sales & Marketplace -> Reports**. There are **%d**\n"+
		"across **%d** categories.\n\n"+
		"Pick one from the catalog list on that screen, fill in any parameter marked\n"+
		"**required**, and run it. Where a report supports **drill-down**, clicking a\n"+
		"figure opens the individual transactions behind it. **Export in Background**\n"+
		"queues a CSV for any report large enough to time out a normal request.\n\n"+
		"Some columns are masked for roles other than HR/Admin and Store Manager - those\n"+
		"are marked *sensitive* below, and render as `•••` rather than being dropped, so the\n"+
		"table has the same shape whoever runs it.\n\n", len(defs), len(cats)))

	b.WriteString("## Contents\n\n")
	for _, c := range cats {
		b.WriteString(fmt.Sprintf("- [%s](#%s) - %d reports\n", c, anchor(c), len(byCat[c])))
	}
	b.WriteString("\n---\n")

	for _, c := range cats {
		rows := byCat[c]
		sort.Slice(rows, func(i, j int) bool { return rows[i].Label < rows[j].Label })
		b.WriteString(fmt.Sprintf("\n## %s\n\n", c))
		for _, d := range rows {
			b.WriteString(fmt.Sprintf("### %s\n\n", d.Label))
			b.WriteString(fmt.Sprintf("**Report id:** `%s`\n\n", d.ID))

			if len(d.Params) == 0 {
				b.WriteString("**Parameters:** none - it runs as soon as you pick it.\n\n")
			} else {
				b.WriteString("**Parameters:**\n\n")
				for _, p := range d.Params {
					req := "optional"
					if p.Required {
						req = "**required**"
					}
					opts := ""
					if p.Options != "" {
						opts = fmt.Sprintf(", one of: %s", p.Options)
					}
					b.WriteString(fmt.Sprintf("- **%s** (`%s`) - %s, %s%s\n", p.Label, p.Key, p.Type, req, opts))
				}
				b.WriteString("\n")
			}

			cols := make([]string, 0, len(d.Columns))
			for _, col := range d.Columns {
				if col.Sensitive {
					cols = append(cols, fmt.Sprintf("%s *(sensitive)*", col.Label))
				} else {
					cols = append(cols, col.Label)
				}
			}
			b.WriteString(fmt.Sprintf("**Columns:** %s\n\n", strings.Join(cols, " · ")))

			if d.HasDrillDown {
				b.WriteString("**Drill-down:** yes - a row's **View Details** opens the transactions behind it.\n\n")
			} else {
				b.WriteString("**Drill-down:** no.\n\n")
			}
		}
	}
	return b.String()
}

func permissionMatrixDoc(stamp, connStr string) (string, error) {
	db.InitDB(connStr)
	if db.DB == nil {
		return "", fmt.Errorf("no database connection")
	}

	rows, err := db.DB.Query(`SELECT role, doctype_name, allow_read, allow_create, allow_update, allow_delete
	                            FROM tenant_default.role_permissions ORDER BY doctype_name, role`)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	type grant struct{ read, create, update, del bool }
	matrix := map[string]map[string]grant{} // doctype -> role -> grant
	roleSet := map[string]bool{}
	for rows.Next() {
		var role, doctype string
		var g grant
		if err := rows.Scan(&role, &doctype, &g.read, &g.create, &g.update, &g.del); err != nil {
			return "", err
		}
		if matrix[doctype] == nil {
			matrix[doctype] = map[string]grant{}
		}
		matrix[doctype][role] = g
		roleSet[role] = true
	}

	roles := make([]string, 0, len(roleSet))
	for r := range roleSet {
		roles = append(roles, r)
	}
	sort.Strings(roles)

	doctypes := make([]string, 0, len(matrix))
	for d := range matrix {
		doctypes = append(doctypes, d)
	}
	sort.Strings(doctypes)

	var b strings.Builder
	b.WriteString(generatedHeader(
		"Permission Matrix",
		stamp,
		"the tenant's own `role_permissions` table",
		"`go run ./cmd/gendocs -db \"postgres://...\"`"))

	b.WriteString(fmt.Sprintf("What each role may do with each record type, read from the default tenant's\n"+
		"grants: **%d record types across %d roles.**\n\n"+
		"**HR/Admin is not listed** - it always has full access to everything and needs no\n"+
		"grant rows. A role with **no row at all** for a record type has **no access to\n"+
		"it**: this system fails closed, so a missing grant is a denial, never a default\n"+
		"allow.\n\n"+
		"Legend: **R** read - **C** create - **U** update - **D** delete - `-` none\n\n"+
		"An administrator changes any of this on **Settings -> Roles** (ADMIN_SOP §A.2).\n"+
		"Since Stage 30.5.7 the app also *hides* what a role cannot do - no **New** or\n"+
		"**Bulk Import** button without create, no row **Edit**/**Delete** icons without\n"+
		"update/delete - so this table also predicts what each role actually sees.\n\n",
		len(doctypes), len(roles)))

	b.WriteString("| Record type |")
	for _, r := range roles {
		b.WriteString(" " + r + " |")
	}
	b.WriteString("\n|---|")
	for range roles {
		b.WriteString("---|")
	}
	b.WriteString("\n")

	for _, d := range doctypes {
		b.WriteString("| **" + d + "** |")
		for _, r := range roles {
			g, ok := matrix[d][r]
			if !ok {
				b.WriteString(" - |")
				continue
			}
			var flags []string
			if g.read {
				flags = append(flags, "R")
			}
			if g.create {
				flags = append(flags, "C")
			}
			if g.update {
				flags = append(flags, "U")
			}
			if g.del {
				flags = append(flags, "D")
			}
			if len(flags) == 0 {
				b.WriteString(" - |")
				continue
			}
			b.WriteString(" " + strings.Join(flags, " ") + " |")
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}

func sortedKeys[T any](m map[string][]T) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// cell makes a value safe to drop into a Markdown table cell.
func cell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if s == "" {
		return "-"
	}
	return s
}

// anchor mirrors GitHub's heading-anchor rules closely enough for the
// contents links in these files.
func anchor(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ', r == '-':
			b.WriteRune('-')
		}
	}
	return b.String()
}
