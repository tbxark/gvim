package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/session"
	"github.com/tbxark/gvim/internal/ui"
)

// openLargeEditor opens a file of about 4 KiB in an editor whose threshold is 1 KiB,
// so the document is large, and types a character at its start.
func openLargeEditor(t *testing.T, dir string) (*editor.Editor, string) {
	t.Helper()
	path := filepath.Join(dir, "big.txt")
	must(t, os.WriteFile(path, []byte(strings.Repeat("some line of text\n", 250)), 0o644))
	ed := editor.New(editor.Options{Empty: true, LargeFileMode: true, LargeFileThresholdBytes: 1024})
	must(t, ed.OpenFile(path))
	if !ed.Active().Large() {
		t.Fatal("the document is not large")
	}
	ed.Do("insert_char", map[string]any{"text": "Z"})
	if !ed.Active().Modified() {
		t.Fatal("the document is not modified")
	}
	return ed, path
}

// The session and the recovery copy keep the unsaved edits of a large document as an
// overlay stash, never as a text: the tests of large_session_test.go check the stash.
// These two check that no text is built for it.

func TestSessionStashesALargeDocumentWithoutItsText(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ed, path := openLargeEditor(t, dir)
	sess := filepath.Join(dir, "session")
	must(t, writeSession(ed, sess, ui.ExplorerState{}, nil))
	s, ok, err := session.Load(sess)
	must(t, err)
	if !ok || len(s.Files) != 1 || s.Files[0].Path != path {
		t.Fatalf("session = %+v (found %v), want the large file listed", s, ok)
	}
	if s.Files[0].StashFile != "" || len(stashFiles(t, sess)) != 0 || s.Files[0].OverlayFile == "" {
		t.Fatalf("a large document has a text stash or no overlay: %+v, files %v", s.Files[0], stashFiles(t, sess))
	}
	if st := ed.Active().Buffer.Stats(); st.OverCap != 0 || st.MaterializedBytes != 0 {
		t.Fatalf("the session save read the text: %+v", st)
	}
}

func TestRecoveryWriterCopiesALargeDocumentWithoutItsText(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ed, _ := openLargeEditor(t, dir)
	rec := filepath.Join(dir, "recovery")
	w := newRecoveryWriter(ed, rec)
	must(t, w.Plan()())
	s, ok, err := session.Load(rec)
	must(t, err)
	if !ok || len(s.Files) != 1 || s.Files[0].OverlayFile == "" || s.Files[0].StashFile != "" {
		t.Fatalf("recovery session = %+v (found %v), want one overlay copy", s, ok)
	}
	if st := ed.Active().Buffer.Stats(); st.OverCap != 0 || st.MaterializedBytes != 0 {
		t.Fatalf("the recovery plan read the text: %+v", st)
	}
}

func TestRestoreKeepsAStashForAFileThatIsLargeNow(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "grown.txt")
	original := strings.Repeat("some line of text\n", 250)
	must(t, os.WriteFile(path, []byte(original), 0o644))
	sess := filepath.Join(dir, "session")
	stash, err := session.StashContent(sess, "stashed edits")
	must(t, err)
	must(t, session.Save(sess, session.Session{Files: []session.FileState{{Path: path, StashFile: stash}}}))
	s, _, err := session.Load(sess)
	must(t, err)

	ed := editor.New(editor.Options{Empty: true, LargeFileMode: true, LargeFileThresholdBytes: 1024})
	failed := restoreSession(ed, sess, s)
	if len(failed) != 1 || failed[0].file.StashFile != stash {
		t.Fatalf("failed = %+v, want the stash kept as unrestored", failed)
	}
	d := ed.Active()
	if d == nil || !d.Large() || d.Modified() || d.Buffer.Line(0) != "some line of text" {
		t.Fatal("the stash replaced the text of a large document")
	}
}

func TestDocumentLexerReadsOnlyTheHeadOfALargeDocument(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "script")
	must(t, os.WriteFile(path, []byte("#!/usr/bin/env python3\n"+strings.Repeat("print('x')\n", 400)), 0o644))
	ed := editor.New(editor.Options{Empty: true, LargeFileMode: true, LargeFileThresholdBytes: 1024})
	must(t, ed.OpenFile(path))
	doc := ed.Active()
	if !doc.Large() {
		t.Fatal("the document is not large")
	}
	lexer := documentLexer(doc)
	if lexer == nil || lexer.Config().Name != "Python" {
		t.Fatalf("lexer = %v, want Python from the shebang", lexer)
	}
	if st := doc.Buffer.Stats(); st.MaterializedBytes != 0 || st.OverCap != 0 {
		t.Fatalf("the lexer choice read the whole text: %+v", st)
	}
}
