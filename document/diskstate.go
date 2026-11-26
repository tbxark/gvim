package document

import (
	"os"
	"time"
)

// diskState is the size and modification time of a file on disk. A document
// records it when it is opened and after each save. Save compares it with the
// file again, so a change made by another program is not overwritten.
type DiskState struct {
	Exists bool
	dir    bool
	// special is set for a file that is neither regular nor a directory: a FIFO, a device
	// or a socket. Opening one can block, so readDocument does not read it.
	Special bool
	Size    int64
	Mtime   time.Time
}

// diskStateOf returns the state of the file at path, following a symbolic link.
// A missing file, or one that cannot be stat'ed, has exists false. A write to
// such a path reports its own error.
func DiskStateOf(path string) DiskState {
	info, err := os.Stat(path)
	if err != nil {
		return DiskState{}
	}
	return DiskState{
		Exists:  true,
		dir:     info.IsDir(),
		Special: !info.Mode().IsRegular() && !info.IsDir(),
		Size:    info.Size(),
		Mtime:   info.ModTime(),
	}
}

// Dir reports whether the path is a directory. The local stat knows its mode; a remote
// access reports directories through Special false and the caller opening no
// directories, so a remote directory is caught when the read fails.
func (s DiskState) Dir() bool { return s.dir }

// SameAs reports whether s and o describe the same file state.
func (s DiskState) SameAs(o DiskState) bool {
	return s.Exists == o.Exists && s.Size == o.Size && s.Mtime.Equal(o.Mtime)
}
