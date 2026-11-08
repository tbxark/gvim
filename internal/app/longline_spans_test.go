package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
)

// With line wrap off, a line of tens of megabytes can be moved over to its real end, and the
// window of columns follows the caret there.
func TestHugeLineWithoutWrapScrollsToItsEnd(t *testing.T) {
	isolateConfig(t)
	const n = 12 << 20
	content := longText(n, "THE-TAIL-MARK")
	rt := openLong(t, "big.json", content)
	d := rt.ed.Active()
	if !d.Large() {
		t.Fatal("not large")
	}
	if err := rt.model.Run("toggle_line_wrap"); err != nil {
		t.Fatal(err)
	}
	screen := key(rt, tea.KeyPressMsg{Code: tea.KeyEnd})
	if got := d.Cursor; got.Line != 0 || got.Col != len(content) {
		t.Fatalf("End: caret %+v, want column %d, the real end of the line", got, len(content))
	}
	if !strings.Contains(screen, "THE-TAIL-MARK") {
		t.Errorf("the window does not show the end of the line:\n%s", screen)
	}
	screen = key(rt, tea.KeyPressMsg{Code: tea.KeyHome})
	if got := d.Cursor; got != (buffer.Pos{}) {
		t.Fatalf("Home: caret %+v", got)
	}
	if !strings.Contains(screen, `{"k":"v`) {
		t.Errorf("the window does not show the start of the line:\n%s", screen)
	}
}
