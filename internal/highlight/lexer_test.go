package highlight

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/apptest"
)

// TestDocumentLexerReadsOnlyTheHeadOfALargeDocument checks that the lexer is
// chosen from the head of a large document, not its whole text.
func TestDocumentLexerReadsOnlyTheHeadOfALargeDocument(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "script")
	apptest.Must(t, os.WriteFile(path, []byte("#!/usr/bin/env python3\n"+strings.Repeat("print('x')\n", 400)), 0o644))
	ed := editor.New(editor.Options{Empty: true, LargeFileMode: true, LargeFileThresholdBytes: 1024})
	apptest.Must(t, ed.OpenFile(path))
	t.Cleanup(ed.Close)
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
