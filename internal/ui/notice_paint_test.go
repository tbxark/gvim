package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/theme"
)

func noticeModel(t *testing.T, w, h int, notice string) *Model {
	t.Helper()
	m := New(Deps{Editor: newFakeEditor("hello"), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock, Notice: notice})
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m
}

func TestALongNoticeIsPaintedOnTheBandAndGoesWithTheNextKey(t *testing.T) {
	t.Parallel()
	m := noticeModel(t, 100, 30, longNotice)
	l := m.layout()
	if len(l.notice) == 0 {
		t.Fatalf("a %d-cell notice did not get a band at 100 columns", len(longNotice))
	}
	rows := screenRows(m)
	var band []string
	for y := l.noticeY; y < l.statusY; y++ {
		band = append(band, strings.TrimSpace(rows[y]))
	}
	if got := strings.Join(band, " "); !strings.Contains(got, "cannot represent U+4E2D") || !strings.Contains(got, "save /private/tmp") {
		t.Errorf("the band reads %q, want the whole notice", got)
	}
	// The row text is padded from the edge.
	if !strings.HasPrefix(rows[l.noticeY], " save") {
		t.Errorf("band row %q: want one padding cell before the text", rows[l.noticeY])
	}
	if strings.Contains(rows[l.statusY], "cannot represent") {
		t.Errorf("the status row repeats the notice: %q", rows[l.statusY])
	}
	press(m, typed("x"))
	if l := m.layout(); len(l.notice) != 0 {
		t.Errorf("the band is still there after a key: %q", l.notice)
	}
}

func TestNoticeBandAtTinySizesDoesNotPanic(t *testing.T) {
	t.Parallel()
	for h := 1; h <= 14; h++ {
		for w := 1; w <= 50; w++ {
			t.Run(fmt.Sprintf("%dx%d", w, h), func(t *testing.T) {
				m := noticeModel(t, w, h, longNotice)
				_ = m.View()
				m.Update(click(w/2, h-1, tea.MouseLeft))
			})
		}
	}
}
