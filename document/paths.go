package document

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

// rename makes path the file name of the buffer. The buffer is no longer
// untitled, and the lexer name is cleared because it depended on the old name.
func (d *Document) Rename(path string) {
	if d.Path != path {
		d.language = ""
	}
	d.Path = path
	d.untitled = 0
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
func (d *Document) FollowDisk(path string) {
	if !d.Disk.Exists || d.large != nil {
		// A large document is never read here: its file is too big, and its text is not
		// held. The record stays, so a move that kept the size and the time of the file
		// reads as no change, and any other move reads as an outside change on save.
		return
	}
	now := diskStateOf(path)
	if !now.Exists {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != d.fileText(d.Saved) {
		return
	}
	d.Disk = now
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
