package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/config"
)

// loadLayerConfig loads a user config file body and returns the config with its
// diagnostics.
func loadLayerConfig(t *testing.T, body string) (config.Config, []config.Diagnostic) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, diags, err := config.Load(config.Options{UserFile: path})
	if err != nil {
		t.Fatal(err)
	}
	return cfg, diags
}
