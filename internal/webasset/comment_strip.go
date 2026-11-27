// Package webasset holds the one change the server makes to a public/ file
// between disk and the browser: it drops comments (item 283.1, ask 285).
//
// Measured 2026-11-26: whole-line // comments were 46.7 KB of app.js's
// 100.7 KB gzip, and CSS comments 15.5 KB of styles.css's 27.6 KB - about a
// third of what every browser downloaded before its first screen. The files
// on disk keep every comment; only the bytes sent change.
//
// What is dropped, and nothing else:
//   - in .js and .mjs, a line whose first characters (after spaces or tabs)
//     are // becomes an empty line. Comments after code on the same line and
//     /* */ blocks stay: they were under 0.3 KB of app.js's gzip.
//   - in .css, every /* */ comment. Its newlines stay.
//
// Line numbers in error reports therefore still match the source. When the
// scanner cannot be sure where code ends and a comment starts (a string or
// template it cannot close, brackets that do not balance) Strip returns an
// error and the caller sends the file exactly as it is on disk.
package webasset

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"strings"
)

// ErrNotStrippable is returned for any file that is not a script or stylesheet.
var ErrNotStrippable = errors.New("webasset: not a script or stylesheet")

// Strippable reports whether Strip changes files with this name.
func Strippable(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".js", ".mjs", ".css":
		return true
	}
	return false
}

// Strip returns body without its comments. On any error the caller sends body
// unchanged.
func Strip(name string, body []byte) ([]byte, error) {
	switch strings.ToLower(path.Ext(name)) {
	case ".js", ".mjs":
		return StripJS(body)
	case ".css":
		return StripCSS(body)
	}
	return nil, ErrNotStrippable
}

// unsure is the error every scanner refusal wraps.
func unsure(format string, args ...interface{}) error {
	return fmt.Errorf("webasset: left as is: "+format, args...)
}

// regexKeywords are the words after which a / starts a regular expression
// rather than a division (return /x/.test(s), typeof /x/, case /x/: ...).
var regexKeywords = map[string]bool{
	"return": true, "typeof": true, "instanceof": true, "in": true, "of": true,
	"new": true, "delete": true, "void": true, "throw": true, "case": true,
	"do": true, "else": true, "yield": true, "await": true,
}

func isWordByte(c byte) bool {
	return c == '_' || c == '$' || c >= 0x80 ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// StripJS empties every line of a script that holds only a // comment.
//
// It reads the script the way a JavaScript tokenizer does, as far as telling
// code from text needs: quoted strings, template literals with their ${}
// expressions, regular expressions, and comments. Brackets are balanced as it
// goes - a misread string or regular expression would throw them out - so a
// file it does not fully understand comes back as an error, never half
// stripped. Lines beginning //# or //@ (source map directives) are kept.
func StripJS(src []byte) ([]byte, error) {
	out := make([]byte, 0, len(src))
	// stack holds open ( [ { plus ` for a template literal and $ for a ${
	// expression inside one. Text inside a template is never stripped.
	var stack []byte
	var lastSig byte    // last significant character of code; 0 at the start
	var lastWord string // the word it ended, when it ended one
	lineStart := true
	n := len(src)
	top := func() byte {
		if len(stack) == 0 {
			return 0
		}
		return stack[len(stack)-1]
	}
	line := func(i int) int { return 1 + bytes.Count(src[:i], []byte{'\n'}) }

	for i := 0; i < n; {
		c := src[i]

		if top() == '`' { // template text
			switch {
			case c == '\\' && i+1 < n:
				out = append(out, c, src[i+1])
				i += 2
			case c == '`':
				stack = stack[:len(stack)-1]
				out = append(out, c)
				lastSig, lastWord = '`', ""
				i++
			case c == '$' && i+1 < n && src[i+1] == '{':
				stack = append(stack, '$')
				out = append(out, '$', '{')
				lastSig, lastWord = '{', ""
				i += 2
			default:
				out = append(out, c)
				i++
			}
			continue
		}

		if lineStart {
			lineStart = false
			j := i
			for j < n && (src[j] == ' ' || src[j] == '\t') {
				j++
			}
			if j+1 < n && src[j] == '/' && src[j+1] == '/' &&
				!(j+2 < n && (src[j+2] == '#' || src[j+2] == '@')) {
				for j < n && src[j] != '\n' {
					j++
				}
				i = j // the newline itself is copied next, so the line stays
				continue
			}
		}

		switch {
		case c == '\n':
			out = append(out, c)
			lineStart = true
			i++
		case c == ' ' || c == '\t' || c == '\r' || c == '\f' || c == '\v':
			out = append(out, c)
			i++
		case c == '/' && i+1 < n && src[i+1] == '/': // comment after code: kept
			j := i
			for j < n && src[j] != '\n' {
				j++
			}
			out = append(out, src[i:j]...)
			i = j
		case c == '/' && i+1 < n && src[i+1] == '*': // block comment: kept
			end := bytes.Index(src[i+2:], []byte("*/"))
			if end < 0 {
				return nil, unsure("unclosed /* comment from line %d", line(i))
			}
			j := i + 2 + end + 2
			out = append(out, src[i:j]...)
			i = j
		case c == '/' && regexAllowed(lastSig, lastWord):
			j, err := scanRegex(src, i)
			if err != nil {
				return nil, unsure("%v at line %d", err, line(i))
			}
			out = append(out, src[i:j]...)
			lastSig, lastWord = ')', "" // a regex is an operand: a / after it divides
			i = j
		case c == '\'' || c == '"':
			j, err := scanString(src, i)
			if err != nil {
				return nil, unsure("%v at line %d", err, line(i))
			}
			out = append(out, src[i:j]...)
			lastSig, lastWord = c, ""
			i = j
		case c == '`':
			stack = append(stack, '`')
			out = append(out, c)
			i++
		case c == '(' || c == '[' || c == '{':
			stack = append(stack, c)
			out = append(out, c)
			lastSig, lastWord = c, ""
			i++
		case c == ')' || c == ']' || c == '}':
			want := byte('(')
			if c == ']' {
				want = '['
			} else if c == '}' {
				want = '{'
			}
			t := top()
			switch {
			case c == '}' && t == '$': // end of a ${} expression: back to template text
				stack = stack[:len(stack)-1]
			case t == want:
				stack = stack[:len(stack)-1]
			default:
				return nil, unsure("unbalanced %q at line %d", c, line(i))
			}
			out = append(out, c)
			lastSig, lastWord = c, ""
			i++
		case isWordByte(c):
			j := i
			for j < n && isWordByte(src[j]) {
				j++
			}
			out = append(out, src[i:j]...)
			lastSig, lastWord = 'a', string(src[i:j])
			i = j
		default:
			out = append(out, c)
			lastSig, lastWord = c, ""
			i++
		}
	}
	if len(stack) != 0 {
		return nil, unsure("%d brackets or templates left open at the end", len(stack))
	}
	return out, nil
}

// regexAllowed reports whether a / after this token starts a regular
// expression. After a value (a name, a number, a string, a closing ) or ])
// it is a division; after an operator, an opening bracket, a block's } or
// one of regexKeywords it is a regular expression.
func regexAllowed(lastSig byte, lastWord string) bool {
	switch lastSig {
	case 'a':
		return regexKeywords[lastWord]
	case ')', ']', '\'', '"', '`':
		return false
	}
	return true
}

// scanRegex returns the index just past the regular expression starting at
// src[i] == '/', flags included.
func scanRegex(src []byte, i int) (int, error) {
	n := len(src)
	inClass := false
	for j := i + 1; j < n; j++ {
		switch src[j] {
		case '\\':
			j++
		case '\n', '\r':
			return 0, errors.New("regular expression runs past its line")
		case '[':
			inClass = true
		case ']':
			inClass = false
		case '/':
			if !inClass {
				j++
				for j < n && isWordByte(src[j]) {
					j++
				}
				return j, nil
			}
		}
	}
	return 0, errors.New("unclosed regular expression")
}

// scanString returns the index just past the quoted string starting at src[i].
// A string may continue on the next line only after a backslash.
func scanString(src []byte, i int) (int, error) {
	n := len(src)
	q := src[i]
	for j := i + 1; j < n; j++ {
		switch src[j] {
		case '\\':
			if j+2 < n && src[j+1] == '\r' && src[j+2] == '\n' {
				j += 2
			} else {
				j++
			}
		case '\n':
			return 0, errors.New("string runs past its line")
		case q:
			return j + 1, nil
		}
	}
	return 0, errors.New("unclosed string")
}

func isCSSNameByte(c byte) bool {
	return isWordByte(c) && c != '$' || c == '-'
}

// StripCSS removes every /* */ comment from a stylesheet. A comment that
// spanned lines leaves its newlines, so line numbers still match. Where a
// comment sat between two characters with no space around it (a/**/.b) it is
// sent as an empty /**/ instead, because removing it outright could join two
// tokens or change a selector.
func StripCSS(src []byte) ([]byte, error) {
	out := make([]byte, 0, len(src))
	n := len(src)
	line := func(i int) int { return 1 + bytes.Count(src[:i], []byte{'\n'}) }
	isSpace := func(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }

	for i := 0; i < n; {
		c := src[i]
		switch {
		case c == '/' && i+1 < n && src[i+1] == '*':
			end := bytes.Index(src[i+2:], []byte("*/"))
			if end < 0 {
				return nil, unsure("unclosed /* comment from line %d", line(i))
			}
			j := i + 2 + end + 2
			newlines := bytes.Count(src[i:j], []byte{'\n'})
			before := len(out) > 0 && !isSpace(out[len(out)-1])
			after := j < n && !isSpace(src[j])
			if before && after {
				out = append(out, '/', '*')
				out = append(out, bytes.Repeat([]byte{'\n'}, newlines)...)
				out = append(out, '*', '/')
			} else {
				out = append(out, bytes.Repeat([]byte{'\n'}, newlines)...)
			}
			i = j
		case c == '"' || c == '\'':
			j := i + 1
			for ; j < n && src[j] != c; j++ {
				if src[j] == '\\' {
					j++
				} else if src[j] == '\n' {
					return nil, unsure("string runs past its line at line %d", line(i))
				}
			}
			if j >= n {
				return nil, unsure("unclosed string from line %d", line(i))
			}
			out = append(out, src[i:j+1]...)
			i = j + 1
		case c == '\\' && i+1 < n:
			out = append(out, c, src[i+1])
			i += 2
		case (c == 'u' || c == 'U') && i+3 < n && strings.EqualFold(string(src[i:i+4]), "url(") &&
			(i == 0 || !isCSSNameByte(src[i-1])):
			// An unquoted url(...) may hold // or /* as plain text.
			j := i + 4
			for j < n && isSpace(src[j]) {
				j++
			}
			if j < n && (src[j] == '"' || src[j] == '\'') {
				out = append(out, src[i:j]...) // the quoted string is read next
				i = j
				continue
			}
			k := bytes.IndexByte(src[j:], ')')
			if k < 0 {
				return nil, unsure("unclosed url( from line %d", line(i))
			}
			out = append(out, src[i:j+k+1]...)
			i = j + k + 1
		default:
			out = append(out, c)
			i++
		}
	}
	return out, nil
}
