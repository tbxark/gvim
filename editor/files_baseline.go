package editor

import "time"

// DiskStamp returns the size and modification time (UnixNano) of the file the
// document last read or wrote. A stash of the document's unsaved text records it,
// so a later start can tell whether the file changed since. ok is false for a
// document with no file, and for a large one, whose stash is an overlay with its own
// identity check.
func (d *Document) DiskStamp() (size, mtimeNano int64, ok bool) {
	if d.large != nil || d.Path == "" || !d.disk.exists {
		return 0, 0, false
	}
	return d.disk.size, d.disk.mtime.UnixNano(), true
}

// ExpectDisk records that the text of d is based on the file as it was at the given
// size and modification time, as a stash of unsaved text does. When the file on disk
// differs, Save refuses to overwrite it (ErrChangedOnDisk), as it would had the
// file been changed while the text was being edited. A file that is gone is left
// alone: a save then creates it.
func (d *Document) ExpectDisk(size, mtimeNano int64) {
	if d.large != nil || !d.disk.exists {
		return
	}
	d.disk.size, d.disk.mtime = size, time.Unix(0, mtimeNano)
}
