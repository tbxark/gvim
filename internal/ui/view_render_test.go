package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/editor"
)

// textRow returns the text cells of body row n (0 is the first) from the editor's text
// column, with trailing blanks removed.
func textRow(t *testing.T, m *Model, n, width int) string {
	t.Helper()
	l := m.layout()
	var b strings.Builder
	for i := range width {
		b.WriteString(viewCell(t, m, l.textX+i, l.bodyTop+n).Text)
	}
	return strings.TrimRight(b.String(), " ")
}

// gutterRow returns the gutter text of body row n.
func gutterRow(t *testing.T, m *Model, n int) string {
	t.Helper()
	l := m.layout()
	var b strings.Builder
	for x := l.panes[0].x; x < l.textX; x++ {
		b.WriteString(viewCell(t, m, x, l.bodyTop+n).Text)
	}
	return strings.TrimRight(b.String(), " ")
}

func TestRelativeLineNumbersOption(t *testing.T) {
	tests := []struct {
		name     string
		relative bool
		want     []string
	}{
		{"off", false, []string{"1 │", "2 │", "3 │"}},
		{"on", true, []string{"1 │", "1 │", "2 │"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := DefaultViewOptions()
			o.RelativeLineNumbers = tc.relative
			m, _ := viewModel(t, o, "a\nb\nc\n", 40, 10)
			for i, want := range tc.want {
				if got := gutterRow(t, m, i); !strings.HasSuffix(got, want) {
					t.Errorf("gutter row %d = %q, want it to end with %q", i, got, want)
				}
			}
		})
	}
}

func TestIndentationGuideOptions(t *testing.T) {
	yes := true
	tests := []struct {
		name     string
		mode     string
		glyph    string
		override *bool
		allowed  bool
		want     string // the text of the second row
	}{
		{"none", "none", "", nil, true, "    b"},
		{"all with the default glyph", "all", "", nil, true, "▏   b"},
		{"all with a configured glyph", "all", "┊", nil, true, "┊   b"},
		{"a wide glyph falls back", "all", "中", nil, true, "▏   b"},
		{"plain text opts out", "all", "", nil, false, "    b"},
		{"the buffer switches it on over none", "none", "", &yes, true, "▏   b"},
		{"the buffer switches it on for plain text", "all", "", &yes, false, "▏   b"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := DefaultViewOptions()
			o.IndentationGuideMode, o.IndentationGuideGlyph = tc.mode, tc.glyph
			m, ed := viewModel(t, o, "a {\n    b\n", 40, 10)
			m.view.languages = func(string, string) editor.LanguageSettings {
				return editor.LanguageSettings{TabSize: 4, IndentationGuide: tc.allowed}
			}
			if tc.override != nil {
				if err := ed.Do("toggle_indentation_guide_current_buffer", nil).Err; err != nil {
					t.Fatal(err)
				}
			}
			if got := textRow(t, m, 1, 8); got != tc.want {
				t.Errorf("row 1 = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRainbowIndentationColorsTheGuideByLevel(t *testing.T) {
	o := DefaultViewOptions()
	o.IndentationGuideMode, o.RainbowIndentation = "all", true
	m, _ := viewModel(t, o, "a {\n    b {\n        c\n", 40, 10)
	l := m.layout()
	at := func(col int) any { return viewCell(t, m, l.textX+col, l.bodyTop+2).Style.FG }
	if a, b := at(0), at(4); a == nil || b == nil || a == b {
		t.Errorf("guide colors = %v and %v, want two different colors", a, b)
	}
	want := m.panes.Decor.IndentRainbow[1].FG
	if got := at(4); got != want {
		t.Errorf("level 1 guide fg = %v, want indent_rainbow_2 %v", got, want)
	}
}

func TestWhitespaceOptions(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*ViewOptions)
		tabs  bool // the language shows tab marks
		text  string
		want  string
	}{
		{"master off", func(o *ViewOptions) {}, true, "a b\t\n", "a b"},
		{"master on, spaces off, tabs on", func(o *ViewOptions) { o.ShowWhitespace = true }, true, "a b\tc\n", "a b→c"},
		{"inner spaces", func(o *ViewOptions) {
			o.ShowWhitespace = true
			o.Whitespace.SpacesInner = true
		}, true, " a b \n", " a·b"},
		{"the language hides tab marks", func(o *ViewOptions) { o.ShowWhitespace = true }, false, "a\tb\n", "a   b"},
		{"newline marks", func(o *ViewOptions) {
			o.ShowWhitespace = true
			o.Whitespace.Newlines = true
		}, true, "ab\ncd\n", "ab↵"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := DefaultViewOptions()
			tc.setup(&o)
			m, _ := viewModel(t, o, tc.text, 40, 10)
			m.view.languages = func(string, string) editor.LanguageSettings {
				return editor.LanguageSettings{TabSize: 4, WhitespaceTabsLeading: tc.tabs, WhitespaceTabsInner: tc.tabs, WhitespaceTabsTrailing: tc.tabs}
			}
			if got := textRow(t, m, 0, len([]rune(tc.want))); got != tc.want {
				t.Errorf("row 0 = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestWhitespaceSwitchOfABufferMarksEverySpace(t *testing.T) {
	m, _ := viewModel(t, DefaultViewOptions(), "a b  \n", 40, 10)
	if got := textRow(t, m, 0, 5); got != "a b" {
		t.Fatalf("setup: row 0 = %q", got)
	}
	runAction(t, m, "toggle_whitespace_indicators")
	if got := textRow(t, m, 0, 5); got != "a·b··" {
		t.Errorf("row 0 after the switch = %q, want every space marked", got)
	}
	runAction(t, m, "toggle_whitespace_indicators")
	if got := textRow(t, m, 0, 5); got != "a b" {
		t.Errorf("row 0 after the second switch = %q, want no marks", got)
	}
}

func TestBracketHighlightOptions(t *testing.T) {
	tests := []struct {
		name      string
		highlight bool
		rainbow   bool
		wantFG    func(m *Model) any
		underline bool
	}{
		{"pair color without the rainbow", true, false, func(m *Model) any { return m.panes.Decor.BracketMatch.FG }, false},
		{"depth color and attributes with the rainbow", true, true, func(m *Model) any { return m.panes.Decor.BracketRainbow[0].FG }, true},
		{"nothing when highlighting is off", false, true, func(m *Model) any { return m.panes.Text.FG }, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := DefaultViewOptions()
			o.HighlightMatchingBrackets, o.RainbowBrackets = tc.highlight, tc.rainbow
			m, ed := viewModel(t, o, "f(a)\n", 40, 10)
			ed.SetCursor(buffer.Pos{Col: 1}, false)
			c := viewCell(t, m, m.layout().textX+1, m.layout().bodyTop)
			if c.Text != "(" || c.Style.FG != tc.wantFG(m) || c.Style.Underline != tc.underline {
				t.Errorf("bracket cell = %q fg %v underline %v, want fg %v underline %v", c.Text, c.Style.FG, c.Style.Underline, tc.wantFG(m), tc.underline)
			}
		})
	}
}

func TestHangingIndentKeepsTheFrameCaretOnTheText(t *testing.T) {
	const line = "      one two three four five six seven eight nine ten eleven"
	m, ed := viewModel(t, DefaultViewOptions(), line+"\n", 40, 10)
	// Wrap width 32 on a 40-wide frame (no explorer): the rows after the first start 6 cells in.
	ed.SetCursor(buffer.Pos{Col: strings.Index(line, "eleven")}, false)
	x, y, ok := m.caret()
	if !ok {
		t.Fatal("no caret")
	}
	if got := viewCell(t, m, x, y).Text; got != "e" {
		t.Errorf("the cell under the caret is %q, want the e of eleven", got)
	}
	if y <= m.layout().bodyTop {
		t.Fatalf("setup: the caret is on the first row (y %d)", y)
	}
	if got := viewCell(t, m, m.layout().textX+5, y).Text; got != " " {
		t.Errorf("the hanging indent holds %q, want blanks", got)
	}
}

func TestWrapColumnOfTheLanguageNarrowsTheRows(t *testing.T) {
	m, _ := viewModel(t, DefaultViewOptions(), "aaaa bbbb cccc dddd\n", 40, 10)
	m.view.languages = func(string, string) editor.LanguageSettings {
		return editor.LanguageSettings{TabSize: 4, LineWrap: true, WrapColumn: 10, IndentationGuide: true}
	}
	if got := textRow(t, m, 0, 20); got != "aaaa bbbb" {
		t.Errorf("row 0 = %q, want the line wrapped at column 10", got)
	}
	if got := textRow(t, m, 1, 20); got != "cccc dddd" {
		t.Errorf("row 1 = %q, want the rest of the line", got)
	}
}

func TestTabsDisplayAtTheLanguageTabSize(t *testing.T) {
	tests := []struct {
		name     string
		resolver func(language, path string) editor.LanguageSettings
		prompt   string // the answer to Set Tab Size, if any
		wantX    int    // the column of the x after the tab
	}{
		{"the language tab size", func(string, string) editor.LanguageSettings { return editor.LanguageSettings{TabSize: 8} }, "", 8},
		{"a language that sets none keeps the default", func(string, string) editor.LanguageSettings { return editor.LanguageSettings{} }, "", 4},
		{"the buffer's tab size wins", func(string, string) editor.LanguageSettings { return editor.LanguageSettings{TabSize: 8} }, "2", 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := viewModel(t, DefaultViewOptions(), "\tx\n", 40, 10)
			m.view.languages = tc.resolver
			if tc.prompt != "" {
				runAction(t, m, "set_tab_size")
				press(m, typed(tc.prompt))
				settle(t, m, press(m, special(tea.KeyEnter)))
			}
			x, y := m.layout().textX, m.layout().bodyTop
			if got := viewCell(t, m, x+tc.wantX, y).Text; got != "x" {
				t.Errorf("cell at +%d is %q, want x", tc.wantX, got)
			}
		})
	}
}

// TestToggleIndentationGuidesFlipsWhatIsDrawn runs the per-buffer toggle through the
// UI route. The first press must change the screen whatever the global mode is.
func TestToggleIndentationGuidesFlipsWhatIsDrawn(t *testing.T) {
	tests := []struct {
		name    string
		mode    string
		allowed bool
		want    []string // row 1 after each press
	}{
		{"all hides then shows", "all", true, []string{"    b", "▏   b"}},
		{"active hides then shows", "active", true, []string{"    b", "▏   b"}},
		{"none shows then hides", "none", true, []string{"▏   b", "    b"}},
		{"all with a language that opts out shows", "all", false, []string{"▏   b", "    b"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := DefaultViewOptions()
			o.IndentationGuideMode = tc.mode
			m, ed := viewModel(t, o, "a {\n    b\n", 40, 10)
			m.view.languages = func(string, string) editor.LanguageSettings {
				return editor.LanguageSettings{TabSize: 4, IndentationGuide: tc.allowed}
			}
			for i, want := range tc.want {
				if _, handled := m.viewAction("toggle_indentation_guide_current_buffer", nil); !handled {
					if err := ed.Do("toggle_indentation_guide_current_buffer", nil).Err; err != nil {
						t.Fatal(err)
					}
				}
				if got := textRow(t, m, 1, 8); got != want {
					t.Errorf("row 1 after press %d = %q, want %q", i+1, got, want)
				}
			}
		})
	}
}
