package main

// The Knowledge Center's generated articles (Stage 39.16 / 39.17).
//
// Three of the Centre's reference pages are lists the code already owns: every
// error code, every registered report, and every country whose phone rules the
// application enforces. Hand-writing any of them would guarantee drift, and the
// repo's rule for that case is settled - generate it.
//
// These are written as Markdown into docs/kb/, not as HTML into
// internal/kb/content/, so genkb remains the only thing that renders an
// article. That keeps one anchor implementation, one search index, one access
// model, and one place where the article vocabulary is defined.

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"custom_erp/engines"
	"custom_erp/internal/kb"
	"custom_erp/internal/server"
)

// kbFrontmatter writes the header genkb parses. `last_verified` is the
// generation date on purpose: these articles are verified against the code
// every time they are produced, which is exactly what that field claims.
func kbFrontmatter(title, section string, order int, summary, audience, stamp string, screens []string) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("title: " + title + "\n")
	b.WriteString("section: " + section + "\n")
	b.WriteString("order: " + strconv.Itoa(order) + "\n")
	b.WriteString("summary: " + summary + "\n")
	b.WriteString("audience: " + audience + "\n")
	b.WriteString("last_verified: " + stamp + "\n")
	if len(screens) > 0 {
		b.WriteString("screens: [" + strings.Join(screens, ", ") + "]\n")
	}
	b.WriteString("---\n\n")
	b.WriteString("<!-- GENERATED ARTICLE - DO NOT EDIT BY HAND.\n")
	b.WriteString("     Regenerate: go run ./cmd/gendocs && go run ./cmd/genkb -->\n\n")
	return b.String()
}

// kbContents writes a contents list whose anchors are computed with the
// Knowledge Center's own heading-slug rule rather than GitHub's, because these
// links are followed inside the application, not on a repo page.
func kbContents(headings []string, suffix func(string) string) string {
	var b strings.Builder
	b.WriteString("## Contents\n\n")
	for _, heading := range headings {
		b.WriteString(fmt.Sprintf("- [%s](#%s)%s\n", heading, kb.HeadingSlug(heading), suffix(heading)))
	}
	b.WriteString("\n")
	return b.String()
}

func kbErrorCodeReference(stamp string) string {
	entries := server.CatalogEntries()

	byModule := map[string][]server.CatalogEntry{}
	for _, entry := range entries {
		module := entry.Module
		if module == "" {
			module = "Uncategorised"
		}
		byModule[module] = append(byModule[module], entry)
	}
	modules := sortedKeys(byModule)

	var b strings.Builder
	b.WriteString(kbFrontmatter(
		"Error code reference",
		"Troubleshooting",
		20,
		"Every code the application can show you, what causes it and what to do about it.",
		"everyone",
		stamp,
		nil))

	b.WriteString("# Error code reference\n\n")
	b.WriteString(fmt.Sprintf("Every refusal in this application carries a code. There are **%d** of them,\n"+
		"across **%d** areas. This page is produced from the running catalog, so it\n"+
		"cannot describe a code the application does not have, or miss one it does.\n\n",
		len(entries), len(modules)))
	b.WriteString("Search is usually faster than scrolling: type the code into the search box\n" +
		"at the top of the sidebar. If you have not met a code yet and want to know how\n" +
		"to read the dialog it appears in, start with\n" +
		"[Reading an error code](error-codes.md).\n\n")
	b.WriteString("> [!NOTE]\n" +
		"> A code with an HTTP status of 500 or above is an internal failure, not\n" +
		"> something you did. The detail line is deliberately withheld in that case -\n" +
		"> give your administrator the correlation id shown in the dialog instead.\n\n")

	b.WriteString(kbContents(modules, func(module string) string {
		return fmt.Sprintf(" - %d codes", len(byModule[module]))
	}))

	for _, module := range modules {
		rows := byModule[module]
		sort.Slice(rows, func(i, j int) bool { return rows[i].Code < rows[j].Code })
		b.WriteString(fmt.Sprintf("## %s\n\n", module))
		b.WriteString("| Code | When it happens | What you see | What to do | Severity | HTTP |\n")
		b.WriteString("|---|---|---|---|---|---|\n")
		for _, entry := range rows {
			b.WriteString(fmt.Sprintf("| `%s` | %s | %s | %s | %s | %d |\n",
				entry.Code, cell(entry.Scenario), cell(entry.UserMessage), cell(entry.UserAction), cell(entry.Severity), entry.HTTPStatus))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func kbReportCatalog(stamp string) string {
	definitions := engines.ListReportDefinitions()

	byCategory := map[string][]engines.ReportDefinition{}
	for _, definition := range definitions {
		category := definition.Category
		if category == "" {
			category = "Uncategorised"
		}
		byCategory[category] = append(byCategory[category], definition)
	}
	categories := sortedKeys(byCategory)

	var b strings.Builder
	b.WriteString(kbFrontmatter(
		"Report catalog",
		"Reference",
		20,
		"Every report you can run, what it asks for, what it returns and whether you can drill into it.",
		"store manager, finance, category manager, admin",
		stamp,
		[]string{"reports"}))

	b.WriteString("# Report catalog\n\n")
	b.WriteString(fmt.Sprintf("**%d** reports across **%d** categories, all reachable from\n"+
		"**Sales & Marketplace » Reports**.\n\n", len(definitions), len(categories)))
	b.WriteString("Pick one from the catalog list on that screen, fill in any parameter marked\n" +
		"**required**, and run it. Where a report supports drill-down, clicking a figure\n" +
		"opens the individual transactions behind it. **Export in Background** queues a\n" +
		"CSV for any report large enough to time out a normal request.\n\n")
	b.WriteString("Columns marked *sensitive* are masked as `•••` for roles other than Super\n" +
		"Admin and Store Manager. They are masked rather than dropped, so the table has\n" +
		"the same shape whoever runs it and a screenshot from one person lines up with a\n" +
		"screenshot from another.\n\n")

	b.WriteString(kbContents(categories, func(category string) string {
		return fmt.Sprintf(" - %d reports", len(byCategory[category]))
	}))

	for _, category := range categories {
		rows := byCategory[category]
		sort.Slice(rows, func(i, j int) bool { return rows[i].Label < rows[j].Label })
		b.WriteString(fmt.Sprintf("## %s\n\n", category))
		for _, definition := range rows {
			b.WriteString(fmt.Sprintf("### %s\n\n", definition.Label))
			b.WriteString(fmt.Sprintf("**Report id:** `%s`\n\n", definition.ID))
			if len(definition.Params) == 0 {
				b.WriteString("**Parameters:** none - it runs as soon as you pick it.\n\n")
			} else {
				b.WriteString("**Parameters:**\n\n")
				for _, param := range definition.Params {
					requirement := "optional"
					if param.Required {
						requirement = "**required**"
					}
					options := ""
					if param.Options != "" {
						options = fmt.Sprintf(", one of: %s", param.Options)
					}
					b.WriteString(fmt.Sprintf("- **%s** (`%s`) - %s, %s%s\n", param.Label, param.Key, param.Type, requirement, options))
				}
				b.WriteString("\n")
			}
			columns := make([]string, 0, len(definition.Columns))
			for _, column := range definition.Columns {
				if column.Sensitive {
					columns = append(columns, fmt.Sprintf("%s *(sensitive)*", column.Label))
				} else {
					columns = append(columns, column.Label)
				}
			}
			b.WriteString(fmt.Sprintf("**Columns:** %s\n\n", strings.Join(columns, " · ")))
			if definition.HasDrillDown {
				b.WriteString("**Drill-down:** yes - a row's **View Details** opens the transactions behind it.\n\n")
			} else {
				b.WriteString("**Drill-down:** no.\n\n")
			}
		}
	}
	return b.String()
}

func kbCountryPhoneRules(stamp string) string {
	countries := engines.PhoneCountries()
	sort.Slice(countries, func(i, j int) bool { return countries[i].Name < countries[j].Name })

	var b strings.Builder
	b.WriteString(kbFrontmatter(
		"Country codes and phone number rules",
		"Reference",
		30,
		"The countries the application knows, their dialling codes, and the phone number lengths each one accepts.",
		"admin, store manager",
		stamp,
		[]string{"configuration"}))

	b.WriteString("# Country codes and phone number rules\n\n")
	b.WriteString(fmt.Sprintf("The application enforces phone number shape per country. **%d** countries are\n"+
		"known. Your tenant's default is set once under **Settings » Configuration**, in\n"+
		"`localization.default_country`, and every phone field in the system follows it\n"+
		"unless the number itself carries a different dialling code.\n\n", len(countries)))

	b.WriteString("## How a number is read\n\n")
	b.WriteString("Two separate things happen, and it is worth knowing which is which:\n\n")
	b.WriteString("**Cleaning** always happens and never rejects anything. Spaces, hyphens, dots,\n" +
		"brackets and unicode dashes are removed; a leading `+91` or `0091` style prefix is\n" +
		"resolved to its country; a national trunk prefix such as a leading `0` is dropped.\n\n")
	b.WriteString("**Validation** is then a policy the screen applies to the cleaned result.\n" +
		"Master records are strict - a Customer must have a number that fits its country.\n" +
		"Orders are not: a sales order records and tags whatever number it is given and\n" +
		"never refuses an order over it, because a contact number cannot tell you whether\n" +
		"an order can be fulfilled.\n\n")
	b.WriteString("> [!NOTE]\n" +
		"> A dialling code is only stripped when what remains is a valid length for that\n" +
		"> country. A real Indian mobile number beginning `91` therefore survives intact\n" +
		"> rather than being read as the country code and truncated.\n\n")

	b.WriteString("## The table\n\n")
	b.WriteString("**Accepted lengths** is the number of digits after the country code and trunk\n" +
		"prefix are removed. **Trunk prefix** is the digit callers dial before a national\n" +
		"number, which the application removes if present.\n\n")
	b.WriteString("| Country | Code | Dials | Accepted lengths | Trunk prefix | Example |\n")
	b.WriteString("|---|---|---|---|---|---|\n")
	for _, country := range countries {
		lengths := make([]string, 0, len(country.Lengths))
		for _, length := range country.Lengths {
			lengths = append(lengths, strconv.Itoa(length))
		}
		trunk := country.TrunkPrefix
		if trunk == "" {
			trunk = "none"
		}
		b.WriteString(fmt.Sprintf("| %s | `%s` | +%s | %s | `%s` | %s |\n",
			cell(country.Name), country.ISO2, country.DialCode,
			strings.Join(lengths, " or "), trunk, cell(country.Example)))
	}
	b.WriteString("\n")

	b.WriteString("## Currencies are not on this list, and why\n\n")
	b.WriteString("Country and phone rules are fixed facts about the world, so the application\n" +
		"carries them in code. Currencies are not: which currencies you trade in, what you\n" +
		"call them, how many decimals you round to and what you are willing to accept as\n" +
		"an exchange rate are all business decisions. They are therefore **records you\n" +
		"create**, under **Setup » Currency** and **Setup » Exchange Rate**, not a list\n" +
		"shipped with the software. Use the ISO 4217 three-letter code as the currency's\n" +
		"code - `INR`, `USD`, `AED` - so that anything you later import or export lines\n" +
		"up.\n\n")
	b.WriteString("Indian states are the same kind of thing and live on the records that need\n" +
		"them: a GSTIN's first two digits are the state code, which is how the application\n" +
		"decides whether a sale is intra-state or inter-state without asking anyone.\n")
	return b.String()
}
