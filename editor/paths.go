package editor

import (
	"fmt"
	"path/filepath"
)

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
	p, err := e.PlanSaveAs(path, force)
	if err != nil {
		return err
	}
	return e.ApplySavePlan(p, p.Run())
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

// RenamePath updates the path of every open document that is oldPath or lies
// under it, so a renamed file or folder keeps its open tabs. It does not change
// the disk: call it after the file or folder has been renamed. Tab names follow
// the new paths. Documents with no path, and paths that match nothing, are left
// alone. A moved document keeps its record of the file on disk when the moved
// file still holds the saved text, so the next save does not take the move for an
// outside change (see followDisk). An empty oldPath or newPath returns an error
// wrapping ErrEmptyPath.
// If a new path is the path of a tab that is not being renamed, RenamePath
// changes nothing and returns an error wrapping ErrAlreadyOpen. A document
// renamed to its own path is not a conflict.
func (e *Editor) RenamePath(oldPath, newPath string) error {
	targets, moving, err := e.renamePlan(oldPath, newPath)
	if err != nil {
		e.setError(err)
		return err
	}
	for i, d := range e.docs {
		if moving[i] {
			d.Rename(targets[i])
			d.FollowDisk(targets[i])
		}
	}
	return nil
}

// renamePlan works out where each open document goes when oldPath is renamed to
// newPath, and returns the first conflict. It changes nothing. When moving[i] is
// set, document i goes to targets[i].
func (e *Editor) renamePlan(oldPath, newPath string) (targets []string, moving []bool, err error) {
	if oldPath == "" || newPath == "" {
		return nil, nil, fmt.Errorf("rename: %w", ErrEmptyPath)
	}
	if oldPath, err = absPath(oldPath); err != nil {
		return nil, nil, err
	}
	if newPath, err = absPath(newPath); err != nil {
		return nil, nil, err
	}
	// Work out every new path before changing any document, so a conflict
	// leaves the state as it was.
	targets = make([]string, len(e.docs))
	moving = make([]bool, len(e.docs))
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
				return nil, nil, fmt.Errorf("rename %s: %w", targets[i], ErrAlreadyOpen)
			}
		}
	}
	return targets, moving, nil
}

// CheckRename reports the error that RenamePath(oldPath, newPath) would return,
// and changes nothing. Call it before the file or folder is renamed on disk, so a
// refused rename leaves the disk alone. Unlike RenamePath, it does not set the
// status message.
func (e *Editor) CheckRename(oldPath, newPath string) error {
	_, _, err := e.renamePlan(oldPath, newPath)
	return err
}

// CloseByPath closes every open document whose path is path or lies under it,
// for a file or folder that has been deleted. A document with unsaved edits is
// kept unless force is true. If one of them blocks the close, none is closed and
// the error wraps ErrUnsaved. Closing the last tab leaves the editor Empty, or
// with a new empty buffer, as CloseTab does. A path that matches no open document is not an error. An empty
// path returns an error wrapping ErrEmptyPath.
func (e *Editor) CloseByPath(path string, force bool) error {
	if path == "" {
		err := fmt.Errorf("close: %w", ErrEmptyPath)
		e.setError(err)
		return err
	}
	path, err := absPath(path)
	if err != nil {
		e.setError(err)
		return err
	}
	victim := func(d *Document) bool {
		_, ok := within(d.Path, path)
		return ok
	}
	if !force {
		for _, d := range e.docs {
			if victim(d) && d.Modified() && (e.shared == nil || e.shared.ClosePrompt(e, d)) {
				err := fmt.Errorf("close %s: %w", d.Name(), ErrUnsaved)
				e.setError(err)
				return err
			}
		}
	}

	// Detach every shared victim first: a promotion can move the canonical document to
	// another workspace and leave a throw-away snapshot in the slot. Every transfer is
	// checked before the first one is made, so a refused one leaves all the tabs and
	// their text untouched (nothing has been detached or released yet).
	if e.shared != nil {
		for _, d := range e.docs {
			if !victim(d) {
				continue
			}
			if err := e.shared.CheckClose(e, d); err != nil {
				e.setError(err)
				return err
			}
		}
		for _, d := range e.docs {
			if !victim(d) {
				continue
			}
			if err := e.shared.BeforeClose(e, d); err != nil {
				e.setError(err)
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
			// A shared victim was detached above; for a promoted owner this slot now holds
			// a throw-away snapshot.
			e.releaseDoc(e.docs[i])
		default:
			if i == e.active {
				newActive = len(kept)
			}
			if i < e.active {
				beforeActive++
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
		e.reopenEmpty()
		return nil
	}
	if newActive >= 0 {
		e.active = newActive
		return nil
	}
	e.switchTo(min(beforeActive, len(kept)-1))
	return nil
}
