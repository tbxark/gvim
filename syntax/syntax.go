// Package syntax assigns a syntax highlighting language to a buffer and turns
// its lines into colored spans.
//
// Highlighting uses github.com/alecthomas/chroma/v2, which is pure Go. Chroma
// token types are collapsed into a small Kind set, and each Kind maps to a
// key in the "syntax" section of a theme (see Kind.ThemeKey).
//
// Span offsets are byte offsets into the line, not display columns. Callers
// that paint by cell must convert them.
package syntax

import (
	"bytes"
	"path"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// maxAnalyseBytes bounds how much content lexers.Analyse scores. Analyse
// runs every registered lexer's heuristic, which is slow on large files, and
// the signals it uses (shebang, doctype, leading markers) are near the start.
const maxAnalyseBytes = 16 << 10

// Span is a half-open byte range [Start, End) within one line, with its Kind.
type Span struct {
	Start, End int
	Kind       Kind
}

// Classify maps a chroma token type to a Kind.
//
// Builtins: a builtin function name (NameBuiltin, such as len or print) is
// KindFunction. A builtin pseudo-variable (NameBuiltinPseudo, such as self or
// this) is KindBuiltin. A decorator or annotation is KindAttribute. Word operators such as "and" are KindKeyword. Heading
// tokens from markup lexers are KindKeyword. Punctuation, plain names, text,
// and error tokens are KindPlain.
func Classify(t chroma.TokenType) Kind {
	switch t.Category() {
	case chroma.Keyword:
		switch t {
		case chroma.KeywordType:
			return KindType
		case chroma.KeywordConstant:
			return KindConstant
		}
		return KindKeyword

	case chroma.Name:
		switch t {
		case chroma.NameClass, chroma.NameException, chroma.NameNamespace:
			return KindType
		case chroma.NameBuiltin:
			return KindFunction
		case chroma.NameBuiltinPseudo:
			return KindBuiltin
		case chroma.NameConstant, chroma.NameEntity:
			return KindConstant
		case chroma.NameDecorator:
			return KindAttribute
		case chroma.NameKeyword, chroma.NameTag:
			return KindKeyword
		case chroma.NameOperator:
			return KindOperator
		case chroma.NameAttribute, chroma.NameProperty:
			return KindVariable
		}
		switch t.SubCategory() {
		case chroma.NameVariable:
			return KindVariable
		case chroma.NameFunction:
			return KindFunction
		}
		return KindPlain

	case chroma.Literal:
		switch t.SubCategory() {
		case chroma.LiteralString:
			return KindString
		case chroma.LiteralNumber:
			return KindNumber
		}
		if t == chroma.LiteralDate {
			return KindString
		}
		return KindPlain

	case chroma.Operator:
		if t == chroma.OperatorWord {
			return KindKeyword
		}
		return KindOperator

	case chroma.Comment:
		switch {
		case t == chroma.CommentPreprocFile:
			return KindString
		case t.SubCategory() == chroma.CommentPreproc:
			return KindKeyword
		}
		return KindComment

	case chroma.Generic:
		switch t {
		case chroma.GenericHeading, chroma.GenericSubheading:
			return KindKeyword
		case chroma.GenericInserted:
			return KindDiffAdd
		case chroma.GenericDeleted:
			return KindDiffRemove
		}
		return KindPlain
	}
	return KindPlain
}

// Detect chooses a lexer for a buffer. It tries, in order:
//
//  1. lexers.Match(filename), which matches the file name or extension
//  2. the interpreter named on a "#!" first line, looked up by lexer name or
//     alias (Analyse only knows shell shebangs, so python and others need this)
//  3. lexers.Analyse(content), which scores the content; only the first
//     maxAnalyseBytes are scored
//  4. lexers.Fallback
//
// Detect never returns nil. Steps 1, 3 and 4 match
// refs/crush/internal/ui/common/highlight.go. Step 2 follows Fresh's
// documented order (filename, extension, shebang), where an extension match
// wins over a shebang.
func Detect(filename string, content []byte) chroma.Lexer {
	if filename != "" {
		if l := lexers.Match(filename); l != nil {
			return l
		}
	}
	if looksLikeUnifiedDiff(content) {
		return lexers.Get("diff")
	}
	if l := shebangLexer(content); l != nil {
		return l
	}
	if len(content) > 0 {
		head := content
		if len(head) > maxAnalyseBytes {
			head = head[:maxAnalyseBytes]
		}
		if l := lexers.Analyse(string(head)); l != nil {
			return l
		}
	}
	return lexers.Fallback
}

// looksLikeUnifiedDiff reports whether content looks like a unified diff,
// before the content analyser is tried. Chroma's analyser can mistake a diff
// whose added or removed text is Go, C, or another language for that language,
// and then loses the diff's per-line roles.
func looksLikeUnifiedDiff(content []byte) bool {
	first, rest, _ := bytes.Cut(content, []byte("\n"))
	first = bytes.TrimSuffix(first, []byte("\r"))
	if !bytes.HasPrefix(first, []byte("diff --git ")) &&
		!bytes.HasPrefix(first, []byte("--- ")) {
		return false
	}
	return bytes.Contains(rest, []byte("\n@@ ")) ||
		bytes.HasPrefix(rest, []byte("@@ "))
}

// shebangLexer returns the lexer for the interpreter on a "#!" first line, or
// nil. It handles "/usr/bin/python3", "/usr/bin/env python3", and
// "/usr/bin/env -S python3 -u". A version suffix is stripped, so "python3.11"
// becomes "python".
func shebangLexer(content []byte) chroma.Lexer {
	if !bytes.HasPrefix(content, []byte("#!")) {
		return nil
	}
	line := content[2:]
	if i := bytes.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	fields := strings.Fields(string(line))
	if len(fields) == 0 {
		return nil
	}
	interp := path.Base(fields[0])
	if interp == "env" {
		interp = ""
		for _, f := range fields[1:] {
			if strings.HasPrefix(f, "-") || strings.Contains(f, "=") {
				continue
			}
			interp = path.Base(f)
			break
		}
	}
	name := strings.TrimRight(interp, "0123456789.")
	if name == "" {
		return nil
	}
	return lexers.Get(name)
}

// Tokenize lexes text, which holds one string per line, and returns the spans
// for each line. The result has len(text) entries. Each entry covers the whole
// line in order, with KindPlain spans for text no token claimed. An empty line
// has a nil entry.
//
// Any '\r' or '\n' inside a line is replaced by a space before lexing, so the
// lexer sees the same byte layout as the input and offsets stay valid.
//
// A nil lexer means lexers.Fallback.
func Tokenize(lexer chroma.Lexer, text []string) ([][]Span, error) {
	return tokenize(lexer, text, false)
}

// tokenize is Tokenize for text that may be a piece from the middle of a document, with inside
// set. A tokenizer that reads what only the start of a document can hold, such as the front
// matter of markdown, then does not look for it.
func tokenize(lexer chroma.Lexer, text []string, inside bool) ([][]Span, error) {
	if lexer == nil {
		lexer = lexers.Fallback
	}
	if tz, ok := tokenizers[lexer.Config().Name]; ok {
		return tz(text, inside), nil
	}
	src := joinLines(text)
	it, err := lexer.Tokenise(nil, src)
	if err != nil {
		return nil, err
	}
	it = fixedTokens(lexer, it)

	raw := make([][]Span, len(text))
	line, col := 0, 0
	for tok := it(); tok != chroma.EOF; tok = it() {
		kind := Classify(tok.Type)
		if line == 0 && col == 0 && tok.Type == chroma.CommentPreproc && strings.HasPrefix(tok.Value, "#!") {
			// A shell lexer reports the "#!" line as preprocessor text. It is a comment.
			kind = KindComment
		}
		v := tok.Value
		for {
			seg := v
			nl := strings.IndexByte(v, '\n')
			if nl >= 0 {
				seg = v[:nl]
			}
			if len(seg) > 0 && line < len(raw) {
				raw[line] = appendSpan(raw[line], Span{Start: col, End: col + len(seg), Kind: kind})
			}
			col += len(seg)
			if nl < 0 {
				break
			}
			line++
			col = 0
			v = v[nl+1:]
		}
	}

	out := make([][]Span, len(text))
	for i, ln := range text {
		out[i] = fillGaps(raw[i], len(ln))
	}
	return out, nil
}

// plainSpans returns one KindPlain span per non-empty line. It is the
// fallback when lexing fails.
func plainSpans(text []string) [][]Span {
	out := make([][]Span, len(text))
	for i, line := range text {
		if len(line) > 0 {
			out[i] = []Span{{Start: 0, End: len(line), Kind: KindPlain}}
		}
	}
	return out
}

// joinLines builds the source string for the lexer: lines joined by '\n', with
// '\r' and '\n' inside a line replaced by ' '.
func joinLines(text []string) string {
	n := len(text)
	for _, l := range text {
		n += len(l)
	}
	var b strings.Builder
	b.Grow(n)
	for i, l := range text {
		if i > 0 {
			b.WriteByte('\n')
		}
		if strings.ContainsAny(l, "\r\n") {
			l = lineSanitizer.Replace(l)
		}
		b.WriteString(l)
	}
	return b.String()
}

// lineSanitizer maps line-break bytes to spaces, keeping byte length.
var lineSanitizer = strings.NewReplacer("\r", " ", "\n", " ")

// appendSpan adds s to spans. It merges s into the previous span when both
// have the same Kind and touch.
func appendSpan(spans []Span, s Span) []Span {
	if n := len(spans); n > 0 && spans[n-1].Kind == s.Kind && spans[n-1].End == s.Start {
		spans[n-1].End = s.End
		return spans
	}
	return append(spans, s)
}

// fillGaps returns spans sorted and covering [0, length). Gaps become
// KindPlain spans. Spans are assumed not to overlap, which holds for chroma
// output.
func fillGaps(spans []Span, length int) []Span {
	if length == 0 {
		return nil
	}
	out := make([]Span, 0, len(spans)+1)
	pos := 0
	for _, s := range spans {
		if s.Start > pos {
			out = appendSpan(out, Span{Start: pos, End: s.Start, Kind: KindPlain})
		}
		out = appendSpan(out, s)
		pos = s.End
	}
	if pos < length {
		out = appendSpan(out, Span{Start: pos, End: length, Kind: KindPlain})
	}
	return out
}
