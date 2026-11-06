package app

import (
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
}

// saveSession writes the open tabs of ed to dir. Each file keeps its cursor and its
// first visible line. A modified tab keeps its text in a stash file, so the unsaved
// edits come back with the session. An untitled tab with no text is not kept. The
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
	var files []session.FileState
	active := 0
	for i := range tabs {
		ed.ActivateTab(i)
		doc := ed.Active()
		if doc == nil || (doc.Path == "" && !doc.Modified()) {
			continue
		}
		fs := session.FileState{
			Path: doc.Path,
			Line: doc.Cursor.Line + 1,
			Col:  doc.Cursor.Col + 1,
			Top:  doc.Top,
		}
		if doc.Modified() {
			name, err := session.StashContent(dir, doc.Buffer.Text())
			if err != nil {
				return err
			}
			fs.StashFile = name
		}
		if i == orig {
			active = len(files)
		}
		files = append(files, fs)
	}
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
	var failed []unrestored
	active := -1
	for i, f := range s.Files {
		opened, err := restoreFile(ed, dir, f)
		if err != nil {
			if f.StashFile != "" {
				failed = append(failed, unrestored{file: f, err: err})
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
func restoreFile(ed *editor.Editor, dir string, f session.FileState) (bool, error) {
	text := ""
	if f.StashFile != "" {
		t, err := session.ReadStash(dir, f.StashFile)
		if err != nil {
			return false, err
		}
		text = t
	}
	if f.Path == "" {
		ed.Do("new", nil)
	} else {
		_, statErr := os.Stat(f.Path)
		if statErr != nil && f.StashFile == "" {
			return false, nil
		}
		if err := ed.OpenFile(f.Path); err != nil {
			return false, err
		}
	}
	doc := ed.Active()
	if doc == nil {
		return false, errors.New("no tab was opened")
	}
	if f.StashFile != "" {
		replaceText(doc, text)
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
	for i, u := range failed {
		names[i] = "untitled buffer"
		if u.file.Path != "" {
			names[i] = filepath.Base(u.file.Path)
		}
	}
	return fmt.Sprintf("session: kept unsaved text of %s, which could not be restored: %v",
		strings.Join(names, ", "), failed[0].err)
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
