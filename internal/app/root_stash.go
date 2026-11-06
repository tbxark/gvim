package app

import (
	"cmp"
	"path/filepath"

	"github.com/tbxark/gvim/internal/session"
	"github.com/tbxark/gvim/internal/ui"
)

// sessionStashes lists the files whose unsaved text an interrupted save kept, from the
// saved session in dir. It reads the session file, so the root calls it from a command.
type sessionStashes struct{ dir string }

// Interrupted returns the files of the saved session that have a stash, in the order
// the session keeps them. Each stash is the full path of its file in dir.
func (s sessionStashes) Interrupted() ([]ui.Interrupted, error) {
	sess, ok, err := session.Load(s.dir)
	if err != nil || !ok {
		return nil, err
	}
	var out []ui.Interrupted
	for _, f := range sess.Files {
		// A large file keeps its unsaved edits as an overlay stash.
		name := cmp.Or(f.StashFile, f.OverlayFile)
		if name == "" {
			continue
		}
		out = append(out, ui.Interrupted{Path: f.Path, Stash: filepath.Join(s.dir, name)})
	}
	return out, nil
}

// stashesFor returns the stash port of the session directory dir, or nil when there is
// no session directory, so the review says so.
func stashesFor(dir string) ui.InterruptedSaves {
	if dir == "" {
		return nil
	}
	return sessionStashes{dir: dir}
}
