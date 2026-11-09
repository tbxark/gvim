package env

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/config"
)

func TestProviderSetDetectorsReDetectsLocally(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".nvmrc"), []byte("20"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := New(root, []config.EnvDetector{{Name: "direnv", Markers: []string{".envrc"}, Kind: config.EnvKindShell, Snippet: "true"}}, func() bool { return true })
	if det, ok := p.Detected(); ok || det.Name != "" {
		t.Fatalf("initial detection = %+v, %v", det, ok)
	}
	p.SetDetectors([]config.EnvDetector{{Name: "node", Markers: []string{".nvmrc"}, Kind: config.EnvKindShell, Snippet: "true"}})
	det, ok := p.Detected()
	if !ok || det.Name != "node" || det.Kind != config.EnvKindShell {
		t.Fatalf("detection after SetDetectors = %+v, %v", det, ok)
	}
	if got := p.Detectors(); len(got) != 1 || got[0].Name != "node" {
		t.Fatalf("detectors = %+v", got)
	}
	// Empty restores the built-in list and re-scans with it.
	p.SetDetectors(nil)
	if got := p.Detectors(); len(got) != len(config.DefaultEnvDetectors()) {
		t.Fatalf("nil SetDetectors = %d detectors, want defaults", len(got))
	}
	if det, ok := p.Detected(); ok {
		t.Fatalf("default detectors detected %+v on a root with only .nvmrc", det)
	}
}

func TestProviderSetDetectorsLeavesActiveRecipeAndCaptureCache(t *testing.T) {
	root := t.TempDir()
	p := New(root, nil, func() bool { return true })
	runs := 0
	p.SetRunner(func(_ string, command string) (string, error) {
		runs++
		if command == "env -0" {
			return "BASE=1\x00", nil
		}
		return "BASE=1\x00ACTIVE=1\x00", nil
	})
	if err := p.Set("true", "", config.EnvKindShell); err != nil {
		t.Fatal(err)
	}
	if got := p.Overlay(); len(got) != 1 || got[0] != "ACTIVE=1" {
		t.Fatalf("overlay = %v", got)
	}
	captured := runs
	if captured != 2 {
		t.Fatalf("initial capture ran %d shell commands, want 2", captured)
	}
	p.SetDetectors([]config.EnvDetector{{Name: "node", Markers: []string{".nvmrc"}, Kind: config.EnvKindShell, Snippet: "true"}})
	// The detector change is detection-only: it must not drop the active recipe or force
	// a re-capture (the input fingerprint did not change).
	if _, ok := p.Active(); !ok {
		t.Fatal("SetDetectors dropped the active recipe")
	}
	_ = p.Overlay()
	if runs != captured {
		t.Fatalf("SetDetectors invalidated the capture cache: %d→%d shell runs", captured, runs)
	}
}
