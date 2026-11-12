package editor

import (
	"fmt"

	"github.com/tbxark/gvim/document"
)

// DiskChange is the result of checking one open file against the state the editor
// recorded when it opened or last saved it.
type DiskChange struct {
	// Path is the file.
	Path string
	// Changed is true when the file exists and differs from the recorded state, as
	// after another program wrote it. A file that was deleted is not a change, and
	// neither is one the editor saved itself.
	Changed bool
	// Stamp names the state of the file that was found, so the caller can tell a new
	// change from one it already reported.
	Stamp string
}

// PlanDiskCheck records, on the editor's goroutine, the disk state of every open
// file, and returns the function that checks them against the disk. The function
// reads only what the plan recorded, so it may run in a command.
func (e *Editor) PlanDiskCheck() func() []DiskChange {
	type recorded struct {
		Path   string
		was    diskState
		access document.FileAccess
	}
	var files []recorded
	for _, d := range e.docs {
		if d.Ext().Virtual || d.IsImage() {
			// An image document is not reloaded under the viewer: closing and
			// reopening its tab is what reads the file again.
			continue
		}
		// A large document keeps its file open. The check is an fstat of that
		// descriptor, which tells a file rewritten in place from one that another
		// program replaced; the latter does not touch the file the document reads.
		// Like the stat of RenamePath, it is the one exception to "Update does no IO".
		_ = d.CheckSource()
		if d.Path != "" && d.Disk.Exists {
			files = append(files, recorded{Path: d.Path, was: d.Disk, access: d.FileAccess()})
		}
	}
	return func() []DiskChange {
		out := make([]DiskChange, 0, len(files))
		for _, f := range files {
			now := document.DiskStateAt(f.access, f.Path)
			out = append(out, DiskChange{
				Path:    f.Path,
				Changed: now.Exists && !now.SameAs(f.was),
				Stamp:   fmt.Sprintf("%d/%d", now.Size, now.Mtime.UnixNano()),
			})
		}
		return out
	}
}
