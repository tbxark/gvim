package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
)

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
