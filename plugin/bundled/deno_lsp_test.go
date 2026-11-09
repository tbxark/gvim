package bundled_test

import (
	"encoding/json"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/pluginapi"

	bundled "github.com/tbxark/gvim/plugin/bundled"
	"github.com/tbxark/gvim/plugin/plugintest"
)

// denoRig loads deno_lsp with the fake OS rooted at /work.
func denoRig(t *testing.T, script func(o *plugintest.FakeOS)) *rig {
	t.Helper()
	return startRig(t, rigOpts{
		plugins: []string{"deno_lsp"},
		script: func(o *plugintest.FakeOS) {
			o.SetDir("/work")
			if script != nil {
				script(o)
			}
		},
	})
}

func TestDenoLspManifestAndCommands(t *testing.T) {
	t.Parallel()
	data, err := fs.ReadFile(bundled.FS, "plugins/deno_lsp/plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	name, _ := m["name"].(string)
	version, _ := m["version"].(string)
	description, _ := m["description"].(string)
	api, _ := m["api"].(float64)
	enabled, enabledOK := m["default_enabled"].(bool)
	if name != "deno_lsp" || version == "" || strings.TrimSpace(description) == "" {
		t.Errorf("manifest = %v", m)
	}
	if api != 2 {
		t.Errorf("api = %v, want 2", m["api"])
	}
	if !enabledOK || enabled {
		t.Errorf("default_enabled = %v, want false (language helper)", m["default_enabled"])
	}

	r := denoRig(t, nil)
	if info, ok := r.h.Plugin("deno_lsp"); !ok || info.Status != "loaded" {
		t.Fatalf("plugin = %+v (log %v)", info, r.h.LogText(""))
	}
	want := map[string]string{
		"deno_lsp.check_project": "Deno: Check Project",
		"deno_lsp.copy_install":  "Deno: Copy Install Command",
		"deno_lsp.help":          "Deno: Language Server Help",
	}
	got := map[string]string{}
	for _, c := range r.host.Commands["deno_lsp"] {
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
	if bindings := r.host.Bindings["deno_lsp"]; len(bindings) != 0 {
		t.Errorf("bindings = %v, want none", bindings)
	}
}

func TestDenoLspCopyInstallAlwaysCurl(t *testing.T) {
	t.Parallel()
	// A local brew must not choose the copied command: in a remote workspace it says
	// nothing about the remote host, so Copy Install always copies the official installer.
	curl := "curl -fsSL https://deno.land/install.sh | sh"
	for _, bin := range []map[string]string{nil, {"/opt/homebrew/bin/brew": "/opt/homebrew/bin/brew"}} {
		r := denoRig(t, func(o *plugintest.FakeOS) {
			for name, path := range bin {
				o.Bin[name] = path
			}
		})
		if err := r.h.Run("deno_lsp.copy_install", nil); err != nil {
			t.Fatal(err)
		}
		if got := r.host.Clipboard; got != curl {
			t.Fatalf("clipboard with brew bin %v = %q, want the curl command %q", bin, got, curl)
		}
		if got := lastNoticeText(r.h); got != "Copied: "+curl {
			t.Errorf("status = %q", got)
		}
	}
}

func TestDenoLspCheckProject(t *testing.T) {
	t.Parallel()
	t.Run("ready through the authority despite a local LookPath miss", func(t *testing.T) {
		t.Parallel()
		// The local LookPath is deliberately empty: the authority probe (gvim.proc.run)
		// is what proves deno is runnable, so a remote workspace is never misjudged.
		r := denoRig(t, func(o *plugintest.FakeOS) {
			o.SetFile("/work/deno.json", "{}\n")
			o.Script([]string{"deno", "--version"}, plugintest.ProcScript{Stdout: "deno 2.1.0\n"})
		})
		if err := r.h.Run("deno_lsp.check_project", nil); err != nil {
			t.Fatal(err)
		}
		got := lastNoticeText(r.h)
		for _, want := range []string{"Deno project ready", "/work/deno.json", "runnable on the workspace"} {
			if !strings.Contains(got, want) {
				t.Errorf("status = %q, want %q", got, want)
			}
		}
		if strings.Contains(got, "not on PATH") {
			t.Errorf("status overclaims a missing deno: %q", got)
		}
	})

	t.Run("jsonc and both project names", func(t *testing.T) {
		t.Parallel()
		r := denoRig(t, func(o *plugintest.FakeOS) {
			o.SetFile("/work/deno.jsonc", "{}\n")
			o.Script([]string{"deno", "--version"}, plugintest.ProcScript{Stdout: "deno 2.1.0\n"})
		})
		if err := r.h.Run("deno_lsp.check_project", nil); err != nil {
			t.Fatal(err)
		}
		got := lastNoticeText(r.h)
		if !strings.Contains(got, "/work/deno.jsonc") {
			t.Errorf("status = %q, want the jsonc path", got)
		}
	})

	t.Run("config but probe cannot run deno", func(t *testing.T) {
		t.Parallel()
		r := denoRig(t, func(o *plugintest.FakeOS) {
			o.SetFile("/work/deno.json", "{}\n")
		})
		if err := r.h.Run("deno_lsp.check_project", nil); err != nil {
			t.Fatal(err)
		}
		got := lastNoticeText(r.h)
		for _, want := range []string{"/work/deno.json", "could not be run on the workspace", "local-only", "cannot prove deno is missing", "curl -fsSL https://deno.land/install.sh | sh"} {
			if !strings.Contains(got, want) {
				t.Errorf("status = %q, want %q", got, want)
			}
		}
		if strings.Contains(got, "not on PATH") {
			t.Errorf("status overclaims a missing deno: %q", got)
		}
	})

	t.Run("deno without config", func(t *testing.T) {
		t.Parallel()
		r := denoRig(t, func(o *plugintest.FakeOS) {
			o.Script([]string{"deno", "--version"}, plugintest.ProcScript{Stdout: "deno 2.1.0\n"})
		})
		if err := r.h.Run("deno_lsp.check_project", nil); err != nil {
			t.Fatal(err)
		}
		got := lastNoticeText(r.h)
		for _, want := range []string{"runnable on the workspace", "no deno.json or deno.jsonc", "/work"} {
			if !strings.Contains(got, want) {
				t.Errorf("status = %q, want %q", got, want)
			}
		}
	})

	t.Run("neither and probe cannot run deno", func(t *testing.T) {
		t.Parallel()
		r := denoRig(t, nil)
		if err := r.h.Run("deno_lsp.check_project", nil); err != nil {
			t.Fatal(err)
		}
		got := lastNoticeText(r.h)
		for _, want := range []string{"No deno.json/deno.jsonc in /work", "could not be run on the workspace", "local-only", "cannot prove deno is missing", "curl -fsSL"} {
			if !strings.Contains(got, want) {
				t.Errorf("status = %q, want %q", got, want)
			}
		}
		if strings.Contains(got, "not on PATH") {
			t.Errorf("status overclaims a missing deno: %q", got)
		}
	})

	t.Run("workspace root", func(t *testing.T) {
		t.Parallel()
		r := denoRig(t, func(o *plugintest.FakeOS) {
			o.SetDir("/work/proj")
			o.SetFile("/work/proj/deno.jsonc", "{}\n")
			o.Script([]string{"deno", "--version"}, plugintest.ProcScript{Stdout: "deno 2.1.0\n"})
		})
		r.host.Workspace().RootDir = "/work/proj"
		if err := r.h.Run("deno_lsp.check_project", nil); err != nil {
			t.Fatal(err)
		}
		if got := lastNoticeText(r.h); !strings.Contains(got, "/work/proj/deno.jsonc") {
			t.Errorf("status = %q, want the workspace-root config", got)
		}
	})
}

func TestDenoLspHelpPopup(t *testing.T) {
	t.Parallel()
	r := denoRig(t, func(o *plugintest.FakeOS) {
		o.SetFile("/work/deno.json", "{}\n")
		o.Script([]string{"deno", "--version"}, plugintest.ProcScript{Stdout: "deno 2.1.0\n"})
	})
	if err := r.h.Run("deno_lsp.help", nil); err != nil {
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
	for _, want := range []string{"copy:curl", "copy:brew", "act:check", "act:lsp_restart", "act:show_lsp_status", "dismiss"} {
		if !ids[want] {
			t.Errorf("help popup lacks %q (actions %v)", want, ids)
		}
	}
	msg := ov.ActionPopup.Message
	for _, want := range []string{
		"deno lsp",
		"deno.json",
		"curl -fsSL https://deno.land/install.sh | sh",
		"brew install deno",
		"https://docs.deno.com/runtime/getting_started/installation/",
		"How gvim chooses deno lsp",
		"Native auto-selection (core behaviour, not this helper plugin)",
		"probed workspace root (the root gvim opened or switched to)",
		"lsp_enabled is true and the language is typescript/javascript",
		"only applies to an LSP manager created for that exact workspace root",
		"nested/per-file TS/JS roots keep their configured server",
		"installing or clearing it never stops a running server",
		"explicit Start/Restart LSP Server for typescript/javascript applies the completed current-root selection",
		"replaces the whole language server list and restarts only that language's servers",
		"other languages and universal servers are untouched",
		"same explicit action restores the configured baseline",
		"Restart Server on any row of that language replaces the entire server list",
		"clicked row's name is not used in that branch",
		"root-wide LSP Restart rebuilds managers",
		"Settings Save/Reload pushes the reloaded config's server baseline",
		"reload itself does not rebuild or restart a running manager",
		"Deno auto-selection is not applied while an explicit/disabled entry is present",
		"Trust gates live",
		"reload and revoke/restore pass the LSP config gate before the baseline",
		"Restricted/Blocked cannot gain auto-starting servers",
		"after a revoke an already-created manager's old AutoStart configs do not start servers on Open",
		"running ones are not stopped",
		"manual Start/Restart stays allowed",
		"Auto-start and launch spawn are two live checks, not atomic",
		"refuses only when the spawn gate denies",
		"Blocked, remote workspace not Trusted, or a project-contained executable",
		"under Restricted a system-installed binary outside the project may still start",
		"A revoke after the spawn gate passes can likewise let that in-flight start proceed",
		"Neither check nor revoke stops an existing process",
		"later Opens do not auto-start after a revoke",
		"Both replacing and same-config Restart preflight the spawn gate before stopping",
		"refusal preserves running servers/config",
		"preflight→launch is non-transactional",
		"old servers may be stopped and the new start refused when the spawn gate denies",
		"though under Restricted a system-installed binary outside the project may still start",
		"repeated Start of a running server returns the live handle without spawning or gating, even when Blocked",
		"missing-binary/initialize failures are not transactional",
		"strictly clean exit 0",
		"disconnected authority select nothing",
		"Explicit configuration wins and opts out",
		"tsx is untouched",
		"only controls its copy/check/help commands",
		"does not control native auto-selection",
		"runs in core regardless of plugin load/enable",
		"auto_start: false",
		"workspace authority",
		"workspace trust gate (the gate may refuse the probe)",
		"only a successful check (exit 0, no timeout or signal) counts",
		"local-only PATH lookup is never used",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("help message lacks %q", want)
		}
	}

	// Copy from the popup.
	r.h.Resolve(pluginapi.Completion{Token: ov.Token, Values: []any{"copy:brew"}})
	if got, want := r.host.Clipboard, "brew install deno"; got != want {
		t.Fatalf("clipboard = %q, want %q", got, want)
	}
	// Check from the popup.
	if err := r.h.Run("deno_lsp.help", nil); err != nil {
		t.Fatal(err)
	}
	ov = r.host.UI().Last("action_popup")
	r.h.Resolve(pluginapi.Completion{Token: ov.Token, Values: []any{"act:check"}})
	if got := lastNoticeText(r.h); !strings.Contains(got, "Deno project ready") {
		t.Fatalf("check status = %q", got)
	}
	// Native restart action from the popup.
	if err := r.h.Run("deno_lsp.help", nil); err != nil {
		t.Fatal(err)
	}
	ov = r.host.UI().Last("action_popup")
	r.h.Resolve(pluginapi.Completion{Token: ov.Token, Values: []any{"act:lsp_restart"}})
	if got := r.host.Cmd().Ran(); !slices.Contains(got, "lsp_restart") {
		t.Fatalf("actions = %v, want lsp_restart", got)
	}
}

func TestDenoLspCheckLocalLookPathHitDoesNotFakeAuthoritySuccess(t *testing.T) {
	t.Parallel()
	r := denoRig(t, func(o *plugintest.FakeOS) {
		o.SetFile("/work/deno.json", "{}\n")
		o.Bin["deno"] = "/usr/local/bin/deno"
	})
	if err := r.h.Run("deno_lsp.check_project", nil); err != nil {
		t.Fatal(err)
	}
	got := lastNoticeText(r.h)
	if !strings.Contains(got, "could not be run on the workspace") {
		t.Fatalf("status = %q, want an authority probe failure", got)
	}
	if strings.Contains(got, "Deno project ready") {
		t.Fatalf("status trusted a local LookPath hit: %q", got)
	}
}

func TestDenoLspCheckNonZeroExitIsNotReady(t *testing.T) {
	t.Parallel()
	t.Run("config and non-zero --version", func(t *testing.T) {
		t.Parallel()
		r := denoRig(t, func(o *plugintest.FakeOS) {
			o.SetFile("/work/deno.json", "{}\n")
			o.Script([]string{"deno", "--version"}, plugintest.ProcScript{Code: 1, Stdout: "deno 2.1.0\n", Stderr: "error: cannot load deno\n"})
		})
		if err := r.h.Run("deno_lsp.check_project", nil); err != nil {
			t.Fatal(err)
		}
		got := lastNoticeText(r.h)
		for _, want := range []string{"cannot be reported ready", "did not exit successfully", "error: cannot load deno"} {
			if !strings.Contains(got, want) {
				t.Errorf("status = %q, want %q", got, want)
			}
		}
		for _, forbidden := range []string{"Deno project ready", "deno is runnable on the workspace"} {
			if strings.Contains(got, forbidden) {
				t.Errorf("non-zero --version reported success (%q): %q", forbidden, got)
			}
		}
		if runs := r.os.Runs(); len(runs) != 1 || !slices.Equal(runs[0].Argv, []string{"deno", "--version"}) {
			t.Fatalf("runs = %+v, want one authority --version probe", runs)
		}
	})

	t.Run("no config and non-zero --version", func(t *testing.T) {
		t.Parallel()
		r := denoRig(t, func(o *plugintest.FakeOS) {
			o.Script([]string{"deno", "--version"}, plugintest.ProcScript{Code: 1, Stdout: "deno 2.1.0\n"})
		})
		if err := r.h.Run("deno_lsp.check_project", nil); err != nil {
			t.Fatal(err)
		}
		got := lastNoticeText(r.h)
		for _, want := range []string{"cannot be reported ready", "no deno.json or deno.jsonc", "exit code 1"} {
			if !strings.Contains(got, want) {
				t.Errorf("status = %q, want %q", got, want)
			}
		}
		if strings.Contains(got, "deno is runnable on the workspace") {
			t.Errorf("non-zero --version reported success: %q", got)
		}
	})

	t.Run("timeout is not ready even with version output", func(t *testing.T) {
		t.Parallel()
		r := denoRig(t, func(o *plugintest.FakeOS) {
			o.SetFile("/work/deno.json", "{}\n")
			o.Script([]string{"deno", "--version"}, plugintest.ProcScript{Stdout: "deno 2.1.0\n", TimedOut: true})
		})
		if err := r.h.Run("deno_lsp.check_project", nil); err != nil {
			t.Fatal(err)
		}
		got := lastNoticeText(r.h)
		if !strings.Contains(got, "timed out") || !strings.Contains(got, "cannot be reported ready") {
			t.Fatalf("timeout status = %q", got)
		}
		if strings.Contains(got, "Deno project ready") {
			t.Fatalf("timed-out --version reported ready: %q", got)
		}
	})

	t.Run("signal is not ready", func(t *testing.T) {
		t.Parallel()
		r := denoRig(t, func(o *plugintest.FakeOS) {
			o.SetFile("/work/deno.json", "{}\n")
			o.Script([]string{"deno", "--version"}, plugintest.ProcScript{Signal: "KILL"})
		})
		if err := r.h.Run("deno_lsp.check_project", nil); err != nil {
			t.Fatal(err)
		}
		got := lastNoticeText(r.h)
		if !strings.Contains(got, "signal KILL") || !strings.Contains(got, "cannot be reported ready") {
			t.Fatalf("signal status = %q", got)
		}
	})

	t.Run("trust-gate refusal is a probe failure, never missing", func(t *testing.T) {
		t.Parallel()
		r := denoRig(t, func(o *plugintest.FakeOS) {
			o.SetFile("/work/deno.json", "{}\n")
			o.Script([]string{"deno", "--version"}, plugintest.ProcScript{Err: pluginapi.Errf(pluginapi.CodeInvalid, "workspace is not trusted")})
		})
		if err := r.h.Run("deno_lsp.check_project", nil); err != nil {
			t.Fatal(err)
		}
		got := lastNoticeText(r.h)
		for _, want := range []string{"workspace is not trusted", "could not be run on the workspace", "trust gate", "local-only", "cannot prove deno is missing"} {
			if !strings.Contains(got, want) {
				t.Errorf("status = %q, want %q", got, want)
			}
		}
	})
}

func denoDisableRig(t *testing.T, lspEnabled bool, buffers ...rigBuffer) *rig {
	t.Helper()
	if len(buffers) == 0 {
		buffers = []rigBuffer{{"/work/a.ts", "const x = 1\n"}}
	}
	r := startRig(t, rigOpts{
		plugins: []string{"deno_lsp"},
		buffers: buffers,
		script:  func(o *plugintest.FakeOS) { o.SetDir("/work") },
	})
	r.host.LSP().Enabled = lspEnabled
	for _, b := range r.host.Buffers {
		switch strings.ToLower(b.Info.Path) {
		case "/work/a.ts", "/work/a.tsx":
			b.Info.Language = "typescript"
		case "/work/a.js", "/work/a.jsx", "/work/a.mjs", "/work/a.cjs":
			b.Info.Language = "javascript"
		}
	}
	return r
}

func denoDisableAction(t *testing.T, r *rig) (pluginapi.Token, string) {
	t.Helper()
	if err := r.h.Run("deno_lsp.help", nil); err != nil {
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

func TestDenoLspDisableTargetsTheActiveLanguage(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ ext, language string }{
		{"ts", "typescript"}, {"tsx", "tsx"},
		{"js", "javascript"}, {"jsx", "javascript"}, {"mjs", "javascript"}, {"cjs", "javascript"},
	} {
		t.Run(tt.ext, func(t *testing.T) {
			t.Parallel()
			path := "/work/a." + tt.ext
			r := denoDisableRig(t, true, rigBuffer{path, "x\n"})
			token, label := denoDisableAction(t, r)
			if label != "Disable Deno LSP for this language" {
				t.Fatalf("disable label = %q", label)
			}
			r.h.Resolve(pluginapi.Completion{Token: token, Values: []any{"act:lsp_disable_language"}})
			found := false
			for _, call := range r.host.Cmd().Calls {
				if call.Action == "lsp_disable_language_target" {
					found = true
					if call.Args["path"] != path || call.Args["language"] != tt.language {
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

func TestDenoLspDisableRefusesNonTargetTab(t *testing.T) {
	t.Parallel()
	r := denoDisableRig(t, true,
		rigBuffer{"/work/a.ts", "x\n"},
		rigBuffer{"/work/notes.md", "# notes\n"},
	)
	token, _ := denoDisableAction(t, r)
	r.host.SetActive(r.ids["/work/notes.md"])
	r.h.Resolve(pluginapi.Completion{Token: token, Values: []any{"act:lsp_disable_language"}})
	for _, action := range r.host.Cmd().Ran() {
		if action == "lsp_disable_language_target" {
			t.Fatalf("disable dispatched for a non-TS/JS active buffer: %v", r.host.Cmd().Ran())
		}
	}
	if got := lastNoticeText(r.h); got != "Open a TypeScript or JavaScript file first (.ts, .tsx, .js, .jsx, .mjs, .cjs)" {
		t.Fatalf("status = %q", got)
	}
}

func TestDenoLspDisableRequiresLSP(t *testing.T) {
	t.Parallel()
	// Without LSP the action is not offered at all.
	r := denoDisableRig(t, false)
	if err := r.h.Run("deno_lsp.help", nil); err != nil {
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
	// The action can still become stale if LSP disappears after the popup opened.
	r2 := denoDisableRig(t, true)
	token, _ := denoDisableAction(t, r2)
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

func TestDenoLspDisableRejectsUnrecognizedLanguages(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, path, language string
	}{
		{"cjs detected as text", "/work/a.cjs", "text"},
		{"tsx detected as text", "/work/a.tsx", "text"},
		{"jsx detected as react", "/work/a.jsx", "react"},
		{"ts detected as text", "/work/a.ts", "text"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := denoDisableRig(t, true, rigBuffer{tt.path, "x\n"})
			for _, b := range r.host.Buffers {
				if b.Info.Path == tt.path {
					b.Info.Language = tt.language
				}
			}
			if err := r.h.Run("deno_lsp.help", nil); err != nil {
				t.Fatal(err)
			}
			ov := r.host.UI().Last("action_popup")
			if ov == nil {
				t.Fatalf("no help popup (log %v)", r.h.LogText(""))
			}
			for _, a := range ov.ActionPopup.Actions {
				if a.ID == "act:lsp_disable_language" {
					t.Fatalf("disable offered for language %q: %+v", tt.language, ov.ActionPopup.Actions)
				}
			}
			if runs := r.host.Cmd().Ran(); len(runs) != 0 {
				t.Fatalf("unrecognized language dispatched: %v", runs)
			}
		})
	}
}

func TestDenoLspDisableRechecksLanguageAfterPopup(t *testing.T) {
	t.Parallel()
	r := denoDisableRig(t, true)
	token, _ := denoDisableAction(t, r)
	for _, b := range r.host.Buffers {
		if b.Info.Path == "/work/a.ts" {
			b.Info.Language = "text"
		}
	}
	r.h.Resolve(pluginapi.Completion{Token: token, Values: []any{"act:lsp_disable_language"}})
	for _, action := range r.host.Cmd().Ran() {
		if action == "lsp_disable_language_target" {
			t.Fatalf("disable dispatched after language became text: %v", r.host.Cmd().Ran())
		}
	}
	if got := lastNoticeText(r.h); got != "Open a TypeScript or JavaScript file first (.ts, .tsx, .js, .jsx, .mjs, .cjs)" {
		t.Fatalf("status = %q", got)
	}
}

func TestDenoLspTsxLanguageIDMapsToTsxTarget(t *testing.T) {
	t.Parallel()
	r := denoDisableRig(t, true, rigBuffer{"/work/a.tsx", "x\n"})
	for _, b := range r.host.Buffers {
		if b.Info.Path == "/work/a.tsx" {
			b.Info.Language = "tsx"
		}
	}
	token, _ := denoDisableAction(t, r)
	r.h.Resolve(pluginapi.Completion{Token: token, Values: []any{"act:lsp_disable_language"}})
	found := false
	for _, call := range r.host.Cmd().Calls {
		if call.Action == "lsp_disable_language_target" {
			found = true
			if call.Args["path"] != "/work/a.tsx" || call.Args["language"] != "tsx" {
				t.Fatalf("target args = %+v", call.Args)
			}
		}
	}
	if !found {
		t.Fatalf("commands = %v, want tsx target", r.host.Cmd().Ran())
	}
}

func TestDenoLspDisableAbsentForUnmappedCJSDefault(t *testing.T) {
	t.Parallel()
	// Use the real default config detection path rather than a hand-set fake language:
	// Fresh/gvim's default languages table does not map cjs to javascript.
	detected := config.Defaults().EditorFor("", "/work/a.cjs").Language
	if detected == "javascript" {
		t.Fatalf("default config detects a.cjs as javascript; scheme B expects an unmapped/Text default")
	}
	if detected != "" && detected != "text" {
		t.Fatalf("default config language for a.cjs = %q, want empty/Text", detected)
	}
	r := denoDisableRig(t, true, rigBuffer{"/work/a.cjs", "x\n"})
	for _, b := range r.host.Buffers {
		if b.Info.Path == "/work/a.cjs" {
			b.Info.Language = detected
		}
	}
	if err := r.h.Run("deno_lsp.help", nil); err != nil {
		t.Fatal(err)
	}
	ov := r.host.UI().Last("action_popup")
	if ov == nil {
		t.Fatalf("no help popup (log %v)", r.h.LogText(""))
	}
	for _, a := range ov.ActionPopup.Actions {
		if a.ID == "act:lsp_disable_language" {
			t.Fatalf("disable offered for default-unmapped .cjs: %+v", ov.ActionPopup.Actions)
		}
	}
	if runs := r.host.Cmd().Ran(); len(runs) != 0 {
		t.Fatalf("default-unmapped .cjs dispatched: %v", runs)
	}
}
