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

func TestDenoLspCopyInstallPicksCurlOrBrew(t *testing.T) {
	t.Parallel()
	r := denoRig(t, nil)
	if err := r.h.Run("deno_lsp.copy_install", nil); err != nil {
		t.Fatal(err)
	}
	curl := "curl -fsSL https://deno.land/install.sh | sh"
	if got := r.host.Clipboard; got != curl {
		t.Fatalf("clipboard = %q, want the curl command %q", got, curl)
	}
	if got := lastNoticeText(r.h); got != "Copied: "+curl {
		t.Errorf("status = %q", got)
	}

	brewer := denoRig(t, func(o *plugintest.FakeOS) {
		o.Bin["brew"] = "/opt/homebrew/bin/brew"
	})
	if err := brewer.h.Run("deno_lsp.copy_install", nil); err != nil {
		t.Fatal(err)
	}
	brew := "brew install deno"
	if got := brewer.host.Clipboard; got != brew {
		t.Fatalf("clipboard = %q, want the brew command %q", got, brew)
	}
	if got := lastNoticeText(brewer.h); got != "Copied: "+brew {
		t.Errorf("status = %q", got)
	}
}

func TestDenoLspCheckProject(t *testing.T) {
	t.Parallel()
	t.Run("ready", func(t *testing.T) {
		t.Parallel()
		r := denoRig(t, func(o *plugintest.FakeOS) {
			o.SetFile("/work/deno.json", "{}\n")
			o.Bin["deno"] = "/usr/local/bin/deno"
		})
		if err := r.h.Run("deno_lsp.check_project", nil); err != nil {
			t.Fatal(err)
		}
		got := lastNoticeText(r.h)
		for _, want := range []string{"Deno project ready", "/work/deno.json", "/usr/local/bin/deno"} {
			if !strings.Contains(got, want) {
				t.Errorf("status = %q, want %q", got, want)
			}
		}
	})

	t.Run("jsonc and both project names", func(t *testing.T) {
		t.Parallel()
		r := denoRig(t, func(o *plugintest.FakeOS) {
			o.SetFile("/work/deno.jsonc", "{}\n")
			o.Bin["deno"] = "/usr/local/bin/deno"
		})
		if err := r.h.Run("deno_lsp.check_project", nil); err != nil {
			t.Fatal(err)
		}
		got := lastNoticeText(r.h)
		if !strings.Contains(got, "/work/deno.jsonc") {
			t.Errorf("status = %q, want the jsonc path", got)
		}
	})

	t.Run("config without deno", func(t *testing.T) {
		t.Parallel()
		r := denoRig(t, func(o *plugintest.FakeOS) {
			o.SetFile("/work/deno.json", "{}\n")
		})
		if err := r.h.Run("deno_lsp.check_project", nil); err != nil {
			t.Fatal(err)
		}
		got := lastNoticeText(r.h)
		for _, want := range []string{"/work/deno.json", "not on PATH", "curl -fsSL https://deno.land/install.sh | sh"} {
			if !strings.Contains(got, want) {
				t.Errorf("status = %q, want %q", got, want)
			}
		}
	})

	t.Run("deno without config", func(t *testing.T) {
		t.Parallel()
		r := denoRig(t, func(o *plugintest.FakeOS) {
			o.Bin["deno"] = "/usr/local/bin/deno"
		})
		if err := r.h.Run("deno_lsp.check_project", nil); err != nil {
			t.Fatal(err)
		}
		got := lastNoticeText(r.h)
		for _, want := range []string{"/usr/local/bin/deno", "no deno.json or deno.jsonc", "/work"} {
			if !strings.Contains(got, want) {
				t.Errorf("status = %q, want %q", got, want)
			}
		}
	})

	t.Run("neither", func(t *testing.T) {
		t.Parallel()
		r := denoRig(t, nil)
		if err := r.h.Run("deno_lsp.check_project", nil); err != nil {
			t.Fatal(err)
		}
		got := lastNoticeText(r.h)
		for _, want := range []string{"No deno.json/deno.jsonc in /work", "not on PATH", "curl -fsSL"} {
			if !strings.Contains(got, want) {
				t.Errorf("status = %q, want %q", got, want)
			}
		}
	})

	t.Run("workspace root", func(t *testing.T) {
		t.Parallel()
		r := denoRig(t, func(o *plugintest.FakeOS) {
			o.SetDir("/work/proj")
			o.SetFile("/work/proj/deno.jsonc", "{}\n")
			o.Bin["deno"] = "/usr/local/bin/deno"
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
		o.Bin["deno"] = "/usr/local/bin/deno"
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
		"auto_start",
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
