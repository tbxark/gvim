package syntax

import (
	"regexp"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// The markdown tokenizer. It reads blocks line by line and the inline marks of each
// line, and gives Fresh's colours to what they hold: headings, strong text, emphasis,
// inline code, links, block quotes, list markers, tables and fenced code, whose
// content is lexed as the language its fence names. The kinds that carry an attribute
// (bold, italic, underline, strike-through) are in kind.go.
//
// Inline marks are read within one line. A mark that is opened on a line and closed on
// the next one is not found, which is what a hard-wrapped paragraph does to a long
// emphasis. A fenced block, an HTML comment and a front-matter block do span lines.

var (
	mdFence      = regexp.MustCompile("^( *)(`{3,}|~{3,})[ \\t]*([^`]*)$")
	mdHeading    = regexp.MustCompile(`^ {0,3}#{1,6}(?:[ \t]|$)`)
	mdQuote      = regexp.MustCompile(`^ {0,3}>`)
	mdList       = regexp.MustCompile(`^( *)([-+*]|\d{1,9}[.)])([ \t]+|$)`)
	mdTask       = regexp.MustCompile(`^\[[ xX]\](?:[ \t]|$)`)
	mdSetext     = regexp.MustCompile(`^ {0,3}(?:=+|-+)[ \t]*$`)
	mdTableRule  = regexp.MustCompile(`^ {0,3}\|?[ \t]*:?-+:?[ \t]*(?:\|[ \t]*:?-+:?[ \t]*)*\|?[ \t]*$`)
	mdDefinition = regexp.MustCompile(`^( {0,3})(\[[^\]]+\]:)([ \t]*)(<[^>]*>|\S*)`)
	mdAutolink   = regexp.MustCompile(`^<(?:[A-Za-z][A-Za-z0-9+.-]{1,31}:[^\s<>]*|[^\s@<>]+@[^\s<>]+)>`)
	mdHTMLTag    = regexp.MustCompile(`^</?[A-Za-z][A-Za-z0-9-]*(?:\s[^<>]*)?/?>`)
	mdBareURL    = regexp.MustCompile(`^(?:https?://|www\.)[^\s<>]*`)
)

// frontMatterLines is how far down a document its front matter may end.
const frontMatterLines = 200

// tokenizeMarkdown returns the spans of the lines of a markdown document, or of a piece from the
// middle of one when inside is set, which cannot start with front matter.
func tokenizeMarkdown(text []string, inside bool) [][]Span {
	t := mdTokenizer{text: text, raw: make([][]Span, len(text)), inside: inside}
	t.run()
	out := make([][]Span, len(text))
	for i, ln := range text {
		out[i] = fillGaps(t.raw[i], len(ln))
	}
	return out
}

// mdTokenizer holds the state of the block pass.
type mdTokenizer struct {
	text []string
	raw  [][]Span
	// inList is set after a list item, until a line at the margin that is not one ends
	// it. A line indented four blanks is a code block only outside a list.
	inList bool
	// inTable is set from a table's header row to its last row.
	inTable bool
	// inside says the text is a piece from the middle of a document.
	inside bool
}

// add records a span of line i.
func (t *mdTokenizer) add(i, from, to int, k Kind) {
	if k != KindPlain && to > from {
		t.raw[i] = appendSpan(t.raw[i], Span{Start: from, End: to, Kind: k})
	}
}

func (t *mdTokenizer) run() {
	i := 0
	if end := t.frontMatterEnd(); end > 0 {
		t.addFrontMatter(end)
		i = end + 1
	}
	prevBlank := true
	for i < len(t.text) {
		s := strings.TrimRight(t.text[i], "\r")
		switch {
		case strings.TrimSpace(s) == "":
			t.inTable = false
			prevBlank = true
			i++
			continue
		case mdFence.MatchString(s):
			i = t.fence(i)
		case strings.HasPrefix(strings.TrimLeft(s, " "), "<!--") && len(s)-len(strings.TrimLeft(s, " ")) < 4:
			i = t.htmlComment(i)
		case (strings.HasPrefix(s, "    ") || strings.HasPrefix(s, "\t")) && !t.inList && prevBlank:
			t.add(i, 0, len(s), KindString)
			i++
			// Further indented lines, blank ones between them included, belong to the block.
			for i < len(t.text) {
				n := strings.TrimRight(t.text[i], "\r")
				if strings.HasPrefix(n, "    ") || strings.HasPrefix(n, "\t") {
					t.add(i, 0, len(n), KindString)
					i++
					continue
				}
				break
			}
			continue
		case mdHeading.MatchString(s):
			t.inList = false
			t.add(i, len(s)-len(strings.TrimLeft(s, " ")), len(s), KindHeading)
			i++
		case isThematicBreak(s):
			t.inList = false
			t.add(i, len(s)-len(strings.TrimLeft(s, " ")), len(s), KindOperator)
			i++
		case mdQuote.MatchString(s):
			t.inList = false
			t.quote(i, s)
			i++
		case mdList.MatchString(s):
			t.listItem(i, s)
			i++
		case mdDefinition.MatchString(s) && !strings.HasPrefix(strings.TrimLeft(s, " "), "[ ]"):
			t.definition(i, s)
			i++
		default:
			i = t.paragraph(i, s)
		}
		prevBlank = false
	}
}

// isThematicBreak reports whether s is a line of three or more "*", "-" or "_", which may
// be spaced apart, and nothing else.
func isThematicBreak(s string) bool {
	body := strings.TrimLeft(s, " ")
	if len(s)-len(body) > 3 || body == "" || strings.IndexByte("*_-", body[0]) < 0 {
		return false
	}
	n := 0
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case body[0]:
			n++
		case ' ', '\t':
		default:
			return false
		}
	}
	return n >= 3
}

// frontMatterEnd returns the index of the line that closes the YAML front matter that
// opens the document, or 0 when there is none.
func (t *mdTokenizer) frontMatterEnd() int {
	if t.inside || len(t.text) < 2 || strings.TrimRight(t.text[0], " \t\r") != "---" {
		return 0
	}
	for i := 1; i < len(t.text) && i <= frontMatterLines; i++ {
		if s := strings.TrimRight(t.text[i], " \t\r"); s == "---" || s == "..." {
			return i
		}
	}
	return 0
}

// addFrontMatter lexes the lines between the front matter's fences as YAML.
func (t *mdTokenizer) addFrontMatter(end int) {
	t.add(0, 0, len(strings.TrimRight(t.text[0], "\r")), KindOperator)
	t.add(end, 0, len(strings.TrimRight(t.text[end], "\r")), KindOperator)
	if lex := lexers.Get("yaml"); lex != nil && end > 1 {
		if spans, err := Tokenize(lex, t.text[1:end]); err == nil {
			copy(t.raw[1:end], spans)
		}
	}
}

// fence handles the fenced code block that opens on line i and returns the line after
// it. The block runs to the closing fence, or to the end of the document. Its lines
// are lexed as the language the info string names, and are strings when there is none.
func (t *mdTokenizer) fence(i int) int {
	t.inList = false
	s := strings.TrimRight(t.text[i], "\r")
	m := mdFence.FindStringSubmatch(s)
	marker, info := m[2], strings.TrimSpace(m[3])
	t.add(i, len(m[1]), len(s), KindString)
	end := len(t.text)
	for j := i + 1; j < len(t.text); j++ {
		if closesFence(strings.TrimRight(t.text[j], "\r"), marker) {
			end = j
			break
		}
	}
	body := t.text[i+1 : end]
	if lex := fenceLexer(info); lex != nil && len(body) > 0 {
		if spans, err := Tokenize(lex, body); err == nil {
			copy(t.raw[i+1:end], spans)
		}
	} else {
		for k, ln := range body {
			t.add(i+1+k, 0, len(strings.TrimRight(ln, "\r")), KindString)
		}
	}
	if end < len(t.text) {
		s := strings.TrimRight(t.text[end], "\r")
		t.add(end, len(s)-len(strings.TrimLeft(s, " ")), len(s), KindString)
		return end + 1
	}
	return end
}

// closesFence reports whether line closes a fence opened with marker: three or more of
// its character, at least as many as the opener, and nothing else.
func closesFence(line, marker string) bool {
	s := strings.TrimRight(strings.TrimLeft(line, " "), " \t")
	return len(s) >= len(marker) && strings.Trim(s, marker[:1]) == ""
}

// fenceLexer returns the lexer that the info string of a fence names, or nil. The
// language is the first word, which may be written "go", "{.python}", "js {1,3}" or
// "language-go". A name that is plain text has no lexer: its lines are strings.
func fenceLexer(info string) chroma.Lexer {
	word := strings.TrimLeft(info, "{. \t")
	if k := strings.IndexAny(word, " \t{},="); k >= 0 {
		word = word[:k]
	}
	word = strings.TrimPrefix(strings.ToLower(word), "language-")
	if word == "" {
		return nil
	}
	lex := lexers.Get(word)
	if lex == nil {
		lex = lexers.Match("x." + word)
	}
	if lex == nil || lex.Config().Name == "plaintext" {
		return nil
	}
	return lex
}

func (t *mdTokenizer) htmlComment(i int) int {
	start := len(t.text[i]) - len(strings.TrimLeft(t.text[i], " "))
	for j := i; j < len(t.text); j++ {
		from := 0
		if j == i {
			from = start
		}
		s := strings.TrimRight(t.text[j], "\r")
		if k := strings.Index(s[from:], "-->"); k >= 0 {
			t.add(j, from, from+k+3, KindComment)
			if rest := from + k + 3; rest < len(s) {
				t.inline(j, s, rest, len(s), KindPlain)
			}
			return j + 1
		}
		t.add(j, from, len(s), KindComment)
	}
	return len(t.text)
}

// quote paints the markers and the text of a block quote line.
func (t *mdTokenizer) quote(i int, s string) {
	pos := 0
	for pos < len(s) {
		k := pos
		for k < len(s) && s[k] == ' ' {
			k++
		}
		if k < len(s) && s[k] == '>' {
			t.add(i, k, k+1, KindQuote)
			pos = k + 1
			continue
		}
		break
	}
	t.inline(i, s, pos, len(s), KindQuote)
}

func leadingBlanks(s string) int { return len(s) - len(strings.TrimLeft(s, " \t")) }

// listItem paints the marker of a list item, its task box and the text after them.
func (t *mdTokenizer) listItem(i int, s string) {
	t.inList = true
	m := mdList.FindStringSubmatchIndex(s)
	t.add(i, m[4], m[5], KindOperator)
	pos := m[1]
	if loc := mdTask.FindStringIndex(s[pos:]); loc != nil {
		t.add(i, pos, pos+3, KindOperator)
		pos += 3
	}
	t.inline(i, s, pos, len(s), KindPlain)
}

// definition paints a link reference definition, "[label]: url "title"".
func (t *mdTokenizer) definition(i int, s string) {
	m := mdDefinition.FindStringSubmatchIndex(s)
	t.add(i, m[4], m[5], KindString)
	t.add(i, m[8], m[9], KindLink)
	if m[9] < len(s) {
		t.add(i, m[9], len(s), KindString)
	}
}

// paragraph paints a line of text, as a table row, as a setext heading when an
// underline follows, or as plain text, and returns the next line to read.
func (t *mdTokenizer) paragraph(i int, s string) int {
	t.inList = t.inList && leadingBlanks(s) > 0
	next := ""
	if i+1 < len(t.text) {
		next = strings.TrimRight(t.text[i+1], "\r")
	}
	if strings.Contains(s, "|") && (t.inTable || strings.Contains(next, "|") && mdTableRule.MatchString(next)) {
		t.tableRow(i, s)
		t.inTable = true
		return i + 1
	}
	t.inTable = false
	if next != "" && mdSetext.MatchString(next) && !t.inList {
		t.add(i, leadingBlanks(s), len(s), KindHeading)
		t.add(i+1, leadingBlanks(next), len(next), KindHeading)
		return i + 2
	}
	t.inline(i, s, 0, len(s), KindPlain)
	return i + 1
}

// tableRow paints a row of a table: its bars are operators, as is the rule under the
// header, and its cells hold inline marks.
func (t *mdTokenizer) tableRow(i int, s string) {
	if mdTableRule.MatchString(s) && strings.Contains(s, "-") {
		t.add(i, leadingBlanks(s), len(s), KindOperator)
		return
	}
	start := 0
	for k := 0; k <= len(s); k++ {
		if k == len(s) || s[k] == '|' && (k == 0 || s[k-1] != '\\') {
			t.inline(i, s, start, k, KindPlain)
			if k < len(s) {
				t.add(i, k, k+1, KindOperator)
			}
			start = k + 1
		}
	}
}

// inline paints the marks of s[lo:hi] on line i, with base the kind of the text that
// holds no mark.
func (t *mdTokenizer) inline(i int, s string, lo, hi int, base Kind) {
	for hi > lo && s[hi-1] == '\r' {
		hi--
	}
	p := mdInline{t: t, line: i, s: s}
	p.parse(lo, hi, base)
}

// mdInline reads the inline marks of one line.
type mdInline struct {
	t    *mdTokenizer
	line int
	s    string
}

func (p *mdInline) emit(from, to int, k Kind) { p.t.add(p.line, from, to, k) }

// parse paints s[lo:hi]. Text between marks takes base.
func (p *mdInline) parse(lo, hi int, base Kind) {
	s := p.s
	seg := lo
	flush := func(to int) {
		p.emit(seg, to, base)
	}
	for i := lo; i < hi; {
		c := s[i]
		switch c {
		case '\\':
			if i+1 < hi && isASCIIPunct(s[i+1]) {
				i += 2
				continue
			}
		case '`':
			n := runLength(s, i, hi, '`')
			if j := findRun(s, i+n, hi, '`', n); j >= 0 {
				flush(i)
				p.emit(i, j+n, KindString)
				i, seg = j+n, j+n
				continue
			}
			i += n
			continue
		case '<':
			if m := inlineHTML(s[i:hi]); m.end > 0 {
				flush(i)
				p.emit(i, i+m.end, m.kind)
				i, seg = i+m.end, i+m.end
				continue
			}
		case '[':
			if end, ok := p.link(i, hi, base, flush, &seg); ok {
				i = end
				continue
			}
		case '*', '_':
			if end, ok := p.emphasis(i, hi, base, flush, &seg); ok {
				i = end
				continue
			}
			i += runLength(s, i, hi, c)
			continue
		case '~':
			if n := runLength(s, i, hi, '~'); n == 2 {
				if j := findRun(s, i+2, hi, '~', 2); j > i+2 {
					flush(i)
					p.emit(i, j+2, KindStrike)
					i, seg = j+2, j+2
					continue
				}
			}
			i += runLength(s, i, hi, '~')
			continue
		case 'h', 'w':
			if (i == lo || !isWordByte(s[i-1])) && (strings.HasPrefix(s[i:hi], "http://") || strings.HasPrefix(s[i:hi], "https://") || strings.HasPrefix(s[i:hi], "www.")) {
				if n := bareURLLength(s[i:hi]); n > 0 {
					flush(i)
					p.emit(i, i+n, KindLink)
					i, seg = i+n, i+n
					continue
				}
			}
		}
		i++
	}
	flush(hi)
}
