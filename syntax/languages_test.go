package syntax

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestLanguagesListsRealLanguagesSorted(t *testing.T) {
	langs := Languages()
	for _, want := range []string{"Go", "Python", "Rust", "JSON", "TOML"} {
		if !slices.Contains(langs, want) {
			t.Errorf("Languages() has no %q", want)
		}
	}
	for _, plain := range []string{"plaintext", "fallback", "Text only"} {
		if slices.Contains(langs, plain) {
			t.Errorf("Languages() lists the plain-text name %q", plain)
		}
	}
	if !slices.IsSortedFunc(langs, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) }) {
		t.Errorf("Languages() is not sorted: %v", langs[:10])
	}
}

func TestLookupResolvesNamesAndAliases(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"go", "Go", true},
		{"Go", "Go", true},
		{" python ", "Python", true},
		{"py", "Python", true},
		{"zzzz", "", false},
		{"", "", false},
		{strings.Repeat("a", 200), "", false},
	}
	for _, tt := range tests {
		got, ok := Lookup(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("Lookup(%q) = %q, %v; want %q, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

// tokensOf lexes src as the language of file, and returns the spans that are not plain as
// "text|kind", in order. It also checks that the spans cover every line exactly.
func tokensOf(t *testing.T, file, src string) []string {
	t.Helper()
	lines := strings.Split(src, "\n")
	spans, err := Tokenize(Detect(file, []byte(src)), lines)
	if err != nil {
		t.Fatal(err)
	}
	reconstruct(t, lines, spans)
	var out []string
	for i, ln := range lines {
		for _, s := range spans[i] {
			if s.Kind != KindPlain {
				out = append(out, fmt.Sprintf("%s|%s", ln[s.Start:s.End], s.Kind))
			}
		}
	}
	return out
}

// containsInOrder reports whether want occurs in got as a subsequence.
func containsInOrder(got, want []string) (missing string, ok bool) {
	pos := 0
	for _, w := range want {
		k := slices.Index(got[pos:], w)
		if k < 0 {
			return w, false
		}
		pos += k + 1
	}
	return "", true
}

// TestLanguageTokens checks, for each common language, that the spans a short program
// gets are the ones a reader expects. want lists tokens that must appear in order, and
// absent lists tokens that must not appear at all.
func TestLanguageTokens(t *testing.T) {
	tests := []struct {
		name   string
		file   string
		src    string
		want   []string
		absent []string
	}{
		{
			name: "go literals",
			file: "a.go",
			src:  "package main\n\nconst c = 0x1F + 0b101 + 0o17 + 1_000 + 2i + 3.5e2\nvar s = \"x\" // note\n",
			want: []string{"package|keyword", "const|keyword", "0x1F|number", "0b101|number", "0o17|number",
				"1_000|number", "2i|number", "3.5e2|number", "\"x\"|string", "// note|comment"},
		},
		{
			name: "python literals decorators and self",
			file: "a.py",
			src:  "@app.route(\"/\")\nclass A:\n    def f(self):\n        return 0x1F + 0b11 + 1_000 + 3.5e-2 + 1j\n",
			want: []string{"@app.route|attribute", "class|keyword", "def|keyword", "self|builtin", "0x1F|number",
				"0b11|number", "1_000|number", "3.5e-2|number", "1j|number"},
		},
		{
			name: "javascript numbers",
			file: "a.js",
			src:  "const a = 0x1F + 1_000n + 3.5e10 + 0b101 + 0o17;\nconst t = x ? 1 : 2; // c\n",
			want: []string{"const|keyword", "0x1F|number", "1_000n|number", "3.5e10|number", "0b101|number",
				"0o17|number", "// c|comment"},
		},
		{
			name: "typescript numbers",
			file: "a.ts",
			src:  "const a = 0x1F + 1_000n + 3.5e10 + 0b101 + 0o17;\n",
			want: []string{"0x1F|number", "1_000n|number", "3.5e10|number", "0b101|number", "0o17|number"},
		},
		{
			name:   "typescript ternary",
			file:   "a.ts",
			src:    "const t = x ? 1 : 2;\nconst u = x ? foo : bar;\nconst w = ok ? 1 : null;\nconst k = y ? 1 : Foo.bar;\n",
			want:   []string{"1|number", ":|operator", "2|number", "?|operator", ":|operator", "1|number", ":|operator", "null|constant"},
			absent: []string{"2|type", "bar|type", "null|type", "Foo.bar|type"},
		},
		{
			name:   "typescript optional chaining and nullish are not ternaries",
			file:   "a.ts",
			src:    "const a = p?.q ?? 0;\nfunction f(a?: number, b: string = \"x\") {}\n",
			want:   []string{"?|operator", "0|number", "number|type", "string|type"},
			absent: []string{"a?|operator"},
		},
		{
			name:   "typescript object values are not types",
			file:   "a.ts",
			src:    "const o = { k: v, w: 2, ok: true, f: obj.prop };\nlet x: Foo = y;\nlet z: ns.Type;\n",
			want:   []string{"2|number", "true|constant", "Foo|type", "ns.Type|type"},
			absent: []string{"v|type", "obj.prop|type", "true|type"},
		},
		{
			name: "typescript generics and decorators",
			file: "a.ts",
			src:  "@Component({})\nclass A<T> { x: Map<string, T>; }\n",
			want: []string{"@Component|attribute", "class|keyword", "T|type"},
			// A type parameter is not a keyword as an HTML tag name is.
			absent: []string{"T|keyword"},
		},
		{
			name: "json",
			file: "a.json",
			src:  "{\"name\": \"x\", \"n\": -1.5e+10, \"ok\": true, \"nil\": null, \"a\": [1, 2.0]}\n",
			want: []string{"\"name\"|keyword", "\"x\"|string", "-1.5e+10|number", "true|constant", "null|constant",
				"1|number", "2.0|number"},
		},
		{
			name: "yaml flow collections and numbers",
			file: "a.yaml",
			src:  "# c\nmap: {a: 1, b: [x, y], c: \"s\"}\nlist:\n  - 0x1F\n  - 1.5e3\n  - true\n  - ~\nn: null\n",
			want: []string{"# c|comment", "map|keyword", "a|keyword", "1|number", "b|keyword", "c|keyword", "\"s\"|string",
				"list|keyword", "0x1F|number", "1.5e3|number", "true|constant", "~|constant", "null|constant"},
			absent: []string{"1, b|keyword"},
		},
		{
			name: "html",
			file: "a.html",
			src:  "<!DOCTYPE html>\n<p class=\"x\">a &amp; b</p>\n<!-- note -->\n",
			want: []string{"<!DOCTYPE html>|keyword", "p|keyword", "class|variable", "\"x\"|string", "&amp;|constant",
				"<!-- note -->|comment"},
		},
		{
			name: "css keeps pseudo classes a function",
			file: "a.css",
			src:  "a:hover { color: #fff; margin: 0 auto; }\n/* c */\n",
			want: []string{"hover|function", "color|keyword", "#fff|number", "auto|constant", "/* c */|comment"},
		},
		{
			name:   "shell shebang is a comment",
			file:   "a.sh",
			src:    "#!/usr/bin/env bash\n# note\nNAME=\"w\"\nif [ -z \"$x\" ]; then exit 1; fi\n",
			want:   []string{"#!/usr/bin/env bash|comment", "# note|comment", "\"w\"|string", "if|keyword", "then|keyword", "fi|keyword"},
			absent: []string{"#!/usr/bin/env bash|keyword"},
		},
		{
			name: "c directives and numbers",
			file: "a.c",
			src:  "#include <stdio.h>\n#define MAX(a, b) ((a) > (b) ? (a) : (b))\n#ifdef DEBUG\nint a = 0x1F + 077 + 1u + 3.5f + 0b101;\n#endif\n",
			want: []string{"#include|keyword", "<stdio.h>|string", "#define|keyword", "MAX|function", ">|operator", "?|operator",
				"#ifdef|keyword", "DEBUG|constant", "int|type", "0x1F|number", "077|number", "1u|number", "3.5f|number",
				"0b101|number", "#endif|keyword"},
			absent: []string{"#define MAX(a, b) ((a) > (b) ? (a) : (b))|keyword"},
		},
		{
			name:   "only the naming c directives paint their operand as a constant",
			file:   "a.c",
			src:    "#if FOO\n#elifdef BAR\n#endif\n",
			want:   []string{"#if|keyword", "#elifdef|keyword", "BAR|constant"},
			absent: []string{"FOO|constant"},
		},
		{
			name: "java has numbers and annotations",
			file: "a.java",
			src:  "@Entity\nclass A {\n    int x = 0x1F + 1_000 + 10L + 3.5f + 1e3 + 0b101;\n    @Override public String s() { return \"s\"; }\n}\n",
			want: []string{"@Entity|attribute", "class|keyword", "int|type", "0x1F|number", "1_000|number", "10L|number",
				"3.5f|number", "1e3|number", "0b101|number", "@Override|attribute", "\"s\"|string"},
		},
		{
			name: "sql numbers",
			file: "a.sql",
			src:  "-- c\nSELECT 1.5e3, 0x1F, 42 FROM t WHERE a = 'x';\n",
			want: []string{"-- c|comment", "SELECT|keyword", "1.5e3|number", "0x1F|number", "42|number", "FROM|keyword", "'x'|string"},
		},
		{
			name: "lua",
			file: "a.lua",
			src:  "-- c\nlocal t = { 0x1F, 3.5e-2, \"x\" }\nif a == nil then return end\n",
			want: []string{"-- c|comment", "local|keyword", "0x1F|number", "3.5e-2|number", "\"x\"|string", "if|keyword",
				"nil|constant", "then|keyword", "end|keyword"},
		},
		{
			name: "rust numbers with suffixes",
			file: "a.rs",
			src:  "let a: u8 = 255u8;\nlet b = 1_000i64 + 1e3f32 + 0xFFu32 + 0b1010_u8 + 0o77usize + 1.5f64 + 2.;\n",
			want: []string{"let|keyword", "u8|type", "255u8|number", "1_000i64|number", "1e3f32|number", "0xFFu32|number",
				"0b1010_u8|number", "0o77usize|number", "1.5f64|number", "2.|number"},
			absent: []string{"u8|keyword", "i64|keyword", "f32|keyword"},
		},
		{
			name:   "rust attributes are not keywords",
			file:   "a.rs",
			src:    "#[derive(Debug)]\n#![allow(dead_code)]\nfn f<'a>(x: &'a str) -> &'static str { x }\n",
			want:   []string{"#[derive(Debug)]|attribute", "#![allow(dead_code)]|attribute", "fn|keyword", "'a|type", "'static|type"},
			absent: []string{"#[derive(Debug)]|keyword", "'a|variable"},
		},
		{
			name: "toml number forms",
			file: "a.toml",
			src:  "a = 0xDEADBEEF\nb = 0o755\nc = 0b1101\nd = 1_000\ne = 3.14\nf = 1e10\ng = +inf\nh = -nan\ni = true\n",
			want: []string{"0xDEADBEEF|number", "0o755|number", "0b1101|number", "1_000|number", "3.14|number", "1e10|number",
				"+inf|number", "-nan|number", "true|constant"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tokensOf(t, tt.file, tt.src)
			if missing, ok := containsInOrder(got, tt.want); !ok {
				t.Errorf("token %q is not found in order in\n%s", missing, strings.Join(got, "  "))
			}
			for _, a := range tt.absent {
				if slices.Contains(got, a) {
					t.Errorf("token %q must not appear in\n%s", a, strings.Join(got, "  "))
				}
			}
		})
	}
}

// A fixup keeps the text of the line as it was: spans cover every byte once, whatever the
// lexer cut.
func TestFixupsKeepTheText(t *testing.T) {
	t.Parallel()
	srcs := map[string]string{
		"a.rs":   "let x = 1__000u8.pow(2); let y = 0x; let z = 1e; let r = 0b2;\n",
		"a.ts":   "a ? : b; a ? b : ; x ? (y) : z ?? q ?. r; 0x; 1_; 0b; 08;\n",
		"a.toml": "a = +\nb = -\nc = 0x\n[x]\n",
		"a.c":    "#define\n#define X\n#if\n#ifdef\n#define F(x) \\\n  (x + 1)\n",
		"a.yaml": "a: {b: [c, d\n",
		"a.java": "int a = 0x + 1_ + .5 + 5. + 1.e;\n",
	}
	for file, src := range srcs {
		tokensOf(t, file, src)
	}
}

func TestMarkdownTokens(t *testing.T) {
	tests := []struct {
		name   string
		src    string
		want   []string
		absent []string
	}{
		{
			name: "headings are bold keywords",
			src:  "# Title\n## Sub *x*\n",
			want: []string{"# Title|heading", "## Sub *x*|heading"},
		},
		{
			name: "emphasis strong and both",
			src:  "a *em* b **st** c ***both*** d\n",
			want: []string{"*em*|emphasis", "**st**|strong", "***both***|strong-emphasis"},
		},
		{
			name: "emphasis nests in strong",
			src:  "**bold *and* more**\n",
			want: []string{"**bold |strong", "*and*|strong-emphasis", " more**|strong"},
		},
		{
			name:   "underscores inside words and spaced stars are text",
			src:    "snake_case_name and 2 * 3 * 4 and under_score_\n",
			absent: []string{"_case_|emphasis", " 3 |emphasis"},
		},
		{
			name: "underscore emphasis",
			src:  "a _em_ and __st__\n",
			want: []string{"_em_|emphasis", "__st__|strong"},
		},
		{
			name: "inline code is a string and keeps its stars",
			src:  "use `a * b` and ``x ` y``\n",
			want: []string{"`a * b`|string", "``x ` y``|string"},
		},
		{
			name: "strike",
			src:  "a ~~gone~~ b\n",
			want: []string{"~~gone~~|strike"},
		},
		{
			name: "links images and autolinks",
			src:  "[txt](http://x.y \"t\") ![alt](a.png) <https://a.b> [r][ref] https://bare.example/p.\n",
			want: []string{"txt|string", "http://x.y|link", " \"t\"|string", "alt|string", "a.png|link",
				"<https://a.b>|link", "ref|link", "https://bare.example/p|link"},
		},
		{
			name: "reference definition",
			src:  "[ref]: http://x.y \"title\"\n",
			want: []string{"[ref]:|string", "http://x.y|link"},
		},
		{
			name: "block quote",
			src:  "> quoted *em*\n> more\n",
			want: []string{"> quoted |quote", "*em*|emphasis", "> more|quote"},
		},
		{
			name: "list markers and tasks",
			src:  "- a\n  - `b`\n* c\n+ d\n1. e\n2) f\n- [ ] g\n- [x] h\n",
			want: []string{"-|operator", "-|operator", "`b`|string", "*|operator", "+|operator", "1.|operator",
				"2)|operator", "-|operator", "[ ]|operator", "-|operator", "[x]|operator"},
		},
		{
			name: "rule and setext heading",
			src:  "Intro\n\n---\n\nTitle\n=====\n\nSub\n---\n* * *\n",
			want: []string{"---|operator", "Title|heading", "=====|heading", "Sub|heading", "---|heading", "* * *|operator"},
		},
		{
			name: "table",
			src:  "| a | *b* |\n|---|:-:|\n| 1 | `2` |\n\nnot | a table\n",
			want: []string{"||operator", "*b*|emphasis", "|---|:-:||operator", "`2`|string"},
			// The line after the blank one is not a table row: its bar is text.
			absent: []string{" not |operator"},
		},
		{
			name: "fenced code uses the language of the fence",
			src:  "```go\nfunc main() { x := 0x1F }\n```\n\n```python\ndef f(): return 1\n```\n",
			want: []string{"```go|string", "func|keyword", "main|function", ":=|operator", "0x1F|number", "```|string",
				"```python|string", "def|keyword", "f|function", "return|keyword", "1|number", "```|string"},
		},
		{
			name: "fence without a language or with an unknown one is a string",
			src:  "```\nplain *text*\n```\n~~~weird\nmore\n~~~\n",
			want: []string{"```|string", "plain *text*|string", "```|string", "~~~weird|string", "more|string", "~~~|string"},
		},
		{
			name: "a fence without a closing line runs to the end",
			src:  "```go\nx := 1\ny := 2",
			want: []string{"```go|string", ":=|operator", "1|number", ":=|operator", "2|number"},
		},
		{
			name: "indented code",
			src:  "para\n\n    code *here*\n\nafter *em*\n",
			want: []string{"    code *here*|string", "*em*|emphasis"},
		},
		{
			name:   "list continuation is not indented code",
			src:    "- item\n\n    continued *em*\n",
			want:   []string{"-|operator", "*em*|emphasis"},
			absent: []string{"    continued *em*|string"},
		},
		{
			name: "html comments span lines",
			src:  "a <!-- x --> b\n<!-- open\nstill *comment*\n-->\nafter *em*\n",
			want: []string{"<!-- x -->|comment", "<!-- open|comment", "still *comment*|comment", "-->|comment", "*em*|emphasis"},
		},
		{
			name: "front matter is yaml",
			src:  "---\ntitle: x\nn: 1\n---\n# H\n",
			want: []string{"---|operator", "title|keyword", "n|keyword", "1|number", "---|operator", "# H|heading"},
		},
		{
			name:   "escapes",
			src:    "\\*not em\\* and \\`not code\\`\n",
			absent: []string{"*not em\\*|emphasis"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tokensOf(t, "a.md", tt.src)
			if missing, ok := containsInOrder(got, tt.want); !ok {
				t.Errorf("token %q is not found in order in\n%s", missing, strings.Join(got, "  "))
			}
			for _, a := range tt.absent {
				if slices.Contains(got, a) {
					t.Errorf("token %q must not appear in\n%s", a, strings.Join(got, "  "))
				}
			}
		})
	}
}

// The styles of the markup kinds add what Fresh's colours cannot show.
func TestKindAttributes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		kind Kind
		want Attr
	}{
		{KindHeading, AttrBold},
		{KindStrong, AttrBold},
		{KindEmphasis, AttrItalic},
		{KindStrongEmphasis, AttrBold | AttrItalic},
		{KindLink, AttrUnderline},
		{KindStrike, AttrStrike},
		{KindKeyword, 0},
		{KindPlain, 0},
	}
	for _, tt := range tests {
		if got := tt.kind.Attrs(); got != tt.want {
			t.Errorf("%v.Attrs() = %b, want %b", tt.kind, got, tt.want)
		}
	}
	if got := KindBuiltin.ThemeFallbackKey(); got != "variable" {
		t.Errorf("KindBuiltin fallback = %q, want variable", got)
	}
	if got := KindKeyword.ThemeFallbackKey(); got != "" {
		t.Errorf("KindKeyword fallback = %q, want none", got)
	}
}
