package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/editor"
	expanel "github.com/tbxark/gvim/internal/ui/explorer"
)

// asyncOpenEditor is a large editor with the ports that read a file off the update loop.
type asyncOpenEditor struct {
	*largeEditor
	opened int // synchronous OpenFile calls
	loads  int // reads started by LoadFunc
}

func (e *asyncOpenEditor) OpenFile(path string) error {
	e.opened++
	return e.largeEditor.OpenFile(path)
}
func (e *asyncOpenEditor) IsLargePath(path string) bool { return e.ed.IsLargePath(path) }
func (e *asyncOpenEditor) LoadFunc(path string) func() (*editor.Document, error) {
	e.loads++
	return e.ed.LoadFunc(path)
}

func TestOpeningALargeFileReadsItInACommand(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "big.txt")
	small := filepath.Join(dir, "small.txt")
	if err := os.WriteFile(big, []byte(strings.Repeat("some line\n", 400)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(small, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	le := newLargeEditor(t)
	ae := &asyncOpenEditor{largeEditor: le}
	m := New(Deps{Editor: ae, Keys: testKeys(), Clock: testClock})
	start(m)

	_, cmd := m.Update(expanel.OpenFileMsg{Path: big})
	if ae.opened != 0 || len(ae.Tabs()) != 0 {
		t.Fatalf("the large file was opened in Update: %d OpenFile calls, %d tabs", ae.opened, len(ae.Tabs()))
	}
	drain(t, m, cmd)
	tabs := ae.Tabs()
	if ae.loads != 1 || len(tabs) != 1 || tabs[0].Path != big {
		t.Fatalf("loads %d, tabs %+v: want the file opened from the command", ae.loads, tabs)
	}

	// A small file still opens in Update, and so does a preview.
	m.Update(expanel.OpenFileMsg{Path: small})
	if ae.opened != 1 || len(ae.Tabs()) != 2 {
		t.Errorf("small file: %d OpenFile calls, %d tabs", ae.opened, len(ae.Tabs()))
	}
}

func TestAFailedAsyncOpenOfALargeFileShowsTheNotice(t *testing.T) {
	dir := t.TempDir()
	latin := filepath.Join(dir, "latin.txt")
	if err := os.WriteFile(latin, []byte(strings.Repeat("caf\xe9\n", 400)), 0o644); err != nil {
		t.Fatal(err)
	}
	ae := &asyncOpenEditor{largeEditor: newLargeEditor(t)}
	m := New(Deps{Editor: ae, Keys: testKeys(), Clock: testClock})
	start(m)
	_, cmd := m.Update(expanel.OpenFileMsg{Path: latin})
	drain(t, m, cmd)
	if !strings.Contains(m.notice, "requires full load") || len(ae.Tabs()) != 0 {
		t.Errorf("notice %q, %d tabs: want the full-load question", m.notice, len(ae.Tabs()))
	}
}
