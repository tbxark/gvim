// Package search finds and replaces text in a buffer.Buffer. It has no UI:
// callers build a Query, get matches in document order, and drive navigation
// and replacement themselves.
//
// Matching uses the standard library regexp package (RE2), so no cgo is
// involved. Literal queries are escaped with regexp.QuoteMeta. Whole-word
// matching follows the legacy Fresh editor, which wraps the pattern in \b and
// judges word characters by Unicode rules (see isWordRune).
package search

import (
	"errors"
	"fmt"
	"regexp"
	"regexp/syntax"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/tbxark/gvim/internal/buffer"
)

// Query describes what to find.
type Query struct {
	// Text is the literal string to find, or the regular expression when
	// Regex is set. An empty Text matches nothing.
	Text string
	// CaseSensitive makes matching case-sensitive. When false, letters match
	// regardless of case.
	CaseSensitive bool
	// WholeWord keeps only matches that are not adjacent to a word character.
	WholeWord bool
	// Regex treats Text as an RE2 regular expression instead of literal text.
	Regex bool
}

// Match is one occurrence of a Query in a buffer. Start is inclusive and End
// is exclusive, both in the document coordinates at the time of the search.
type Match struct {
	Start, End buffer.Pos
}

// Find returns every match of q in b, in document order. It reads the whole
// buffer, so a large buffer is refused with ErrLargeBuffer; use FindNext,
// FindVisible or Scan there. Matches do not overlap. A regex can give empty matches, such as ^ or x*, except right where
// the previous match ended. An empty query returns no matches
// and no error. A regex that does not compile returns an error.
func Find(b *buffer.Buffer, q Query) ([]Match, error) {
	r, err := run(b, q)
	if err != nil {
		return nil, err
	}
	if len(r.hits) == 0 {
		return nil, nil
	}
	out := make([]Match, len(r.hits))
	for i, h := range r.hits {
		out[i] = h.Match
	}
	return out, nil
}

// hit is a match together with what replacement needs: its byte offsets in
// the document text and the submatch offsets of the same search.
type hit struct {
	Match
	start, end int
	groups     []int
}

// result is one search run: the document snapshot it was run on, the compiled
// pattern, and the matches found in that snapshot.
type result struct {
	doc  document
	re   *regexp.Regexp
	hits []hit
}

// run searches b for q. It returns an empty result for an empty query. A large
// buffer is refused before any text is read, because newDocument would read
// every line of it.
func run(b *buffer.Buffer, q Query) (result, error) {
	if b.Large() {
		return result{}, ErrLargeBuffer
	}
	if q.Text == "" {
		return result{}, nil
	}
	re, err := compile(q)
	if err != nil {
		return result{}, err
	}
	d := newDocument(b)
	r := result{doc: d, re: re}
	for _, loc := range d.locate(re, q) {
		r.hits = append(r.hits, d.hit(loc))
	}
	return r, nil
}

// compileCacheSize is how many compiled queries are kept. The find bar asks for the
// highlights of the visible rows on every frame, and each ask would compile the
// query again, which for a long query takes longer than the frame.
const compileCacheSize = 4

// compiled is the result of compiling a query, kept with the query it is for. An error
// is kept too, so a query that does not compile is not tried again on every frame.
type compiled struct {
	q  Query
	re *regexp.Regexp
	// err is the error compile gives for q.
	err error
}

var (
	compileMu    sync.Mutex
	compileCache []compiled // the most recent first
)

// compile builds the regexp for q, from the cache when q was compiled lately. A
// *regexp.Regexp is safe for concurrent use, so the scan of a large document, which
// runs in another goroutine, shares it.
func compile(q Query) (*regexp.Regexp, error) {
	compileMu.Lock()
	for i, c := range compileCache {
		if c.q == q {
			copy(compileCache[1:i+1], compileCache[:i])
			compileCache[0] = c
			compileMu.Unlock()
			return c.re, c.err
		}
	}
	compileMu.Unlock()
	re, err := compileQuery(q)
	compileMu.Lock()
	compileCache = append([]compiled{{q: q, re: re, err: err}}, compileCache...)
	if len(compileCache) > compileCacheSize {
		compileCache = compileCache[:compileCacheSize]
	}
	compileMu.Unlock()
	return re, err
}

// maxErrorQuery is the longest query text that an error message quotes. A longer
// one (a large selection used as the search) would make the message as large as the
// selection, and the message is measured on every frame it is shown.
const maxErrorQuery = 200

// compileQuery builds the regexp for q. Literal text is escaped, so it is never
// interpreted as a pattern.
func compileQuery(q Query) (*regexp.Regexp, error) {
	pattern := q.Text
	if !q.Regex {
		pattern = regexp.QuoteMeta(pattern)
	}
	// "m" makes ^ and $ match at line breaks, as in the legacy editor.
	flags := "m"
	if !q.CaseSensitive {
		flags += "i"
	}
	// As in the legacy editor, "." crosses lines only when a regex asks for a
	// newline, so a multi-line query is written as one.
	if q.Regex && strings.Contains(q.Text, "\n") {
		flags += "s"
	}
	re, err := regexp.Compile("(?" + flags + ")" + pattern)
	if err != nil {
		if len(q.Text) > maxErrorQuery {
			// The cause quotes the pattern too, so only its code is kept.
			var se *syntax.Error
			if errors.As(err, &se) {
				return nil, fmt.Errorf("search: invalid pattern (%d bytes): %s", len(q.Text), se.Code)
			}
			return nil, fmt.Errorf("search: invalid pattern (%d bytes)", len(q.Text))
		}
		return nil, fmt.Errorf("search: invalid pattern %q: %w", q.Text, err)
	}
	return re, nil
}

// document is the buffer text as one string, with the byte offset where each
// line starts, so byte offsets from regexp convert to buffer positions. first is
// the buffer line number of the first line of text: 0 for a whole buffer, more
// for a window of it (see newWindow).
type document struct {
	text      string
	lineStart []int
	first     int
}

func newDocument(b *buffer.Buffer) document {
	var sb strings.Builder
	starts := make([]int, b.Lines())
	for i := range starts {
		if i > 0 {
			sb.WriteByte('\n')
		}
		starts[i] = sb.Len()
		sb.WriteString(b.Line(i))
	}
	return document{text: sb.String(), lineStart: starts}
}

// pos converts a byte offset in d.text to a buffer position. Columns count
// runes, as buffer.Pos does.
func (d document) pos(off int) buffer.Pos {
	line := sort.Search(len(d.lineStart), func(i int) bool {
		return d.lineStart[i] > off
	}) - 1
	return buffer.Pos{Line: d.first + line, Col: utf8.RuneCountInString(d.text[d.lineStart[line]:off])}
}

// hit builds a hit from submatch offsets of d.text. loc[0:2] is the whole match.
func (d document) hit(loc []int) hit {
	return hit{
		Start: d.pos(loc[0]), End: d.pos(loc[1]),
		start:  loc[0],
		end:    loc[1],
		groups: loc,
	}
}

// locate returns the offsets of the matches of q in d, in order, as submatch
// index slices whose first two entries are the match bounds.
func (d document) locate(re *regexp.Regexp, q Query) [][]int {
	if q.Regex {
		all := re.FindAllStringSubmatchIndex(d.text, -1)
		kept := all[:0]
		for _, loc := range all {
			s, e := loc[0], loc[1]
			if q.WholeWord && !isWholeWord(d.text, s, e) {
				continue
			}
			kept = append(kept, loc)
		}
		return kept
	}

	// A literal has no anchors or \b, so searching from an offset is exact.
	// After a rejected whole-word candidate, the scan resumes one rune past
	// its start, so a valid match that overlaps it is still found. This is
	// what the legacy \b(?:...)\b pattern does.
	var out [][]int
	for pos := 0; pos < len(d.text); {
		loc := re.FindStringIndex(d.text[pos:])
		if loc == nil {
			break
		}
		s, e := pos+loc[0], pos+loc[1]
		if !q.WholeWord || isWholeWord(d.text, s, e) {
			out = append(out, []int{s, e})
			pos = e
			continue
		}
		_, size := utf8.DecodeRuneInString(d.text[s:])
		pos = s + size
	}
	return out
}

// isWholeWord reports whether text[s:e] has word boundaries on both sides,
// which is the \b test of the legacy \b(?:P)\b pattern. A boundary sits
// between two runes when exactly one of them is a word character.
func isWholeWord(text string, s, e int) bool {
	return wordBefore(text, s) != wordAt(text, s) && wordBefore(text, e) != wordAt(text, e)
}

// wordAt reports whether the rune starting at byte i is a word character.
// Past the end of text it reports false.
func wordAt(text string, i int) bool {
	if i >= len(text) {
		return false
	}
	r, _ := utf8.DecodeRuneInString(text[i:])
	return isWordRune(r)
}

// wordBefore reports whether the rune ending at byte i is a word character.
// At the start of text it reports false.
func wordBefore(text string, i int) bool {
	if i <= 0 {
		return false
	}
	r, _ := utf8.DecodeLastRuneInString(text[:i])
	return isWordRune(r)
}

// isWordRune is the word-character rule of the legacy editor, which uses
// regex_syntax::is_word_character (refs/fresh/crates/fresh-editor-core/src/
// primitives/word_navigation.rs). That is Alphabetic, Mark, Nd, Pc and
// Join_Control. Alphabetic is the union of L*, Nl and Other_Alphabetic.
func isWordRune(r rune) bool {
	return unicode.IsLetter(r) ||
		unicode.Is(unicode.Nl, r) ||
		unicode.Is(unicode.Other_Alphabetic, r) ||
		unicode.IsMark(r) ||
		unicode.Is(unicode.Nd, r) ||
		unicode.Is(unicode.Pc, r) ||
		r == 0x200C || r == 0x200D
}

// expansion returns the text that replaces h. A literal query replaces with
// repl unchanged. A regex query expands $1, ${name} and the like from the
// captures of h, after reading \n, \t, \r and \\ as characters.
func (r result) expansion(h hit, q Query, repl string) string {
	if !q.Regex {
		return repl
	}
	groups := make([]int, len(h.groups))
	for i, g := range h.groups {
		if g < 0 {
			groups[i] = -1
			continue
		}
		groups[i] = g - h.start
	}
	repl = normalizeReplacement(interpretEscapes(repl))
	return string(r.re.ExpandString(nil, repl, r.doc.text[h.start:h.end], groups))
}

// interpretEscapes turns \n, \t, \r and \\ in a replacement template into the
// characters they stand for, as Fresh does. Any other escape, and a trailing
// lone backslash, pass through unchanged.
func interpretEscapes(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 == len(s) {
			sb.WriteByte(s[i])
			continue
		}
		switch s[i+1] {
		case 'n':
			sb.WriteByte('\n')
		case 't':
			sb.WriteByte('\t')
		case 'r':
			sb.WriteByte('\r')
		case '\\':
			sb.WriteByte('\\')
		default:
			sb.WriteByte('\\')
			continue
		}
		i++
	}
	return sb.String()
}

// normalizeReplacement rewrites $N as ${N}, so a number followed by letters
// ("$1px") is group 1 and not a group named "1px". "$$" stays a literal dollar.
func normalizeReplacement(s string) string {
	if !strings.Contains(s, "$") {
		return s
	}
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '$' || i+1 == len(s) {
			sb.WriteByte(s[i])
			continue
		}
		if s[i+1] == '$' {
			sb.WriteString("$$")
			i++
			continue
		}
		j := i + 1
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		if j == i+1 {
			sb.WriteByte('$')
			continue
		}
		sb.WriteString("${" + s[i+1:j] + "}")
		i = j - 1
	}
	return sb.String()
}
