package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/apptest"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/internal/uiadapter"
	"github.com/tbxark/gvim/keymap"
	"github.com/tbxark/gvim/session"
	"github.com/tbxark/gvim/theme"
)

func quitModel(ed *editor.Editor) *tui.Model {
	return tui.New(uiadapter.NewDeps(ed, keymap.Default(), theme.Theme{}, nil, nil))
}

func TestStashForQuit(t *testing.T) {
	t.Parallel()
	t.Run("a saved session keeps the stash in the session", func(t *testing.T) {
		dir := t.TempDir()
		ed, _ := apptest.LargeSessionFile(t, dir)
		sess := filepath.Join(dir, "session")
		rt := &runtime{ed: ed, model: quitModel(ed), sessionDir: sess, stashDir: sess}
		must(t, rt.stashForQuit())
		s, ok, err := session.Load(sess)
		must(t, err)
		if !ok || len(s.Files) != 1 || s.Files[0].OverlayFile == "" {
			t.Fatalf("session = %+v (found %v), want the overlay stash", s, ok)
		}
	})
	t.Run("without a saved session each stash is a file of its own", func(t *testing.T) {
		dir := t.TempDir()
		ed, _ := apptest.LargeSessionFile(t, dir)
		sess := filepath.Join(dir, "session")
		rt := &runtime{ed: ed, stashDir: sess}
		must(t, rt.stashForQuit())
		files, err := filepath.Glob(filepath.Join(sess, "overlay-*.json"))
		must(t, err)
		if len(files) != 1 {
			t.Fatalf("%d overlay stashes, want 1", len(files))
		}
		if _, err := os.Stat(filepath.Join(sess, "session.json")); err == nil {
			t.Errorf("the stash replaced the saved session")
		}
	})
	t.Run("a stash that cannot be written stops the quit", func(t *testing.T) {
		dir := t.TempDir()
		ed, _ := apptest.LargeSessionFile(t, dir)
		blocker := filepath.Join(dir, "file")
		must(t, os.WriteFile(blocker, []byte("x"), 0o644))
		runtimes := map[string]*runtime{
			"session dir under a file": {ed: ed, model: quitModel(ed), sessionDir: filepath.Join(blocker, "s"), stashDir: filepath.Join(blocker, "s")},
			"stash dir under a file":   {ed: ed, stashDir: filepath.Join(blocker, "s")},
			"no directory":             {ed: ed},
		}
		for name, rt := range runtimes {
			if err := rt.stashForQuit(); err == nil {
				t.Errorf("%s: stashForQuit() = nil, want an error", name)
			}
		}
	})
	t.Run("nothing unsaved writes nothing", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "big.txt")
		must(t, os.WriteFile(path, []byte(strings.Repeat("some line of text\n", 250)), 0o644))
		ed := editor.New(apptest.LargeOpts())
		must(t, ed.OpenFile(path))
		sess := filepath.Join(dir, "session")
		rt := &runtime{ed: ed, model: quitModel(ed), sessionDir: sess, stashDir: sess}
		must(t, rt.stashForQuit())
		if _, err := os.Stat(sess); err == nil {
			t.Errorf("a quit with nothing unsaved wrote %s", sess)
		}
	})
}
