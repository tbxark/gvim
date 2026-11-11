package workspace

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/config"
)

func TestRecentMigratesLegacyAndPreservesNewState(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "migrate", true: "new state wins"}[existing], func(t *testing.T) {
			isolateConfig(t)
			oldDir, err := config.Dir()
			if err != nil {
				t.Fatal(err)
			}
			stateDir, err := config.StateDir()
			if err != nil {
				t.Fatal(err)
			}
			legacyRoot, newRoot := t.TempDir(), t.TempDir()
			write := func(path, root string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				b, err := json.Marshal(recentFile{Roots: []string{root}})
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, b, 0600); err != nil {
					t.Fatal(err)
				}
			}
			oldPath, newPath := filepath.Join(oldDir, "recent.json"), filepath.Join(stateDir, "recent.json")
			write(oldPath, legacyRoot)
			if existing {
				write(newPath, newRoot)
			}
			roots, err := Recent()
			if err != nil {
				t.Fatal(err)
			}
			want := legacyRoot
			if existing {
				want = newRoot
			}
			if len(roots) != 1 || roots[0].Path() != want {
				t.Fatalf("Recent = %v, want %s", roots, want)
			}
			_, err = os.Stat(oldPath)
			if existing && err != nil {
				t.Fatalf("legacy file changed: %v", err)
			}
			if !existing && !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("legacy file remains: %v", err)
			}
		})
	}
}
