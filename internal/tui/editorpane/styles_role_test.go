package editorpane

import (
	"testing"

	"github.com/tbxark/gvim/internal/tui/style"
	"github.com/tbxark/gvim/syntax"
	"github.com/tbxark/gvim/theme"
)

// TestNewStylesRecordsThemeKeys checks the pane roles carry the theme-key ids the
// theme inspector reports: text, gutter, selection, current line and each syntax
// kind (with the editor background for syntax).
func TestNewStylesRecordsThemeKeys(t *testing.T) {
	t.Parallel()
	dark, err := theme.Load("dark")
	if err != nil {
		t.Fatal(err)
	}
	st := NewStyles(dark)
	if st.Text.FGKey != style.KeyIDFor("editor", "fg") || st.Text.BGKey != style.KeyIDFor("editor", "bg") {
		t.Errorf("Text keys = %d/%d, want editor.fg/editor.bg", st.Text.FGKey, st.Text.BGKey)
	}
	if st.Gutter.FGKey != style.KeyIDFor("editor", "line_number_fg") || st.Gutter.BGKey != style.KeyIDFor("editor", "line_number_bg") {
		t.Errorf("Gutter keys = %d/%d, want line_number_fg/line_number_bg", st.Gutter.FGKey, st.Gutter.BGKey)
	}
	if st.Selection.FGKey != style.KeyIDFor("editor", "fg") || st.Selection.BGKey != style.KeyIDFor("editor", "selection_bg") {
		t.Errorf("Selection keys = %d/%d, want editor.fg/selection_bg", st.Selection.FGKey, st.Selection.BGKey)
	}
	if st.CurrentLine.BGKey != style.KeyIDFor("editor", "current_line_bg") {
		t.Errorf("CurrentLine BGKey = %d, want editor.current_line_bg", st.CurrentLine.BGKey)
	}
	keyword := st.Syntax[syntax.KindKeyword]
	if keyword.FGKey != style.KeyIDFor("syntax", "keyword") || keyword.BGKey != style.KeyIDFor("editor", "bg") {
		t.Errorf("syntax keyword keys = %d/%d, want syntax.keyword/editor.bg", keyword.FGKey, keyword.BGKey)
	}
	if plain := st.Syntax[syntax.KindPlain]; plain.FGKey != style.KeyIDFor("editor", "fg") || plain.BGKey != style.KeyIDFor("editor", "bg") {
		t.Errorf("plain syntax keys = %d/%d, want editor.fg/editor.bg", plain.FGKey, plain.BGKey)
	}
}
