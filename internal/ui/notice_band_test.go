package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/theme"
	expanel "github.com/tbxark/gvim/internal/ui/explorer"
)

// TestAClickOnTheNoticeBandOnlyDismissesIt checks that the band is inert: the click
// ends the message, and it is not resolved against the body cell under the band.
func TestAClickOnTheNoticeBandOnlyDismissesIt(t *testing.T) {
	t.Parallel()
	m := noticeModel(t, 100, 30, longNotice)
	l := m.layout()
	if len(l.notice) == 0 {
		t.Fatal("setup: no notice band")
	}
	before := m.caretNow()
	for y := l.noticeY; y < l.statusY; y++ {
		m := noticeModel(t, 100, 30, longNotice)
		m.Update(click(10, y, tea.MouseLeft))
		if got := m.caretNow(); got != before {
			t.Errorf("click on band row %d moved the caret from %v to %v", y, before, got)
		}
		if got := m.layout().notice; len(got) != 0 {
			t.Errorf("click on band row %d left the band: %q", y, got)
		}
	}
}

// TestNoticeBandDoesNotHideTheCaretLine checks that a message that appears while the
// caret is on a row the band covers scrolls the body, so the caret line stays visible.
func TestNoticeBandDoesNotHideTheCaretLine(t *testing.T) {
	t.Parallel()
	lines := make([]string, 60)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i)
	}
	for _, size := range [][2]int{{100, 20}, {80, 24}, {60, 12}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			t.Parallel()
			ed := newFakeEditor(lines...)
			m := New(Deps{Editor: ed, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			l := m.layout()
			ed.doc.cursor = buffer.Pos{Line: l.bodyBottom - l.bodyTop}
			m.Update(expanel.ErrorMsg{Err: errors.New(longNotice)})
			l = m.layout()
			if len(l.notice) == 0 {
				t.Fatal("setup: no notice band")
			}
			_, y, ok := m.caret()
			if !ok {
				t.Fatal("no caret")
			}
			if y >= l.noticeY {
				t.Errorf("caret row %d is under the band (rows %d..%d)", y, l.noticeY, l.statusY-1)
			}
			if got := viewRow(m, y); !strings.Contains(got, fmt.Sprintf("line %d", ed.doc.cursor.Line)) {
				t.Errorf("caret row %d reads %q, want the caret line", y, got)
			}
		})
	}
}
