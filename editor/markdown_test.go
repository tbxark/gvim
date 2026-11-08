package editor

import (
	"testing"

	"github.com/tbxark/gvim/buffer"
)

// markdownEditor is a one-document editor over text whose document is named path,
// so the markdown-source gate sees the file suffix. The editor options keep the
// auto rules off unless a test sets them: the markdown rules must not depend on
// auto_indent (Fresh's markdown language turns it off).
func markdownEditor(t *testing.T, path, text string) *Editor {
	t.Helper()
	e := docWith(Options{TabSize: 4}, text)
	e.Active().Path = path
	return e
}

// markdownLanguageEditor is a one-document editor whose unnamed buffer resolves to
// language, the plugin's other activation gate (a language entry instead of a file
// suffix).
func markdownLanguageEditor(t *testing.T, language, text string) *Editor {
	t.Helper()
	e := docWith(Options{TabSize: 4}, text)
	e.SetLanguageSettings(func(string, string) LanguageSettings {
		return LanguageSettings{Language: language, TabSize: 4}
	})
	return e
}

// TestMarkdownSourceEnter covers the markdown-source Enter rule: continuation with
// the marker's indent and kind, the empty marker removal, and the leading-blank
// fallback for a line that is not a list.
func TestMarkdownSourceEnter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		text       string
		line, col  int
		want       string
		wantCursor buffer.Pos
	}{
		{"dash bullet", "- foo", 0, 5, "- foo\n- ", buffer.Pos{Line: 1, Col: 2}},
		{"star bullet", "* foo", 0, 5, "* foo\n* ", buffer.Pos{Line: 1, Col: 2}},
		{"plus bullet", "+ foo", 0, 5, "+ foo\n+ ", buffer.Pos{Line: 1, Col: 2}},
		{"indented bullet keeps the indent", "  - foo", 0, 7, "  - foo\n  - ", buffer.Pos{Line: 1, Col: 4}},
		{"tab-indented bullet keeps the tab", "\t- foo", 0, 6, "\t- foo\n\t- ", buffer.Pos{Line: 1, Col: 3}},
		{"ordered list counts up", "1. foo", 0, 6, "1. foo\n2. ", buffer.Pos{Line: 1, Col: 3}},
		{"ordered list keeps the digit count", "9. foo", 0, 6, "9. foo\n10. ", buffer.Pos{Line: 1, Col: 4}},
		{"checkbox continues unchecked", "- [x] done", 0, 10, "- [x] done\n- [ ] ", buffer.Pos{Line: 1, Col: 6}},
		{"upper-case checked box continues unchecked", "* [X] done", 0, 10, "* [X] done\n* [ ] ", buffer.Pos{Line: 1, Col: 6}},
		{"empty dash item removes the marker", "- ", 0, 2, "", buffer.Pos{}},
		{"empty ordered item removes the marker", "2. ", 0, 3, "", buffer.Pos{}},
		{"empty checkbox removes the marker", "- [ ] ", 0, 6, "", buffer.Pos{}},
		{"empty indented item removes the marker", "  - ", 0, 4, "", buffer.Pos{}},
		{"no marker copies the leading blanks", "  text", 0, 6, "  text\n  ", buffer.Pos{Line: 1, Col: 2}},
		{"no blanks after the break", "text", 0, 4, "text\n", buffer.Pos{Line: 1, Col: 0}},
		{"a lone dash is not a marker", "-", 0, 1, "-\n", buffer.Pos{Line: 1, Col: 0}},
		{"a paren list is not a marker", "1) foo", 0, 6, "1) foo\n", buffer.Pos{Line: 1, Col: 0}},
		{"caret mid-item continues before the rest", "- foo", 0, 2, "- \n- foo", buffer.Pos{Line: 1, Col: 2}},
		{"content after the marker keeps the item", "-  spaced", 0, 9, "-  spaced\n- ", buffer.Pos{Line: 1, Col: 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := markdownEditor(t, "notes.md", tt.text)
			e.SetCursor(buffer.Pos{Line: tt.line, Col: tt.col}, false)
			run(t, e, "insert_newline")
			if got := text(e); got != tt.want {
				t.Fatalf("text = %q, want %q", got, tt.want)
			}
			if got := e.Active().Cursor; got != tt.wantCursor {
				t.Errorf("cursor = %+v, want %+v", got, tt.wantCursor)
			}
		})
	}
}

// TestMarkdownSourceEnterOnOtherLanguages checks the control case: a non-Markdown
// buffer keeps the editor's usual Enter behavior, with no list continuation.
func TestMarkdownSourceEnterOnOtherLanguages(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ name, path, text string }{
		{"a go file", "main.go", "- foo"},
		{"an unnamed text buffer", "", "- foo"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := markdownEditor(t, tt.path, tt.text)
			e.SetCursor(buffer.Pos{Col: len(tt.text)}, false)
			run(t, e, "insert_newline")
			if got := text(e); got != tt.text+"\n" {
				t.Fatalf("text = %q, want no list continuation", got)
			}
		})
	}
}

// TestMarkdownSourceActivatesByLanguage checks the plugin's other gate: a buffer
// whose language is markdown or multimarkdown gets the rules even with no suffix.
func TestMarkdownSourceActivatesByLanguage(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"markdown", "multimarkdown"} {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			e := markdownLanguageEditor(t, language, "- foo")
			e.SetCursor(buffer.Pos{Col: 5}, false)
			run(t, e, "insert_newline")
			if got := text(e); got != "- foo\n- " {
				t.Fatalf("text = %q, want the list continued for language %s", got, language)
			}
		})
	}
	// A .mdx file is Markdown by suffix even though gvim's language table has no
	// mdx entry (the plugin's isMarkdownFile lists it).
	e := markdownEditor(t, "page.mdx", "- foo")
	e.SetCursor(buffer.Pos{Col: 5}, false)
	run(t, e, "insert_newline")
	if got := text(e); got != "- foo\n- " {
		t.Fatalf("mdx text = %q, want the list continued", got)
	}
	// A suffixless file that the language table names markdown is Markdown too: the
	// plugin's isMarkdownLanguage covers README, which gvim's builtin table maps to
	// markdown by file name.
	readme := docWith(Options{TabSize: 4}, "- foo")
	readme.Active().Path = "README"
	readme.SetLanguageSettings(configResolver())
	readme.SetCursor(buffer.Pos{Col: 5}, false)
	run(t, readme, "insert_newline")
	if got := text(readme); got != "- foo\n- " {
		t.Fatalf("README text = %q, want the list continued by language", got)
	}
}

// TestMarkdownSourceTab covers Tab: blank list items indent by four spaces and
// cycle the bullet forward, checkboxes keep their state, and everything else takes
// four spaces.
func TestMarkdownSourceTab(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		text       string
		col        int
		want       string
		wantCursor buffer.Pos
	}{
		{"dash cycles to plus", "- ", 2, "    + ", buffer.Pos{Col: 6}},
		{"star cycles to dash", "* ", 2, "    - ", buffer.Pos{Col: 6}},
		{"plus cycles to star", "+ ", 2, "    * ", buffer.Pos{Col: 6}},
		{"the indent is kept and widened", "  - ", 4, "      + ", buffer.Pos{Col: 8}},
		{"checkbox keeps unchecked", "- [ ] ", 6, "    + [ ] ", buffer.Pos{Col: 10}},
		{"checkbox keeps checked", "- [x] ", 6, "    + [x] ", buffer.Pos{Col: 10}},
		{"an ordered item takes four spaces", "1. ", 3, "1.     ", buffer.Pos{Col: 7}},
		{"a marked item with content takes four spaces", "- foo", 5, "- foo    ", buffer.Pos{Col: 9}},
		{"a plain line takes four spaces", "text", 4, "text    ", buffer.Pos{Col: 8}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := markdownEditor(t, "notes.md", tt.text)
			e.SetCursor(buffer.Pos{Col: tt.col}, false)
			run(t, e, "insert_tab")
			if got := text(e); got != tt.want {
				t.Fatalf("text = %q, want %q", got, tt.want)
			}
			if got := e.Active().Cursor; got != tt.wantCursor {
				t.Errorf("cursor = %+v, want %+v", got, tt.wantCursor)
			}
		})
	}
}

// TestMarkdownSourceTabControl checks that a non-Markdown buffer keeps the editor's
// Tab: one indent unit at the caret, with no bullet cycle.
func TestMarkdownSourceTabControl(t *testing.T) {
	t.Parallel()
	e := markdownEditor(t, "main.go", "- ")
	e.SetCursor(buffer.Pos{Col: 2}, false)
	run(t, e, "insert_tab")
	if got := text(e); got != "-     " {
		t.Fatalf("text = %q, want the indent unit with no cycle", got)
	}
}

// TestMarkdownSourceTabWithSelectionFallsBack checks the plugin's selection guard:
// with a selection Tab keeps the editor's line indent instead of cycling the bullet.
func TestMarkdownSourceTabWithSelectionFallsBack(t *testing.T) {
	t.Parallel()
	e := markdownEditor(t, "notes.md", "* ")
	e.SelectRange(buffer.Pos{Col: 0}, buffer.Pos{Col: 2})
	run(t, e, "insert_tab")
	if got := text(e); got != "    * " {
		t.Fatalf("text = %q, want the line indented without cycling", got)
	}
}

// TestMarkdownSourceShiftTab covers Shift+Tab: a blank item with at least four blanks
// of indent loses four and cycles its bullet in reverse; the rest keeps the usual
// dedent.
func TestMarkdownSourceShiftTab(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		text       string
		col        int
		want       string
		wantCursor buffer.Pos
	}{
		{"dash reverse-cycles to star", "    - ", 6, "* ", buffer.Pos{Col: 2}},
		{"star reverse-cycles to plus", "    * ", 6, "+ ", buffer.Pos{Col: 2}},
		{"plus reverse-cycles to dash", "    + ", 6, "- ", buffer.Pos{Col: 2}},
		{"one level of the indent is removed", "        * ", 10, "    + ", buffer.Pos{Col: 6}},
		{"checkbox keeps checked", "    - [x] ", 10, "* [x] ", buffer.Pos{Col: 6}},
		{"too little indent falls through to dedent", "  - ", 4, "- ", buffer.Pos{Col: 2}},
		{"a blank line just dedents", "    ", 4, "", buffer.Pos{Col: 0}},
		{"an ordered item just dedents", "    1. ", 7, "1. ", buffer.Pos{Col: 3}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := markdownEditor(t, "notes.md", tt.text)
			e.SetCursor(buffer.Pos{Col: tt.col}, false)
			run(t, e, "dedent_selection")
			if got := text(e); got != tt.want {
				t.Fatalf("text = %q, want %q", got, tt.want)
			}
			if got := e.Active().Cursor; got != tt.wantCursor {
				t.Errorf("cursor = %+v, want %+v", got, tt.wantCursor)
			}
		})
	}
}

// TestMarkdownSourceShiftTabControl checks the non-Markdown control for Shift+Tab:
// the usual dedent, no bullet cycle.
func TestMarkdownSourceShiftTabControl(t *testing.T) {
	t.Parallel()
	e := markdownEditor(t, "main.go", "    - ")
	e.SetCursor(buffer.Pos{Col: 6}, false)
	run(t, e, "dedent_selection")
	if got := text(e); got != "- " {
		t.Fatalf("text = %q, want the usual dedent with no cycle", got)
	}
}

// TestMarkdownSourceStepsAsideForMultipleCarets checks the plugin's fallbacks: with
// extra carets Enter and Tab keep the editor's multi-caret behavior instead of the
// list rules (which only know one caret's line).
func TestMarkdownSourceStepsAsideForMultipleCarets(t *testing.T) {
	t.Parallel()
	e := markdownEditor(t, "notes.md", "- a\n- b")
	e.SetCursor(buffer.Pos{Line: 0, Col: 3}, false)
	run(t, e, "add_cursor_below") // two carets, one on each line
	run(t, e, "insert_tab")
	if got := text(e); got != "- a    \n- b    " {
		t.Fatalf("text = %q, want the plain indent unit at both carets, not a marker rewrite", got)
	}
}

// TestMarkdownAutoCloseSingleQuote checks the Markdown single-quote exception at the
// file-suffix gate too (Fresh's get_auto_close_char): an apostrophe is typed alone
// even when auto_close is on, while another language still pairs it. The primitives
// live in document/pairFor; this is the path-based regression the round asked for.
func TestMarkdownAutoCloseSingleQuote(t *testing.T) {
	t.Parallel()
	quoteEditor := func(t *testing.T, language, path string) *Editor {
		t.Helper()
		e := docWith(Options{AutoClose: true, TabSize: 4}, "")
		e.Active().Path = path
		e.SetLanguageSettings(func(string, string) LanguageSettings {
			return LanguageSettings{Language: language, TabSize: 4, AutoClose: true}
		})
		return e
	}
	e := quoteEditor(t, "markdown", "notes.md")
	typeText(t, e, "'")
	if got := text(e); got != "'" {
		t.Fatalf("markdown apostrophe = %q, want a single quote", got)
	}
	goEditor := quoteEditor(t, "go", "main.go")
	typeText(t, goEditor, "'")
	if got := text(goEditor); got != "''" {
		t.Fatalf("go quote = %q, want the pair", got)
	}
}
