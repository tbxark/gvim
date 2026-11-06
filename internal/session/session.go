// Package session saves and restores editor session state: the open files,
// their cursor positions, and the unsaved text stashed beside them.
package session

import (
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Version is the only session format version this package reads and writes.
const Version = 1

const (
	fileName    = "session.json"
	stashPrefix = "stash-"
	stashSuffix = ".txt"
)

// Session is the saved state of one editor session.
type Session struct {
	Version     int         `json:"version"`
	Root        string      `json:"root"`
	Explorer    bool        `json:"explorer"`
	ShowHidden  bool        `json:"showHidden"`
	ShowIgnored bool        `json:"showIgnored"`
	Active      int         `json:"active"`
	Files       []FileState `json:"files"`
}

// FileState is the saved state of one open file.
type FileState struct {
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Col       int    `json:"col"`
	Top       int    `json:"top"`
	StashFile string `json:"stashFile,omitempty"`
}

// Save writes s to dir/session.json. Save always writes the current Version,
// whatever s.Version holds.
//
// Save also deletes the stash files that the session it replaces referenced and
// s does not. Stash files that no saved session referenced, such as one another
// writer created after the last Save, are left alone. If the replaced session
// cannot be read, Save deletes no stash file, because nothing shows which ones
// it used.
func Save(dir string, s Session) error {
	s.Version = Version
	data, err := json.Marshal(s, jsontext.Multiline(true))
	if err != nil {
		return fmt.Errorf("session: encode: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// Read the session being replaced before it is overwritten. Its stash files
	// are the only candidates for removal.
	prev, _, prevErr := Load(dir)
	if err := writeAtomic(dir, fileName, data); err != nil {
		return fmt.Errorf("session: write %s: %w", fileName, err)
	}
	if prevErr != nil {
		return nil
	}
	keep := make(map[string]bool, len(s.Files))
	for _, f := range s.Files {
		keep[f.StashFile] = true
	}
	var stale []string
	for _, f := range prev.Files {
		if !keep[f.StashFile] {
			stale = append(stale, f.StashFile)
		}
	}
	// Remove stashes only after the new session is on disk. If Save fails
	// before this point, the old session still finds its stash files.
	return removeStashes(dir, stale)
}

// removeStashes deletes the named stash files in dir and syncs dir after each
// removal. A name that is not a stash name is skipped, so a name read from
// session.json cannot reach a file outside the stash set. A file that is already
// gone is not an error.
func removeStashes(dir string, names []string) error {
	for _, name := range names {
		if !isStashName(name) {
			continue
		}
		path := filepath.Join(dir, name)
		fi, err := os.Lstat(path)
		if errors.Is(err, fs.ErrNotExist) || (err == nil && !fi.Mode().IsRegular()) {
			continue
		}
		if err != nil {
			return fmt.Errorf("session: stat stash %s: %w", name, err)
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("session: remove stash %s: %w", name, err)
		}
		if err := syncDir(dir); err != nil {
			return fmt.Errorf("session: sync %s: %w", dir, err)
		}
	}
	return nil
}

// Load reads dir/session.json. A missing file returns false and no error. A
// file with a version other than Version is an error.
func Load(dir string) (Session, bool, error) {
	data, err := os.ReadFile(filepath.Join(dir, fileName))
	if errors.Is(err, fs.ErrNotExist) {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, err
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return Session{}, false, fmt.Errorf("session: decode %s: %w", fileName, err)
	}
	if s.Version != Version {
		return Session{}, false, fmt.Errorf("session: unsupported version %d, want %d", s.Version, Version)
	}
	return s, true, nil
}

// StashContent stores text in a new stash file in dir and returns the file's
// name. Put the name in FileState.StashFile so that Save keeps the file.
func StashContent(dir, text string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, stashPrefix+"*"+stashSuffix)
	if err != nil {
		return "", err
	}
	_, err = f.WriteString(text)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return filepath.Base(f.Name()), nil
}

// ReadStash returns the text of the stash file name in dir. name must be a
// stash name returned by StashContent.
func ReadStash(dir, name string) (string, error) {
	if !isStashName(name) {
		return "", fmt.Errorf("session: invalid stash name %q", name)
	}
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// isStashName reports whether name is a bare file name that StashContent could
// have returned. A name with a directory part is rejected, so ReadStash never
// leaves dir.
func isStashName(name string) bool {
	return strings.HasPrefix(name, stashPrefix) &&
		strings.HasSuffix(name, stashSuffix) &&
		filepath.Base(name) == name
}

// writeAtomic writes data to dir/name through a temporary file and a rename,
// so a reader sees either the old file or the new one. The temporary file is
// synced before the rename, and dir is synced after it, so the new file is
// still there after a crash.
func writeAtomic(dir, name string, data []byte) (err error) {
	f, err := os.CreateTemp(dir, name+".*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = f.Close()
			_ = os.Remove(f.Name())
		}
	}()
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), filepath.Join(dir, name)); err != nil {
		return err
	}
	return syncDir(dir)
}

// syncDir flushes the entries of dir to disk, so a rename or removal inside dir
// survives a crash. Windows cannot flush a directory handle, so there syncDir
// does nothing.
func syncDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = d.Sync()
	if cerr := d.Close(); err == nil {
		err = cerr
	}
	return err
}
