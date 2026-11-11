package trust

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/tbxark/gvim/config"
)

func TestMigrationFailureKeepsTrustStoreAvailable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("state and config share AppData on Windows")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	blocker := filepath.Join(home, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_STATE_HOME", blocker)
	oldDir, err := config.Dir()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test"), 0600); err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(oldDir, "trust.json")
	if err := NewStore(oldPath).Set(root, Restricted); err != nil {
		t.Fatal(err)
	}
	store, err := DefaultStore()
	if err != nil || store == nil {
		t.Fatalf("DefaultStore = %v, %v; want available store", store, err)
	}
	// The move failed, so the store keeps using the legacy file: the recorded
	// Restricted decision survives instead of falling back to the open policy
	// (which would trust a marker-bearing directory).
	if store.Path() != oldPath {
		t.Fatalf("store path = %q, want the legacy file %q", store.Path(), oldPath)
	}
	state, err := store.Open(root)
	if err != nil || state.Level != Restricted || !state.Decided {
		t.Fatalf("Open = %+v, %v; want the decided restricted state from the legacy file", state, err)
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("legacy trust file lost: %v", err)
	}
}

func TestDefaultStoreMigratesLegacyAndPreservesNewState(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "migrate", true: "new state wins"}[existing], func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("AppData", home)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			oldDir, err := config.Dir()
			if err != nil {
				t.Fatal(err)
			}
			stateDir, err := config.StateDir()
			if err != nil {
				t.Fatal(err)
			}
			oldPath, newPath := filepath.Join(oldDir, "trust.json"), filepath.Join(stateDir, "trust.json")
			root := t.TempDir()
			if err := NewStore(oldPath).Set(root, Trusted); err != nil {
				t.Fatal(err)
			}
			if existing {
				if err := NewStore(newPath).Set(root, Restricted); err != nil {
					t.Fatal(err)
				}
			}
			store, err := DefaultStore()
			if err != nil {
				t.Fatal(err)
			}
			if store.Path() != newPath {
				t.Fatalf("store path = %q, want %q", store.Path(), newPath)
			}
			want := Trusted
			if existing {
				want = Restricted
			}
			if got, ok := store.LevelFor(root); !ok || got != want {
				t.Fatalf("decision = %v, %v; want %v", got, ok, want)
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
