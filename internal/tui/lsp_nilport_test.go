package tui

import (
	"strings"
	"testing"
)

// TestDisableLanguageWithoutPortIsANotice checks lsp_disable_language with no
// language-server session: the action shows the usual no-server notice and starts no
// command (the old path built a closure over the nil port and panicked when it ran).
func TestDisableLanguageWithoutPortIsANotice(t *testing.T) {
	lt := newLSPTest(t, nil, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
	lt.act("lsp_disable_language")
	if !strings.Contains(lt.m.notice, "No language server for go") {
		t.Fatalf("notice = %q, want the no-server notice", lt.m.notice)
	}
}

// TestEnableLanguageWithoutPortIsANotice is the same guard for lsp_enable_language.
func TestEnableLanguageWithoutPortIsANotice(t *testing.T) {
	lt := newLSPTest(t, nil, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
	lt.act("lsp_enable_language")
	if !strings.Contains(lt.m.notice, "No language server for go") {
		t.Fatalf("notice = %q, want the no-server notice", lt.m.notice)
	}
}

// TestStatusPopupMuteWithoutPortDoesNotPanic covers the other lspSetLanguage caller:
// a status-popup mute with no port must return nil and report it, not build a command
// that calls the nil port.
func TestStatusPopupMuteWithoutPortDoesNotPanic(t *testing.T) {
	lt := newLSPTest(t, nil, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
	if cmd := lt.m.lspStatusRowChosen("mute" + lspStatusSep + "go"); cmd != nil {
		t.Fatalf("mute without a port returned a command")
	}
	if !strings.Contains(lt.m.notice, "No language server for go") {
		t.Fatalf("notice = %q, want the no-server notice", lt.m.notice)
	}
}
