package editor

import (
	"path/filepath"

	"github.com/tbxark/gvim/session"
)

// StashEntry is one file whose unsaved text an interrupted save kept in a stash file.
type StashEntry struct {
	// Path is the file, or "" for an untitled buffer.
	Path string
	// Stash is the full path of the stash file that holds the unsaved text.
	Stash string
}

// InterruptedSaves lists the files of the saved session in Options.SessionDir that have
// a stash, in the order the session keeps them. It reads the session file, so the caller
// runs it from a command, not on the update loop. With no session directory, or no saved
// session, it returns nothing and no error. A stash name with a directory part is left out,
// so the list never names a file outside the session directory.
func (e *Editor) InterruptedSaves() ([]StashEntry, error) {
	dir := e.opts.SessionDir
	if dir == "" {
		return nil, nil
	}
	sess, ok, err := session.Load(dir)
	if err != nil || !ok {
		return nil, err
	}
	var out []StashEntry
	for _, f := range sess.Files {
		if f.StashFile == "" || filepath.Base(f.StashFile) != f.StashFile {
			continue
		}
		out = append(out, StashEntry{Path: f.Path, Stash: filepath.Join(dir, f.StashFile)})
	}
	return out, nil
}
