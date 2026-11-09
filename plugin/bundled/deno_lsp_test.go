package bundled_test

import (
	"encoding/json"
	"io/fs"
	"slices"
	"strings"
	"testing"

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
		"already existed before the async probe answer landed",
		"plain Start/Restart restarts that manager, not Deno",
		"Explicit configuration wins and opts out",
		"tsx is untouched",
		"only controls its copy/check/help commands",
		"does not control the native Deno auto-selection",
		"runs in core regardless of whether this helper plugin is loaded/enabled",
		"auto_start: false",
		"workspace authority",
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
