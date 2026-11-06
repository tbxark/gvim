package editor

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// newFileMode is the mode of a file that a save creates. A file that already
// exists keeps its own mode.
const newFileMode fs.FileMode = 0o644

// writeFileAtomic replaces the file at path with data. The bytes go to a
// temporary file in the same directory, which is synced and then renamed over
// the target, so a crash or a failed write never leaves a partial file. The
// temporary file gets the mode of the existing file, or newFileMode for a new
// one. A symbolic link at path is followed, so the link stays and its target
// is replaced. A dangling link is an error, and nothing is created at its path.
// On failure the temporary file is removed and the old file is left as it was.
func writeFileAtomic(path string, data []byte) error {
	target, err := saveTarget(path)
	if err != nil {
		return err
	}
	mode := newFileMode
	info, err := os.Stat(target)
	switch {
	case err == nil:
		mode = info.Mode().Perm()
	case !errors.Is(err, fs.ErrNotExist):
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".gvim-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if err := writeTemp(tmp, data, mode); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, target); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
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

// diskState is the size and modification time of a file on disk. A document
// records it when it is opened and after each save. Save compares it with the
// file again, so a change made by another program is not overwritten.
type diskState struct {
	exists bool
	size   int64
	mtime  time.Time
}

// diskStateOf returns the state of the file at path, following a symbolic link.
// A missing file, or one that cannot be stat'ed, has exists false. A write to
// such a path reports its own error.
func diskStateOf(path string) diskState {
	info, err := os.Stat(path)
	if err != nil {
		return diskState{}
	}
	return diskState{exists: true, size: info.Size(), mtime: info.ModTime()}
}

// sameAs reports whether s and o describe the same file state.
func (s diskState) sameAs(o diskState) bool {
	return s.exists == o.exists && s.size == o.size && s.mtime.Equal(o.mtime)
}

// writeTemp fills f with data and gives it mode, then syncs and closes it. The
// file is closed on every path.
func writeTemp(f *os.File, data []byte, mode fs.FileMode) error {
	err := f.Chmod(mode)
	if err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	return errors.Join(err, f.Close())
}
