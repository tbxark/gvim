package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/env"
	"github.com/tbxark/gvim/internal/tui"
)

// TestSettingsEnvironmentRowsShowTheProjectEnvironment: the read-only Environment
// section and the Detectors page report detection, activation state and the
// resolved detector list.
func TestSettingsEnvironmentRowsShowTheProjectEnvironment(t *testing.T) {
	root := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(root, ".venv", "bin"), 0o755))
	must(t, os.WriteFile(filepath.Join(root, ".venv", "bin", "python"), nil, 0o644))
	p := env.New(root, nil, func() bool { return true })
	svc := &settingsService{root: root, trusted: func() bool { return true }, env: p}
	cfg := config.Defaults()
	snap := svc.snapshotFor(cfg, nil)
	section := settingsSectionNamed(snap, "Environment")
	if section == nil {
		t.Fatalf("no Environment section: %+v", snap.Sections)
	}
	values := map[string]string{}
	for _, item := range section.Items {
		values[item.Name] = item.Value
	}
	if got := values["Project environment"]; !strings.Contains(got, ".venv") || !strings.Contains(got, "path-only") {
		t.Errorf("Project environment row = %q, want .venv (path-only)", got)
	}
	if got := values["Active environment"]; got != "system" {
		t.Errorf("Active environment row = %q, want system", got)
	}
	if got := values["Detectors"]; got != "6 detectors" {
		t.Errorf("Detectors row = %q, want 6 detectors", got)
	}
	structure := svc.structureEnvDetectors(cfg)
	if structure.Kind != "entries" || len(structure.Rows) != 6 {
		t.Fatalf("detector structure = %+v", structure)
	}
	if structure.Rows[0].Name != ".venv" || !strings.Contains(structure.Rows[0].Value, "path-only") {
		t.Errorf("first detector row = %+v", structure.Rows[0])
	}
	if err := p.Set("source .venv/bin/activate", root, config.EnvKindPathOnly); err != nil {
		t.Fatal(err)
	}
	snap = svc.snapshotFor(cfg, nil)
	section = settingsSectionNamed(snap, "Environment")
	for _, item := range section.Items {
		if item.Name == "Active environment" && item.Value != ".venv" {
			t.Errorf("Active environment row = %q after activation", item.Value)
		}
	}
}

// settingsSectionNamed returns the snapshot section called name.
func settingsSectionNamed(snap tui.SettingsSnapshot, name string) *tui.SettingsSection {
	for i := range snap.Sections {
		if snap.Sections[i].Name == name {
			return &snap.Sections[i]
		}
	}
	return nil
}
