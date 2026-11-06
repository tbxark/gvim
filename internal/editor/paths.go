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
		e.setStatus(err.Error())
		return err
	}
	for i, d := range e.docs {
		if moving[i] {
			d.rename(targets[i])
			d.followDisk(targets[i])
		}
	}
	return nil
}

// followDisk records the state of the file at path after the document has been
// moved there, so a move does not read as an outside change. A move across
// devices copies the file and gives it a new modification time, so the recorded
// state is replaced only when the file still holds the saved text, the bytes
// a save would produce. Bytes that differ mean the file was changed outside the
// editor, even if its size did not change. Then the old record stays and Save
// still refuses to overwrite it.
//
// Reading the file here is a documented exception to the rule that Update does
// no IO. RenamePath already stats the moved file in Update, and the bytes are the
// only way to tell a move from an outside edit that keeps the size. The state is
// taken before the read, as readDocument does, so a change between the two shows
// as a difference on save.
func (d *Document) followDisk(path string) {
	if !d.disk.exists || d.large != nil {
		// A large document is never read here: its file is too big, and its text is not
		// held. The record stays, so a move that kept the size and the time of the file
		// reads as no change, and any other move reads as an outside change on save.
		return
	}
	now := diskStateOf(path)
	if !now.exists {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != d.fileText(d.saved) {
		return
	}
	d.disk = now
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
// the error wraps ErrUnsaved. Closing the last tab leaves the editor Empty, as
// CloseTab does. A path that matches no open document is not an error. An empty
// path returns an error wrapping ErrEmptyPath.
func (e *Editor) CloseByPath(path string, force bool) error {
	if path == "" {
		err := fmt.Errorf("close: %w", ErrEmptyPath)
		e.setStatus(err.Error())
		return err
	}
	path, err := absPath(path)
	if err != nil {
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
			e.releaseDoc(d)
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

// absPath returns the absolute form of path. Document paths are kept absolute, so
// a path given relative to the working directory matches the absolute paths the
// explorer reports. An empty path stays empty, so the callers still report
// ErrEmptyPath for it.
//
// filepath.Abs cleans ".." lexically, before the kernel resolves symbolic links.
// If "link" is a symbolic link, "link/../f" can name a different file after that
// clean than the one the kernel opens. So when a path element is "..", absPath
// splits off the base name before cleaning and resolves the directory part with
// filepath.EvalSymlinks. The physical directory is kept only when it is a
// different directory from the lexical one. When the two are the same directory,
// which is the usual case and includes the macOS /var link, the caller's spelling
// is kept, so the stored path matches the spelling the explorer uses. A directory
// part that does not exist keeps the lexical result.
func absPath(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	if !hasDotDotElement(path) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("%s: %w", path, err)
		}
		return abs, nil
	}
	if !filepath.IsAbs(path) {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("%s: %w", path, err)
		}
		path = wd + string(filepath.Separator) + path
	}
	lexical := filepath.Clean(path)
	i := strings.LastIndexByte(path, filepath.Separator)
	dir, base := path[:i+1], path[i+1:]
	if base == "" || base == "." || base == ".." {
		return lexical, nil
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil || sameDirectory(resolved, filepath.Dir(lexical)) {
		return lexical, nil
	}
	return filepath.Join(resolved, base), nil
}

// hasDotDotElement reports whether one element of path is exactly "..". A name
// that only contains two dots, such as "a..b", is not a parent reference.
func hasDotDotElement(path string) bool {
	for _, el := range strings.Split(path, string(filepath.Separator)) {
		if el == ".." {
			return true
		}
	}
	return false
}

// sameDirectory reports whether a and b are the same existing directory, so that
// their spellings differ only by links. A path that cannot be stat'd is not the
// same directory.
func sameDirectory(a, b string) bool {
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ai, bi)
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
