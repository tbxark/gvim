package app

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/session"
	"github.com/tbxark/gvim/internal/ui"
)

// defaultSessionDir is the session directory Run uses when Options.SessionDir is
// empty: os.UserCacheDir()/gvim/session.
func defaultSessionDir() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("session: cache dir: %w", err)
	}
	return filepath.Join(cache, "gvim", "session"), nil
}

// unrestored is a saved file with unsaved text that restoreSession could not put
// back. Its stash file must survive the next save, and the user is told about it.
type unrestored struct {
	file session.FileState
	err  error
	// stash is the full path of the stash file that is kept, or "".
	stash string
}

// saveSession writes the open tabs of ed to dir. Each file keeps its cursor and its
// first visible line. A modified tab keeps its text in a stash file, so the unsaved
// edits come back with the session. An untitled tab with no text is not kept, and neither
// is a virtual one, which belongs to a plugin and is not a file. The
// explorer state goes in as it is. The active tab is kept as the index among the
// saved tabs, and the editor's own active tab is put back.
func saveSession(ed *editor.Editor, dir string, st ui.ExplorerState) error {
	return writeSession(ed, dir, st, nil)
}

// writeSession writes the session as saveSession does, and lists the kept files after
// the open tabs. A kept file is one whose unsaved text restoreSession could not put
// back. Its stash file is named in the new session, so the save does not remove it,
// and the next start tries to restore it again.
func writeSession(ed *editor.Editor, dir string, st ui.ExplorerState, kept []session.FileState) error {
	tabs := ed.Tabs()
	orig := -1
	for i, t := range tabs {
		if t.Active {
			orig = i
		}
	}
	defer func() {
		if orig >= 0 {
			ed.ActivateTab(orig)
		}
	}()
	var files, failedStashes []session.FileState
	active := 0
	for i := range tabs {
		ed.ActivateTab(i)
		doc := ed.Active()
		if doc == nil || doc.Virtual() || (doc.Path == "" && !doc.Modified()) {
			continue
		}
		fs := session.FileState{
			Path: doc.Path,
			Line: doc.Cursor.Line + 1,
			Col:  doc.Cursor.Col + 1,
			Top:  doc.Top,
		}
		if doc.Large() {
			// The unsaved text of a large document is not held whole. Its stash is the
			// edited lines and the lines of the file they leave alone.
			if st, ok := doc.OverlayStash(); ok {
				name, err := session.StashOverlay(dir, st)
				if err != nil {
					return err
				}
				fs.OverlayFile = name
			}
			// A stash that could not be put back is kept next to the edits made since.
			if st, ok := doc.FailedStash(); ok {
				name, err := session.StashOverlay(dir, st)
				if err != nil {
					return err
				}
				failedStashes = append(failedStashes, session.FileState{Path: doc.Path, OverlayFile: name})
			}
		} else if doc.Modified() {
			name, err := session.StashContent(dir, doc.Buffer.Text())
			if err != nil {
				return err
			}
			fs.StashFile = name
			stampDisk(&fs, doc)
		}
		if i == orig {
			active = len(files)
		}
		files = append(files, fs)
	}
	files = append(files, failedStashes...)
	files = append(files, kept...)
	return session.Save(dir, session.Session{
		Root:        st.Root,
		Explorer:    st.Open,
		ShowHidden:  st.ShowHidden,
		ShowIgnored: st.ShowIgnored,
		Active:      active,
		Files:       files,
	})
}

// restoreSession opens the files of s into ed, in order, with their cursors and
// first visible lines, and puts the unsaved text of each stash file back into its
// tab. A saved file that no longer exists is left out, unless it has unsaved text,
// because the editor would open it as an empty buffer. The saved active tab becomes
// the active one when it was restored. A file with unsaved text that cannot be
// restored gets no tab, and it is returned, so the caller keeps its stash and tells
// the user. A saved file with no unsaved text that fails is left out silently, as
// before.
func restoreSession(ed *editor.Editor, dir string, s session.Session) []unrestored {
	return restoreFiles(ed, dir, s, false)
}

// restoreFiles is restoreSession. With replace set, the stashed edits of a large file
// replace the ones that the document holds already: the copy is newer than what the
// editor restored before (see editor.ReplaceOverlay).
func restoreFiles(ed *editor.Editor, dir string, s session.Session, replace bool) []unrestored {
	var failed []unrestored
	active := -1
	for i, f := range s.Files {
		opened, err := restoreFile(ed, dir, f, replace)
		if err != nil {
			if f.StashFile != "" || f.OverlayFile != "" {
				failed = append(failed, unrestored{file: f, err: err, stash: filepath.Join(dir, cmp.Or(f.OverlayFile, f.StashFile))})
			}
			continue
		}
		if !opened {
			continue
		}
		if i == s.Active {
			active = activeIndex(ed)
		}
	}
	if active >= 0 {
		ed.ActivateTab(active)
	}
	return failed
}

// restoreFile opens one saved file and applies its state. It reports whether a tab
// was opened. The stash is read before the file is opened, so a file whose unsaved
// text cannot be read is not opened either, and an error is returned with no tab.
//
// The unsaved edits of a large file are an overlay stash. They fit only the file they
// were made on, so a file that is gone, or is not opened as a large file now, keeps its
// stash and is reported. The edits wait for the line index when it has not reached the
// lines they refer to (see editor.RestoreOverlay). With replace set, the stash replaces
// the edits the document holds already (see restoreFiles).
func restoreFile(ed *editor.Editor, dir string, f session.FileState, replace bool) (bool, error) {
	text := ""
	if f.StashFile != "" {
		t, err := session.ReadStash(dir, f.StashFile)
		if err != nil {
			return false, err
		}
		text = t
	}
	var overlay *session.OverlayStash
	if f.OverlayFile != "" {
		st, err := session.ReadOverlay(dir, f.OverlayFile)
		if err != nil {
			return false, err
		}
		overlay = &st
	}
	if f.Path == "" {
		ed.Do("new", nil)
	} else {
		_, statErr := os.Stat(f.Path)
		if statErr != nil && overlay != nil {
			return false, fmt.Errorf("%s no longer exists; its stashed changes were kept", filepath.Base(f.Path))
		}
		if statErr != nil && f.StashFile == "" {
			return false, nil
		}
		if err := openFile(ed, f.Path); err != nil {
			return false, err
		}
	}
	doc := ed.Active()
	if doc == nil {
		return false, errors.New("no tab was opened")
	}
	if f.StashFile != "" && doc.Large() {
		// The stash is a whole text, and a large document is never replaced whole.
		return true, fmt.Errorf("%s is a large file now; its stashed text was kept", filepath.Base(f.Path))
	}
	if overlay != nil {
		restore := ed.RestoreOverlay
		if replace {
			restore = ed.ReplaceOverlay
		}
		if err := restore(doc, *overlay); err != nil {
			return true, fmt.Errorf("%s: %w; its stashed changes were kept", filepath.Base(f.Path), err)
		}
	}
	if f.StashFile != "" {
		replaceText(doc, text)
		if f.MTimeUnixNano != 0 {
			// The text was edited from the file as it was then. Keep that as the
			// baseline, so a file that changed since is not overwritten unasked.
			doc.ExpectDisk(f.Size, f.MTimeUnixNano)
		}
	}
	if f.Line > 0 {
		_ = ed.GotoLine(f.Line, max(f.Col, 1))
	}
	if f.Top > 0 {
		ed.ScrollTo(f.Top)
	}
	return true, nil
}

// restoreNotice is the status row message that names the files whose unsaved text was
// kept because they could not be restored. It is empty when none failed.
func restoreNotice(failed []unrestored) string {
	if len(failed) == 0 {
		return ""
	}
	names := make([]string, len(failed))
	var stashes []string
	for i, u := range failed {
		names[i] = "untitled buffer"
		if u.file.Path != "" {
			names[i] = filepath.Base(u.file.Path)
		}
		if u.stash != "" {
			stashes = append(stashes, u.stash)
		}
	}
	notice := fmt.Sprintf("session: kept unsaved text of %s, which could not be restored: %v",
		strings.Join(names, ", "), failed[0].err)
	if len(stashes) > 0 {
		notice += " (stash: " + strings.Join(stashes, ", ") + ")"
	}
	return notice
}

// replaceText puts text in place of the whole buffer of doc. The change is one undo
// step, and the document is modified unless text is its saved text.
func replaceText(doc *editor.Document, text string) {
	doc.Buffer.Group(func() {
		doc.Buffer.Delete(buffer.Pos{}, doc.Buffer.End())
		doc.Buffer.Insert(buffer.Pos{}, text)
	})
}

// activeIndex is the index of the active tab of ed, or -1.
func activeIndex(ed *editor.Editor) int {
	for i, t := range ed.Tabs() {
		if t.Active {
			return i
		}
	}
	return -1
}

// stampDisk records in fs the state of the file that the unsaved text of doc is based
// on, so a restore can tell whether the file changed after the stash was taken.
func stampDisk(fs *session.FileState, doc *editor.Document) {
	if size, mtime, ok := doc.DiskStamp(); ok {
		fs.Size, fs.MTimeUnixNano = size, mtime
	}
}
