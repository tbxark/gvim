package bundled_test

import (
	"encoding/json"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin/bundled"
	"github.com/tbxark/gvim/pluginapi"
)

func envManagerRig(t *testing.T) *rig {
	t.Helper()
	return startRig(t, rigOpts{plugins: []string{"env_manager"}, timeout: 500 * time.Millisecond})
}

func TestEnvManagerManifestAndCommands(t *testing.T) {
	data, err := fs.ReadFile(bundled.FS, "plugins/env_manager/plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		APIVersion     int  `json:"api"`
		DefaultEnabled bool `json:"default_enabled"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.APIVersion != 2 || manifest.DefaultEnabled {
		t.Fatalf("manifest = %+v, want api 2 and off by default", manifest)
	}
	r := envManagerRig(t)
	var got []string
	for _, c := range r.host.Commands["env_manager"] {
		if !c.Hidden {
			got = append(got, c.ID+"="+c.Title)
		}
	}
	want := []string{
		"env_manager.activate=Env: Activate Detected Environment (venv / direnv / mise)",
		"env_manager.reload=Env: Reload Environment (re-capture after .envrc/mise.toml change)",
		"env_manager.status=Env: Show Environment Status",
		"env_manager.use_system=Env: Use System (Deactivate Environment)",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("commands = %v, want %v", got, want)
	}
	if len(r.host.Bindings["env_manager"]) != 0 {
		t.Errorf("bindings = %+v, want none", r.host.Bindings["env_manager"])
	}
	var status []string
	for _, it := range r.host.Status["env_manager"] {
		status = append(status, it.ID)
	}
	if len(status) != 1 || status[0] != "env" {
		t.Errorf("status items = %v, want [env]", status)
	}
}

func TestEnvManagerAutoActivatesPathOnlyWhenTrusted(t *testing.T) {
	r := envManagerRig(t)
	r.host.HasEnvDetected = true
	r.host.EnvDetected = pluginapi.EnvInfo{Name: ".venv", Kind: "path-only", Snippet: "source .venv/bin/activate"}
	r.host.EnvTrust = "trusted"
	r.h.Emit(pluginapi.Event{Name: "ready"})
	r.h.Settle()
	if len(r.host.EnvSets) != 1 {
		t.Fatalf("SetEnv calls = %+v, want the path-only auto activation", r.host.EnvSets)
	}
	if got := r.host.EnvSets[0]; got.Snippet != "source .venv/bin/activate" || got.Kind != "path-only" || got.Plugin != "env_manager" {
		t.Fatalf("SetEnv = %+v", got)
	}
	if len(r.host.Status["env_manager"]) == 0 || !strings.Contains(r.host.Status["env_manager"][0].Text, ".venv") {
		t.Errorf("status pill = %+v, want the detected name", r.host.Status["env_manager"])
	}
}

func TestEnvManagerShellKindWaitsForTheCommand(t *testing.T) {
	r := envManagerRig(t)
	r.host.HasEnvDetected = true
	r.host.EnvDetected = pluginapi.EnvInfo{Name: "direnv", Kind: "shell", Snippet: "eval \"$(direnv export bash)\""}
	r.host.EnvTrust = "trusted"
	r.h.Emit(pluginapi.Event{Name: "ready"})
	r.h.Settle()
	if len(r.host.EnvSets) != 0 {
		t.Fatalf("a shell environment auto-activated: %+v", r.host.EnvSets)
	}
	if err := r.h.Run("env_manager.activate", nil); err != nil {
		t.Fatal(err)
	}
	if len(r.host.EnvSets) != 1 || r.host.EnvSets[0].Kind != "shell" {
		t.Fatalf("SetEnv after the command = %+v", r.host.EnvSets)
	}
}

func TestEnvManagerUntrustedRequestsTheCorePrompt(t *testing.T) {
	r := envManagerRig(t)
	r.host.HasEnvDetected = true
	r.host.EnvDetected = pluginapi.EnvInfo{Name: ".venv", Kind: "path-only", Snippet: "source .venv/bin/activate"}
	r.host.EnvTrust = "restricted"
	r.h.Emit(pluginapi.Event{Name: "ready"})
	r.h.Settle()
	if len(r.host.EnvSets) != 0 {
		t.Fatalf("activated in an untrusted workspace: %+v", r.host.EnvSets)
	}
	if err := r.h.Run("env_manager.activate", nil); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, call := range r.host.Cmd().Calls {
		if call.Action == "workspace_trust_prompt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("actions = %v, want workspace_trust_prompt", r.host.Cmd().Calls)
	}
}

func TestEnvManagerActivatesOnTrustChanged(t *testing.T) {
	r := envManagerRig(t)
	r.host.HasEnvDetected = true
	r.host.EnvDetected = pluginapi.EnvInfo{Name: ".venv", Kind: "path-only", Snippet: "source .venv/bin/activate"}
	r.host.EnvTrust = "restricted"
	r.h.Emit(pluginapi.Event{Name: "ready"})
	r.h.Settle()
	r.host.EnvTrust = "trusted"
	r.h.Emit(pluginapi.Event{Name: "trust_changed", Data: map[string]any{"level": "trusted"}})
	r.h.Settle()
	if len(r.host.EnvSets) != 1 {
		t.Fatalf("SetEnv after trust_changed = %+v, want one activation", r.host.EnvSets)
	}
	if err := r.h.Run("env_manager.use_system", nil); err != nil {
		t.Fatal(err)
	}
	if r.host.EnvClears != 1 {
		t.Fatalf("ClearEnv calls = %d, want 1", r.host.EnvClears)
	}
}
