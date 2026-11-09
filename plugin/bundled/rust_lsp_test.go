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

func TestRustLspCheckProbeFailureIsNotAuthoritativeMissing(t *testing.T) {
	t.Parallel()
	// No LookPath entry and no scripted process: the plugin's local PATH cannot decide
	// anything for a remote workspace, so a failed authority probe must not say the binary
	// was "not found on PATH".
	r := rustRig(t, nil)
	if err := r.h.Run("rust_lsp.check", nil); err != nil {
		t.Fatal(err)
	}
	got := lastNoticeText(r.h)
	for _, want := range []string{
		"Could not run rust-analyzer on the workspace",
		"local-only",
		"cannot prove rust-analyzer is missing",
		"rustup component add rust-analyzer",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("status = %q, want %q", got, want)
		}
	}
	if strings.Contains(got, "not found on PATH") || strings.Contains(got, "is missing on the workspace") || strings.Contains(got, "deno is not on PATH") {
		t.Fatalf("status overclaims a missing binary: %q", got)
	}
	if runs := r.os.Runs(); len(runs) != 1 || !slices.Equal(runs[0].Argv, []string{"rust-analyzer", "--version"}) {
		t.Fatalf("runs = %+v, want one authority --version probe", runs)
	}
}

func TestRustLspCheckAuthorityProbeFindsDespiteLocalLookPathMiss(t *testing.T) {
	t.Parallel()
	// This is the remote case: the local LookPath is empty, but the authority probe
	// (gvim.proc.run) succeeds, so the plugin must report found — never missing.
	r := rustRig(t, func(o *plugintest.FakeOS) {
		o.Script([]string{"rust-analyzer", "--version"}, plugintest.ProcScript{Stdout: "rust-analyzer 1.2.3\n"})
	})
	if err := r.h.Run("rust_lsp.check", nil); err != nil {
		t.Fatal(err)
	}
	if got := lastNoticeText(r.h); got != "rust-analyzer is runnable on the workspace (rust-analyzer 1.2.3)" {
		t.Fatalf("status = %q", got)
	}
}

func TestRustLspCheckFoundReportsVersion(t *testing.T) {
	t.Parallel()
	r := rustRig(t, func(o *plugintest.FakeOS) {
		o.Script([]string{"rust-analyzer", "--version"}, plugintest.ProcScript{Stdout: "rust-analyzer 1.2.3\ncommit abc\n"})
	})
	if err := r.h.Run("rust_lsp.check", nil); err != nil {
		t.Fatal(err)
	}
	want := "rust-analyzer is runnable on the workspace (rust-analyzer 1.2.3)"
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
		o.Script([]string{"rust-analyzer", "--version"}, plugintest.ProcScript{Code: 1, Stderr: "error: toolchain missing\nmore"})
	})
	if err := r.h.Run("rust_lsp.check", nil); err != nil {
		t.Fatal(err)
	}
	if got := lastNoticeText(r.h); !strings.Contains(got, "runnable on the workspace, but --version failed: error: toolchain missing") {
		t.Fatalf("status = %q", got)
	}
}

func TestRustLspCheckStartFailure(t *testing.T) {
	t.Parallel()
	r := rustRig(t, func(o *plugintest.FakeOS) {
		o.Script([]string{"rust-analyzer", "--version"}, plugintest.ProcScript{Err: pluginapi.Errf(pluginapi.CodeIO, "exec: permission denied")})
	})
	if err := r.h.Run("rust_lsp.check", nil); err != nil {
		t.Fatal(err)
	}
	got := lastNoticeText(r.h)
	if !strings.Contains(got, "Could not run rust-analyzer on the workspace (io: exec: permission denied)") ||
		!strings.Contains(got, "cannot prove rust-analyzer is missing") {
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
	for _, want := range []string{
		"rustup component add rust-analyzer",
		"How gvim starts rust-analyzer",
		"Cargo.toml",
		"explicit user configuration always wins",
		"never changes the native selection",
		"enable/disable toggle only controls these helper commands",
		"does not start, stop or select the native Rust LSP server",
		"workspace authority",
		"workspace trust gate may refuse it",
		"Only a successful check (exit 0, no timeout or signal) counts",
		"local-only PATH lookup is never used",
		"https://rust-analyzer.github.io/book/installation.html",
	} {
		if !strings.Contains(ov.ActionPopup.Message, want) {
			t.Errorf("help message lacks %q: %q", want, ov.ActionPopup.Message)
		}
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
	if got := lastNoticeText(r.h); !strings.Contains(got, "rust-analyzer is runnable on the workspace") {
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

func TestRustLspCheckLocalLookPathHitDoesNotFakeAuthoritySuccess(t *testing.T) {
	t.Parallel()
	// The reverse misjudgment: a local LookPath entry must not make the plugin report the
	// binary found when the workspace-authority probe cannot run it (e.g. a remote host
	// without rust-analyzer).
	r := rustRig(t, func(o *plugintest.FakeOS) {
		o.Bin["rust-analyzer"] = "/usr/local/bin/rust-analyzer"
	})
	if err := r.h.Run("rust_lsp.check", nil); err != nil {
		t.Fatal(err)
	}
	got := lastNoticeText(r.h)
	if !strings.Contains(got, "Could not run rust-analyzer on the workspace") {
		t.Fatalf("status = %q, want an authority probe failure", got)
	}
	if strings.Contains(got, "runnable on the workspace") {
		t.Fatalf("status trusted a local LookPath hit: %q", got)
	}
	if runs := r.os.Runs(); len(runs) != 1 {
		t.Fatalf("runs = %+v, want one authority probe", runs)
	}
}

func TestRustLspCheckTimeoutOrSignalIsNotRunnable(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		s    plugintest.ProcScript
		want string
	}{
		{"timeout", plugintest.ProcScript{Stdout: "rust-analyzer 1.2.3\n", TimedOut: true}, "timed out"},
		{"signal", plugintest.ProcScript{Stdout: "rust-analyzer 1.2.3\n", Signal: "KILL"}, "signal KILL"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := rustRig(t, func(o *plugintest.FakeOS) {
				o.Script([]string{"rust-analyzer", "--version"}, tt.s)
			})
			if err := r.h.Run("rust_lsp.check", nil); err != nil {
				t.Fatal(err)
			}
			got := lastNoticeText(r.h)
			if !strings.Contains(got, "runnable on the workspace, but --version failed") || !strings.Contains(got, tt.want) {
				t.Fatalf("status = %q, want a failed --version containing %q", got, tt.want)
			}
			if strings.Contains(got, "rust-analyzer is runnable on the workspace (") {
				t.Fatalf("abnormal exit reported as a successful version: %q", got)
			}
		})
	}
}

func TestRustLspCheckTrustGateRefusalIsNotMissing(t *testing.T) {
	t.Parallel()
	r := rustRig(t, func(o *plugintest.FakeOS) {
		o.Script([]string{"rust-analyzer", "--version"}, plugintest.ProcScript{Err: pluginapi.Errf(pluginapi.CodeInvalid, "workspace is not trusted")})
	})
	if err := r.h.Run("rust_lsp.check", nil); err != nil {
		t.Fatal(err)
	}
	got := lastNoticeText(r.h)
	for _, want := range []string{"workspace is not trusted", "Could not run rust-analyzer on the workspace", "workspace trust gate", "local-only", "cannot prove rust-analyzer is missing"} {
		if !strings.Contains(got, want) {
			t.Errorf("status = %q, want %q", got, want)
		}
	}
}
