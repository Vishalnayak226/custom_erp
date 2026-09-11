package kb

// Manuals reuse the KB renderer and content. They are repository reading/print
// projections, never additional embedded server assets or independently edited books.
import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path"
	"regexp"
	"strings"
)

type Manual struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Audience string   `json:"audience"`
	Owner    string   `json:"owner"`
	Topics   []string `json:"topics"`
}

type ManualSelection struct {
	SchemaVersion int      `json:"schema_version"`
	Manuals       []Manual `json:"manuals"`
}

// BuildManuals returns HTML filenames relative to docs/user. Every selected
// topic must exist exactly once. No approval or verification date is inferred.
func BuildManuals(result *BuildResult, sourceRoot, selectionPath, release string) (map[string][]byte, error) {
	// Keep documentation-only regular expressions out of server package init.
	manualID := regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	manualAttr := regexp.MustCompile(`\b(id|href|src)="([^"]*)"`)
	manualHeading := regexp.MustCompile(`</?h([1-6])\b`)
	manualSourceLink := regexp.MustCompile(`\]\(([^)]+)\)`)
	data, err := os.ReadFile(selectionPath)
	if err != nil {
		return nil, err
	}
	var selection ManualSelection
	if err := json.Unmarshal(data, &selection); err != nil {
		return nil, err
	}
	if selection.SchemaVersion != 1 || len(selection.Manuals) == 0 {
		return nil, fmt.Errorf("manual selection must have schema_version 1 and at least one manual")
	}
	bySlug := map[string]Article{}
	for _, article := range result.Articles {
		bySlug[article.Slug] = article
	}
	files := map[string][]byte{}
	for _, manual := range selection.Manuals {
		if !manualID.MatchString(manual.ID) || manual.Title == "" || manual.Audience == "" || manual.Owner == "" || len(manual.Topics) == 0 {
			return nil, fmt.Errorf("manual needs a safe id, title, audience, owner and topics")
		}
		name := manual.ID + ".html"
		if _, exists := files[name]; exists {
			return nil, fmt.Errorf("duplicate manual %s", manual.ID)
		}
		selected := map[string]bool{}
		for _, slug := range manual.Topics {
			if _, exists := bySlug[slug]; !exists {
				return nil, fmt.Errorf("manual %s: unknown topic %s", manual.ID, slug)
			}
			if selected[slug] {
				return nil, fmt.Errorf("manual %s: repeated topic %s", manual.ID, slug)
			}
			selected[slug] = true
		}
		var out strings.Builder
		out.WriteString("<!doctype html>\n<html lang=\"en\"><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width, initial-scale=1\"><title>" + html.EscapeString(manual.Title) + "</title><style>" + manualCSS + "</style></head><body>\n")
		out.WriteString("<a class=\"skip\" href=\"#manual-content\">Skip to topics</a><header><h1>" + html.EscapeString(manual.Title) + "</h1>\n")
		out.WriteString("<p>Audience: " + html.EscapeString(manual.Audience) + ". Content owner: " + html.EscapeString(manual.Owner) + ". Source release: " + html.EscapeString(strings.TrimSpace(release)) + ".</p>\n")
		out.WriteString("<p><strong>Draft reading and print edition.</strong> Generated from canonical Knowledge Center topics. Individual source verification dates are retained; generation does not establish release acceptance. Check the <a href=\"../generated/capability-catalog.md\">capability catalog</a> for configuration limits. Some linked references require the repository or the signed-in Knowledge Center.</p></header>\n<nav aria-label=\"Manual contents\"><h2>Contents</h2><ol>\n")
		for _, slug := range manual.Topics {
			out.WriteString("<li><a href=\"#topic-" + slug + "\">" + html.EscapeString(bySlug[slug].Title) + "</a></li>\n")
		}
		out.WriteString("</ol></nav><main id=\"manual-content\">\n")
		for _, slug := range manual.Topics {
			article := bySlug[slug]
			raw, err := os.ReadFile(path.Join(sourceRoot, article.SourcePath))
			if err != nil {
				return nil, err
			}
			// Resolve links outside the KB from their actual authored path. The KB
			// renderer normalizes all Markdown links to /help/<basename>.
			sourceLinks := map[string]string{}
			for _, match := range manualSourceLink.FindAllStringSubmatch(string(raw), -1) {
				u := safeURL(match[1])
				if strings.HasPrefix(articleURL(u), "/help/") {
					sourceLinks[articleURL(u)] = path.Clean(path.Join("../kb", path.Dir(article.SourcePath), u))
				}
			}
			body := manualAttr.ReplaceAllStringFunc(article.HTML, func(attr string) string {
				parts := manualAttr.FindStringSubmatch(attr)
				key, value := parts[1], html.UnescapeString(parts[2])
				switch {
				case key == "id":
					value = slug + "--" + value
				case key == "href" && strings.HasPrefix(value, "#"):
					value = "#" + slug + "--" + strings.TrimPrefix(value, "#")
				case key == "href" && strings.HasPrefix(value, "/help/"):
					dest, anchor, _ := strings.Cut(strings.TrimPrefix(value, "/help/"), "#")
					if selected[dest] {
						value = "#topic-" + dest
						if anchor != "" {
							value = "#" + dest + "--" + anchor
						}
					} else if target, ok := bySlug[dest]; ok {
						value = "../kb/" + target.SourcePath
						if anchor != "" {
							value += "#" + anchor
						}
					} else if original, ok := sourceLinks[value]; ok {
						value = original
					}
				case key == "src" && !strings.Contains(value, ":") && !strings.HasPrefix(value, "/"):
					value = path.Clean(path.Join("../kb", path.Dir(article.SourcePath), value))
				}
				return key + "=\"" + html.EscapeString(value) + "\""
			})
			body = manualHeading.ReplaceAllStringFunc(body, func(tag string) string {
				level := tag[len(tag)-1]
				if level < '6' {
					level++
				}
				return tag[:len(tag)-1] + string(level)
			})
			// Wide tables and code blocks remain reachable with a keyboard.
			body = strings.ReplaceAll(body, "<table>", "<table tabindex=\"0\" aria-label=\""+html.EscapeString(article.Title)+" reference table\">")
			body = strings.ReplaceAll(body, "<pre>", "<pre tabindex=\"0\">")
			out.WriteString("<article id=\"topic-" + slug + "\"><p class=\"provenance\"><a href=\"../kb/" + html.EscapeString(article.SourcePath) + "\">Canonical topic</a> · Source last verified: " + html.EscapeString(article.LastVerified) + "</p>\n" + body + "</article>\n")
		}
		out.WriteString("</main></body></html>\n")
		files[name] = []byte(out.String())
	}
	return files, nil
}

const manualCSS = `:root{color-scheme:light dark}body{font:1rem/1.6 system-ui,sans-serif;max-width:72rem;margin:auto;padding:1.5rem;overflow-wrap:anywhere}a{text-underline-offset:.18em}a:focus-visible{outline:3px solid currentColor;outline-offset:3px}.skip{display:block}table{border-collapse:collapse;display:block;overflow-x:auto}td,th{border:1px solid #888;padding:.45rem;text-align:left}pre{overflow:auto;padding:1rem;border:1px solid #888}img{max-width:100%;height:auto}article{margin-block:3rem;border-top:2px solid #888;padding-top:1rem}.provenance{font-size:.9rem}blockquote{border-left:3px solid #888;margin-left:0;padding-left:1rem}@media print{body{font-size:10pt;color:#000;background:#fff}article{break-before:page}h2,h3,h4{break-after:avoid}table{display:table;font-size:9pt}pre{white-space:pre-wrap;overflow-wrap:anywhere}.skip,.kb-anchor{display:none}a{color:inherit}}`
