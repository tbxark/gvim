package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/trust"
)

func TestOpenTrustMigratesGlobalDecision(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	oldDir, err := config.Dir()
	if err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(oldDir, "trust.json")
	if err := trust.NewStore(oldPath).Set(root, trust.Restricted); err != nil {
		t.Fatal(err)
	}
	store, state, warning := openTrust(root)
	if store == nil || state.Level != trust.Restricted || !state.Decided || warning != "" {
		t.Fatalf("openTrust = %v, %+v, %q", store, state, warning)
	}
	stateDir, err := config.StateDir()
	if err != nil {
		t.Fatal(err)
	}
	if store.Path() != filepath.Join(stateDir, "trust.json") {
		t.Fatalf("store path = %q", store.Path())
	}
	if _, err := os.Stat(filepath.Join(root, ".gvim")); !os.IsNotExist(err) {
		t.Fatalf("trust created project state: %v", err)
	}
}
