package config

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// loadLSP writes body as the user config and returns the effective config.
func loadLSP(t *testing.T, body string) (Config, []Diagnostic) {
	t.Helper()
	return loadBody(t, body)
}

// TestLoadLSPKeepsEveryServerOfALanguage checks Fresh's multi-server shape: the lsp
// key of a language takes one server or a list, and every enabled server is kept, in
// the order the config gives them.
func TestLoadLSPKeepsEveryServerOfALanguage(t *testing.T) {
	cfg, diags := loadLSP(t, `{"lsp": {
		"python": [
			{"command": "pyright-langserver", "args": ["--stdio"]},
			{"command": "ruff", "args": ["server"], "only_features": ["format", "diagnostics"]}
		],
		"go": {"command": "gopls"}
	}}`)
	if len(diags) != 0 {
		t.Fatalf("diags = %v", diags)
	}
	if got := len(cfg.LSP["python"]); got != 2 {
		t.Fatalf("python servers = %d, want 2", got)
	}
	if cfg.LSP["python"][0].Command != "pyright-langserver" || cfg.LSP["python"][1].Command != "ruff" {
		t.Fatalf("python servers = %+v, want pyright then ruff", cfg.LSP["python"])
	}
	if got := cfg.LSP["python"][1].OnlyFeatures; !reflect.DeepEqual(got, []string{"format", "diagnostics"}) {
		t.Fatalf("only_features = %v", got)
	}
	if got := len(cfg.LSP["go"]); got != 1 {
		t.Fatalf("go servers = %d, want 1", got)
	}
}

// TestLoadLSPKeepsFreshServerFields checks the fields gvim used to accept and ignore:
// a Fresh config loads them into the effective server, and process_limits is kept
// even though gvim does not enforce it.
func TestLoadLSPKeepsFreshServerFields(t *testing.T) {
	cfg, diags := loadLSP(t, `{"lsp": {"go": {
		"command": "gopls",
		"name": "Go Language Server",
		"env": {"GOFLAGS": "-mod=mod"},
		"language_id_overrides": {"go": "golang"},
		"initialization_options": {"usePlaceholders": true},
		"only_features": ["diagnostics", "hover", "definition"],
		"process_limits": {"max_memory_percent": 40, "max_cpu_percent": 80, "enabled": true}
	}}}`)
	if len(diags) != 0 {
		t.Fatalf("diags = %v", diags)
	}
	server := cfg.LSP["go"][0]
	if server.Name != "Go Language Server" {
		t.Errorf("name = %q", server.Name)
	}
	if !reflect.DeepEqual(server.Env, map[string]string{"GOFLAGS": "-mod=mod"}) {
		t.Errorf("env = %v", server.Env)
	}
	if !reflect.DeepEqual(server.LanguageIDOverrides, map[string]string{"go": "golang"}) {
		t.Errorf("language_id_overrides = %v", server.LanguageIDOverrides)
	}
	if !jsonEqual(t, server.InitializationOptions, `{"usePlaceholders": true}`) {
		t.Errorf("initialization_options = %s", server.InitializationOptions)
	}
	if !reflect.DeepEqual(server.OnlyFeatures, []string{"diagnostics", "hover", "definition"}) {
		t.Errorf("only_features = %v", server.OnlyFeatures)
	}
	limits := server.ProcessLimits
	if limits == nil || limits.MaxMemoryPercent == nil || *limits.MaxMemoryPercent != 40 ||
		limits.MaxCPUPercent == nil || *limits.MaxCPUPercent != 80 || limits.Enabled == nil || !*limits.Enabled {
		t.Errorf("process_limits = %+v", limits)
	}
}

// TestLoadLSPRejectsUnknownFeature checks that a typo in a feature filter is a load
// diagnostic, not a silently empty filter.
func TestLoadLSPRejectsUnknownFeature(t *testing.T) {
	_, diags := loadLSP(t, `{"lsp": {"go": {"command": "gopls", "only_features": ["diag"]}}}`)
	if len(diags) != 1 || diags[0].Key != "lsp" || !strings.Contains(diags[0].Message, "unknown feature") {
		t.Fatalf("diags = %v, want one for key lsp naming the unknown feature", diags)
	}
}

// TestLoadLSPUniversalServers checks the universal_lsp key: servers there attach to
// every language, keep Fresh's single-or-list shape, and an entry turned off is not
// run.
func TestLoadLSPUniversalServers(t *testing.T) {
	cfg, diags := loadLSP(t, `{"universal_lsp": {
		"spell": {"command": "harper-ls", "args": ["--stdio"], "except_features": ["format"]},
		"off": {"command": "true", "enabled": false}
	}}`)
	if len(diags) != 0 {
		t.Fatalf("diags = %v", diags)
	}
	if len(cfg.UniversalLSP) != 1 {
		t.Fatalf("universal servers = %+v, want only the enabled one", cfg.UniversalLSP)
	}
	spell := cfg.UniversalLSP["spell"]
	if len(spell) != 1 || spell[0].Command != "harper-ls" || !reflect.DeepEqual(spell[0].ExceptFeatures, []string{"format"}) {
		t.Fatalf("spell = %+v", spell)
	}
	if _, ok := cfg.UniversalLSP["off"]; ok {
		t.Error("a disabled universal server is kept")
	}

	if _, diags := loadLSP(t, `{"universal_lsp": []}`); len(diags) != 1 {
		t.Errorf("a list for universal_lsp gave %d diagnostics, want 1", len(diags))
	}
}

// TestProjectLSPCannotShapeTheProgram checks the C11 rule for the new fields: a
// repository may switch a server on or off and route its features, but the fields
// that choose or shape the program are refused, each named in a diagnostic, and the
// user's entry is untouched.
func TestProjectLSPCannotShapeTheProgram(t *testing.T) {
	const user = `{"lsp": {"go": {"command": "my-gopls"}}}`
	dir := t.TempDir()
	userFile := filepath.Join(dir, "user.json")
	writeFile(t, userFile, user)
	root := filepath.Join(dir, "proj")
	writeFile(t, filepath.Join(root, ".gvim", "config.json"), `{"lsp": {"rust": {
		"command": "cargo",
		"env": {"LD_PRELOAD": "/tmp/x.so"},
		"initialization_options": {"run": "x"},
		"process_limits": {"enabled": true},
		"name": "renamed",
		"args": ["-x"]
	}}}`)
	cfg, diags, err := Load(Options{Root: root, UserFile: userFile})
	if err != nil {
		t.Fatal(err)
	}
	// command, env, initialization_options, process_limits, name and args: the
	// command drops the entry whole, so its other fields are not reported one by one;
	// the remaining ones each get a diagnostic.
	if len(diags) == 0 {
		t.Fatal("project config shaped the program with no diagnostic")
	}
	for _, d := range diags {
		if d.Key != "lsp" || !strings.Contains(d.Message, "user config") {
			t.Errorf("diagnostic %+v does not tell the user to set it in the user config", d)
		}
	}
	if _, ok := cfg.LSP["rust"]; ok {
		t.Errorf("project config added a rust server: %+v", cfg.LSP["rust"])
	}
	if len(cfg.LSP["go"]) != 1 || cfg.LSP["go"][0].Command != "my-gopls" {
		t.Errorf("the user's go server changed: %+v", cfg.LSP["go"])
	}
}

// TestProjectLSPRoutingFieldsAreAllowed checks the fields that do not shape the
// program: enablement, auto_start, root markers and feature filters come through.
func TestProjectLSPRoutingFieldsAreAllowed(t *testing.T) {
	dir := t.TempDir()
	userFile := filepath.Join(dir, "user.json")
	writeFile(t, userFile, `{}`)
	root := filepath.Join(dir, "proj")
	writeFile(t, filepath.Join(root, ".gvim", "config.json"), `{"lsp": {
		"python": {"enabled": true, "auto_start": false, "root_markers": ["pyproject.toml"], "only_features": ["diagnostics"]}
	}}`)
	cfg, diags, err := Load(Options{Root: root, UserFile: userFile})
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Fatalf("diags = %v", diags)
	}
	server := cfg.LSP["python"][0]
	if !server.Dormant || !reflect.DeepEqual(server.RootMarkers, []string{"pyproject.toml"}) ||
		!reflect.DeepEqual(server.OnlyFeatures, []string{"diagnostics"}) {
		t.Fatalf("python server = %+v", server)
	}
}

// TestProjectUniversalLSPIsRefused checks that a repository cannot add a universal
// server, which would run for every buffer.
func TestProjectUniversalLSPIsRefused(t *testing.T) {
	dir := t.TempDir()
	userFile := filepath.Join(dir, "user.json")
	writeFile(t, userFile, `{}`)
	root := filepath.Join(dir, "proj")
	writeFile(t, filepath.Join(root, ".gvim", "config.json"),
		`{"universal_lsp": {"evil": {"command": "sh", "args": ["-c", "id"]}}}`)
	cfg, diags, err := Load(Options{Root: root, UserFile: userFile})
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 1 || diags[0].Key != "universal_lsp" || !strings.Contains(diags[0].Message, "user config") {
		t.Fatalf("diags = %v, want one refusing universal_lsp", diags)
	}
	if cfg.UniversalLSP != nil {
		t.Fatalf("UniversalLSP = %+v, want none", cfg.UniversalLSP)
	}
}

// TestProcessLimitsValidation checks the load-time validation of process_limits: 1..100
// percent of memory, and 1..1000 percent CPU (100 is one core), while the fields are kept
// for the runtime to apply or report.
func TestProcessLimitsValidation(t *testing.T) {
	cfg, diags := loadLSP(t, `{"lsp": {"go": {"command": "gopls",
		"process_limits": {"max_memory_percent": 50, "max_cpu_percent": 200}}}}`)
	if len(diags) != 0 {
		t.Fatalf("diags = %+v, want none", diags)
	}
	limits := cfg.LSP["go"][0].ProcessLimits
	if limits == nil || limits.MaxMemoryPercent == nil || *limits.MaxMemoryPercent != 50 ||
		limits.MaxCPUPercent == nil || *limits.MaxCPUPercent != 200 || limits.Enabled != nil {
		t.Fatalf("process_limits = %+v", limits)
	}
	tests := []struct {
		name string
		body string
		want string
	}{
		{"memory zero", `{"lsp": {"go": {"command": "gopls", "process_limits": {"max_memory_percent": 0}}}}`,
			"max_memory_percent must be between 1 and 100"},
		{"memory over", `{"lsp": {"go": {"command": "gopls", "process_limits": {"max_memory_percent": 101}}}}`,
			"max_memory_percent must be between 1 and 100"},
		{"cpu zero", `{"lsp": {"go": {"command": "gopls", "process_limits": {"max_cpu_percent": 0}}}}`,
			"max_cpu_percent must be between 1 and 1000"},
		{"cpu over", `{"lsp": {"go": {"command": "gopls", "process_limits": {"max_cpu_percent": 1001}}}}`,
			"max_cpu_percent must be between 1 and 1000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := loadLSP(t, tt.body)
			var messages []string
			for _, d := range diags {
				messages = append(messages, d.Message)
			}
			if len(diags) == 0 || !strings.Contains(strings.Join(messages, "\n"), tt.want) {
				t.Fatalf("diags = %+v, want %q", diags, tt.want)
			}
		})
	}
	// The boundaries are accepted, and enabled:false is kept for the runtime.
	cfg, diags = loadLSP(t, `{"lsp": {"go": {"command": "gopls",
		"process_limits": {"max_memory_percent": 1, "max_cpu_percent": 1000, "enabled": false}}}}`)
	if len(diags) != 0 {
		t.Fatalf("boundary diags = %+v", diags)
	}
	limits = cfg.LSP["go"][0].ProcessLimits
	if limits == nil || limits.Enabled == nil || *limits.Enabled {
		t.Fatalf("process_limits = %+v, want enabled false kept", limits)
	}
}
