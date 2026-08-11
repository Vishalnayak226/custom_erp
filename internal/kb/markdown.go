// Package kb contains the dependency-free build primitives for the Stage 39
// Knowledge Center. Rendering happens at build time; browsers receive inert,
// escaped HTML rather than executing a Markdown parser over repository text.
package kb

import (
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var orderedItemPattern = regexp.MustCompile(`^([0-9]+)[.]\s+(.+)$`)

type renderer struct {
	slugs map[string]int
}

// RenderMarkdown renders the intentionally small Knowledge Center Markdown
// vocabulary: headings, paragraphs, ordered/unordered lists, tables, fenced
// code, links, emphasis, images and GitHub-style admonitions. Raw HTML is
// always escaped and unsafe URL schemes are replaced with #.
func RenderMarkdown(source string) string {
	r := &renderer{slugs: map[string]int{}}
	source = strings.ReplaceAll(source, "\r\n", "\n")
	source = strings.ReplaceAll(source, "\r", "\n")
	lines := strings.Split(source, "\n")
	var out strings.Builder
	for i := 0; i < len(lines); {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			i++
			continue
		}

		if strings.HasPrefix(line, "```") {
			language := safeLanguage(strings.TrimSpace(strings.TrimPrefix(line, "```")))
			i++
			var code []string
			for i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "```") {
				code = append(code, lines[i])
				i++
			}
			if i < len(lines) {
				i++
			}
			out.WriteString("<pre><code")
			if language != "" {
				out.WriteString(` class="language-` + language + `"`)
			}
			out.WriteString(">")
			out.WriteString(html.EscapeString(strings.Join(code, "\n")))
			out.WriteString("</code></pre>\n")
			continue
		}

		if level, text, ok := heading(line); ok {
			slug := r.uniqueSlug(text)
			out.WriteString("<h" + strconv.Itoa(level) + ` id="` + html.EscapeString(slug) + `">`)
			out.WriteString(renderInline(text))
			out.WriteString(` <a class="kb-anchor" href="#` + html.EscapeString(slug) + `" aria-label="Copy link to this section">#</a>`)
			out.WriteString("</h" + strconv.Itoa(level) + ">\n")
			i++
			continue
		}

		if kind, ok := admonitionKind(line); ok {
			i++
			var body []string
			for i < len(lines) {
				trimmed := strings.TrimSpace(lines[i])
				if !strings.HasPrefix(trimmed, ">") {
					break
				}
				body = append(body, strings.TrimSpace(strings.TrimPrefix(trimmed, ">")))
				i++
			}
			out.WriteString(`<aside class="kb-admonition kb-admonition-` + kind + `" role="note"><p class="kb-admonition-title">`)
			out.WriteString(admonitionLabel(kind))
			out.WriteString("</p><p>")
			out.WriteString(renderInline(strings.Join(body, " ")))
			out.WriteString("</p></aside>\n")
			continue
		}

		if i+1 < len(lines) && isTableDelimiter(lines[i+1]) {
			head := splitTableRow(lines[i])
			i += 2
			out.WriteString("<table><thead><tr>")
			for _, cell := range head {
				out.WriteString("<th>" + renderInline(cell) + "</th>")
			}
			out.WriteString("</tr></thead><tbody>")
			for i < len(lines) && strings.Contains(lines[i], "|") && strings.TrimSpace(lines[i]) != "" {
				cells := splitTableRow(lines[i])
				out.WriteString("<tr>")
				for column := range head {
					value := ""
					if column < len(cells) {
						value = cells[column]
					}
					out.WriteString("<td>" + renderInline(value) + "</td>")
				}
				out.WriteString("</tr>")
				i++
			}
			out.WriteString("</tbody></table>\n")
			continue
		}

		if _, ok := unorderedItem(line); ok {
			out.WriteString("<ul>")
			for i < len(lines) {
				item, itemOK := unorderedItem(strings.TrimSpace(lines[i]))
				if !itemOK {
					break
				}
				out.WriteString("<li>" + renderInline(item) + "</li>")
				i++
			}
			out.WriteString("</ul>\n")
			continue
		}

		if _, ok := orderedItem(line); ok {
			out.WriteString("<ol>")
			for i < len(lines) {
				item, itemOK := orderedItem(strings.TrimSpace(lines[i]))
				if !itemOK {
					break
				}
				out.WriteString("<li>" + renderInline(item) + "</li>")
				i++
			}
			out.WriteString("</ol>\n")
			continue
		}

		var paragraph []string
		for i < len(lines) {
			trimmed := strings.TrimSpace(lines[i])
			if trimmed == "" || (len(paragraph) > 0 && startsBlock(lines, i)) {
				break
			}
			paragraph = append(paragraph, trimmed)
			i++
		}
		out.WriteString("<p>" + renderInline(strings.Join(paragraph, " ")) + "</p>\n")
	}
	return out.String()
}

func startsBlock(lines []string, index int) bool {
	line := strings.TrimSpace(lines[index])
	if strings.HasPrefix(line, "```") {
		return true
	}
	if _, _, ok := heading(line); ok {
		return true
	}
	if _, ok := admonitionKind(line); ok {
		return true
	}
	if _, ok := unorderedItem(line); ok {
		return true
	}
	if _, ok := orderedItem(line); ok {
		return true
	}
	return index+1 < len(lines) && isTableDelimiter(lines[index+1])
}

func heading(line string) (int, string, bool) {
	level := 0
	for level < len(line) && level < 6 && line[level] == '#' {
		level++
	}
	if level == 0 || level >= len(line) || line[level] != ' ' {
		return 0, "", false
	}
	text := strings.TrimSpace(line[level:])
	return level, text, text != ""
}

func unorderedItem(line string) (string, bool) {
	if len(line) >= 3 && (line[0] == '-' || line[0] == '*' || line[0] == '+') && line[1] == ' ' {
		return strings.TrimSpace(line[2:]), true
	}
	return "", false
}

func orderedItem(line string) (string, bool) {
	match := orderedItemPattern.FindStringSubmatch(line)
	if len(match) == 3 {
		return strings.TrimSpace(match[2]), true
	}
	return "", false
}

func splitTableRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	parts := strings.Split(line, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func isTableDelimiter(line string) bool {
	cells := splitTableRow(line)
	if len(cells) == 0 {
		return false
	}
	for _, cell := range cells {
		cell = strings.TrimSpace(strings.Trim(cell, ":"))
		if len(cell) < 3 || strings.Trim(cell, "-") != "" {
			return false
		}
	}
	return true
}

func admonitionKind(line string) (string, bool) {
	line = strings.ToUpper(strings.TrimSpace(line))
	for _, kind := range []string{"NOTE", "TIP", "IMPORTANT", "WARNING", "CAUTION"} {
		if line == "> [!"+kind+"]" {
			return strings.ToLower(kind), true
		}
	}
	return "", false
}

func admonitionLabel(kind string) string {
	labels := map[string]string{
		"note": "Note", "tip": "Tip", "important": "Important",
		"warning": "Warning", "caution": "Caution",
	}
	return labels[kind]
}

func safeLanguage(language string) string {
	var out strings.Builder
	for _, r := range strings.ToLower(language) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func (r *renderer) uniqueSlug(text string) string {
	var slug strings.Builder
	lastDash := false
	for _, char := range strings.ToLower(text) {
		if unicode.IsLetter(char) || unicode.IsDigit(char) {
			slug.WriteRune(char)
			lastDash = false
		} else if slug.Len() > 0 && !lastDash {
			slug.WriteByte('-')
			lastDash = true
		}
	}
	base := strings.Trim(slug.String(), "-")
	if base == "" {
		base = "section"
	}
	r.slugs[base]++
	if r.slugs[base] == 1 {
		return base
	}
	return base + "-" + strconv.Itoa(r.slugs[base])
}

func renderInline(input string) string {
	var out strings.Builder
	for len(input) > 0 {
		if strings.HasPrefix(input, "![") {
			if closeAlt := strings.Index(input[2:], "]("); closeAlt >= 0 {
				closeAlt += 2
				if closeURL := strings.Index(input[closeAlt+2:], ")"); closeURL >= 0 {
					closeURL += closeAlt + 2
					alt := input[2:closeAlt]
					url := safeURL(input[closeAlt+2 : closeURL])
					out.WriteString(`<img src="` + html.EscapeString(url) + `" alt="` + html.EscapeString(alt) + `" loading="lazy">`)
					input = input[closeURL+1:]
					continue
				}
			}
		}
		if strings.HasPrefix(input, "[") {
			if closeText := strings.Index(input[1:], "]("); closeText >= 0 {
				closeText++
				if closeURL := strings.Index(input[closeText+2:], ")"); closeURL >= 0 {
					closeURL += closeText + 2
					label := input[1:closeText]
					url := safeURL(input[closeText+2 : closeURL])
					out.WriteString(`<a href="` + html.EscapeString(url) + `">` + renderInline(label) + `</a>`)
					input = input[closeURL+1:]
					continue
				}
			}
		}
		if strings.HasPrefix(input, "`") {
			if end := strings.Index(input[1:], "`"); end >= 0 {
				end++
				out.WriteString("<code>" + html.EscapeString(input[1:end]) + "</code>")
				input = input[end+1:]
				continue
			}
		}
		if strings.HasPrefix(input, "**") {
			if end := strings.Index(input[2:], "**"); end >= 0 {
				end += 2
				out.WriteString("<strong>" + renderInline(input[2:end]) + "</strong>")
				input = input[end+2:]
				continue
			}
			out.WriteString("**")
			input = input[2:]
			continue
		}
		if strings.HasPrefix(input, "*") {
			if end := strings.Index(input[1:], "*"); end > 0 {
				end++
				out.WriteString("<em>" + renderInline(input[1:end]) + "</em>")
				input = input[end+1:]
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(input)
		if r == utf8.RuneError && size == 1 {
			out.WriteString("&#xfffd;")
		} else {
			out.WriteString(html.EscapeString(input[:size]))
		}
		input = input[size:]
	}
	return out.String()
}

func safeURL(raw string) string {
	value := strings.TrimSpace(raw)
	parsed, err := url.Parse(value)
	if err != nil {
		return "#"
	}
	switch strings.ToLower(parsed.Scheme) {
	case "", "http", "https", "mailto":
		return value
	default:
		return "#"
	}
}
