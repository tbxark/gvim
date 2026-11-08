package app

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/session"
)

// recoveryDirName is the folder of the session directory that holds the crash-recovery
// copy. It is a session of its own, so the saved session and its stash files stay as
// they are.
const recoveryDirName = "recovery"

// recoveryDir is the crash-recovery directory of the session directory dir.
func recoveryDir(dir string) string {
	return filepath.Join(dir, recoveryDirName)
}

// recoveryWriter keeps a copy of the unsaved text of the open documents in a directory,
// as a session with one stash file for each document. It is the hot-exit copy that
// recoverCrash reads after a crash. Plan runs on the editor's goroutine and the
// function it returns does the writes, so the editor is not read from a command.
type recoveryWriter struct {
	ed  *editor.Editor
	dir string

	mu    sync.Mutex
	last  map[*editor.Document]stashed // what the last write holds for each document
	wrote bool                         // the directory holds a session with files
}

// stashed is the stash file that holds the text of a document at version ver of buffer buf.
// The buffer is part of the key because a reload with another encoding gives the document
// a new buffer, whose version counts from 0 again.
type stashed struct {
	buf  *buffer.Buffer
	ver  uint64
	name string
}

// newRecoveryWriter returns a writer of the unsaved text of ed into dir.
func newRecoveryWriter(ed *editor.Editor, dir string) *recoveryWriter {
	return &recoveryWriter{ed: ed, dir: dir}
}

// Plan reads the documents with unsaved text and returns the function that writes
// them. A document whose text has not changed since the last write keeps its stash
// file, so a quiet editor writes nothing but a small session file, and an editor with
// nothing unsaved writes nothing at all. When the last unsaved document is saved, the
// function empties the copy.
func (w *recoveryWriter) Plan() func() error {
	type entry struct {
		doc     *editor.Document
		state   session.FileState
		buf     *buffer.Buffer
		ver     uint64
		text    string                      // the text to stash; empty when name is reused
		overlay func() session.OverlayStash // the edits of a large document to stash
		large   bool                        // the stash is an overlay, not a text
		name    string                      // the stash file to reuse
	}
	w.mu.Lock()
	var entries []entry
	for _, d := range w.ed.UnsavedDocs() {
		e := entry{
			doc: d, large: d.Large(),
			state: session.FileState{
				Path: d.Path,
				Line: d.Cursor.Line + 1,
				Col:  d.Cursor.Col + 1,
				Top:  d.Top,
			},
			buf: d.Buffer,
			ver: d.Buffer.Version(),
		}
		stampDisk(&e.state, d)
		if prev, ok := w.last[d]; ok && prev.buf == e.buf && prev.ver == e.ver {
			e.name = prev.name
		} else if d.Large() {
			// The unsaved text of a large document is not held whole: its copy is the
			// edited lines over the file.
			later, ok := d.OverlayStashLater()
			if !ok {
				continue
			}
			e.overlay = later
		} else {
			e.text = d.Buffer.Text()
		}
		entries = append(entries, e)
	}
	idle := len(entries) == 0 && !w.wrote
	w.mu.Unlock()
	if idle {
		return func() error { return nil }
	}
	return func() error {
		var files []session.FileState
		var created []string
		last := make(map[*editor.Document]stashed, len(entries))
		fail := func(err error) error {
			for _, name := range created {
				_ = os.Remove(filepath.Join(w.dir, name))
			}
			return err
		}
		for _, e := range entries {
			if e.name == "" {
				var name string
				var err error
				if e.overlay != nil {
					name, err = session.StashOverlay(w.dir, e.overlay())
				} else {
					name, err = session.StashContent(w.dir, e.text)
				}
				if err != nil {
					return fail(err)
				}
				e.name = name
				created = append(created, name)
			}
			if e.large {
				e.state.OverlayFile = e.name
			} else {
				e.state.StashFile = e.name
			}
			files = append(files, e.state)
			last[e.doc] = stashed{buf: e.buf, ver: e.ver, name: e.name}
		}
		if err := session.Save(w.dir, session.Session{Files: files}); err != nil {
			return fail(err)
		}
		w.mu.Lock()
		w.last, w.wrote = last, len(files) > 0
		w.mu.Unlock()
		return nil
	}
}

// recoverCrash puts the unsaved text that a crashed session left in dir back into ed:
// each document is opened, or found if it is open already, and takes the stashed text.
// It returns the files it could not restore and the number it did. The tab that was
// active stays active. When all were restored the copy is removed, and the text now
// lives in the modified buffers. Otherwise the whole directory is moved to dir+".failed",
// so the writer, which starts again in dir, does not replace the stashes of the files
// that failed.
func recoverCrash(ed *editor.Editor, dir string) ([]unrestored, int) {
	failed, n, spent := restoreCrash(ed, dir)
	if spent {
		_ = clearRecovery(dir)
	}
	return failed, n
}

// restoreCrash is recoverCrash without the removal of the copy. spent reports that every
// file was restored, so the copy in dir may go once the text is safe somewhere else. A
// copy with failures has been moved aside already.
func restoreCrash(ed *editor.Editor, dir string) (failed []unrestored, n int, spent bool) {
	s, ok, err := session.Load(dir)
	if err != nil || !ok || len(s.Files) == 0 {
		return nil, 0, false
	}
	prev := activeIndex(ed)
	failed = restoreFiles(ed, dir, s, true)
	if prev >= 0 {
		ed.ActivateTab(prev)
	}
	if len(failed) == 0 {
		return nil, len(s.Files), true
	}
	_ = os.RemoveAll(dir + ".failed")
	_ = os.Rename(dir, dir+".failed")
	return failed, len(s.Files) - len(failed), false
}

// clearRecovery empties the copy in dir: a clean exit has nothing to recover.
func clearRecovery(dir string) error {
	if _, ok, _ := session.Load(dir); !ok {
		return nil
	}
	return session.Save(dir, session.Session{})
}
