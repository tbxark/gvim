package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/editor"
)

// openText writes text to name in a temporary folder and opens it in a new editor.
func openText(t *testing.T, name, text string) *editor.Editor {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	must(t, os.WriteFile(path, []byte(text), 0o644))
	ed := editor.New(editor.Options{})
	must(t, ed.OpenFile(path))
	return ed
}

func TestStringContext(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		file string
		text string
		at   buffer.Pos
		want bool
	}{
		{"inside a string", "a.go", "x := \"hello\"\n", buffer.Pos{Col: 7}, true},
		{"on the opening quote", "a.go", "x := \"hello\"\n", buffer.Pos{Col: 5}, true},
		{"on the closing quote", "a.go", "x := \"hello\"\n", buffer.Pos{Col: 11}, true},
		{"outside the string", "a.go", "x := \"hello\"\n", buffer.Pos{Col: 1}, false},
		{"after the string", "a.go", "x := \"hello\" + y\n", buffer.Pos{Col: 14}, false},
		{"at the end of the line", "a.go", "x := \"hello\"\n", buffer.Pos{Col: 12}, false},
		{"a comment is not a string", "a.go", "// \"hello\"\n", buffer.Pos{Col: 4}, false},
		{"a raw string spans lines", "a.go", "x := `a\nb\nc`\n", buffer.Pos{Line: 1, Col: 0}, true},
		{"after wide runes", "a.go", "x := \"héllo\" + y\n", buffer.Pos{Col: 14}, false},
		{"wide runes inside the string", "a.go", "x := \"héllo\"\n", buffer.Pos{Col: 9}, true},
		{"a document with no lexer", "a.zzz", "\"hello\"\n", buffer.Pos{Col: 2}, false},
		{"a python docstring spans lines", "a.py", "\"\"\"doc\nmore\"\"\"\n", buffer.Pos{Line: 1, Col: 1}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ed := openText(t, tt.file, tt.text)
			if got := newStringContext()(ed.Active(), tt.at); got != tt.want {
				t.Fatalf("stringContext(%+v) = %v, want %v", tt.at, got, tt.want)
			}
		})
	}
}

func TestStringContextOnALargeDocumentLexesABoundedWindow(t *testing.T) {
	t.Parallel()
	const n = 20000
	text := strings.Repeat("func f(a int) string { return \"hello\" + fmt.Sprint(a) } // c\n", n)
	ed := openText(t, "a.go", text)
	doc := ed.Active()
	c := &stringCache{}
	tests := []struct {
		name string
		at   buffer.Pos
		want bool
	}{
		{"inside a string far from the start", buffer.Pos{Line: 10000, Col: 30}, true},
		{"outside a string far from the start", buffer.Pos{Line: 10000, Col: 5}, false},
		{"inside a string on the last line", buffer.Pos{Line: n - 1, Col: 30}, true},
		{"inside a string on the first line", buffer.Pos{Col: 30}, true},
	}
	for _, tt := range tests {
		if got := c.inString(doc, tt.at); got != tt.want {
			t.Errorf("%s: inString(%+v) = %v, want %v", tt.name, tt.at, got, tt.want)
		}
		if got := c.end - c.start; got > 3*stringReach {
			t.Errorf("%s: lexed %d lines, want a bounded window", tt.name, got)
		}
	}
}

func TestStringContextReusesTheWindowUntilTheTextChanges(t *testing.T) {
	t.Parallel()
	ed := openText(t, "a.go", "x := \"a\"\ny := \"b\"\n")
	doc := ed.Active()
	c := &stringCache{}
	for _, p := range []buffer.Pos{{Col: 7}, {Line: 1, Col: 7}, {Col: 1}} {
		c.inString(doc, p)
	}
	if c.lexed != 1 {
		t.Fatalf("lexed %d windows for carets of one text, want 1", c.lexed)
	}
	doc.Buffer.Insert(buffer.Pos{}, "z")
	if !c.inString(doc, buffer.Pos{Col: 8}) {
		t.Fatalf("a position inside the string after the edit is not in a string")
	}
	if c.lexed != 2 {
		t.Fatalf("lexed %d windows after an edit, want 2", c.lexed)
	}
}

func TestNonCodeContextMasksCommentsAndStrings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		file string
		text string
		line int
		want string // one character per rune: "c" for code, "x" for a comment or string
	}{
		{"string and comment", "a.go", "x := \"hi\" // c\n", 0, "cccccxxxxcxxxx"},
		{"all code", "a.go", "x := 1\n", 0, ""},
		{"wide runes keep their column", "a.go", "é := \"é\" + y\n", 0, "cccccxxxcccc"},
		{"a raw string spans lines", "a.go", "x := `a\nb\nc`\n", 1, "x"},
		{"a python comment", "a.py", "if x: # else:\n", 0, "ccccccxxxxxxx"},
		{"a document with no lexer", "a.zzz", "\"hello\"\n", 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ed := openText(t, tt.file, tt.text)
			mask := newNonCodeContext()(ed.Active(), tt.line)
			var got strings.Builder
			for _, m := range mask {
				if m {
					got.WriteByte('x')
				} else {
					got.WriteByte('c')
				}
			}
			if got.String() != tt.want {
				t.Fatalf("mask = %q, want %q", got.String(), tt.want)
			}
		})
	}
}

func TestStringWindow(t *testing.T) {
	t.Parallel()
	never := func(int) bool { return false }
	tests := []struct {
		name         string
		n, line      int
		blank        func(int) bool
		wantS, wantE int
	}{
		{"a small document is whole", 100, 50, never, 0, 100},
		{"near the start", 10000, 100, never, 0, 100 + stringReach + 1},
		{"far from the ends without a blank line", 10000, 5000, never, 5000 - stringReach, 5000 + stringReach + 1},
		{"starts at a blank line above", 10000, 5000, func(i int) bool { return i == 4600 }, 4600, 5000 + stringReach + 1},
		{"ignores a blank line that is too far", 10000, 5000, func(i int) bool { return i == 4000 }, 5000 - stringReach, 5000 + stringReach + 1},
		{"stops at the last line", 10000, 9999, never, 9999 - stringReach, 10000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s, e := stringWindow(tt.n, tt.line, tt.blank)
			if s != tt.wantS || e != tt.wantE {
				t.Fatalf("stringWindow = [%d, %d), want [%d, %d)", s, e, tt.wantS, tt.wantE)
			}
		})
	}
}

// TestRuntimeEditorTypesSmartly checks the editor that buildModel makes: it has the
// string context and the per-language settings, so Go uses tabs, brackets pair, and a
// quote inside a string does not.
func TestRuntimeEditorTypesSmartly(t *testing.T) {
	var ed *editor.Editor
	prev := newEditor
	newEditor = func(o editor.Options) *editor.Editor {
		ed = prev(o)
		return ed
	}
	t.Cleanup(func() { newEditor = prev })
	root := projectRoot(t, "")
	file := filepath.Join(t.TempDir(), "main.go")
	must(t, os.WriteFile(file, []byte("x := \"ab\"\n"), 0o644))
	if _, err := buildCaptured(t, root); err != nil {
		t.Fatalf("buildModel() error = %v", err)
	}
	must(t, ed.OpenFile(file))
	typeKeys := func(s string) {
		t.Helper()
		for _, r := range s {
			if res := ed.Do("insert_char", map[string]any{"text": string(r)}); res.Err != nil {
				t.Fatal(res.Err)
			}
		}
	}
	text := func() string { return ed.Active().Buffer.Text() }

	// A quote inside the string stays a single quote.
	ed.SetCursor(buffer.Pos{Col: 7}, false)
	typeKeys(`"`)
	if got := text(); got != "x := \"a\"b\"\n" {
		t.Fatalf("quote inside a string: text = %q", got)
	}
	// A bracket at the end of the line pairs.
	ed.SetCursor(buffer.Pos{Line: 1}, false)
	typeKeys("f(")
	if got := text(); got != "x := \"a\"b\"\nf()" {
		t.Fatalf("bracket: text = %q", got)
	}
	// Go indents with tabs, at the width of the language.
	ed.SetCursor(buffer.Pos{Line: 1, Col: 2}, false)
	typeKeys("{")
	if res := ed.Do("insert_newline", nil); res.Err != nil {
		t.Fatal(res.Err)
	}
	if got := text(); got != "x := \"a\"b\"\nf({\n\t\n})" {
		t.Fatalf("enter after an opener: text = %q", got)
	}
}

// TestRuntimeEditorIndentsByLanguageRules checks the editor that buildModel makes: the
// comment mask is installed, so a colon in a Python comment opens no block, and the
// electric dedent of a Python "else:" works.
func TestRuntimeEditorIndentsByLanguageRules(t *testing.T) {
	var ed *editor.Editor
	prev := newEditor
	newEditor = func(o editor.Options) *editor.Editor {
		ed = prev(o)
		return ed
	}
	t.Cleanup(func() { newEditor = prev })
	root := projectRoot(t, "")
	file := filepath.Join(t.TempDir(), "main.py")
	must(t, os.WriteFile(file, []byte("if x:\n    y\n    else\nz = 1  # note:"), 0o644))
	if _, err := buildCaptured(t, root); err != nil {
		t.Fatalf("buildModel() error = %v", err)
	}
	must(t, ed.OpenFile(file))
	ed.SetCursor(buffer.Pos{Line: 2, Col: 8}, false)
	if res := ed.Do("insert_char", map[string]any{"text": ":"}); res.Err != nil {
		t.Fatal(res.Err)
	}
	ed.SetCursor(buffer.Pos{Line: 3, Col: 17}, false)
	if res := ed.Do("insert_newline", nil); res.Err != nil {
		t.Fatal(res.Err)
	}
	if got, want := ed.Active().Buffer.Text(), "if x:\n    y\nelse:\nz = 1  # note:\n"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
}

// TestTabsDisplayAtTheTabSizeOfTheLanguage checks the model buildModel makes: a tab in
// a Go file is drawn as wide as the Go tab size, which is 8, not the global tab size.
func TestTabsDisplayAtTheTabSizeOfTheLanguage(t *testing.T) {
	column := func(name string) int {
		isolateConfig(t)
		root := t.TempDir()
		file := filepath.Join(root, name)
		writeTestFile(t, file, "\tQ\n")
		m, files, err := buildModel(t.Context(), Options{Root: root, Files: []FileTarget{{Path: file}}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(files.Close)
		m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
		for line := range strings.SplitSeq(ansi.Strip(m.View().Content), "\n") {
			if i := strings.Index(line, "Q"); i >= 0 {
				return i
			}
		}
		t.Fatalf("%s: no row shows the Q", name)
		return -1
	}
	txt, goFile := column("a.txt"), column("a.go")
	if goFile-txt != 4 {
		t.Fatalf("Q is at column %d in a Go file and %d in a text file, want 4 more (tab size 8 against 4)", goFile, txt)
	}
}
