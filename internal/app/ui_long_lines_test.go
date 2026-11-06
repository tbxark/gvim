package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/keymap"
	"github.com/tbxark/gvim/internal/theme"
	"github.com/tbxark/gvim/internal/ui"
)

// TestRootFramesOfLongLinesReadOnlyTheWindows is the regression test for a frame that
// read every visible line of 250 KB whole, 15 MB in all, which is more than the page
// cache holds, so even an idle frame read the same bytes again from the file.
func TestRootFramesOfLongLinesReadOnlyTheWindows(t *testing.T) {
	line := "\t{\"k0\": [1, 22, 333, \"é中\"," + strings.Repeat(" 4444, é中,", 25000) + "]},\n"
	path := filepath.Join(t.TempDir(), "long.txt")
	must(t, os.WriteFile(path, []byte(strings.Repeat(line, 80)), 0o644))
	ed := openBig(t, path)
	finishIndex(t, editorAdapter{ed: ed})
	m := ui.New(newUIDeps(ed, keymap.Default(), theme.Theme{}, nil, nil))
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 45})
	ed.Do("goto_line", map[string]any{"line": 40})
	m.View()
	b := ed.Active().Buffer
	const limit = 2 << 20
	for _, step := range []struct {
		name string
		key  *tea.KeyPressMsg
	}{{"idle", nil}, {"idle again", nil}, {"move down", &tea.KeyPressMsg{Code: tea.KeyDown}}, {"move down again", &tea.KeyPressMsg{Code: tea.KeyDown}}} {
		before := b.Stats().ReadBytes
		if step.key != nil {
			m.Update(*step.key)
		}
		m.View()
		if read := b.Stats().ReadBytes - before; read > limit {
			t.Errorf("%s frame read %d bytes of the file, want at most %d", step.name, read, limit)
		}
	}
}
