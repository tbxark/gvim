package bundled_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/tbxark/gvim/config"
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

func slangDisableRig(t *testing.T, lspEnabled bool, buffers ...rigBuffer) *rig {
	t.Helper()
	if len(buffers) == 0 {
		buffers = []rigBuffer{{"/work/a.slang", "float3 x;\n"}}
	}
	r := startRig(t, rigOpts{
		plugins: []string{"slang_lsp"},
		buffers: buffers,
		script:  func(o *plugintest.FakeOS) { o.SetDir("/work") },
	})
	r.host.LSP().Enabled = lspEnabled
	for _, b := range r.host.Buffers {
		if strings.HasSuffix(strings.ToLower(b.Info.Path), ".slang") || strings.HasSuffix(strings.ToLower(b.Info.Path), ".slangh") {
			b.Info.Language = "slang"
		}
	}
	return r
}

func slangDisableAction(t *testing.T, r *rig) (pluginapi.Token, string) {
	t.Helper()
	if err := r.h.Run("slang_lsp.help", nil); err != nil {
		t.Fatal(err)
	}
	ov := r.host.UI().Last("action_popup")
	if ov == nil {
		t.Fatalf("no help popup (log %v)", r.h.LogText(""))
	}
	for _, a := range ov.ActionPopup.Actions {
		if a.ID == "act:lsp_disable_language" {
			return ov.Token, a.Label
		}
	}
	t.Fatalf("help popup lacks disable action: %+v", ov.ActionPopup.Actions)
	return 0, ""
}

func TestSlangLspDisableTargetsTheActiveLanguage(t *testing.T) {
	t.Parallel()
	for _, ext := range []string{"slang", "slangh"} {
		t.Run(ext, func(t *testing.T) {
			t.Parallel()
			path := "/work/a." + ext
			r := slangDisableRig(t, true, rigBuffer{path, "float3 x;\n"})
			token, label := slangDisableAction(t, r)
			if label != "Disable Slang LSP" {
				t.Fatalf("disable label = %q", label)
			}
			r.h.Resolve(pluginapi.Completion{Token: token, Values: []any{"act:lsp_disable_language"}})
			found := false
			for _, call := range r.host.Cmd().Calls {
				if call.Action == "lsp_disable_language_target" {
					found = true
					if call.Args["path"] != path || call.Args["language"] != "slang" {
						t.Fatalf("target args = %+v", call.Args)
					}
				}
			}
			if !found {
				t.Fatalf("commands = %v, want the controlled disable target", r.host.Cmd().Ran())
			}
			for _, notice := range r.h.NoticeTexts() {
				if strings.Contains(strings.ToLower(notice), "disabled") {
					t.Fatalf("plugin claimed async disable success: %v", r.h.NoticeTexts())
				}
			}
		})
	}
}

func TestSlangLspDisableRefusesNonTargetTab(t *testing.T) {
	t.Parallel()
	r := slangDisableRig(t, true,
		rigBuffer{"/work/a.slang", "float3 x;\n"},
		rigBuffer{"/work/notes.md", "# notes\n"},
	)
	token, _ := slangDisableAction(t, r)
	r.host.SetActive(r.ids["/work/notes.md"])
	r.h.Resolve(pluginapi.Completion{Token: token, Values: []any{"act:lsp_disable_language"}})
	for _, action := range r.host.Cmd().Ran() {
		if action == "lsp_disable_language_target" {
			t.Fatalf("disable dispatched for a non-Slang active buffer: %v", r.host.Cmd().Ran())
		}
	}
	if got := lastNoticeText(r.h); got != "Open a Slang file first (.slang, .slangh)" {
		t.Fatalf("status = %q", got)
	}
}

func TestSlangLspDisableRequiresLSP(t *testing.T) {
	t.Parallel()
	r := slangDisableRig(t, false)
	if err := r.h.Run("slang_lsp.help", nil); err != nil {
		t.Fatal(err)
	}
	ov := r.host.UI().Last("action_popup")
	if ov == nil {
		t.Fatalf("no help popup (log %v)", r.h.LogText(""))
	}
	for _, a := range ov.ActionPopup.Actions {
		if a.ID == "act:lsp_disable_language" {
			t.Fatalf("disable action offered without LSP: %+v", ov.ActionPopup.Actions)
		}
	}
	r2 := slangDisableRig(t, true)
	token, _ := slangDisableAction(t, r2)
	r2.host.LSP().Enabled = false
	r2.h.Resolve(pluginapi.Completion{Token: token, Values: []any{"act:lsp_disable_language"}})
	for _, action := range r2.host.Cmd().Ran() {
		if action == "lsp_disable_language_target" {
			t.Fatalf("disable dispatched after LSP disappeared: %v", r2.host.Cmd().Ran())
		}
	}
	if got := lastNoticeText(r2.h); got != "No language server is configured" {
		t.Fatalf("status = %q", got)
	}
}

func TestSlangLspDisableRejectsTextLanguage(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ path, language string }{
		{"/work/a.slang", "text"},
		{"/work/a.slangh", "text"},
		{"/work/a.slangh", ""},
	} {
		r := slangDisableRig(t, true, rigBuffer{tt.path, "float3 x;\n"})
		for _, b := range r.host.Buffers {
			if b.Info.Path == tt.path {
				b.Info.Language = tt.language
			}
		}
		if err := r.h.Run("slang_lsp.help", nil); err != nil {
			t.Fatal(err)
		}
		ov := r.host.UI().Last("action_popup")
		if ov == nil {
			t.Fatalf("no help popup (log %v)", r.h.LogText(""))
		}
		for _, a := range ov.ActionPopup.Actions {
			if a.ID == "act:lsp_disable_language" {
				t.Fatalf("disable offered for %s with language %q: %+v", tt.path, tt.language, ov.ActionPopup.Actions)
			}
		}
		if runs := r.host.Cmd().Ran(); len(runs) != 0 {
			t.Fatalf("language %q dispatched for %s: %v", tt.language, tt.path, runs)
		}
	}
}

func TestSlangLspDisableRechecksLanguageAfterPopup(t *testing.T) {
	t.Parallel()
	r := slangDisableRig(t, true)
	token, _ := slangDisableAction(t, r)
	for _, b := range r.host.Buffers {
		if b.Info.Path == "/work/a.slang" {
			b.Info.Language = "text"
		}
	}
	r.h.Resolve(pluginapi.Completion{Token: token, Values: []any{"act:lsp_disable_language"}})
	for _, action := range r.host.Cmd().Ran() {
		if action == "lsp_disable_language_target" {
			t.Fatalf("disable dispatched after language became text: %v", r.host.Cmd().Ran())
		}
	}
	if got := lastNoticeText(r.h); got != "Open a Slang file first (.slang, .slangh)" {
		t.Fatalf("status = %q", got)
	}
}

func TestSlangLspDisableAbsentForUnmappedSlanghDefault(t *testing.T) {
	t.Parallel()
	// Real default config detection: Fresh/gvim's default languages table maps only
	// .slang, so .slangh is unmapped (Text/empty) until a languages entry is added.
	detected := config.Defaults().EditorFor("", "/work/a.slangh").Language
	if detected == "slang" {
		t.Fatalf("default config detects a.slangh as slang; scheme B expects an unmapped/Text default")
	}
	if detected != "" && detected != "text" {
		t.Fatalf("default config language for a.slangh = %q, want empty/Text", detected)
	}
	r := slangDisableRig(t, true, rigBuffer{"/work/a.slangh", "float3 x;\n"})
	for _, b := range r.host.Buffers {
		if b.Info.Path == "/work/a.slangh" {
			b.Info.Language = detected
		}
	}
	if err := r.h.Run("slang_lsp.help", nil); err != nil {
		t.Fatal(err)
	}
	ov := r.host.UI().Last("action_popup")
	if ov == nil {
		t.Fatalf("no help popup (log %v)", r.h.LogText(""))
	}
	for _, a := range ov.ActionPopup.Actions {
		if a.ID == "act:lsp_disable_language" {
			t.Fatalf("disable offered for default-unmapped .slangh: %+v", ov.ActionPopup.Actions)
		}
	}
	if runs := r.host.Cmd().Ran(); len(runs) != 0 {
		t.Fatalf("default-unmapped .slangh dispatched: %v", runs)
	}
}
