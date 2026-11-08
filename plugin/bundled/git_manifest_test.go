package bundled_test

import (
	"encoding/json"
	"io/fs"
	"strings"
	"testing"

	"github.com/tbxark/gvim/plugin/bundled"
)

// The git ports ship a manifest with a description, a version and default_enabled, and the
// library they share is found as gvim.git.
func TestGitPortsShipManifests(t *testing.T) {
	for _, name := range []string{"git_gutter", "git_statusbar", "git_find_file", "git_blame"} {
		t.Run(name, func(t *testing.T) {
			data, err := fs.ReadFile(bundled.FS, "plugins/"+name+"/plugin.json")
			if err != nil {
				t.Fatal(err)
			}
			var m struct {
				Name           string `json:"name"`
				Version        string `json:"version"`
				Description    string `json:"description"`
				API            int    `json:"api"`
				DefaultEnabled *bool  `json:"default_enabled"`
			}
			if err := json.Unmarshal(data, &m); err != nil {
				t.Fatal(err)
			}
			if m.Name != name || m.Version == "" || strings.TrimSpace(m.Description) == "" || m.API != 1 {
				t.Errorf("manifest = %+v", m)
			}
			if m.DefaultEnabled == nil || !*m.DefaultEnabled {
				t.Errorf("default_enabled = %v, want true", m.DefaultEnabled)
			}
			if _, err := fs.Stat(bundled.FS, "plugins/"+name+"/init.lua"); err != nil {
				t.Error(err)
			}
		})
	}
	if src, ok := bundled.Lib("git"); !ok || !strings.Contains(src, "protocol.ext.allow=never") {
		t.Errorf("Lib(git) = %d bytes, %v", len(src), ok)
	}
}
