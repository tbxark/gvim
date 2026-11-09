package bundled_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

func rustRig(t *testing.T, script func(o *plugintest.FakeOS)) *rig {
	t.Helper()
	return startRig(t, rigOpts{
		plugins: []string{"rust_lsp"},
		script: func(o *plugintest.FakeOS) {
			o.SetDir("/work")
			if script != nil {
				script(o)
			}
		},
	})
}

func TestRustLspManifestAndCommands(t *testing.T) {
	t.Parallel()
	r := rustRig(t, nil)
	if info, ok := r.h.Plugin("rust_lsp"); !ok || info.Status != "loaded" {
		t.Fatalf("plugin = %+v (log %v)", info, r.h.LogText(""))
	}
	want := map[string]string{
		"rust_lsp.copy_rustup": "Rust: Copy rustup Install Command",
		"rust_lsp.copy_brew":   "Rust: Copy brew Install Command",
		"rust_lsp.check":       "Rust: Check rust-analyzer",
		"rust_lsp.help":        "Rust: Language Server Help",
	}
	got := map[string]string{}
	for _, c := range r.host.Commands["rust_lsp"] {
		if !c.Hidden {
			got[c.ID] = c.Title
		}
	}
	if len(got) != len(want) {
		t.Fatalf("visible commands = %v, want %v", got, want)
	}
	for id, title := range want {
		if got[id] != title {
			t.Errorf("command %s = %q, want %q", id, got[id], title)
		}
	}
	if bindings := r.host.Bindings["rust_lsp"]; len(bindings) != 0 {
		t.Errorf("bindings = %v, want none", bindings)
	}
}

func TestRustLspCopyInstallCommands(t *testing.T) {
	t.Parallel()
	r := rustRig(t, nil)
	if err := r.h.Run("rust_lsp.copy_rustup", nil); err != nil {
		t.Fatal(err)
	}
	if got, want := r.host.Clipboard, "rustup component add rust-analyzer"; got != want {
		t.Fatalf("clipboard = %q, want %q", got, want)
	}
	if got := lastNoticeText(r.h); got != "Copied: rustup component add rust-analyzer" {
		t.Errorf("status = %q", got)
	}
	if err := r.h.Run("rust_lsp.copy_brew", nil); err != nil {
		t.Fatal(err)
	}
	if got, want := r.host.Clipboard, "brew install rust-analyzer"; got != want {
		t.Fatalf("clipboard = %q, want %q", got, want)
	}
	if got := lastNoticeText(r.h); got != "Copied: brew install rust-analyzer" {
		t.Errorf("status = %q", got)
	}
}

func TestRustLspCheckMissing(t *testing.T) {
	t.Parallel()
	r := rustRig(t, nil)
	if err := r.h.Run("rust_lsp.check", nil); err != nil {
		t.Fatal(err)
	}
	if got := lastNoticeText(r.h); !strings.Contains(got, "rust-analyzer not found on PATH") ||
		!strings.Contains(got, "rustup component add rust-analyzer") {
		t.Fatalf("status = %q", got)
	}
	if runs := r.os.Runs(); len(runs) != 0 {
		t.Fatalf("a missing binary was still run: %v", runs)
	}
}

func TestRustLspCheckFoundReportsVersion(t *testing.T) {
	t.Parallel()
	r := rustRig(t, func(o *plugintest.FakeOS) {
		o.Bin["rust-analyzer"] = "/usr/local/bin/rust-analyzer"
		o.Script([]string{"rust-analyzer", "--version"}, plugintest.ProcScript{Stdout: "rust-analyzer 1.2.3\ncommit abc\n"})
	})
	if err := r.h.Run("rust_lsp.check", nil); err != nil {
		t.Fatal(err)
	}
	want := "rust-analyzer found: /usr/local/bin/rust-analyzer (rust-analyzer 1.2.3)"
	if got := lastNoticeText(r.h); got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	if runs := r.os.Runs(); len(runs) != 1 || !slices.Equal(runs[0].Argv, []string{"rust-analyzer", "--version"}) {
		t.Fatalf("runs = %+v, want one --version probe", runs)
	}
}

func TestRustLspCheckVersionFailure(t *testing.T) {
	t.Parallel()
	r := rustRig(t, func(o *plugintest.FakeOS) {
		o.Bin["rust-analyzer"] = "/usr/local/bin/rust-analyzer"
		o.Script([]string{"rust-analyzer", "--version"}, plugintest.ProcScript{Code: 1, Stderr: "error: toolchain missing\nmore"})
	})
	if err := r.h.Run("rust_lsp.check", nil); err != nil {
		t.Fatal(err)
	}
	if got := lastNoticeText(r.h); !strings.Contains(got, "but --version failed: error: toolchain missing") {
		t.Fatalf("status = %q", got)
	}
}

func TestRustLspCheckStartFailure(t *testing.T) {
	t.Parallel()
	r := rustRig(t, func(o *plugintest.FakeOS) {
		o.Bin["rust-analyzer"] = "/usr/local/bin/rust-analyzer"
		o.Script([]string{"rust-analyzer", "--version"}, plugintest.ProcScript{Err: pluginapi.Errf(pluginapi.CodeIO, "exec: permission denied")})
	})
	if err := r.h.Run("rust_lsp.check", nil); err != nil {
		t.Fatal(err)
	}
	if got := lastNoticeText(r.h); !strings.Contains(got, "but could not start it: io: exec: permission denied") {
		t.Fatalf("status = %q", got)
	}
}

func TestRustLspHelpPopupActions(t *testing.T) {
	t.Parallel()
	r := rustRig(t, func(o *plugintest.FakeOS) {
		o.Bin["rust-analyzer"] = "/usr/local/bin/rust-analyzer"
		o.Script([]string{"rust-analyzer", "--version"}, plugintest.ProcScript{Stdout: "rust-analyzer 1.2.3\n"})
	})
	if err := r.h.Run("rust_lsp.help", nil); err != nil {
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
	for _, want := range []string{"copy:rustup", "copy:brew", "act:check", "act:lsp_restart", "act:show_lsp_status", "dismiss"} {
		if !ids[want] {
			t.Errorf("help popup lacks %q (actions %v)", want, ids)
		}
	}
	if !strings.Contains(ov.ActionPopup.Message, "rustup component add rust-analyzer") ||
		!strings.Contains(ov.ActionPopup.Message, "https://rust-analyzer.github.io/book/installation.html") {
		t.Errorf("help message = %q", ov.ActionPopup.Message)
	}
	// Copy from the popup.
	r.h.Resolve(pluginapi.Completion{Token: ov.Token, Values: []any{"copy:brew"}})
	if got, want := r.host.Clipboard, "brew install rust-analyzer"; got != want {
		t.Fatalf("clipboard = %q, want %q", got, want)
	}
	// Check from the popup.
	if err := r.h.Run("rust_lsp.help", nil); err != nil {
		t.Fatal(err)
	}
	ov = r.host.UI().Last("action_popup")
	r.h.Resolve(pluginapi.Completion{Token: ov.Token, Values: []any{"act:check"}})
	if got := lastNoticeText(r.h); !strings.Contains(got, "rust-analyzer found: /usr/local/bin/rust-analyzer") {
		t.Fatalf("check status = %q", got)
	}
	// Native restart action from the popup.
	if err := r.h.Run("rust_lsp.help", nil); err != nil {
		t.Fatal(err)
	}
	ov = r.host.UI().Last("action_popup")
	r.h.Resolve(pluginapi.Completion{Token: ov.Token, Values: []any{"act:lsp_restart"}})
	if got := r.host.Cmd().Ran(); !slices.Contains(got, "lsp_restart") {
		t.Fatalf("actions = %v, want lsp_restart", got)
	}
}
