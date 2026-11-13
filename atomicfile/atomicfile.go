// Package atomicfile replaces a file in one step, without ever leaving a partial
// file behind: the bytes go to a temporary file in the same directory, which is
// synced and then renamed over the target, so a reader sees either the old file
// or the new one.
//
// A write keeps what a save should keep of the file it replaces: its mode, and on
// Unix its owner, group, set-id bits and extended attributes, as far as the
// system lets the user. A symbolic link is followed, so the link stays and its
// target is replaced; a dangling link is an error, and nothing is created at its
// path. What a save could not keep is reported as a note, not as an error.
//
// The target is never opened for writing: the only way bytes reach it is the
// rename of the temporary file.
package atomicfile

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"unicode/utf8"
)

// newFileMode is the mode of a file that a write creates. A file that already
// exists keeps its own mode.
const newFileMode fs.FileMode = 0o644

// RenameFn puts a finished temporary file in place. Tests replace it to make the
// rename fail.
var RenameFn = os.Rename

// WriteFile replaces the file at path with data. The bytes go to a temporary
// file in the same directory, which is synced and then renamed over the target,
// so a crash or a failed write never leaves a partial file. The temporary file
// gets the mode of the existing file, or the default new-file mode (0644) for a
// new one. A symbolic link at path is followed, so the link stays and its target
// is replaced. A dangling link is an error, and nothing is created at its path. On
// failure the temporary file is removed and the old file is left as it was.
//
// The new file takes the owner, group, set-id bits and extended attributes of the old
// one as far as the system lets the user. note says what could not be kept, and that
// the other names of a file with several links keep the old text; it is "" otherwise.
func WriteFile(path string, data []byte) (note string, err error) {
	return WriteFileMode(path, data, 0)
}

// WriteFileMode replaces the file at path with data as WriteFile does, but a new
// file gets mode instead of the default 0644. A mode of 0 means the default. A file
// that already exists keeps its own mode whatever mode says, as it does for
// WriteFile.
func WriteFileMode(path string, data []byte, mode fs.FileMode) (note string, err error) {
	return writeFileStream(path, func(w io.Writer) error {
		_, err := w.Write(data)
		return err
	}, nil, mode)
}

// WriteFileStream replaces the file at path with what fill writes, as WriteFile
// does with a byte slice, but the bytes are never held whole: fill writes them to
// the temporary file as it produces them. onTemp, when it is not nil, receives
// the state of the finished temporary file, taken before it is renamed, so the
// caller can tell that the file at path afterwards is that file.
//
// The target is never opened for writing. The only way bytes reach it is the rename
// of the temporary file; there is no fallback that writes in place. If the temporary
// file cannot be made, a write, the sync or the rename fails, the error is returned,
// the temporary file is removed and the old file is left as it was.
func WriteFileStream(path string, fill func(io.Writer) error, onTemp func(os.FileInfo)) (note string, err error) {
	return writeFileStream(path, fill, onTemp, 0)
}

// writeFileStream is WriteFileStream with the mode of a new file; a zero mode means
// the default new-file mode.
func writeFileStream(path string, fill func(io.Writer) error, onTemp func(os.FileInfo), mode fs.FileMode) (note string, err error) {
	target, err := saveTarget(path)
	if err != nil {
		return "", err
	}
	if mode == 0 {
		mode = newFileMode
	}
	var old os.FileInfo
	info, err := os.Stat(target)
	switch {
	case err == nil:
		mode, old = info.Mode().Perm(), info
	case errors.Is(err, fs.ErrNotExist):
		// A new file is created as open(2) would create it: the umask applies.
		mode &^= umask
	default:
		return "", err
	}

	tmp, err := os.CreateTemp(filepath.Dir(target), TempPrefix(filepath.Base(target))+"*")
	if err != nil {
		return "", err
	}
	name := tmp.Name()
	if note, err = writeTemp(tmp, mode, target, old, fill, onTemp); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	if err := RenameFn(name, target); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	syncDir(filepath.Dir(target))
	return note, nil
}

// maxTempBase bounds the part of the target's name that the temporary name keeps: with
// the dot prefix and the ".gvim-" random suffix it must stay inside the 255-byte limit of
// a file name.
const maxTempBase = 200

// TempPrefix is the start of the name of the temporary file a write to a file called
// name creates next to it: ".NAME.gvim-", with NAME shortened as tempBase does. A
// caller that looks for the leftovers of an interrupted write matches on it.
func TempPrefix(name string) string {
	return "." + tempBase(name) + ".gvim-"
}

// tempBase shortens name to maxTempBase bytes without cutting a UTF-8 sequence.
func tempBase(name string) string {
	if len(name) <= maxTempBase {
		return name
	}
	cut := maxTempBase
	for cut > 0 && !utf8.RuneStart(name[cut]) {
		cut--
	}
	return name[:cut]
}

// saveTarget returns the file that a write to path replaces. For a symbolic link
// it is the final target, so the link itself stays. A link whose target is
// missing is an error, so no regular file takes the link's place.
func saveTarget(path string) (string, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return path, nil
	}
	if err != nil {
		return "", err
	}
	if info.Mode()&fs.ModeSymlink == 0 {
		return path, nil
	}
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve symbolic link: %w", err)
	}
	return target, nil
}

// writeTemp gives f mode, lets fill write its content, makes it like old (see
// inheritFile; old is nil for a new file, and path is where old is), then syncs it,
// reports its state to onTemp and closes it. The file is closed on every path. note is
// what could not be kept of old.
func writeTemp(f *os.File, mode fs.FileMode, path string, old os.FileInfo, fill func(io.Writer) error, onTemp func(os.FileInfo)) (note string, err error) {
	err = f.Chmod(mode)
	if err == nil {
		err = fill(f)
	}
	if err == nil {
		note = inheritFile(f, path, old)
		err = f.Sync()
	}
	if err == nil && onTemp != nil {
		if fi, statErr := f.Stat(); statErr == nil {
			onTemp(fi)
		}
	}
	return note, errors.Join(err, f.Close())
}
