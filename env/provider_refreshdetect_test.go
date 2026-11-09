package env

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/config"
)

func TestProviderRefreshDetection(t *testing.T) {
	root := t.TempDir()
	detectors := []config.EnvDetector{{Name: "node", Markers: []string{".nvmrc"}, Kind: config.EnvKindShell, Snippet: "true"}}
	p := New(root, detectors, func() bool { return true })
	if det, ok := p.Detected(); ok || det.Name != "" {
		t.Fatalf("initial detection = %+v, %v", det, ok)
	}
	marker := filepath.Join(root, ".nvmrc")
	if err := os.WriteFile(marker, []byte("20"), 0o600); err != nil {
		t.Fatal(err)
	}
	p.RefreshDetection()
	if det, ok := p.Detected(); !ok || det.Name != "node" {
		t.Fatalf("detection after marker creation = %+v, %v", det, ok)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	p.RefreshDetection()
	if det, ok := p.Detected(); ok || det.Name != "" {
		t.Fatalf("detection after marker removal = %+v, %v", det, ok)
	}
	p.SetWatchNotice("Local environment: watching unavailable; use Env: Reload")
	if got := p.WatchNotice(); got == "" {
		t.Fatal("watch notice was not recorded")
	}
}
