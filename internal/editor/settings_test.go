package editor

import (
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
)

func TestSettingsForWithoutResolverUsesOptions(t *testing.T) {
	e := New(Options{TabSize: 2, UseTabs: true, AutoIndent: true, AutoClose: true})
	got := e.SettingsFor(e.Active())
	if got.TabSize != 2 || !got.UseTabs || !got.AutoIndent || !got.AutoClose || got.AutoSurround {
		t.Errorf("settings = %+v, want the Options values", got)
	}
	if !got.LineWrap || got.PageWidth != DefaultPageWidth || !got.IndentationGuide ||
		!got.WhitespaceTabsLeading || !got.WhitespaceTabsInner || !got.WhitespaceTabsTrailing {
		t.Errorf("settings = %+v, want the view defaults", got)
	}
}

func TestSettingsForPassesLanguageAndPathToTheResolver(t *testing.T) {
	e := New(Options{TabSize: 4})
	var gotLang, gotPath string
	e.SetLanguageSettings(func(language, path string) LanguageSettings {
		gotLang, gotPath = language, path
		return LanguageSettings{Language: "go", TabSize: 8, UseTabs: true}
	})
	d := e.Active()
	d.Path = "/src/main.go"
	e.SetLanguage("Go")
	s := e.SettingsFor(d)
	if gotLang != "Go" || gotPath != "/src/main.go" {
		t.Errorf("resolver got (%q, %q), want (\"Go\", \"/src/main.go\")", gotLang, gotPath)
	}
	if s.TabSize != 8 || !s.UseTabs || s.Language != "go" {
		t.Errorf("settings = %+v", s)
	}
	if s := e.SettingsFor(nil); gotLang != "" || gotPath != "" || s.TabSize != 8 {
		t.Errorf("SettingsFor(nil) called the resolver with (%q, %q): %+v", gotLang, gotPath, s)
	}
}

func TestSettingsForViewSettingsWinOverLanguage(t *testing.T) {
	yes, no := true, false
	tests := []struct {
		name  string
		view  ViewSettings
		check func(LanguageSettings) bool
	}{
		{"no override", ViewSettings{}, func(s LanguageSettings) bool {
			return s.TabSize == 8 && s.UseTabs && s.LineWrap && !s.PageView && s.PageWidth == 72
		}},
		{"tab size", ViewSettings{TabSize: 3}, func(s LanguageSettings) bool { return s.TabSize == 3 }},
		{"indent tabs off", ViewSettings{IndentTabs: &no}, func(s LanguageSettings) bool { return !s.UseTabs }},
		{"line wrap off", ViewSettings{LineWrap: &no}, func(s LanguageSettings) bool { return !s.LineWrap }},
		{"page view", ViewSettings{PageView: true, PageWidth: 100}, func(s LanguageSettings) bool {
			return s.PageView && s.PageWidth == 100
		}},
		{"line wrap on keeps the rest", ViewSettings{LineWrap: &yes}, func(s LanguageSettings) bool {
			return s.LineWrap && s.TabSize == 8
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := New(Options{})
			e.SetLanguageSettings(func(string, string) LanguageSettings {
				return LanguageSettings{TabSize: 8, UseTabs: true, LineWrap: true, PageWidth: 72}
			})
			d := e.Active()
			d.view = tt.view
			if s := e.SettingsFor(d); !tt.check(s) {
				t.Errorf("settings = %+v", s)
			}
		})
	}
}

func TestEphemeralFilePatternsAreCopied(t *testing.T) {
	e := New(Options{})
	in := []string{"**/.git/**"}
	e.SetEphemeralFilePatterns(in)
	in[0] = "changed"
	if got := e.EphemeralFilePatterns(); len(got) != 1 || got[0] != "**/.git/**" {
		t.Errorf("patterns = %v, want the copy", got)
	}
}

// widthEditor returns an editor with a global tab size of 4 whose document has the
// language tab size of 8, and holds text.
func widthEditor(text string) *Editor {
	e := docWith(Options{TabSize: 4}, text)
	e.SetLanguageSettings(func(string, string) LanguageSettings { return LanguageSettings{TabSize: 8} })
	return e
}

func TestAddCursorBelowUsesTheLanguageTabSize(t *testing.T) {
	t.Parallel()
	e := widthEditor("\tx\nabcdefghi")
	e.SetCursor(buffer.Pos{Col: 1}, false)
	run(t, e, "add_cursor_below")
	cs := e.Active().allCarets()
	if len(cs) != 2 || cs[1].head != (buffer.Pos{Line: 1, Col: 8}) {
		t.Fatalf("carets = %+v, want the second at 1:8, below the tab's 8 columns", cs)
	}
}

func TestToggleFoldUsesTheLanguageTabSize(t *testing.T) {
	t.Parallel()
	// A tab is deeper than four spaces only when it is 8 columns wide.
	e := widthEditor("a\n    b\n\tc")
	e.SetCursor(buffer.Pos{Line: 1}, false)
	if r := e.Do("toggle_fold", nil); r.Err != nil {
		t.Fatalf("toggle_fold = %v, want a fold on line 1 at a tab size of 8", r.Err)
	}
}
