package editor

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/tbxark/gvim/internal/buffer"
)

// newFileMode is the mode of a file that a save creates. A file that already
// exists keeps its own mode.
const newFileMode fs.FileMode = 0o644

// renameFn puts a finished temporary file in place. Tests replace it to make the
// rename fail.
var renameFn = os.Rename

// writeFileAtomic replaces the file at path with data. The bytes go to a
// temporary file in the same directory, which is synced and then renamed over
// the target, so a crash or a failed write never leaves a partial file. The
// temporary file gets the mode of the existing file, or newFileMode for a new
// one. A symbolic link at path is followed, so the link stays and its target
// is replaced. A dangling link is an error, and nothing is created at its path.
// On failure the temporary file is removed and the old file is left as it was.
//
// The new file takes the owner, group, set-id bits and extended attributes of the old
// one as far as the system lets the user. note says what could not be kept, and that
// the other names of a file with several links keep the old text; it is "" otherwise.
func writeFileAtomic(path string, data []byte) (note string, err error) {
	return writeFileAtomicStream(path, func(w io.Writer) error {
		_, err := w.Write(data)
		return err
	}, nil)
}

// writeFileAtomicStream replaces the file at path with what fill writes, as
// writeFileAtomic does with a byte slice, but the bytes are never held whole: fill
// writes them to the temporary file as it produces them. onTemp, when it is not nil,
// receives the state of the finished temporary file, taken before it is renamed, so
// the caller can tell that the file at path afterwards is that file.
//
// The target is never opened for writing. The only way bytes reach it is the rename
// of the temporary file; there is no fallback that writes in place. If the temporary
// file cannot be made, a write, the sync or the rename fails, the error is returned,
// the temporary file is removed and the old file is left as it was.
func writeFileAtomicStream(path string, fill func(io.Writer) error, onTemp func(os.FileInfo)) (note string, err error) {
	target, err := saveTarget(path)
	if err != nil {
		return "", err
	}
	mode := newFileMode
	var old os.FileInfo
	info, err := os.Stat(target)
	switch {
	case err == nil:
		mode, old = info.Mode().Perm(), info
	case !errors.Is(err, fs.ErrNotExist):
		return "", err
	}

	tmp, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".gvim-*")
	if err != nil {
		return "", err
	}
	name := tmp.Name()
	if note, err = writeTemp(tmp, mode, target, old, fill, onTemp); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	if err := renameFn(name, target); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return note, nil
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

// openSavedSource opens the file at path as the source of a large document that was
// just written there. info is the state of the open file itself, taken from its
// descriptor and not from the path, so the caller can tell which file was opened. It is
// nil for a source that is not a file.
func openSavedSource(path string) (src buffer.Source, info os.FileInfo, err error) {
	src, err = buffer.OpenFileSource(path)
	if err != nil {
		return nil, nil, err
	}
	if st, ok := src.(interface{ Stat() (os.FileInfo, error) }); ok {
		info, _ = st.Stat()
	}
	return src, info, nil
}
