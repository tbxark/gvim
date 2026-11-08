package daemon

// The workspace registry of the daemon (#19a): one durable file per workspace under the
// user data directory, no central list. The directory IS the registry; discovery scans it.
//
// It mirrors Fresh's workspace store (docs/internal/orchestrator-sessions.md): a file named
// <encoded-canonical-root>.<stable-id>.json, a stable_id minted once and adopted from the
// file on restore, and atomic writes. Registry v2 keys a workspace by (canonical root,
// stable id), so several co-tenant workspaces may share one root (Fresh's extracted tabs
// model); a v1 file (one workspace per root) still loads unchanged and is rewritten as v2
// only by a later save - its stable id, and therefore its session directory, is kept.
// Dedup is by identical stable id only (a stale duplicate file with the same identity keeps
// the freshest copy); different identities under one root are distinct workspaces. The
// file carries identity and the session layout; the per-workspace session directory
// (sessions/<stable-id>/) keeps the stashes and the terminal record, which is
// terminal.SavedState and stays its own schema. Closing a workspace removes only its
// registry file: the session directory is data, never deleted by the registry.

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tbxark/gvim/session"
)

// WorkspaceVersion is the registry file format version. Version 2 allows several workspaces
// per canonical root (co-tenant workspaces); a version-1 file (one workspace per root)
// still loads unchanged. A file with a newer version is refused, never read as this
// version and never rewritten.
const WorkspaceVersion = 2

// ErrWorkspaceVersionTooNew reports a registry file written by a newer gvim.
var ErrWorkspaceVersionTooNew = errors.New("workspace: file version is newer than this gvim")

// WorkspaceFile is one workspace's durable state.
type WorkspaceFile struct {
	Version int `json:"version"`
	// StableID is the durable identity, minted once at creation and adopted from the
	// file on restore. It is the session directory name.
	StableID string `json:"stable_id"`
	// Label is the display name; it defaults to the basename of Root and survives a
	// rename without changing the identity.
	Label string `json:"label"`
	// Root is the canonical workspace root (absolute, symlinks resolved). Version 2
	// allows several workspaces to share one root; identity is the stable id.
	Root string `json:"root"`
	// AuthoritySpec is the canonical remote target of a workspace bound to a remote
	// authority (the daemon jump-box mode): a user@host[:port]:path or ssh://... target
	// text. Empty means a local workspace; registry v2 files written before this field
	// load as local. One workspace is one backend.
	AuthoritySpec string `json:"authority_spec,omitempty"`
	// LastFocusedAt is when the workspace was last the active one, in Unix
	// milliseconds. It orders the root groups and the workspaces inside a root, and
	// breaks a duplicate-identity tie.
	LastFocusedAt int64 `json:"last_focused_at"`
	// Layout is the saved editor layout in the existing session format; nil for a
	// workspace that has never been captured. The terminal record is not here: it
	// lives in the per-workspace session directory (terminal.SavedState).
	Layout *session.Session `json:"layout,omitempty"`
}

// Workspace is one registry entry with its runtime state.
type Workspace struct {
	File WorkspaceFile
	Path string
	// Materialized is true when a runtime exists for the workspace. It is runtime
	// state; the file does not store it.
	Materialized bool
	// Active is true for the workspace whose program is attached. Runtime state.
	Active bool
}

// Registry is the scanned workspace directory. Open it once per daemon and keep it: the
// directory is the source of truth.
type Registry struct {
	dir string
	now func() time.Time
	// mu serializes the file operations: the UI goroutine, the socket goroutine and
	// the capture writers share one registry. Disk work never runs under the UI.
	mu sync.Mutex
}

// WorkspaceDir returns the workspace directory: <user data dir>/gvim/workspaces, mode
// 0700. It creates the directory.
func WorkspaceDir() (string, error) {
	if dir := os.Getenv("GVIM_WORKSPACE_DIR"); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", fmt.Errorf("workspace: %w", err)
		}
		return dir, nil
	}
	data, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("workspace: user data dir: %w", err)
	}
	dir := filepath.Join(data, "gvim", "workspaces")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("workspace: %w", err)
	}
	return dir, nil
}

// OpenRegistry opens the registry at dir. An empty dir means WorkspaceDir. The directory
// is created when missing; unusable or corrupt files are reported by List, never deleted.
func OpenRegistry(dir string) (*Registry, error) {
	if dir == "" {
		var err error
		if dir, err = WorkspaceDir(); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("workspace: %w", err)
	}
	return &Registry{dir: dir, now: time.Now}, nil
}

// Dir is the registry directory.
func (r *Registry) Dir() string { return r.dir }

// CanonicalRoot resolves a workspace root the way the registry keys it: absolute, clean,
// symlinks resolved when the path exists. It errors when path is not a directory.
func CanonicalRoot(path string) (string, error) {
	if path == "" {
		return "", errors.New("workspace: empty root")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("workspace: root %q: %w", path, err)
	}
	abs = filepath.Clean(abs)
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("workspace: root %q: %w", path, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("workspace: root %q: %w", path, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace: root %q is not a directory", path)
	}
	return resolved, nil
}

// canonicalRemoteRoot normalizes a remote workspace root without touching the local file
// system: POSIX-cleaned, with "~"/"~/..." kept and "." for a relative root. A remote root
// does not exist on this host, so it is never stat'ed or symlink-resolved.
func canonicalRemoteRoot(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", errors.New("workspace: empty remote root")
	}
	if strings.ContainsAny(root, "\x00\r\n") {
		return "", errors.New("workspace: remote root contains a control character")
	}
	return path.Clean(root), nil
}

// workspaceRoot resolves the root a workspace is stored under: a remote authority's root is
// normalized remotely (authoritySpec non-empty), a local one is canonicalized through the
// local file system.
func (r *Registry) workspaceRoot(root, authoritySpec string) (string, error) {
	if strings.TrimSpace(authoritySpec) != "" {
		return canonicalRemoteRoot(root)
	}
	return CanonicalRoot(root)
}

// List scans the registry and returns every workspace (registry v2: several may share one
// canonical root), ordered by root group: roots by their freshest LastFocusedAt first, then
// the workspaces of one root freshest first. Dedup keeps only an identical stable id's
// freshest file. Files from a newer gvim and unreadable files are skipped and reported in
// problems; they are never modified or deleted. Runtime fields are false.
func (r *Registry) List() ([]Workspace, []error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.scan()
}

// Get returns the workspace with the stable id.
func (r *Registry) Get(id string) (Workspace, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.getLocked(id)
}

// getLocked is Get without the lock.
func (r *Registry) getLocked(id string) (Workspace, bool, error) {
	if err := validateStableID(id); err != nil {
		return Workspace{}, false, err
	}
	entries, _ := r.scan()
	for _, w := range entries {
		if w.File.StableID == id {
			return w, true, nil
		}
	}
	return Workspace{}, false, nil
}

// Create makes a workspace for root, or returns an existing one for the same canonical
// root (registry v2: the freshest workspace of that root, so a known root reopens its most
// recently used workspace). The label defaults to the root's basename. The stable id is
// minted here once. Use CreateCoTenant to add another workspace over the same root.
func (r *Registry) Create(root, label string) (Workspace, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.createLocked(root, label)
}

// CreateWithAuthority makes a workspace over a remote root bound to the canonical remote
// target text, or returns the existing workspace for the same (root, spec). The remote root
// is normalized without local IO. This is the daemon jump-box foreground workspace; local
// workspaces keep using Create.
func (r *Registry) CreateWithAuthority(root, label, authoritySpec string) (Workspace, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if strings.TrimSpace(authoritySpec) == "" {
		return Workspace{}, false, errors.New("workspace: empty authority spec")
	}
	return r.createWithAuthorityLocked(root, label, authoritySpec)
}

// createLocked is Create without the lock: a local workspace (empty authority spec).
func (r *Registry) createLocked(root, label string) (Workspace, bool, error) {
	return r.createWithAuthorityLocked(root, label, "")
}

// createWithAuthorityLocked makes a workspace over root bound to authoritySpec, or returns
// the existing one with the same (root, spec). An empty authoritySpec is the local path and
// goes through CanonicalRoot; a remote one normalizes the root without local IO. Identity is
// the stable id, so a remote workspace survives daemon restarts.
func (r *Registry) createWithAuthorityLocked(root, label, authoritySpec string) (Workspace, bool, error) {
	spec := strings.TrimSpace(authoritySpec)
	canonical, err := r.workspaceRoot(root, spec)
	if err != nil {
		return Workspace{}, false, err
	}
	entries, _ := r.scan()
	for _, w := range entries {
		if w.File.Root == canonical && w.File.AuthoritySpec == spec {
			return w, false, nil
		}
	}
	id, err := newStableID()
	if err != nil {
		return Workspace{}, false, err
	}
	if strings.TrimSpace(label) == "" {
		if spec != "" {
			label = path.Base(canonical)
		} else {
			label = filepath.Base(canonical)
		}
	}
	w := Workspace{File: WorkspaceFile{
		Version:       WorkspaceVersion,
		StableID:      id,
		Label:         label,
		Root:          canonical,
		AuthoritySpec: spec,
		LastFocusedAt: r.now().UnixMilli(),
	}}
	saved, err := r.saveLocked(w)
	if err != nil {
		return Workspace{}, false, err
	}
	return saved, true, nil
}

// CreateCoTenant makes a NEW workspace over root: unlike Create it never reuses an
// existing one at the root. This is Fresh's extracted-tab model (several workspaces over
// the same project root), so the workspace is keyed by a fresh stable id and gets its own
// session directory. An empty label gets Fresh's precedence: the root basename, then
// "basename (2)", "basename (3)", ... counted from the workspaces already at the root; an
// explicitly given label is used as given unless it would collide at the root, in which
// case the same numbered precedence applies. Creation is repeatable: the same registry
// state always yields the same next label.
func (r *Registry) CreateCoTenant(root, label string) (Workspace, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.createCoTenantLocked(root, label)
}

// createCoTenantLocked is CreateCoTenant without the lock.
func (r *Registry) createCoTenantLocked(root, label string) (Workspace, bool, error) {
	canonical, err := CanonicalRoot(root)
	if err != nil {
		return Workspace{}, false, err
	}
	entries, _ := r.scan()
	label = strings.TrimSpace(label)
	if label == "" || coTenantLabelTaken(label, entries, canonical) {
		label = coTenantLabel(filepath.Base(canonical), entries, canonical)
	}
	id, err := newStableID()
	if err != nil {
		return Workspace{}, false, err
	}
	w := Workspace{File: WorkspaceFile{
		Version:       WorkspaceVersion,
		StableID:      id,
		Label:         label,
		Root:          canonical,
		LastFocusedAt: r.now().UnixMilli(),
	}}
	saved, err := r.saveLocked(w)
	if err != nil {
		return Workspace{}, false, err
	}
	return saved, true, nil
}

// coTenantLabel picks the label of a new co-tenant over root: Fresh's precedence counts the
// workspaces already at the root, so the first co-tenant of an existing root is
// "basename (2)", the second "basename (3)", ...; a counted label taken by a rename is
// skipped (the count moves on) so labels stay distinguishable and the result is repeatable.
func coTenantLabel(base string, entries []Workspace, root string) string {
	labels := make(map[string]bool)
	n := 0
	for _, w := range entries {
		if w.File.Root != root {
			continue
		}
		n++
		labels[w.File.Label] = true
	}
	for {
		candidate := base
		if n > 0 {
			candidate = fmt.Sprintf("%s (%d)", base, n+1)
		}
		if !labels[candidate] {
			return candidate
		}
		n++
	}
}

// coTenantLabelTaken reports whether label is already used by a workspace at root.
func coTenantLabelTaken(label string, entries []Workspace, root string) bool {
	for _, w := range entries {
		if w.File.Root == root && w.File.Label == label {
			return true
		}
	}
	return false
}

// Save writes the workspace file atomically and returns the stored entry. A workspace
// without a stable id is refused: identity is never minted by a save (Create does that).
// Save never rewrites a file it did not read as this version; specifically it refuses to
// save over a stable-id file whose current version is newer.
func (r *Registry) Save(w Workspace) (Workspace, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.saveLocked(w)
}

// saveLocked is Save without the lock.
func (r *Registry) saveLocked(w Workspace) (Workspace, error) {
	if err := validateStableID(w.File.StableID); err != nil {
		return Workspace{}, err
	}
	if w.File.Root == "" {
		return Workspace{}, errors.New("workspace: root is empty")
	}
	if w.File.Version > WorkspaceVersion {
		return Workspace{}, fmt.Errorf("%w: %d", ErrWorkspaceVersionTooNew, w.File.Version)
	}
	// A load of an older file (registry v1) is saved as the current version: the stable
	// id, and therefore the session directory, is unchanged, only the format is upgraded.
	w.File.Version = WorkspaceVersion
	if w.File.Label == "" {
		if w.File.AuthoritySpec != "" {
			w.File.Label = path.Base(w.File.Root)
		} else {
			w.File.Label = filepath.Base(w.File.Root)
		}
	}
	if w.File.LastFocusedAt == 0 {
		w.File.LastFocusedAt = r.now().UnixMilli()
	}
	path := r.path(w.File.Root, w.File.StableID)
	// A future-version file at the same path is not ours to overwrite. A corrupt or
	// absent file is ours to replace; a newer one would be data loss.
	current, readErr := readWorkspaceFile(path)
	switch {
	case readErr == nil && current.Version > WorkspaceVersion:
		return Workspace{}, fmt.Errorf("%w: %d", ErrWorkspaceVersionTooNew, current.Version)
	case readErr != nil && errors.Is(readErr, ErrWorkspaceVersionTooNew):
		return Workspace{}, readErr
	}
	if err := writeWorkspaceFile(path, w.File); err != nil {
		return Workspace{}, err
	}
	w.Path = path
	return w, nil
}

// Rename changes the label of a workspace without changing its identity or file name.
func (r *Registry) Rename(id, label string) (Workspace, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	w, ok, err := r.getLocked(id)
	if err != nil {
		return Workspace{}, err
	}
	if !ok {
		return Workspace{}, fmt.Errorf("workspace: no workspace %q", id)
	}
	if strings.TrimSpace(label) == "" {
		return Workspace{}, errors.New("workspace: label is empty")
	}
	w.File.Label = strings.TrimSpace(label)
	return r.saveLocked(w)
}

// Touch records that a workspace was focused, for the dedup order.
func (r *Registry) Touch(id string, when time.Time) (Workspace, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	w, ok, err := r.getLocked(id)
	if err != nil {
		return Workspace{}, err
	}
	if !ok {
		return Workspace{}, fmt.Errorf("workspace: no workspace %q", id)
	}
	if when.IsZero() {
		when = r.now()
	}
	w.File.LastFocusedAt = when.UnixMilli()
	return r.saveLocked(w)
}

// UpdateLayout stores the captured session layout of a workspace.
func (r *Registry) UpdateLayout(id string, layout *session.Session) (Workspace, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	w, ok, err := r.getLocked(id)
	if err != nil {
		return Workspace{}, err
	}
	if !ok {
		return Workspace{}, fmt.Errorf("workspace: no workspace %q", id)
	}
	w.File.Layout = layout
	return r.saveLocked(w)
}

// Remove deletes the workspace's registry file. The per-workspace session directory is
// data and is kept: use RemoveSessionData for an explicit discard.
func (r *Registry) Remove(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	w, ok, err := r.getLocked(id)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("workspace: no workspace %q", id)
	}
	if err := os.Remove(w.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("workspace: remove %s: %w", w.Path, err)
	}
	return nil
}

// RemoveSessionData deletes the per-workspace session directory. It is separate from
// Remove so closing a workspace never loses unsaved text by accident.
func (r *Registry) RemoveSessionData(id string) error {
	dir, err := r.SessionDir(id)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

// SessionDir is the per-workspace session directory (sessions/<stable-id>), where the
// stashes and the terminal record live. It creates the directory.
func (r *Registry) SessionDir(id string) (string, error) {
	if err := validateStableID(id); err != nil {
		return "", err
	}
	dir := filepath.Join(r.dir, "sessions", id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("workspace: session dir: %w", err)
	}
	return dir, nil
}

// scan reads every registry file. Version 2 allows several workspaces per canonical root,
// so dedup is by identical stable id: of two files with the same identity, the fresher
// LastFocusedAt wins and a remaining tie picks the lexicographically first path. Distinct
// identities under one root are distinct workspaces (co-tenants). The result is ordered
// root group first (roots by their freshest LastFocusedAt, then root path) and freshest
// first inside a root, so the ordering is deterministic.
func (r *Registry) scan() ([]Workspace, []error) {
	dirEntries, err := os.ReadDir(r.dir)
	if err != nil {
		return nil, []error{fmt.Errorf("workspace: read %s: %w", r.dir, err)}
	}
	type candidate struct {
		path string
		file WorkspaceFile
	}
	best := map[string]candidate{}
	var problems []error
	for _, de := range dirEntries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
			continue
		}
		path := filepath.Join(r.dir, de.Name())
		file, err := readWorkspaceFile(path)
		if err != nil {
			problems = append(problems, fmt.Errorf("workspace: %s: %w", path, err))
			continue
		}
		old, ok := best[file.StableID]
		if !ok || fresher(file, path, old.file, old.path) {
			best[file.StableID] = candidate{path: path, file: file}
		}
	}
	out := make([]Workspace, 0, len(best))
	rootFresh := make(map[string]int64, len(best))
	for _, c := range best {
		out = append(out, Workspace{File: c.file, Path: c.path})
		if c.file.LastFocusedAt > rootFresh[c.file.Root] {
			rootFresh[c.file.Root] = c.file.LastFocusedAt
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].File, out[j].File
		if rootFresh[a.Root] != rootFresh[b.Root] {
			return rootFresh[a.Root] > rootFresh[b.Root]
		}
		if a.Root != b.Root {
			return a.Root < b.Root
		}
		if a.LastFocusedAt != b.LastFocusedAt {
			return a.LastFocusedAt > b.LastFocusedAt
		}
		if a.Label != b.Label {
			return a.Label < b.Label
		}
		return a.StableID < b.StableID
	})
	return out, problems
}

// fresher reports whether a beats b in the duplicate-identity arbitration.
func fresher(a WorkspaceFile, aPath string, b WorkspaceFile, bPath string) bool {
	if a.LastFocusedAt != b.LastFocusedAt {
		return a.LastFocusedAt > b.LastFocusedAt
	}
	if (a.StableID != "") != (b.StableID != "") {
		return a.StableID != ""
	}
	return aPath < bPath
}

// path is the registry file path for a root and stable id.
func (r *Registry) path(root, id string) string {
	return filepath.Join(r.dir, encodeRoot(root)+"."+id+".json")
}

// encodeRoot percent-encodes a canonical root into one file-name component: letters,
// digits, '-', '_' and '.' are kept, every other byte becomes %XX. A root whose encoded
// form would exceed one filename uses a deterministic sha256 name instead; the recorded
// root inside the file is authoritative, so the name is only a locator.
func encodeRoot(root string) string {
	var b strings.Builder
	for i := 0; i < len(root); i++ {
		ch := root[i]
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9', ch == '-', ch == '_', ch == '.':
			b.WriteByte(ch)
		default:
			fmt.Fprintf(&b, "%%%02X", ch)
		}
	}
	enc := b.String()
	if len(enc) > 200 {
		sum := sha256.Sum256([]byte(root))
		return "sha256-" + hex.EncodeToString(sum[:16])
	}
	return enc
}

// newStableID mints a durable workspace identity: 16 random bytes, hex.
func newStableID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("workspace: stable id: %w", err)
	}
	return hex.EncodeToString(buf[:]), nil
}

// validateStableID checks a stable id is a plain hex token (no path characters).
func validateStableID(id string) error {
	if len(id) < 8 || len(id) > 64 {
		return fmt.Errorf("workspace: invalid stable id %q", id)
	}
	for _, r := range id {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return fmt.Errorf("workspace: invalid stable id %q", id)
		}
	}
	return nil
}

// readWorkspaceFile reads and validates one registry file. Version 1 (one workspace per
// root) and version 2 (co-tenant workspaces) load; a newer version is an error and the file
// is never modified. The version is upgraded only when the workspace is saved again.
func readWorkspaceFile(path string) (WorkspaceFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return WorkspaceFile{}, err
	}
	var file WorkspaceFile
	if err := json.Unmarshal(data, &file); err != nil {
		return WorkspaceFile{}, fmt.Errorf("workspace: decode: %w", err)
	}
	if file.Version > WorkspaceVersion {
		return WorkspaceFile{}, fmt.Errorf("%w: %d (%s)", ErrWorkspaceVersionTooNew, file.Version, path)
	}
	if file.Version < 1 {
		return WorkspaceFile{}, fmt.Errorf("workspace: invalid version %d", file.Version)
	}
	// Version 1 files (one workspace per canonical root) are registry v1; they keep
	// loading unchanged and are rewritten as version 2 by the next save, keeping the
	// stable id and therefore the workspace's session directory.
	if file.Root == "" {
		return WorkspaceFile{}, errors.New("workspace: root is empty")
	}
	file.AuthoritySpec = strings.TrimSpace(file.AuthoritySpec)
	if strings.ContainsAny(file.AuthoritySpec, "\x00\r\n") {
		return WorkspaceFile{}, errors.New("workspace: authority spec contains a control character")
	}
	if err := validateStableID(file.StableID); err != nil {
		return WorkspaceFile{}, err
	}
	return file, nil
}

// writeWorkspaceFile writes one registry file atomically: a temp file in the same
// directory (0600), write, fsync, close, rename over the target, then fsync the directory.
// A reader sees the old file or the new one, never a partial one.
func writeWorkspaceFile(path string, file WorkspaceFile) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".workspace-*.tmp")
	if err != nil {
		return fmt.Errorf("workspace: write %s: %w", path, err)
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}
	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return fmt.Errorf("workspace: write %s: %w", path, err)
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		cleanup()
		return fmt.Errorf("workspace: encode: %w", err)
	}
	data = append(data, '\n')
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("workspace: write %s: %w", path, err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("workspace: sync %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("workspace: close %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("workspace: rename %s: %w", path, err)
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// Discovery is the daemon-start view of the registry: the foreground workspace (the
// launch directory, created when it is not registered yet) and every other workspace as
// an inert descriptor. No runtime is built here; the app materializes the foreground one
// eagerly and keeps the rest dormant until a switch.
type Discovery struct {
	Foreground Workspace
	Dormant    []Workspace
	Problems   []error
}

// Discover resolves root to the foreground workspace -- creating and saving it when the
// registry has no workspace for that canonical root -- and returns the other registered
// workspaces, freshest first, as dormant descriptors. Materialized and Active are false
// on every returned entry: the caller owns the runtime state.
func (r *Registry) Discover(root string) (Discovery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	foreground, _, err := r.createLocked(root, "")
	if err != nil {
		return Discovery{}, err
	}
	return r.discoveryLocked(foreground), nil
}

// DiscoverWithAuthority is Discover for a daemon started with a remote target: it resolves
// a foreground workspace over the remote root bound to the canonical authority spec,
// creating and saving it when absent, and returns every other workspace as dormant. A
// restart with the same target reuses the workspace (and its stable id), so the session
// directory and captured layout stay; local workspaces remain dormant entries that stay
// local when switched to.
func (r *Registry) DiscoverWithAuthority(root, authoritySpec string) (Discovery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if strings.TrimSpace(authoritySpec) == "" {
		return Discovery{}, errors.New("workspace: empty authority spec")
	}
	foreground, _, err := r.createWithAuthorityLocked(root, "", authoritySpec)
	if err != nil {
		return Discovery{}, err
	}
	return r.discoveryLocked(foreground), nil
}

// discoveryLocked assembles the discover result around foreground. The caller holds mu.
func (r *Registry) discoveryLocked(foreground Workspace) Discovery {
	list, problems := r.scan()
	out := Discovery{Foreground: foreground, Problems: problems}
	for _, w := range list {
		if w.File.StableID == foreground.File.StableID {
			continue
		}
		out.Dormant = append(out.Dormant, w)
	}
	return out
}
