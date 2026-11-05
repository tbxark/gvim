package editor

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// newFileMode is the mode of a file that a save creates. A file that already
// exists keeps its own mode.
const newFileMode fs.FileMode = 0o644

// writeFileAtomic replaces the file at path with data. The bytes go to a
// temporary file in the same directory, which is synced and then renamed over
// the target, so a crash or a failed write never leaves a partial file. The
// temporary file gets the mode of the existing file, or newFileMode for a new
// one. A symbolic link at path is followed, so the link stays and its target
// is replaced. On failure the temporary file is removed and the old file is
// left as it was.
func writeFileAtomic(path string, data []byte) error {
	target := path
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		target = resolved
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
