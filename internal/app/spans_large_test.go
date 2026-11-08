package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/apptest"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/internal/uiadapter"
	"github.com/tbxark/gvim/keymap"
	"github.com/tbxark/gvim/theme"
)

const (
	spanFuncs = 30_000 // lines of the generated Go file
	rawAt     = 20_000 // the line where a raw string starts
	rawLen    = 300    // the lines the raw string spans
)

// TestRootPaintsALargeGoFileThroughOneWindow runs the real adapter under the real root:
// a frame in the middle of a file of 30000 lines reads the lines of the window and of
// the rows, and the frames after it read the rows only.
func TestRootPaintsALargeGoFileThroughOneWindow(t *testing.T) {
	t.Parallel()
	ed := apptest.OpenBig(t, goFile(t, t.TempDir(), -1, 0))
	m := tui.New(uiadapter.NewDeps(ed, keymap.Default(), theme.Theme{}, nil, nil))
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	must(t, ed.GotoLine(15_001, 1))
	b := ed.Active().Buffer
	before := b.Stats().LineCalls
	first := ansi.Strip(m.View().Content)
	firstCalls := b.Stats().LineCalls - before
	if !strings.Contains(first, "func f15000() int") {
		t.Fatalf("the frame does not show line 15001:\n%s", first)
	}
	if firstCalls > 2000 {
		t.Errorf("the first frame decoded %d lines, want the window and the rows (under 2000 of %d)", firstCalls, spanFuncs)
	}
	before = b.Stats().LineCalls
	for range 3 {
		_ = m.View()
	}
	if per := (b.Stats().LineCalls - before) / 3; per > 300 {
		t.Errorf("a frame of the same text decoded %d lines, want the rows only", per)
	}
	if st := b.Stats(); st.OverCap != 0 {
		t.Errorf("painting went over the cap: %+v", st)
	}
}

// goFile writes a Go file of spanFuncs lines (about 800 KiB), with a raw string that
// starts at line rawAt and spans rawLen lines. The line extra, when it is not negative,
// is replaced by a line of extraLen bytes.
func goFile(t *testing.T, dir string, extra, extraLen int) string {
	t.Helper()
	var sb strings.Builder
	for i := range spanFuncs {
		switch {
		case i == extra:
			sb.WriteString("var long = \"" + strings.Repeat("y", extraLen) + "\"\n")
		case i == rawAt:
			sb.WriteString("var raw = `\n")
		case i > rawAt && i < rawAt+rawLen:
			sb.WriteString("    x := 1\n")
		case i == rawAt+rawLen:
			sb.WriteString("`\n")
		default:
			fmt.Fprintf(&sb, "func f%d() int { return %d }\n", i, i)
		}
	}
	path := filepath.Join(dir, "big.go")
	must(t, os.WriteFile(path, []byte(sb.String()), 0o644))
	return path
}
