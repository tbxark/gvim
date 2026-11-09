package tui

import (
	"strings"
	"testing"
)

// TestLSPDisableLanguageTargetValidatesActiveDocument is the controlled action used by the
// asm_lsp popup: it captures the target path/language and mutes only when the exact same
// document is still active in the same Update slice.
func TestLSPDisableLanguageTargetValidatesActiveDocument(t *testing.T) {
	port := &fakeLSP{}
	lt := newLSPTest(t, port,
		&lspTab{path: "/p/a.go", lines: []string{"package a"}},
		&lspTab{path: "/p/b.go", lines: []string{"package b"}},
	)
	cmd, err := lt.m.doAction("lsp_disable_language_target", map[string]any{
		"path":     "/p/a.go",
		"language": "go",
	}, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	lt.settle(cmd, 3)
	if !port.muted["go"] {
		t.Fatalf("muted = %+v, want go muted for the captured target", port.muted)
	}

	// A tab switch between the capture and the action must not mute the new language.
	delete(port.muted, "go")
	lt.ed.active = 1
	lt.m.notice = ""
	cmd, err = lt.m.doAction("lsp_disable_language_target", map[string]any{
		"path":     "/p/a.go",
		"language": "go",
	}, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	lt.settle(cmd, 3)
	if len(port.muted) != 0 {
		t.Fatalf("muted after target switch = %+v, want none", port.muted)
	}
	if !strings.Contains(lt.m.notice, "active document changed") {
		t.Fatalf("notice = %q, want the active-document refusal", lt.m.notice)
	}

	// The same path with a different captured language is also refused.
	lt.ed.active = 0
	lt.m.notice = ""
	cmd, err = lt.m.doAction("lsp_disable_language_target", map[string]any{
		"path":     "/p/a.go",
		"language": "rust",
	}, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	lt.settle(cmd, 3)
	if len(port.muted) != 0 {
		t.Fatalf("muted after language mismatch = %+v, want none", port.muted)
	}
	if !strings.Contains(lt.m.notice, "active document changed") {
		t.Fatalf("notice = %q, want the active-document refusal", lt.m.notice)
	}
}

func TestLSPDisableLanguageTargetWithoutPortIsANotice(t *testing.T) {
	lt := newLSPTest(t, nil, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
	cmd, err := lt.m.doAction("lsp_disable_language_target", map[string]any{
		"path":     "/p/a.go",
		"language": "go",
	}, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	lt.settle(cmd, 3)
	if !strings.Contains(lt.m.notice, "No language server for go") {
		t.Fatalf("notice = %q, want the no-server notice", lt.m.notice)
	}
}

func TestLSPDisableLanguageTargetMapsTsxOpenLanguage(t *testing.T) {
	port := &fakeLSP{}
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.tsx", lines: []string{"export const x = <div/>"}})
	cmd, err := lt.m.doAction("lsp_disable_language_target", map[string]any{
		"path":     "/p/a.tsx",
		"language": "tsx",
	}, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	lt.settle(cmd, 3)
	if !port.muted["tsx"] {
		t.Fatalf("muted = %+v, want tsx (the native openLanguage target)", port.muted)
	}
	if strings.Contains(lt.m.notice, "active document changed") {
		t.Fatalf("notice = %q, want the valid tsx mapping accepted", lt.m.notice)
	}
}

func TestLSPDisableLanguageTargetAcceptsTsxLanguageMapping(t *testing.T) {
	port := &fakeLSP{}
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.tsx", lines: []string{"export const x = <div/>"}})
	// Simulate a user language mapping that names the buffer's language tsx (rather than
	// the TypeScript detection default) before the target action runs.
	lt.m.langs["/p/a.tsx"] = "tsx"
	cmd, err := lt.m.doAction("lsp_disable_language_target", map[string]any{
		"path":     "/p/a.tsx",
		"language": "tsx",
	}, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	lt.settle(cmd, 3)
	if !port.muted["tsx"] {
		t.Fatalf("muted = %+v, want the tsx language mapping target", port.muted)
	}
}
