package syntax

import (
	"strings"
	"testing"
)

// kindsOfSub returns the Kind of every byte of the first occurrence of sub on line row of
// lines, which are lexed as the file called name would be. It fails when sub is not there.
func kindsOfSub(t *testing.T, name string, lines []string, row int, sub string) []Kind {
	t.Helper()
	spans, err := Tokenize(Detect(name, []byte(strings.Join(lines, "\n"))), lines)
	if err != nil {
		t.Fatalf("Tokenize: %v", err)
	}
	col := strings.Index(lines[row], sub)
	if col < 0 {
		t.Fatalf("%q is not on line %q", sub, lines[row])
	}
	kinds := make([]Kind, 0, len(sub))
	for i := col; i < col+len(sub); i++ {
		kinds = append(kinds, kindOfByte(spans[row], i))
	}
	return kinds
}

// kindOfByte returns the Kind of the span that covers byte i, or KindPlain.
func kindOfByte(spans []Span, i int) Kind {
	for _, s := range spans {
		if s.Start <= i && i < s.End {
			return s.Kind
		}
	}
	return KindPlain
}

// Each common language paints the token a reader looks for with its Kind, and every byte of
// the token gets it, so a token split by the lexer fails.
func TestCommonLanguagesTokenKinds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, file string
		line, sub  string
		want       Kind
	}{
		{"Go keyword", "main.go", "package main", "package", KindKeyword},
		{"Go string", "main.go", `import "fmt"`, `"fmt"`, KindString},
		{"Go type", "main.go", "func f(a int) {}", "int", KindType},
		{"Go nil", "main.go", "var p = nil", "nil", KindConstant},
		{"Python keyword", "a.py", "def f(x):", "def", KindKeyword},
		{"Python comment", "a.py", "x = 1  # note", "# note", KindComment},
		{"Python decorator", "a.py", "@decorator", "@decorator", KindAttribute},
		{"JavaScript keyword", "a.js", "const x = 1;", "const", KindKeyword},
		{"JavaScript regex", "a.js", "let re = /ab+c/gi;", "/ab+c/gi", KindString},
		{"JSON constant", "a.json", `{"t": true, "z": null}`, "true", KindConstant},
		{"JSON number", "a.json", `{"n": -1.5e+3}`, "-1.5e+3", KindNumber},
		{"JSON string value", "a.json", `{"s": "a\"b"}`, `"a\"b"`, KindString},
		{"YAML comment", "a.yaml", `- "quoted"  # comment`, "# comment", KindComment},
		{"YAML constant", "a.yaml", "enabled: yes", "yes", KindConstant},
		{"YAML string", "a.yaml", "key: 'single'", "'single'", KindString},
		{"TOML date", "a.toml", "f = 2024-01-02T03:04:05Z", "2024-01-02T03:04:05Z", KindString},
		{"TOML hex", "a.toml", "a = 0xDEAD_beef", "0xDEAD_beef", KindNumber},
		{"C include", "a.c", "#include <stdio.h>", "include", KindKeyword},
		{"C include path", "a.c", "#include <stdio.h>", "<stdio.h>", KindString},
		{"C type", "a.c", "unsigned long n;", "unsigned", KindType},
		{"Java annotation", "A.java", "@Override", "@Override", KindAttribute},
		{"Java keyword", "A.java", "public final class A {}", "public", KindKeyword},
		{"HTML tag", "a.html", "<p>hi</p>", "p", KindKeyword},
		{"HTML comment", "a.html", "<!-- note -->", "<!-- note -->", KindComment},
		{"CSS at-rule", "a.css", "@media (max-width: 600px) {}", "media", KindKeyword},
		{"CSS colour", "a.css", "p { color: #FFF; }", "#FFF", KindNumber},
		{"Shell builtin", "run.sh", "echo hi", "echo", KindFunction},
		{"Shell variable", "run.sh", `echo "$name"`, "$name", KindVariable},
		{"SQL keyword", "q.sql", "SELECT id FROM users;", "SELECT", KindKeyword},
		{"SQL number", "q.sql", "SELECT 18;", "18", KindNumber},
		{"Lua comment", "a.lua", "-- comment", "-- comment", KindComment},
		{"Lua keyword", "a.lua", "local t = {}", "local", KindKeyword},
		{"Lua number", "a.lua", "local a = 0xFF", "0xFF", KindNumber},
		{"Rust attribute", "a.rs", "#[derive(Debug)]", "#[derive(Debug)]", KindAttribute},
		{"Rust suffixed integer", "a.rs", "let a = 255u8;", "255u8", KindNumber},
		{"Rust suffixed float", "a.rs", "let c = 1e3f32;", "1e3f32", KindNumber},
		{"TypeScript ternary else", "a.ts", "let x = c ? d : e;", "e;", KindPlain},
		{"TypeScript number", "a.ts", "let n = 0x1F + 1_000n;", "1_000n", KindNumber},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			for i, k := range kindsOfSub(t, tt.file, []string{tt.line}, 0, tt.sub) {
				if k != tt.want {
					t.Errorf("%q byte %d is %s, want %s", tt.sub, i, k, tt.want)
				}
			}
		})
	}
}

// Markdown paints its markup with the Kinds Fresh's markdown scopes give it, and a fenced block is
// lexed as its language.
func TestMarkdownMarkupKinds(t *testing.T) {
	t.Parallel()
	doc := []string{
		"# Title",
		"",
		"Some *emphasis* and **strong** and `code` here.",
		"A [link](http://x.y) and ~~gone~~ text.",
		"",
		"> quoted line",
		"",
		"- item one",
		"1. ordered",
		"",
		"## Sub __strong__",
		"text",
		"```go",
		"func main() {}",
		"```",
		"",
		"> quoted **bold** text",
		"",
		"```html",
		"<div class=\"a\">hi</div>",
		"```",
	}
	tests := []struct {
		name string
		row  int
		sub  string
		want Kind
	}{
		{"heading", 0, "# Title", KindHeading},
		{"bold inside a quote", 16, "**bold**", KindStrong},
		{"quote text around bold", 16, "quoted ", KindQuote},
		{"html tag in a fence is HTML's", 19, "div", KindKeyword},
		{"html attribute in a fence is HTML's", 19, "class", KindVariable},
		{"emphasis", 2, "*emphasis*", KindEmphasis},
		{"strong", 2, "**strong**", KindStrong},
		{"inline code", 2, "`code`", KindString},
		{"plain text", 2, "Some", KindPlain},
		{"link target", 3, "http://x.y", KindLink},
		{"strikethrough", 3, "~~gone~~", KindStrike},
		{"block quote text", 5, "quoted line", KindQuote},
		{"block quote marker", 5, ">", KindQuote},
		{"bullet marker", 7, "-", KindOperator},
		{"ordered list marker", 8, "1.", KindOperator},
		{"second heading", 10, "## Sub", KindHeading},
		{"fenced code keyword is Go's", 13, "func", KindKeyword},
		{"fenced code name is Go's", 13, "main", KindFunction},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			for i, k := range kindsOfSub(t, "notes.md", doc, tt.row, tt.sub) {
				if k != tt.want {
					t.Errorf("%q byte %d is %s, want %s", tt.sub, i, k, tt.want)
				}
			}
		})
	}
}
