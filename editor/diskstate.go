package editor

import (
	"os"
	"time"
)

// diskState is the size and modification time of a file on disk. A document
// records it when it is opened and after each save. Save compares it with the
// file again, so a change made by another program is not overwritten.
type diskState struct {
	exists bool
	// special is set for a file that is neither regular nor a directory: a FIFO, a device
	// or a socket. Opening one can block, so readDocument does not read it.
	special bool
	size    int64
	mtime   time.Time
}

// diskStateOf returns the state of the file at path, following a symbolic link.
// A missing file, or one that cannot be stat'ed, has exists false. A write to
// such a path reports its own error.
func diskStateOf(path string) diskState {
	info, err := os.Stat(path)
	if err != nil {
		return diskState{}
	}
	return diskState{
		exists:  true,
		special: !info.Mode().IsRegular() && !info.IsDir(),
		size:    info.Size(),
		mtime:   info.ModTime(),
	}
}

// sameAs reports whether s and o describe the same file state.
func (s diskState) sameAs(o diskState) bool {
	return s.exists == o.exists && s.size == o.size && s.mtime.Equal(o.mtime)
}
