package syntax

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/alecthomas/chroma/v2"
)

// fixedTokens returns it with the fixups of lexer applied, or it itself for a lexer
// that has none, which keeps streaming.
func fixedTokens(lexer chroma.Lexer, it chroma.Iterator) chroma.Iterator {
	fs := fixups[lexer.Config().Name]
	if len(fs) == 0 {
		return it
	}
	toks := it.Tokens()
	for _, f := range fs {
		toks = f(toks)
	}
	return chroma.Literator(toks...)
}

// tokenizers maps a lexer name to a tokenizer that takes the place of chroma's lexer for
// that language. Markdown has one, because the chroma lexer reads a hard-wrapped
// paragraph character by character, loses the emphasis at a line start and makes a block
// quote italic, none of which a reader of markdown expects. Diff has one because its
// line backgrounds need one span per line.
var tokenizers = map[string]func(text []string, inside bool) [][]Span{}

// The markdown tokenizer lexes fenced code through Tokenize, so it is registered here and
// not in the literal, which would make the initialization of the table a cycle. Chroma
// names its diff lexer "Diff".
func init() {
	tokenizers["markdown"] = tokenizeMarkdown
	tokenizers["Diff"] = tokenizeDiff
}

var diffHunkHeader = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+\d+(?:,\d+)? @@`)

// tokenizeDiff gives every added and removed line the kinds that paint a full-row
// diff background. Chroma's diff lexer wraps a whole run of equal lines in one token,
// which is fine for a foreground-only style but not for a line background.
func tokenizeDiff(text []string, inside bool) [][]Span {
	out := make([][]Span, len(text))
	inHunk := false
	// A window from the middle of a document may start inside a hunk, so its text
	// has no header to say so. Its changed lines still carry their prefixes.
	inferred := inside
	oldLeft, newLeft := 0, 0
	for i, line := range text {
		if line == "" {
			continue
		}
		k := KindPlain
		if strings.HasPrefix(line, "diff --git ") {
			inHunk, inferred = false, false
		}
		if diffHunkHeader.MatchString(line) {
			oldLeft, newLeft, inHunk = diffHunkCounts(line)
			inferred = false
			k = KindKeyword
		}
		switch {
		case inHunk && strings.HasPrefix(line, "\\"):
			k = KindComment
		case inHunk && strings.HasPrefix(line, "+"):
			k = KindDiffAdd
			newLeft--
		case inHunk && strings.HasPrefix(line, "-"):
			k = KindDiffRemove
			oldLeft--
		case inHunk && strings.HasPrefix(line, " "):
			oldLeft--
			newLeft--
		case inferred && strings.HasPrefix(line, "\\"):
			k = KindComment
		case inferred && strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++ "):
			k = KindDiffAdd
		case inferred && strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "--- "):
			k = KindDiffRemove
		case !inHunk && !inferred && strings.HasPrefix(line, "diff "):
			k = KindKeyword
		case !inHunk && !inferred && strings.HasPrefix(line, "index "):
			k = KindKeyword
		}
		out[i] = []Span{{Start: 0, End: len(line), Kind: k}}
		if inHunk && oldLeft <= 0 && newLeft <= 0 {
			inHunk = false
		}
	}
	return out
}

// diffHunkCounts reads the old and new line counts of a unified diff hunk
// header ("@@ -1,3 +1,4 @@"). A missing count means one line.
func diffHunkCounts(line string) (oldCount, newCount int, ok bool) {
	fields := strings.Fields(line)
	if len(fields) < 3 || fields[0] != "@@" {
		return 0, 0, false
	}
	oldCount, okOld := diffRangeCount(fields[1], '-')
	newCount, okNew := diffRangeCount(fields[2], '+')
	return oldCount, newCount, okOld && okNew && (oldCount > 0 || newCount > 0)
}

// diffRangeCount reads one side of a hunk header range, such as "-1,3".
func diffRangeCount(field string, sign byte) (int, bool) {
	if len(field) < 2 || field[0] != sign {
		return 0, false
	}
	number, count, hasCount := strings.Cut(field[1:], ",")
	if _, err := strconv.Atoi(number); err != nil {
		return 0, false
	}
	if !hasCount {
		return 1, true
	}
	n, err := strconv.Atoi(count)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}
