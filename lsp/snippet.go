package lsp

// Snippet support for completion items (insertTextFormat = 2). The syntax is the
// subset Fresh expands: tabstops ($1), tabstops with defaults, choices, the final
// cursor stop ($0), and the escapes \\ \$ \} \| in and out of placeholders.
// Nothing here tracks tabstop navigation: defaults are inserted as text and the
// caret goes to the final stop, like Fresh.
//
// Offsets are rune offsets so a CJK or emoji default moves the caret correctly;
// the editor's buffer.Pos counts runes too.

// SnippetExpansion is the plain text of an expanded snippet and the rune offset
// inside Text where the caret belongs (the final stop, or the end when absent).
type SnippetExpansion struct {
	Text         string
	CursorOffset int
}

// ExpandSnippet turns snippet text into plain text and the final caret offset.
// An item without the final stop places the caret after the text, and a later
// final stop wins over an earlier one.
func ExpandSnippet(snippet string) SnippetExpansion {
	runes := []rune(snippet)
	var out []rune
	cursor := -1
	for i := 0; i < len(runes); i++ {
		switch runes[i] {
		case '\\':
			if i+1 < len(runes) && (runes[i+1] == '$' || runes[i+1] == '\\' || runes[i+1] == '}') {
				out = append(out, runes[i+1])
				i++
				continue
			}
			out = append(out, '\\')
		case '$':
			if i+1 >= len(runes) {
				out = append(out, '$')
				continue
			}
			if runes[i+1] == '{' {
				expanded, final, consumed := parseBracedSnippet(runes[i+2:])
				if final {
					cursor = len(out)
				}
				out = append(out, expanded...)
				i += 1 + consumed
				continue
			}
			if isDigit(runes[i+1]) {
				j := i + 1
				for j < len(runes) && isDigit(runes[j]) {
					j++
				}
				if string(runes[i+1:j]) == "0" {
					cursor = len(out)
				}
				i = j - 1
				continue
			}
			out = append(out, '$')
		default:
			out = append(out, runes[i])
		}
	}
	if cursor < 0 {
		cursor = len(out)
	}
	return SnippetExpansion{Text: string(out), CursorOffset: cursor}
}

// parseBracedSnippet parses the content after an opening brace. It returns the
// expanded text, whether the placeholder is the final stop, and how many runes it
// consumed including the closing brace (to the end when unterminated).
func parseBracedSnippet(rest []rune) (expanded []rune, final bool, consumed int) {
	var content []rune
	depth := 1
	i := 0
	for ; i < len(rest); i++ {
		switch rest[i] {
		case '{':
			depth++
			content = append(content, '{')
		case '}':
			depth--
			if depth == 0 {
				i++
				goto parsed
			}
			content = append(content, '}')
		case '\\':
			if i+1 < len(rest) && (rest[i+1] == '$' || rest[i+1] == '\\' || rest[i+1] == '}' || rest[i+1] == '|') {
				content = append(content, rest[i+1])
				i++
				continue
			}
			content = append(content, '\\')
		default:
			content = append(content, rest[i])
		}
	}
parsed:
	text, final := parseSnippetPlaceholder(string(content))
	return []rune(text), final, i
}

// parseSnippetPlaceholder parses a tabstop number, a number with a default, or a
// number with choices. Only the first choice is inserted. A default may contain
// nested snippet syntax, which is expanded recursively (its own caret is ignored,
// like Fresh).
func parseSnippetPlaceholder(content string) (text string, final bool) {
	runes := []rune(content)
	digits := 0
	for digits < len(runes) && isDigit(runes[digits]) {
		digits++
	}
	number := string(runes[:digits])
	final = number == "0"
	if digits == len(runes) {
		return "", final
	}
	switch runes[digits] {
	case ':':
		defaults := string(runes[digits+1:])
		return ExpandSnippet(defaults).Text, final
	case '|':
		choices := trimTrailingPipes(string(runes[digits+1:]))
		first := choices
		for i, r := range choices {
			if r == ',' {
				first = choices[:i]
				break
			}
		}
		return first, final
	}
	return "", final
}

// trimTrailingPipes removes the closing pipes of a choices placeholder.
func trimTrailingPipes(s string) string {
	runes := []rune(s)
	for len(runes) > 0 && runes[len(runes)-1] == '|' {
		runes = runes[:len(runes)-1]
	}
	return string(runes)
}

// isDigit reports whether r is an ASCII digit.
func isDigit(r rune) bool { return r >= '0' && r <= '9' }
