package editor

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// viewFlagCase runs one per-document flag action with args and checks the flag
// it names reads want.
type viewFlagCase struct {
	Name   string
	action Action
	Args   map[string]any
	flag   func(ViewSettings) bool
	Want   bool
}

func TestViewFlagActionsSetTheirOverride(t *testing.T) {
	t.Parallel()
	lineNumbers := func(s ViewSettings) bool { return OverrideOr(s.LineNumbers, false) }
	wrap := func(s ViewSettings) bool { return OverrideOr(s.LineWrap, false) }
	current := func(s ViewSettings) bool { return OverrideOr(s.CurrentLine, false) }
	occurrences := func(s ViewSettings) bool { return OverrideOr(s.Occurrences, false) }
	whitespace := func(s ViewSettings) bool { return OverrideOr(s.Whitespace, false) }
	tests := []viewFlagCase{
		{"line numbers on", "toggle_line_numbers_current_buffer", map[string]any{"value": true}, lineNumbers, true},
		{"line numbers off", "toggle_line_numbers_current_buffer", map[string]any{"value": false}, lineNumbers, false},
		{"wrap off", "toggle_line_wrap_current_buffer", map[string]any{"value": false}, wrap, false},
		{"current line off", "toggle_current_line_highlight_current_buffer", map[string]any{"value": false}, current, false},
		{"occurrences on", "toggle_occurrence_highlight_current_buffer", map[string]any{"value": true}, occurrences, true},
		{"whitespace on", "toggle_whitespace_indicators", map[string]any{"value": true}, whitespace, true},
		{"virtual space on", "toggle_virtual_space_current_buffer", map[string]any{"value": true},
			func(s ViewSettings) bool { return s.VirtualSpaceOn() }, true},
		{"indent guides on", "toggle_indentation_guide_current_buffer", map[string]any{"value": true},
			func(s ViewSettings) bool { return s.IndentGuidesOn() }, true},
		{"fold indicators off", "toggle_fold_indicators_current_buffer", map[string]any{"value": false},
			func(s ViewSettings) bool { return s.FoldIndicatorsOn() }, false},
		{"tab indicators off", "toggle_tab_indicators", map[string]any{"value": false},
			func(s ViewSettings) bool { return s.TabIndicatorsOn() }, false},
		{"page view on", "toggle_page_view", map[string]any{"value": true},
			func(s ViewSettings) bool { return s.PageView != nil && *s.PageView }, true},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e, _ := openFixture(t, Options{}, "abc\n")
			if r := e.Do(tt.action, tt.Args); r.Err != nil {
				t.Fatalf("Do(%s): %v", tt.action, r.Err)
			}
			if got := tt.flag(e.Active().ViewSettings()); got != tt.Want {
				t.Fatalf("%s: flag = %v, want %v", tt.action, got, tt.Want)
			}
		})
	}
}

func TestViewFlagWithoutValueFlipsFromItsDefault(t *testing.T) {
	t.Parallel()
	tests := []struct {
		action Action
		Before bool // the flag as a new document reads it
	}{
		{"toggle_fold_indicators_current_buffer", true},
		{"toggle_tab_indicators", true},
		{"toggle_indentation_guide_current_buffer", false},
		{"toggle_virtual_space_current_buffer", false},
		{"toggle_page_view", false},
	}
	for _, tt := range tests {
		t.Run(string(tt.action), func(t *testing.T) {
			t.Parallel()
			e, _ := openFixture(t, Options{}, "abc\n")
			run(t, e, tt.action)
			after := e.Active().ViewSettings()
			if got := viewFlagOf(after, tt.action); got == tt.Before {
				t.Fatalf("one toggle left the flag at %v, want %v", got, !tt.Before)
			}
			run(t, e, tt.action)
			if got := viewFlagOf(e.Active().ViewSettings(), tt.action); got != tt.Before {
				t.Fatalf("two toggles gave %v, want the default %v", got, tt.Before)
			}
		})
	}
}

// viewFlagOf reads the flag that action names, for the flip tests.
func viewFlagOf(s ViewSettings, action Action) bool {
	switch action {
	case "toggle_fold_indicators_current_buffer":
		return s.FoldIndicatorsOn()
	case "toggle_tab_indicators":
		return s.TabIndicatorsOn()
	case "toggle_indentation_guide_current_buffer":
		return s.IndentGuidesOn()
	case "toggle_virtual_space_current_buffer":
		return s.VirtualSpaceOn()
	case "toggle_page_view":
		return s.PageView != nil && *s.PageView
	}
	return false
}

func TestIndentationStyleFlipsFromTheEditorOption(t *testing.T) {
	t.Parallel()
	for _, useTabs := range []bool{false, true} {
		e, _ := openFixture(t, Options{UseTabs: useTabs}, "abc\n")
		run(t, e, "toggle_indentation_style")
		if got := OverrideOr(e.Active().ViewSettings().IndentTabs, !useTabs); got == useTabs {
			t.Fatalf("UseTabs=%v: one toggle kept the style, want it flipped", useTabs)
		}
	}
}

func TestRulersAddOnceAndRemove(t *testing.T) {
	t.Parallel()
	e, _ := openFixture(t, Options{}, "abc\n")
	ruler := func() []int { return e.Active().ViewSettings().Rulers }
	run := func(a Action, col int) error {
		return e.Do(a, map[string]any{"column": col}).Err
	}
	if err := run("add_ruler", 80); err != nil {
		t.Fatalf("add_ruler 80: %v", err)
	}
	if err := run("add_ruler", 80); err != nil {
		t.Fatalf("second add_ruler 80: %v", err)
	}
	if err := run("add_ruler", 100); err != nil {
		t.Fatalf("add_ruler 100: %v", err)
	}
	if got := ruler(); len(got) != 2 || got[0] != 80 || got[1] != 100 {
		t.Fatalf("rulers = %v, want [80 100]", got)
	}
	if err := run("remove_ruler", 80); err != nil {
		t.Fatalf("remove_ruler 80: %v", err)
	}
	if got := ruler(); len(got) != 1 || got[0] != 100 {
		t.Fatalf("after remove rulers = %v, want [100]", got)
	}
	if err := run("remove_ruler", 7); err == nil {
		t.Fatal("remove_ruler of a missing column succeeded, want an error")
	}
}

// The configured rulers start every buffer, as Fresh seeds them ("Rulers are per-buffer.
// The rulers config setting can also set default rulers"), so a buffer can remove one.
func TestConfigRulersSeedEveryBuffer(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	first := filepath.Join(dir, "a.txt")
	second := filepath.Join(dir, "b.txt")
	for _, p := range []string{first, second} {
		if err := os.WriteFile(p, []byte("abc\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	e := New(Options{})
	e.SetRulers([]int{20, 40})
	if err := e.OpenFile(first); err != nil {
		t.Fatal(err)
	}
	rulers := func() []int { return e.Active().ViewSettings().Rulers }
	if got := rulers(); !slices.Equal(got, []int{20, 40}) {
		t.Fatalf("first buffer rulers = %v, want the configured [20 40]", got)
	}
	if err := e.Do("remove_ruler", map[string]any{"column": 20}).Err; err != nil {
		t.Fatalf("remove_ruler of a configured column: %v", err)
	}
	if got := rulers(); !slices.Equal(got, []int{40}) {
		t.Fatalf("after remove_ruler rulers = %v, want [40]", got)
	}
	if err := e.OpenFile(second); err != nil {
		t.Fatal(err)
	}
	if got := rulers(); !slices.Equal(got, []int{20, 40}) {
		t.Fatalf("second buffer rulers = %v, want [20 40]: the removal is per buffer", got)
	}
	if err := e.Do("reset_buffer_settings", nil).Err; err != nil {
		t.Fatal(err)
	}
	if got := rulers(); !slices.Equal(got, []int{20, 40}) {
		t.Fatalf("after reset rulers = %v, want the configured [20 40] back", got)
	}
	id := e.NewVirtual(VirtualOptions{Name: "list", Text: "x\n"})
	d, _, err := e.docByID(id)
	if err != nil {
		t.Fatal(err)
	}
	if got := d.ViewSettings().Rulers; len(got) != 0 {
		t.Fatalf("virtual buffer rulers = %v, want none (Fresh draws no rulers on virtual buffers)", got)
	}
}

// The editor opens with an empty buffer, which exists before the config is read, so the
// configured rulers reach it too.
func TestConfigRulersReachTheStartingBuffer(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	e.SetRulers([]int{20})
	if got := e.Active().ViewSettings().Rulers; !slices.Equal(got, []int{20}) {
		t.Fatalf("starting buffer rulers = %v, want the configured [20]", got)
	}
}

func TestRulerRejectsBadColumns(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name   string
		action Action
		Args   map[string]any
	}{
		{"no column", "add_ruler", nil},
		{"zero", "add_ruler", map[string]any{"column": 0}},
		{"too large", "add_ruler", map[string]any{"column": 5000}},
		{"not a number", "add_ruler", map[string]any{"column": "80"}},
		{"remove missing", "remove_ruler", map[string]any{"column": 3}},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e, _ := openFixture(t, Options{}, "abc\n")
			if r := e.Do(tt.action, tt.Args); r.Err == nil {
				t.Fatalf("Do(%s, %v) succeeded, want an error", tt.action, tt.Args)
			}
			if got := e.Active().ViewSettings().Rulers; len(got) != 0 {
				t.Fatalf("rulers = %v after a refused action, want none", got)
			}
		})
	}
}

func TestTabSizeAndPageWidthAreStoredPerDocument(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name    string
		action  Action
		Args    map[string]any
		wantErr bool
		check   func(ViewSettings) int
		Want    int
	}{
		{"tab size 2", "set_tab_size", map[string]any{"size": 2}, false,
			func(s ViewSettings) int { return s.TabSize }, 2},
		{"tab size from JSON number", "set_tab_size", map[string]any{"size": float64(8)}, false,
			func(s ViewSettings) int { return s.TabSize }, 8},
		{"tab size zero", "set_tab_size", map[string]any{"size": 0}, true,
			func(s ViewSettings) int { return s.TabSize }, 0},
		{"tab size too big", "set_tab_size", map[string]any{"size": 17}, true,
			func(s ViewSettings) int { return s.TabSize }, 0},
		{"tab size missing", "set_tab_size", nil, true,
			func(s ViewSettings) int { return s.TabSize }, 0},
		{"page width 100", "set_page_width", map[string]any{"width": 100}, false,
			func(s ViewSettings) int { return s.PageWidth }, 100},
		{"page width zero", "set_page_width", map[string]any{"width": 0}, true,
			func(s ViewSettings) int { return s.PageWidth }, 0},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e, _ := openFixture(t, Options{}, "abc\n")
			r := e.Do(tt.action, tt.Args)
			if (r.Err != nil) != tt.wantErr {
				t.Fatalf("Do(%s) error = %v, want error %v", tt.action, r.Err, tt.wantErr)
			}
			if got := tt.check(e.Active().ViewSettings()); got != tt.Want {
				t.Fatalf("stored value = %d, want %d", got, tt.Want)
			}
		})
	}
}

func TestSetLanguageStoresTheNameOnTheDocument(t *testing.T) {
	t.Parallel()
	e, _ := openFixture(t, Options{}, "abc\n")
	if r := e.Do("set_language", map[string]any{"name": " Go "}); r.Err != nil {
		t.Fatalf("set_language: %v", r.Err)
	}
	if got := e.Tabs()[0].Language; got != "Go" {
		t.Fatalf("tab language = %q, want %q", got, "Go")
	}
	if r := e.Do("set_language", map[string]any{"name": 3}); r.Err == nil {
		t.Fatal("set_language with a number succeeded, want an error")
	}
}

func TestViewSettingsArePerDocument(t *testing.T) {
	t.Parallel()
	e, _ := openFixture(t, Options{}, "abc\n")
	first := e.Active()
	run(t, e, "new")
	second := e.Active()
	if err := e.Do("add_ruler", map[string]any{"column": 40}).Err; err != nil {
		t.Fatalf("add_ruler: %v", err)
	}
	e.Do("toggle_page_view", map[string]any{"value": true})
	if len(first.ViewSettings().Rulers) != 0 || first.ViewSettings().PageView != nil {
		t.Fatal("a ruler or page view on one document leaked into another")
	}
	if got := second.ViewSettings().Rulers; len(got) != 1 || got[0] != 40 {
		t.Fatalf("second document rulers = %v, want [40]", got)
	}
}

func TestOverrideOrReturnsTheDefaultWithoutAnOverride(t *testing.T) {
	t.Parallel()
	yes, no := true, false
	tests := []struct {
		Name string
		p    *bool
		def  bool
		Want bool
	}{
		{"nil follows default on", nil, true, true},
		{"nil follows default off", nil, false, false},
		{"override on beats default off", &yes, false, true},
		{"override off beats default on", &no, true, false},
	}
	for _, tt := range tests {
		if got := OverrideOr(tt.p, tt.def); got != tt.Want {
			t.Errorf("%s: OverrideOr = %v, want %v", tt.Name, got, tt.Want)
		}
	}
}

func TestBadViewValueLeavesFlagUnchanged(t *testing.T) {
	t.Parallel()
	e, _ := openFixture(t, Options{}, "abc\n")
	r := e.Do("toggle_fold_indicators_current_buffer", map[string]any{"value": "yes"})
	if r.Err == nil || !strings.Contains(r.Err.Error(), "value") {
		t.Fatalf("error = %v, want one about value", r.Err)
	}
	if !e.Active().ViewSettings().FoldIndicatorsOn() {
		t.Fatal("a refused value changed the fold indicators flag")
	}
}
