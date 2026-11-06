package ui

import (
	"testing"

	"github.com/tbxark/gvim/internal/editor"
)

func TestLanguageSettingsUseTheResolver(t *testing.T) {
	var gotLang, gotPath string
	m := &Model{}
	m.view.languages = func(language, path string) editor.LanguageSettings {
		gotLang, gotPath = language, path
		return editor.LanguageSettings{TabSize: 8, UseTabs: true}
	}
	s := m.languageSettings("Go", "a.go")
	if gotLang != "Go" || gotPath != "a.go" || s.TabSize != 8 || !s.UseTabs {
		t.Errorf("settings = %+v for (%q, %q)", s, gotLang, gotPath)
	}
}

func TestLanguageSettingsWithoutResolverFollowTheGlobals(t *testing.T) {
	o := DefaultViewOptions()
	o.LineWrap = false
	m := &Model{tabSize: 2, view: newViewState(o)}
	s := m.languageSettings("Go", "a.go")
	if s.TabSize != 2 || s.LineWrap || s.PageWidth != editor.DefaultPageWidth || !s.IndentationGuide ||
		!s.WhitespaceTabsInner || s.UseTabs {
		t.Errorf("settings = %+v", s)
	}
}

func TestNewKeepsTheDepsLanguagesAndViewOptions(t *testing.T) {
	o := DefaultViewOptions()
	o.ShowTilde = false
	o.MouseWheelScrollLines = 9
	m, _ := viewModel(t, o, "x\n", 40, 10)
	if m.view.opts.ShowTilde || m.view.opts.MouseWheelScrollLines != 9 {
		t.Errorf("view.opts = %+v, want the Deps.View values", m.view.opts)
	}
}
