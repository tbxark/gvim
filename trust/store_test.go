package trust

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/config"
)

// storeAt returns a store for the file path in a fresh temporary directory.
func storeAt(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config", "trust.json")
	return NewStore(path), path
}

// writeStoreFile writes body as JSON to path, creating the parent directory.
func writeStoreFile(t *testing.T, path string, body any) {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCanonicalRootResolvesSymlinks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	link := filepath.Join(dir, "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Skipf("symlinks are not available: %v", err)
	}
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{dir, link} {
		got, err := CanonicalRoot(root)
		if err != nil {
			t.Fatalf("CanonicalRoot(%q) error = %v", root, err)
		}
		if got != want {
			t.Errorf("CanonicalRoot(%q) = %q, want %q", root, got, want)
		}
	}
}

func TestCanonicalRootOfMissingPathIsAbsolute(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	missing := filepath.Join(dir, "not", "there")
	got, err := CanonicalRoot(missing)
	if err != nil {
		t.Fatalf("CanonicalRoot(%q) error = %v", missing, err)
	}
	if got != filepath.Clean(missing) {
		t.Fatalf("CanonicalRoot = %q, want %q", got, filepath.Clean(missing))
	}
}

func TestLevelForMissingStoreIsUndecided(t *testing.T) {
	t.Parallel()
	store, _ := storeAt(t)
	if level, decided := store.LevelFor(t.TempDir()); decided || level != Undecided {
		t.Fatalf("LevelFor = (%q, %v), want undecided", string(level), decided)
	}
}

func TestSetAndLevelForRoundTrip(t *testing.T) {
	t.Parallel()
	store, path := storeAt(t)
	root := t.TempDir()
	if err := store.Set(root, Restricted); err != nil {
		t.Fatalf("Set error = %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("store file was not written: %v", err)
	}
	if level, decided := store.LevelFor(root); !decided || level != Restricted {
		t.Fatalf("LevelFor = (%q, %v), want (restricted, true)", string(level), decided)
	}
	// Overwrite wins, and a fresh store reads the same file.
	if err := store.Set(root, Trusted); err != nil {
		t.Fatalf("Set error = %v", err)
	}
	if level, decided := NewStore(path).LevelFor(root); !decided || level != Trusted {
		t.Fatalf("LevelFor after overwrite = (%q, %v), want (trusted, true)", string(level), decided)
	}
}

func TestSetKeepsOtherWorkspaces(t *testing.T) {
	t.Parallel()
	store, path := storeAt(t)
	a, b := t.TempDir(), t.TempDir()
	if err := store.Set(a, Trusted); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(b, Blocked); err != nil {
		t.Fatal(err)
	}
	fresh := NewStore(path)
	if level, decided := fresh.LevelFor(a); !decided || level != Trusted {
		t.Errorf("a = (%q, %v), want (trusted, true)", string(level), decided)
	}
	if level, decided := fresh.LevelFor(b); !decided || level != Blocked {
		t.Errorf("b = (%q, %v), want (blocked, true)", string(level), decided)
	}
}

func TestSetCreatesTheDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "trust.json")
	if err := NewStore(path).Set(t.TempDir(), Trusted); err != nil {
		t.Fatalf("Set error = %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("store file was not written: %v", err)
	}
}

func TestSetRejectsInvalidLevel(t *testing.T) {
	t.Parallel()
	store, path := storeAt(t)
	if err := store.Set(t.TempDir(), Undecided); err == nil {
		t.Fatal("Set(undecided) returned no error")
	}
	if err := store.Set(t.TempDir(), Level("nope")); err == nil {
		t.Fatal("Set(unknown level) returned no error")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("a refused Set wrote the store: %v", err)
	}
}

// TestFileFormat locks the on-disk shape: a version and a map of canonical
// workspace roots to the stored level names.
func TestFileFormat(t *testing.T) {
	t.Parallel()
	store, path := storeAt(t)
	root := t.TempDir()
	canonical, err := CanonicalRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(root, Blocked); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Version    int               `json:"version"`
		Workspaces map[string]string `json:"workspaces"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("store is not valid JSON: %v\n%s", err, data)
	}
	if got.Version != storeVersion {
		t.Errorf("version = %d, want %d", got.Version, storeVersion)
	}
	if got.Workspaces[canonical] != "blocked" {
		t.Errorf("workspaces[%q] = %q, want %q", canonical, got.Workspaces[canonical], "blocked")
	}
}

func TestCorruptStoreReadsUndecidedAndIsReplaced(t *testing.T) {
	t.Parallel()
	store, path := storeAt(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if _, decided := store.LevelFor(root); decided {
		t.Fatal("a corrupt store reported a decision")
	}
	if err := store.Set(root, Trusted); err != nil {
		t.Fatalf("Set over a corrupt store error = %v", err)
	}
	if level, decided := NewStore(path).LevelFor(root); !decided || level != Trusted {
		t.Fatalf("LevelFor after rewrite = (%q, %v), want (trusted, true)", string(level), decided)
	}
}

func TestNewerStoreVersionReadsUndecided(t *testing.T) {
	t.Parallel()
	store, path := storeAt(t)
	root := t.TempDir()
	canonical, err := CanonicalRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	writeStoreFile(t, path, map[string]any{
		"version":    99,
		"workspaces": map[string]string{canonical: "trusted"},
	})
	if _, decided := store.LevelFor(root); decided {
		t.Fatal("a newer store version reported a decision")
	}
}

func TestUnknownLevelEntryReadsUndecided(t *testing.T) {
	t.Parallel()
	store, path := storeAt(t)
	good, bad := t.TempDir(), t.TempDir()
	goodKey, err := CanonicalRoot(good)
	if err != nil {
		t.Fatal(err)
	}
	badKey, err := CanonicalRoot(bad)
	if err != nil {
		t.Fatal(err)
	}
	writeStoreFile(t, path, map[string]any{
		"version":    1,
		"workspaces": map[string]string{goodKey: "trusted", badKey: "maybe"},
	})
	if level, decided := store.LevelFor(good); !decided || level != Trusted {
		t.Errorf("good entry = (%q, %v), want (trusted, true)", string(level), decided)
	}
	if _, decided := store.LevelFor(bad); decided {
		t.Error("an entry with an unknown level reported a decision")
	}
}

func TestOpenHonorsRecordedDecision(t *testing.T) {
	t.Parallel()
	for _, level := range []Level{Trusted, Restricted, Blocked} {
		t.Run(string(level), func(t *testing.T) {
			t.Parallel()
			store, _ := storeAt(t)
			root := t.TempDir()
			// A marker is present, yet the recorded decision wins.
			if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := store.Set(root, level); err != nil {
				t.Fatal(err)
			}
			state, err := store.Open(root)
			if err != nil {
				t.Fatalf("Open error = %v", err)
			}
			if state.Level != level || !state.Decided || state.NeedsPrompt() {
				t.Fatalf("Open = %+v, want level %q decided with no prompt", state, string(level))
			}
			if len(state.Markers) != 1 || state.Markers[0] != "go.mod" {
				t.Fatalf("Markers = %v, want [go.mod] for the dialog to list", state.Markers)
			}
		})
	}
}

func TestOpenTrustsPlainFolderSilentlyAndRecordsIt(t *testing.T) {
	t.Parallel()
	store, path := storeAt(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	state, err := store.Open(root)
	if err != nil {
		t.Fatalf("Open error = %v", err)
	}
	if state.Level != Trusted || !state.Decided || state.NeedsPrompt() {
		t.Fatalf("Open = %+v, want trusted and decided", state)
	}
	if level, decided := NewStore(path).LevelFor(root); !decided || level != Trusted {
		t.Fatalf("silent decision was not persisted: (%q, %v)", string(level), decided)
	}
}

func TestOpenMarkerFolderStaysUndecidedAndRestricted(t *testing.T) {
	t.Parallel()
	store, path := storeAt(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Cargo.toml"), []byte("[package]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	state, err := store.Open(root)
	if err != nil {
		t.Fatalf("Open error = %v", err)
	}
	if state.Level != Restricted || state.Decided || !state.NeedsPrompt() {
		t.Fatalf("Open = %+v, want restricted and undecided with a prompt", state)
	}
	if len(state.Markers) != 1 || state.Markers[0] != "Cargo.toml" {
		t.Fatalf("Markers = %v, want [Cargo.toml]", state.Markers)
	}
	// Nothing is written until the user decides.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("Open wrote a decision for an undecided workspace: %v", err)
	}
}

func TestOpenReportsASaveFailureAndStillTrusts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// A regular file where the store's directory should be: the write fails.
	blocker := filepath.Join(dir, "config")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewStore(filepath.Join(blocker, "trust.json"))
	state, err := store.Open(t.TempDir())
	if err == nil {
		t.Fatal("Open with an unwritable store returned no error")
	}
	if state.Level != Trusted || !state.Decided {
		t.Fatalf("Open = %+v, want a usable trusted state", state)
	}
}

// TestDefaultStoreIsInTheStateDir keeps trust global rather than project-owned.
func TestDefaultStoreIsInTheStateDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("AppData", home)

	store, err := DefaultStore()
	if err != nil {
		t.Fatalf("DefaultStore error = %v", err)
	}
	dir, err := config.StateDir()
	if err != nil {
		t.Fatalf("config.StateDir error = %v", err)
	}
	want := filepath.Join(dir, "trust.json")
	if got := store.Path(); got != want {
		t.Fatalf("Path() = %q, want %q", got, want)
	}
}

func TestPathReturnsTheStoreFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "trust.json")
	if got := NewStore(path).Path(); got != path {
		t.Fatalf("Path() = %q, want %q", got, path)
	}
}
