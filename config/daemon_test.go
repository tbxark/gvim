package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// projectBody writes body as the project config file of a fresh root and returns the root.
func daemonProject(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, ".gvim")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

// TestDaemonDefaults: the daemon caps materialized workspaces at the documented default.
func TestDaemonDefaults(t *testing.T) {
	t.Parallel()
	cfg, diags := loadBody(t, "{}")
	if len(diags) != 0 {
		t.Fatalf("diagnostics = %v", diags)
	}
	if cfg.Daemon.MaterializedWorkspaces != DefaultMaterializedWorkspaces {
		t.Fatalf("materialized_workspaces = %d, want %d", cfg.Daemon.MaterializedWorkspaces, DefaultMaterializedWorkspaces)
	}
	if DefaultMaterializedWorkspaces != 8 {
		t.Fatalf("documented default changed: %d", DefaultMaterializedWorkspaces)
	}
}

// TestDaemonMaterializedWorkspaces: the user layer sets the cap; out-of-range, non-integer,
// null and unknown keys are diagnostics and leave the default.
func TestDaemonMaterializedWorkspaces(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		body    string
		want    int
		wantErr string
	}{
		{"a value", `{"daemon": {"materialized_workspaces": 3}}`, 3, ""},
		{"the minimum", `{"daemon": {"materialized_workspaces": 1}}`, 1, ""},
		{"the maximum", `{"daemon": {"materialized_workspaces": 64}}`, 64, ""},
		{"zero is refused", `{"daemon": {"materialized_workspaces": 0}}`, DefaultMaterializedWorkspaces, "between 1 and 64"},
		{"above the maximum", `{"daemon": {"materialized_workspaces": 65}}`, DefaultMaterializedWorkspaces, "between 1 and 64"},
		{"a non-integer", `{"daemon": {"materialized_workspaces": 2.5}}`, DefaultMaterializedWorkspaces, "must be an integer"},
		{"null", `{"daemon": {"materialized_workspaces": null}}`, DefaultMaterializedWorkspaces, "null is not a valid value"},
		{"an unknown key", `{"daemon": {"warm": 2}}`, DefaultMaterializedWorkspaces, "unknown setting"},
		{"not an object", `{"daemon": 4}`, DefaultMaterializedWorkspaces, "must be a JSON object"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, diags := loadBody(t, tt.body)
			if cfg.Daemon.MaterializedWorkspaces != tt.want {
				t.Fatalf("materialized_workspaces = %d, want %d (diagnostics %v)", cfg.Daemon.MaterializedWorkspaces, tt.want, diags)
			}
			if tt.wantErr == "" {
				if len(diags) != 0 {
					t.Fatalf("diagnostics = %v", diags)
				}
				return
			}
			if len(diags) != 1 || !strings.Contains(diags[0].Message, tt.wantErr) {
				t.Fatalf("diagnostics = %+v, want %q", diags, tt.wantErr)
			}
			if diags[0].Key != "daemon" && !strings.HasPrefix(diags[0].Key, "daemon.") {
				t.Fatalf("diagnostic key = %q, want the daemon section", diags[0].Key)
			}
		})
	}
}

// TestDaemonProjectLayerRefused: a repository cannot bound the daemon's memory.
func TestDaemonProjectLayerRefused(t *testing.T) {
	t.Parallel()
	root := daemonProject(t, `{"daemon": {"materialized_workspaces": 1}}`)
	user := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(user, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, diags, err := Load(Options{Root: root, UserFile: user})
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 1 || !strings.Contains(diags[0].Message, "project config cannot set daemon") {
		t.Fatalf("diagnostics = %+v, want the project refusal", diags)
	}
	if cfg.Daemon.MaterializedWorkspaces != DefaultMaterializedWorkspaces {
		t.Fatalf("project daemon config applied: %+v", cfg.Daemon)
	}
}
