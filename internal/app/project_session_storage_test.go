package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectSessionStorage(t *testing.T) {
	for _, tc := range []struct {
		name, config string
		enabled      bool
	}{
		{"default off", `{}`, false},
		{"disabled", `{"editor":{"save_session":false}}`, false},
		{"enabled", `{"editor":{"save_session":true}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateConfig(t)
			root := t.TempDir()
			rt, err := buildRuntime(t.Context(), Options{Root: root, Config: userConfigFile(t, tc.config), NoPlugins: true}, false)
			must(t, err)
			defer rt.close()
			must(t, rt.save())
			must(t, rt.saveSearch())
			if tc.enabled {
				if _, err := os.Stat(filepath.Join(root, ".gvim", "session", "session.json")); err != nil {
					t.Fatal(err)
				}
			} else if _, err := os.Stat(filepath.Join(root, ".gvim")); !os.IsNotExist(err) {
				t.Fatalf("unexpected .gvim: %v", err)
			}
		})
	}
}
