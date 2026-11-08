package trust

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tbxark/gvim/atomicfile"
	"github.com/tbxark/gvim/config"
)

// storeVersion is the version of the on-disk form this package writes. A file
// with a newer version reads as no decisions: an older gvim must not take a
// newer file's entries for decisions it understands.
const storeVersion = 1

// Store is the per-user workspace-trust store: a JSON file that maps the
// canonical root of each workspace to the level the user chose for it.
//
// The file lives in the user's config directory and never inside a project: a
// repository must not be able to vouch for itself. One file holds every
// workspace, keyed by the canonical root, so a workspace keeps its decision
// across restarts. The file is written through atomicfile, so a half-written
// file is never observed. Concurrent gvim processes that decide at the same
// moment may keep the later write only; the decision is a rare user action, and
// a lost entry simply asks again.
type Store struct {
	path string
}

// DefaultStore returns the store at trust.json under gvim's user config
// directory (config.Dir: ~/.config/gvim on Unix, including macOS, and
// os.UserConfigDir()/gvim on Windows). It fails only when the user config
// directory cannot be found.
func DefaultStore() (*Store, error) {
	dir, err := config.Dir()
	if err != nil {
		return nil, fmt.Errorf("trust: config dir: %w", err)
	}
	return NewStore(filepath.Join(dir, "trust.json")), nil
}

// NewStore returns the store backed by the file at path. The file is read and
// written when a method needs it; the directory is created on the first write.
func NewStore(path string) *Store { return &Store{path: path} }

// Path returns the file the store reads and writes.
func (s *Store) Path() string { return s.path }

// trustFile is the on-disk form of the store. The version leaves room for a
// later format change.
type trustFile struct {
	Version    int              `json:"version"`
	Workspaces map[string]Level `json:"workspaces"`
}

// CanonicalRoot returns the key a workspace is stored under: the absolute path
// with symlinks resolved when the folder exists, and its cleaned absolute path
// otherwise. The canonical form is what makes two spellings of one folder, such
// as a symlinked path and its target, share a decision.
func CanonicalRoot(root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("trust: workspace root: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return filepath.Clean(resolved), nil
	}
	return filepath.Clean(abs), nil
}

// LevelFor returns the recorded level for root, and decided false when the
// store has no valid decision for it. A missing store, a corrupt store, an
// unknown level name and a newer store version all read as undecided: the
// caller then applies the startup policy. A corrupt file is not an error; the
// next Set rewrites it cleanly.
func (s *Store) LevelFor(root string) (level Level, decided bool) {
	key, err := CanonicalRoot(root)
	if err != nil {
		return Undecided, false
	}
	entry, ok := s.read()[key]
	if !ok {
		return Undecided, false
	}
	parsed, ok := ParseLevel(string(entry))
	if !ok {
		return Undecided, false
	}
	return parsed, true
}

// Set records level for root and writes the store atomically. The decisions of
// other workspaces are kept. It creates the config directory when it is
// missing. An invalid level is an error; nothing is written.
func (s *Store) Set(root string, level Level) error {
	if !level.Valid() {
		return fmt.Errorf("trust: invalid level %q", string(level))
	}
	key, err := CanonicalRoot(root)
	if err != nil {
		return err
	}
	decisions := s.read()
	decisions[key] = level
	data, err := json.MarshalIndent(trustFile{Version: storeVersion, Workspaces: decisions}, "", "  ")
	if err != nil {
		return fmt.Errorf("trust: encode: %w", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("trust: make config dir: %w", err)
	}
	if _, err := atomicfile.WriteFile(s.path, data); err != nil {
		return fmt.Errorf("trust: save %s: %w", s.path, err)
	}
	return nil
}

// Open returns the trust state of the workspace at root and applies Fresh's
// startup policy. A recorded decision is honored. An undecided workspace with
// no executable content is trusted silently and the decision is recorded, so
// the prompt does not come back; a write failure is reported, and the state is
// still valid. An undecided workspace that can execute code stays Restricted
// and undecided: its State.NeedsPrompt is true until the user decides, and
// nothing is written.
//
// The executable-content markers are scanned for every open, decided or not:
// the trust dialog lists them when the user opens it, as Fresh's does.
//
// The error reports only a store that could not be written or a root that
// cannot be canonicalized; the returned state is usable in either case.
func (s *Store) Open(root string) (State, error) {
	canonical, err := CanonicalRoot(root)
	if err != nil {
		return State{Level: Restricted}, err
	}
	markers := ExecutableContentMarkers(canonical)
	if level, decided := s.LevelFor(canonical); decided {
		return State{Root: canonical, Level: level, Decided: true, Markers: markers}, nil
	}
	if len(markers) == 0 {
		state := State{Root: canonical, Level: Trusted, Decided: true}
		if err := s.Set(canonical, Trusted); err != nil {
			return state, err
		}
		return state, nil
	}
	return State{Root: canonical, Level: Restricted, Markers: markers}, nil
}

// read returns the stored decisions. A missing file, a corrupt file, a newer
// version and entries with an unknown level are left out, so every failure
// degrades to "undecided" instead of stopping the editor.
func (s *Store) read() map[string]Level {
	decisions := make(map[string]Level)
	data, err := os.ReadFile(s.path)
	if err != nil {
		// A missing or unreadable store reads as undecided; the next Set
		// replaces it.
		return decisions
	}
	var f trustFile
	if err := json.Unmarshal(data, &f); err != nil {
		return decisions
	}
	if f.Version > storeVersion {
		return decisions
	}
	for root, raw := range f.Workspaces {
		if level, ok := ParseLevel(string(raw)); ok {
			decisions[root] = level
		}
	}
	return decisions
}
