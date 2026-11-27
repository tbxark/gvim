package syntax

import (
	"html"
	"strings"
)

// MarkdownBlockImage returns the destination of an image that occupies its own
// Markdown line. Callers exclude lines inside fenced or literal blocks.
func MarkdownBlockImage(line string) string {
	s := strings.TrimLeft(line, " ")
	if len(line)-len(s) > 3 {
		return ""
	}
	s = strings.TrimRight(s, " \t")
	if !strings.HasPrefix(s, "![") {
		return ""
	}
	close := matchBracket(s, 1, len(s))
	if close < 0 || close+2 >= len(s) || s[close+1] != '(' || s[len(s)-1] != ')' {
		return ""
	}
	dest := strings.TrimSpace(s[close+2 : len(s)-1])
	if rest, ok := strings.CutPrefix(dest, "<"); ok {
		path, title, found := strings.Cut(rest, ">")
		if found && (title == "" || imageLinkTitle(title)) {
			return unescapeImageLink(path)
		}
		return ""
	}
	depth, end := 0, len(dest)
	for i := 0; i < len(dest); i++ {
		switch dest[i] {
		case '\\':
			i++
		case '(':
			depth++
		case ')':
			if depth == 0 {
				return ""
			}
			depth--
		case ' ', '\t':
			if depth != 0 || !imageLinkTitle(dest[i:]) {
				return ""
			}
			end, i = i, len(dest)
		}
	}
	if depth != 0 {
		return ""
	}
	return unescapeImageLink(dest[:end])
}

func imageLinkTitle(s string) bool {
	if s[0] != ' ' && s[0] != '\t' {
		return false
	}
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return false
	}
	switch s[0] {
	case '"', '\'':
		return s[len(s)-1] == s[0]
	case '(':
		return matchParen(s, 0, len(s)) == len(s)-1
	}
	return false
}

func unescapeImageLink(s string) string {
	var out strings.Builder
	out.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && isASCIIPunct(s[i+1]) {
			i++
		}
		out.WriteByte(s[i])
	}
	return html.UnescapeString(out.String())
}
