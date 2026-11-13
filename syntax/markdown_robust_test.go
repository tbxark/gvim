package syntax

import (
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/alecthomas/chroma/v2"
)

// Whatever the text, the spans of a markdown document cover every line once, in order. The text
// is random runs of the characters that mean something to markdown, which is what breaks a
// tokenizer that reads brackets, fences and emphasis.
func TestMarkdownSpansAlwaysCoverTheLines(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewPCG(9, 17))
	pieces := []string{"*", "**", "***", "_", "__", "`", "``", "~~", "[", "]", "(", ")", "![", "](", "<", ">", "<!--", "-->",
		"```", "```go", "~~~", "#", "## ", "- ", "* ", "1. ", "> ", "|", "|---|", "---", "===", "\\", "\\*", "http://x.y/z", "www.a.b",
		" ", "  ", "\t", "word", "é", "日本", "\n", "\n\n", "\n    ", "[x]: u", "- [ ] t", "&amp;", "{.py}"}
	for range 500 {
		var sb strings.Builder
		for range 1 + rng.IntN(40) {
			sb.WriteString(pieces[rng.IntN(len(pieces))])
		}
		lines := strings.Split(sb.String(), "\n")
		spans, err := Tokenize(Detect("a.md", nil), lines)
		if err != nil {
			t.Fatal(err)
		}
		reconstruct(t, lines, spans)
		if t.Failed() {
			t.Fatalf("text %q", sb.String())
		}
	}
}

// The fixups of every language that has them keep the spans a cover of each line, whatever the
// text: they split and merge tokens, and an off-by-one there would leave a gap or an overlap.
func TestFixupsNeverBreakTheSpans(t *testing.T) {
	t.Parallel()
	pieces := []string{"0", "1", "9", "x", "0x", "0b", "0o", "1_", "_", ".", "e", "e+", "-", "+", "u8", "i64", "f32", "n", "L", "j", "?", ":", " ? ", " : ", "??",
		"?.", "(", ")", "{", "}", "[", "]", ",", ";", "<", ">", "T", "foo", "'a", "'", "\"", "#", "#[", "#![", "define", "#define ", "#if ", "#ifdef ", "\\\n",
		"@", "@Dec", "inf", "nan", "~", "=", "key", " ", "\t", "\n", "é", "日"}
	files := []string{"a.ts", "a.tsx", "a.js", "a.rs", "a.toml", "a.c", "a.cpp", "a.java", "a.kt", "a.go", "a.py", "a.sql", "a.yaml", "a.yml", "a.css", "a.sh"}
	for _, file := range files {
		t.Run(file, func(t *testing.T) {
			t.Parallel()
			rng := rand.New(rand.NewPCG(uint64(len(file)), 3))
			for range 250 {
				var sb strings.Builder
				for range 1 + rng.IntN(30) {
					sb.WriteString(pieces[rng.IntN(len(pieces))])
				}
				lines := strings.Split(sb.String(), "\n")
				lexer := Detect(file, nil)
				spans, err := Tokenize(lexer, lines)
				if err != nil {
					t.Fatal(err)
				}
				reconstruct(t, lines, spans)
				if t.Failed() {
					t.Fatalf("text %q", sb.String())
				}
				// The tokens of the fixups join to the text the lexer read.
				it, err := lexer.Tokenise(nil, joinLines(lines))
				if err != nil {
					t.Fatal(err)
				}
				raw := it.Tokens()
				var before strings.Builder
				for _, tok := range raw {
					before.WriteString(tok.Value)
				}
				var after strings.Builder
				for _, tok := range fixedTokens(lexer, chroma.Literator(raw...)).Tokens() {
					after.WriteString(tok.Value)
				}
				if before.String() != after.String() {
					t.Fatalf("the fixups changed the text of %q into %q", before.String(), after.String())
				}
			}
		})
	}
}

// A piece from the middle of a document is not given front matter: a rule, some text and a second
// rule are what they look like.
func TestMarkdownWindowHasNoFrontMatter(t *testing.T) {
	t.Parallel()
	lines := []string{"---", "", "title: x", "", "---", "# H"}
	lexer := Detect("a.md", nil)
	whole, _ := tokenize(lexer, lines, false)
	piece, _ := tokenize(lexer, lines, true)
	if got := kindsOf(whole[2]); len(got) == 0 || got[0] != KindKeyword {
		t.Errorf("the key of the front matter has kinds %v, want a keyword", got)
	}
	for _, k := range kindsOf(piece[2]) {
		if k != KindPlain {
			t.Errorf("text between two rules of a piece has kind %v, want plain", k)
		}
	}
}

func BenchmarkTokenizeMarkdown(b *testing.B) {
	doc := strings.Repeat("# Title\n\nSome *emphasis* and **strong** text with `code` and a [link](http://x.y).\n\n- item one\n- item two\n\n```go\nfunc f() { return 1 }\n```\n\n", 1500)
	lines := strings.Split(doc, "\n")
	lexer := Detect("a.md", nil)
	b.SetBytes(int64(len(doc)))
	b.ResetTimer()
	for range b.N {
		if _, err := Tokenize(lexer, lines); err != nil {
			b.Fatal(err)
		}
	}
}

func TestMarkdownUnclosedOpenersAreNotExponential(t *testing.T) {
	t.Parallel()
	// Every "*" here opens emphasis and none closes it.
	line := strings.Repeat("*.go ", 60)
	done := make(chan struct{})
	go func() {
		tokenizeMarkdown([]string{line}, false)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("tokenizeMarkdown did not finish a line of unclosed emphasis openers")
	}
}
