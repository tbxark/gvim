package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/trust"
)

// openTrust uses the per-user state dir store and ignores a trust.json left in the
// config dir by an older release.
func TestOpenTrustUsesTheStateDirStore(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	oldDir, err := config.Dir()
	if err != nil {
		t.Fatal(err)
	}
	if err := trust.NewStore(filepath.Join(oldDir, "trust.json")).Set(root, trust.Restricted); err != nil {
		t.Fatal(err)
	}
	store, state, warning := openTrust(root)
	// The old decision was Restricted; a markerless folder with no recorded decision
	// in the state store is trusted silently.
	if store == nil || state.Level != trust.Trusted || warning != "" {
		t.Fatalf("openTrust = %v, %+v, %q; the old config-dir decision was read", store, state, warning)
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
