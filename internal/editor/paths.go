package editor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrExists is returned by SaveAs when the target is an existing file other
// than the document's own. Do("save_as", {"path": p, "force": true}) overwrites it.
var ErrExists = errors.New("file already exists")

// ErrEmptyPath is returned by SaveAs, RenamePath and CloseByPath for an empty path.
var ErrEmptyPath = errors.New("empty path")

// ErrAlreadyOpen is returned by SaveAs and RenamePath when the new path is the
// path of a different open tab. Two tabs never share one file, so the operation
// is refused and the editor state does not change. The "save_as" action refuses
// it even with force set, because force only allows overwriting a file on disk.
var ErrAlreadyOpen = errors.New("file already open in another tab")

// SaveAs writes the active document to path and makes path its file. It works
// for an unnamed buffer and for one that already has a file. If path is the path
// of another open tab, SaveAs writes nothing and returns an error wrapping
// ErrAlreadyOpen. If path names an existing file other than the document's own,
// SaveAs writes nothing and returns an error wrapping ErrExists; the "save_as"
// action with force set overwrites it. On failure the document keeps its old
// path. The tab name follows the new path, and the document becomes a permanent
// tab.
func (e *Editor) SaveAs(path string) error {
	return e.saveAs(path, false)
}

// saveAs implements SaveAs and the "save_as" action. Errors are also written to
// the status message, as Do does for its own failures.
func (e *Editor) saveAs(path string, force bool) error {
	d := e.Active()
	if d == nil {
		err := fmt.Errorf("save as: %w", ErrNoDocument)
		e.setStatus(err.Error())
		return err
	}
	if i := e.openElsewhere(path, e.active); i >= 0 {
		err := fmt.Errorf("save as %s: %w", path, ErrAlreadyOpen)
		e.setStatus(err.Error())
		return err
	}
	if err := d.saveAs(path, force); err != nil {
		e.setStatus(err.Error())
		return err
	}
	d.preview = false
	e.setStatus("Saved")
	return nil
}

// openElsewhere returns the index of the first tab other than skip whose path
// is path, or -1. An empty path never matches.
func (e *Editor) openElsewhere(path string, skip int) int {
	for i, d := range e.docs {
		if i != skip && sameFile(d.Path, path) {
			return i
		}
	}
	return -1
}

// saveAs writes the buffer to path and, on success, makes path the document's path.
func (d *Document) saveAs(path string, force bool) error {
	if path == "" {
		return fmt.Errorf("save as: %w", ErrEmptyPath)
	}
	if !force && !sameFile(d.Path, path) {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("save as %s: %w", path, ErrExists)
		}
	}
	if err := d.writeTo(path); err != nil {
		return err
	}
	d.rename(path)
	return nil
}

// rename makes path the file name of the buffer. The buffer is no longer
// untitled, and the lexer name is cleared because it depended on the old name.
func (d *Document) rename(path string) {
	if d.Path != path {
		d.language = ""
	}
	d.Path = path
	d.untitled = 0
}

// RenamePath updates the path of every open document that is oldPath or lies
// under it, so a renamed file or folder keeps its open tabs. It does not touch
// the disk: call it after the file or folder has been renamed. Tab names follow
// the new paths. Documents with no path, and paths that match nothing, are left
// alone. An empty oldPath or newPath returns an error wrapping ErrEmptyPath.
// If a new path is the path of a tab that is not being renamed, RenamePath
// changes nothing and returns an error wrapping ErrAlreadyOpen. A document
// renamed to its own path is not a conflict.
func (e *Editor) RenamePath(oldPath, newPath string) error {
	if oldPath == "" || newPath == "" {
		err := fmt.Errorf("rename: %w", ErrEmptyPath)
		e.setStatus(err.Error())
		return err
	}
	// Work out every new path before changing any document, so a conflict
	// leaves the state as it was.
	targets := make([]string, len(e.docs))
	moving := make([]bool, len(e.docs))
	for i, d := range e.docs {
		rest, ok := within(d.Path, oldPath)
		if !ok {
			continue
		}
		moving[i] = true
		if rest == "" {
			targets[i] = newPath
		} else {
			targets[i] = filepath.Join(newPath, rest)
		}
	}
	for i := range e.docs {
		if !moving[i] {
			continue
		}
		for j, other := range e.docs {
			if !moving[j] && sameFile(other.Path, targets[i]) {
				err := fmt.Errorf("rename %s: %w", targets[i], ErrAlreadyOpen)
				e.setStatus(err.Error())
				return err
			}
		}
	}
	for i, d := range e.docs {
		if moving[i] {
			d.rename(targets[i])
		}
	}
	return nil
}

// CloseByPath closes every open document whose path is path or lies under it,
// for a file or folder that has been deleted. A document with unsaved edits is
// kept unless force is true. If one of them blocks the close, none is closed and
// the error wraps ErrUnsaved. Closing the last tab leaves the editor Empty, as
// CloseTab does. A path that matches no open document is not an error. An empty
// path returns an error wrapping ErrEmptyPath.
func (e *Editor) CloseByPath(path string, force bool) error {
	if path == "" {
		err := fmt.Errorf("close: %w", ErrEmptyPath)
		e.setStatus(err.Error())
		return err
	}
	victim := func(d *Document) bool {
		_, ok := within(d.Path, path)
		return ok
	}
	if !force {
		for _, d := range e.docs {
			if victim(d) && d.Modified() {
				err := fmt.Errorf("close %s: %w", d.Name(), ErrUnsaved)
				e.setStatus(err.Error())
				return err
			}
		}
	}

	kept := make([]*Document, 0, len(e.docs))
	removed := 0
	newActive := -1   // index of the active document in kept, if it survives
	beforeActive := 0 // surviving documents that sat before the active tab
	for i, d := range e.docs {
		switch {
		case victim(d):
			removed++
			if i < e.active {
				beforeActive++
			}
		default:
			if i == e.active {
				newActive = len(kept)
			}
			kept = append(kept, d)
		}
	}
	if removed == 0 {
		return nil
	}
	e.docs = kept
	if len(kept) == 0 {
		e.switchTo(0)
		return nil
	}
	if newActive >= 0 {
		e.active = newActive
		return nil
	}
	e.switchTo(min(beforeActive, len(kept)-1))
	return nil
}

// within reports whether path is root or lies under it. It returns the part of
// path below root, using the platform separator, and "" when path is root. Both
// paths are cleaned before comparing. An empty path never matches.
func within(path, root string) (rest string, ok bool) {
	if path == "" || root == "" {
		return "", false
	}
	path, root = filepath.Clean(path), filepath.Clean(root)
	if path == root {
		return "", true
	}
	prefix := root
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	return path[len(prefix):], true
}

// sameFile reports whether a and b name the same file on disk. An empty name
// never matches.
func sameFile(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	fa, errA := os.Stat(a)
	fb, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(fa, fb)
}
