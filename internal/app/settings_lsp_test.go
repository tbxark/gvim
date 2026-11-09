package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui"
)

// fakeSettingsLSP is the Settings page's LSP port: scripted statuses and recorded
// manual actions.
type fakeSettingsLSP struct {
	servers  []tui.LSPServerStatus
	started  []string
	stopped  [][2]string
	restarts [][2]string
	err      error
}

func (f *fakeSettingsLSP) Servers() []tui.LSPServerStatus { return f.servers }
func (f *fakeSettingsLSP) StartLanguage(language string) error {
	f.started = append(f.started, language)
	return f.err
}
func (f *fakeSettingsLSP) StopServer(language, name string) error {
	f.stopped = append(f.stopped, [2]string{language, name})
	return f.err
}
func (f *fakeSettingsLSP) RestartServer(language, name string) error {
	f.restarts = append(f.restarts, [2]string{language, name})
	return f.err
}

// lspSettingsService builds the page service over cfg with the fake LSP port and a
// user config file holding userJSON (when not empty). root is the workspace root.
func lspSettingsService(t *testing.T, root, userJSON string, cfg config.Config, lsp settingsLSP) *settingsService {
	t.Helper()
	user := filepath.Join(t.TempDir(), "config.json")
	if userJSON != "" {
		if err := os.WriteFile(user, []byte(userJSON), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return newSettingsService(root, Options{Config: user}, editor.New(editor.Options{}), func() bool { return true }, cfg, nil, settingsRuntime{lsp: lsp})
}

// TestSettingsLSPPageListsEffectiveServers checks the Language Servers page: the
// built-in table, the user's entries (which replace the built-in one for the
// language) and universal servers all appear with their live state.
func TestSettingsLSPPageListsEffectiveServers(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.LSP = map[string][]config.LSPServer{
		"go": {{Command: "my-gopls", Name: "gopls2"}},
	}
	cfg.UniversalLSP = map[string][]config.LSPServer{
		"eslint": {{Command: "eslint-lsp"}},
	}
	lsp := &fakeSettingsLSP{servers: []tui.LSPServerStatus{
		{Language: "go", Name: "gopls2", Command: "my-gopls", State: tui.LSPServerRunning, AutoStart: true},
		{Language: "", Name: "eslint-lsp", Command: "eslint-lsp", State: tui.LSPServerFailed, Error: "exec: not found"},
	}}
	s := lspSettingsService(t, "", "", cfg, lsp)
	st, err := s.Structure("lsp", tui.SettingsLayerUser)
	if err != nil {
		t.Fatal(err)
	}
	if st.Kind != "map" || st.AddLabel == "" {
		t.Fatalf("structure = %+v, want the addable language map", st)
	}
	find := func(name string) (tui.SettingsStructureRow, bool) {
		for _, row := range st.Rows {
			if row.Name == name {
				return row, true
			}
		}
		return tui.SettingsStructureRow{}, false
	}
	goRow, ok := find("go")
	if !ok || goRow.Value != "gopls2" || !strings.Contains(goRow.Note, "running") {
		t.Errorf("go row = %+v (ok %v), want the user's gopls2 running", goRow, ok)
	}
	if rust, ok := find("rust"); !ok || rust.Value != "rust-analyzer" {
		t.Errorf("rust row = %+v (ok %v), want the built-in server", rust, ok)
	}
	if uni, ok := find("universal: eslint"); !ok || !strings.Contains(uni.Note, "failed") {
		t.Errorf("universal row = %+v (ok %v), want the failing universal server", uni, ok)
	}

	// The language page shows the server read-only plus the manual actions and the
	// editable whole-array JSON.
	lang, err := s.Structure("lsp.go", tui.SettingsLayerUser)
	if err != nil {
		t.Fatal(err)
	}
	var jsonRow, startRow tui.SettingsStructureRow
	for _, row := range lang.Rows {
		if row.Kind == "json" {
			jsonRow = row
		}
		if row.Action == "act:lsp-start" {
			startRow = row
		}
	}
	var servers []config.LSPServer
	if err := json.Unmarshal([]byte(jsonRow.Value), &servers); err != nil || len(servers) != 1 || servers[0].Command != "my-gopls" {
		t.Errorf("JSON row = %q (%v), want the user's server array", jsonRow.Value, err)
	}
	if startRow.Value != "go" {
		t.Errorf("start action = %+v, want the go language", startRow)
	}
}

// TestSettingsLSPProjectLayer checks the project restrictions: the project page
// lists only the layer's own entries, and the JSON row is locked; the config
// validation refuses a program-choosing write while accepting a mute.
func TestSettingsLSPProjectLayer(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	project := filepath.Join(root, ".gvim", "config.json")
	if err := os.MkdirAll(filepath.Dir(project), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(project, []byte(`{"lsp": {"go": [{"enabled": true}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	s := lspSettingsService(t, root, "", cfg, &fakeSettingsLSP{})
	st, err := s.Structure("lsp", tui.SettingsLayerProject)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Rows) != 1 || st.Rows[0].Name != "go" {
		t.Fatalf("project rows = %+v, want only the layer's go entry", st.Rows)
	}
	lang, err := s.Structure("lsp.go", tui.SettingsLayerProject)
	if err != nil {
		t.Fatal(err)
	}
	var locked bool
	for _, row := range lang.Rows {
		if row.Kind == "json" {
			locked = row.Locked
		}
	}
	if !locked {
		t.Error("the project layer's JSON row is editable")
	}
	// The config rules: a project write that names a program is refused, a mute is
	// accepted.
	program := []config.Edit{{Path: []string{"lsp", "go"}, Raw: json.RawMessage(`[{"command": "evil"}]`)}}
	if diags := config.ValidateEdits(program, true); len(diags) == 0 {
		t.Error("a project layer program write passed validation")
	}
	mute := []config.Edit{{Path: []string{"lsp", "go"}, Raw: json.RawMessage(`[{"enabled": false}]`)}}
	if diags := config.ValidateEdits(mute, true); len(diags) != 0 {
		t.Errorf("a project mute failed validation: %v", diags)
	}
}

// TestSettingsLSPActions checks the manual start/stop/restart actions.
func TestSettingsLSPActions(t *testing.T) {
	t.Parallel()
	lsp := &fakeSettingsLSP{}
	s := lspSettingsService(t, "", "", config.Defaults(), lsp)
	msg, err := s.Act("lsp-start", "go")
	if err != nil || !strings.Contains(msg, "started") {
		t.Fatalf("start = %q, %v", msg, err)
	}
	if len(lsp.started) != 1 || lsp.started[0] != "go" {
		t.Errorf("started = %v, want go", lsp.started)
	}
	if _, err := s.Act("lsp-restart", "go\x00gopls2"); err != nil {
		t.Fatal(err)
	}
	if len(lsp.restarts) != 1 || lsp.restarts[0] != [2]string{"go", "gopls2"} {
		t.Errorf("restarts = %v, want go/gopls2", lsp.restarts)
	}
	if _, err := s.Act("lsp-stop", "go\x00"); err != nil {
		t.Fatal(err)
	}
	if len(lsp.stopped) != 1 || lsp.stopped[0] != [2]string{"go", ""} {
		t.Errorf("stopped = %v, want go", lsp.stopped)
	}
	if _, err := s.Act("nonsense", ""); err == nil {
		t.Error("an unknown action did not error")
	}
}

// TestSettingsReloadPushesReloadedLSPConfig checks the reload hook receives the new config so
// the LSP port can update its baseline (the port's initial server plan is otherwise fixed).
func TestSettingsReloadPushesReloadedLSPConfig(t *testing.T) {
	s := lspSettingsService(t, "", "{}", config.Defaults(), &fakeSettingsLSP{})
	var got config.Config
	s.rt.lspReload = func(next config.Config) { got = next }
	if err := os.WriteFile(s.userFile, []byte("{\"lsp\":{\"typescript\":{\"command\":\"new-ts\"}}}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.reload(nil, ""); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if list, ok := got.LSP["typescript"]; !ok || len(list) != 1 || list[0].Command != "new-ts" {
		t.Fatalf("reload hook config LSP[typescript] = %+v ok=%v, want the reloaded explicit server", list, ok)
	}
}
