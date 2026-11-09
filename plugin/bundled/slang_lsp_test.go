package bundled_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

func slangRig(t *testing.T, script func(o *plugintest.FakeOS)) *rig {
	t.Helper()
	return startRig(t, rigOpts{
		plugins: []string{"slang_lsp"},
		script: func(o *plugintest.FakeOS) {
			o.SetDir("/work")
			if script != nil {
				script(o)
			}
		},
	})
}

func slangScript(o *plugintest.FakeOS, slangd, slangc plugintest.ProcScript) {
	o.Script([]string{"slangd", "--version"}, slangd)
	o.Script([]string{"slangc", "-v"}, slangc)
}

func TestSlangLspManifestAndCommands(t *testing.T) {
	t.Parallel()
	r := slangRig(t, nil)
	if info, ok := r.h.Plugin("slang_lsp"); !ok || info.Status != "loaded" {
		t.Fatalf("plugin = %+v (log %v)", info, r.h.LogText(""))
	}
	want := map[string]string{
		"slang_lsp.check":        "Slang: Check slangd / slangc",
		"slang_lsp.copy_install": "Slang: Copy Install Command",
		"slang_lsp.help":         "Slang: Language Server Help",
	}
	got := map[string]string{}
	for _, c := range r.host.Commands["slang_lsp"] {
		if !c.Hidden {
			got[c.ID] = c.Title
		}
	}
	for id, title := range want {
		if got[id] != title {
			t.Errorf("command %s = %q, want %q", id, got[id], title)
		}
	}
	if bindings := r.host.Bindings["slang_lsp"]; len(bindings) != 0 {
		t.Errorf("bindings = %v, want none", bindings)
	}
}

func TestSlangLspCopyInstall(t *testing.T) {
	t.Parallel()
	r := slangRig(t, nil)
	if err := r.h.Run("slang_lsp.copy_install", nil); err != nil {
		t.Fatal(err)
	}
	want := "Download the Slang binary release from https://github.com/shader-slang/slang/releases and put slangc and slangd on PATH"
	if got := r.host.Clipboard; got != want {
		t.Fatalf("clipboard = %q, want %q", got, want)
	}
	if got := lastNoticeText(r.h); got != "Copied: "+want {
		t.Errorf("status = %q", got)
	}
}

func TestSlangLspCheckBothOK(t *testing.T) {
	t.Parallel()
	r := slangRig(t, func(o *plugintest.FakeOS) {
		slangScript(o,
			plugintest.ProcScript{Stdout: "slangd 2025.1\nmore"},
			plugintest.ProcScript{Stdout: "slangc version 2025.1\nmore"})
	})
	if err := r.h.Run("slang_lsp.check", nil); err != nil {
		t.Fatal(err)
	}
	want := "slangd: slangd 2025.1; slangc: slangc version 2025.1"
	if got := lastNoticeText(r.h); got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	if runs := r.os.Runs(); len(runs) != 2 ||
		!slices.Equal(runs[0].Argv, []string{"slangd", "--version"}) ||
		!slices.Equal(runs[1].Argv, []string{"slangc", "-v"}) {
		t.Fatalf("runs = %+v, want the two version probes", runs)
	}
}

func TestSlangLspCheckOneFails(t *testing.T) {
	t.Parallel()
	r := slangRig(t, func(o *plugintest.FakeOS) {
		slangScript(o,
			plugintest.ProcScript{Stdout: "slangd 2025.1\n"},
			plugintest.ProcScript{Code: 1, Stderr: "slangc: bad environment\nmore"})
	})
	if err := r.h.Run("slang_lsp.check", nil); err != nil {
		t.Fatal(err)
	}
	want := "slangd: slangd 2025.1; slangc failed (exit 1): slangc: bad environment"
	if got := lastNoticeText(r.h); got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
}

func TestSlangLspCheckStartFailureIsHonest(t *testing.T) {
	t.Parallel()
	r := slangRig(t, func(o *plugintest.FakeOS) {
		slangScript(o,
			plugintest.ProcScript{Err: pluginapi.Errf(pluginapi.CodeUnsupported, "remote authority is disconnected")},
			plugintest.ProcScript{Stdout: "slangc version 2025.1\n"})
	})
	if err := r.h.Run("slang_lsp.check", nil); err != nil {
		t.Fatal(err)
	}
	got := lastNoticeText(r.h)
	if !strings.Contains(got, "Could not start slangd: unsupported: remote authority is disconnected") {
		t.Fatalf("status = %q, want the actual start error", got)
	}
	if strings.Contains(strings.ToLower(got), "not found") || strings.Contains(strings.ToLower(got), "missing") {
		t.Fatalf("status %q claims the binary is missing instead of reporting the authority error", got)
	}
}

func TestSlangLspHelpPopup(t *testing.T) {
	t.Parallel()
	r := slangRig(t, func(o *plugintest.FakeOS) {
		slangScript(o,
			plugintest.ProcScript{Stdout: "slangd 2025.1\n"},
			plugintest.ProcScript{Stdout: "slangc version 2025.1\n"})
	})
	if err := r.h.Run("slang_lsp.help", nil); err != nil {
		t.Fatal(err)
	}
	ov := r.host.UI().Last("action_popup")
	if ov == nil {
		t.Fatalf("no help popup (log %v)", r.h.LogText(""))
	}
	ids := map[string]bool{}
	for _, a := range ov.ActionPopup.Actions {
		ids[a.ID] = true
	}
	for _, want := range []string{"copy:install", "act:check", "act:lsp_restart", "act:show_lsp_status", "dismiss"} {
		if !ids[want] {
			t.Errorf("help popup lacks %q (actions %v)", want, ids)
		}
	}
	msg := ov.ActionPopup.Message
	for _, want := range []string{
		"lsp.slang",
		"slang-synth://core/core.builtin",
		"slangd --print-builtin-module",
		"gvim opens those targets natively (no plugin hook)",
		"5 s / 4 MiB bound",
		"read-only",
		"remote/SSH workspace explicitly refuses this native flow",
		"no local fallback",
		"independent of it",
		"https://shader-slang.org/docs/",
		"https://github.com/shader-slang/slang/releases",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("help message lacks %q:\n%s", want, msg)
		}
	}
	// Copy from the popup.
	r.h.Resolve(pluginapi.Completion{Token: ov.Token, Values: []any{"copy:install"}})
	if got := r.host.Clipboard; !strings.Contains(got, "github.com/shader-slang/slang/releases") {
		t.Fatalf("popup copy clipboard = %q", got)
	}
	// Check from the popup.
	if err := r.h.Run("slang_lsp.help", nil); err != nil {
		t.Fatal(err)
	}
	ov = r.host.UI().Last("action_popup")
	r.h.Resolve(pluginapi.Completion{Token: ov.Token, Values: []any{"act:check"}})
	if got := lastNoticeText(r.h); !strings.Contains(got, "slangd: slangd 2025.1") {
		t.Fatalf("popup check status = %q", got)
	}
	// Native restart from the popup.
	if err := r.h.Run("slang_lsp.help", nil); err != nil {
		t.Fatal(err)
	}
	ov = r.host.UI().Last("action_popup")
	r.h.Resolve(pluginapi.Completion{Token: ov.Token, Values: []any{"act:lsp_restart"}})
	if got := r.host.Cmd().Ran(); !slices.Contains(got, "lsp_restart") {
		t.Fatalf("actions = %v, want lsp_restart", got)
	}
}
