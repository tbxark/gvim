package trust

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/config"
)

// writeFile writes a file under root, creating parents.
func writeEnvFile(t *testing.T, root, rel, body string) string {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDetectEnvVirtualenvNeedsEvidence(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".venv"), 0o755); err != nil {
		t.Fatal(err)
	}
	det, incomplete := DetectEnv(root, config.DefaultEnvDetectors())
	if det.Name != "" {
		t.Fatalf("bare .venv detected as %q, want none", det.Name)
	}
	if !incomplete {
		t.Error("bare .venv not reported as incomplete")
	}
	writeEnvFile(t, root, ".venv/bin/python", "")
	det, _ = DetectEnv(root, config.DefaultEnvDetectors())
	if det.Name != ".venv" || det.Kind != config.EnvKindPathOnly {
		t.Fatalf("detected = %+v, want .venv path-only", det)
	}
	if det.Snippet != "source .venv/bin/activate" {
		t.Errorf("snippet = %q", det.Snippet)
	}
}

func TestDetectEnvFirstMatchWins(t *testing.T) {
	root := t.TempDir()
	writeEnvFile(t, root, ".venv/bin/python", "")
	writeEnvFile(t, root, ".envrc", "export X=1")
	det, _ := DetectEnv(root, config.DefaultEnvDetectors())
	if det.Name != ".venv" {
		t.Fatalf("detected = %q, want the first detector .venv", det.Name)
	}
}

func TestDetectEnvExpandsDir(t *testing.T) {
	root := t.TempDir()
	writeEnvFile(t, root, "custom.marker", "")
	detectors := []config.EnvDetector{{
		Name: "custom", Markers: []string{"custom.marker"}, Kind: config.EnvKindShell,
		Snippet: "activate {dir}",
	}}
	det, _ := DetectEnv(root, detectors)
	if det.Name != "custom" || !strings.Contains(det.Snippet, root) {
		t.Fatalf("detected = %+v, want {dir} expanded to %s", det, root)
	}
}

func TestEnvMarkersSharedWithTrustMarkers(t *testing.T) {
	markers := EnvMarkers()
	for _, want := range []string{".venv", ".envrc", "mise.toml", "Pipfile", "poetry.lock"} {
		found := false
		for _, m := range markers {
			if m == want {
				found = true
			}
		}
		if !found {
			t.Errorf("env markers %v miss %q", markers, want)
		}
	}
	root := t.TempDir()
	writeEnvFile(t, root, ".envrc", "export X=1")
	contentMarkers := ExecutableContentMarkers(root)
	if !containsString(contentMarkers, ".envrc") {
		t.Errorf("executable markers %v miss .envrc", contentMarkers)
	}
}

func containsString(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}
