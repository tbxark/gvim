package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/ui/space"
)

// noticeInput is a layoutInput whose status row carries message.
func noticeInput(message string) layoutInput {
	return layoutInput{status: statusView{position: "Ln 1, Col 1", message: message, eol: "LF", encoding: "UTF-8", language: "Go", lines: 10}}
}

const longNotice = "save /private/tmp/gvim-dogfood/writer/w/se_e.txt: encoding: cannot represent U+4E2D in Windows-1252, choose another encoding"

func TestAShortMessageStaysOnTheStatusRow(t *testing.T) {
	t.Parallel()
	l := computeLayout(100, 30, noticeInput("Saved"))
	if len(l.notice) != 0 {
		t.Errorf("notice %q: want no band", l.notice)
	}
}

func TestALongMessageWrapsAboveTheStatusRow(t *testing.T) {
	t.Parallel()
	for _, w := range []int{60, 80, 100, 120, 200} {
		l := computeLayout(w, 30, noticeInput(longNotice))
		if len(l.notice) == 0 {
			// The message fits the status row at this width: nothing to wrap.
			if w < 200 {
				t.Errorf("width %d: a %d-cell message was not wrapped", w, len(longNotice))
			}
			continue
		}
		// The band is painted over the bottom body rows: the body keeps its size.
		if l.noticeY != l.statusY-len(l.notice) || l.bodyBottom != l.statusY-1 {
			t.Errorf("width %d: noticeY %d, statusY %d, bodyBottom %d, %d rows", w, l.noticeY, l.statusY, l.bodyBottom, len(l.notice))
		}
		joined := strings.Join(l.notice, " ")
		for _, word := range []string{"se_e.txt:", "encoding:", "another"} {
			if !strings.Contains(joined, word) {
				t.Errorf("width %d: wrapped notice %q lost %q", w, joined, word)
			}
		}
		for _, row := range l.notice {
			if ansi.StringWidth(row) > w-2 {
				t.Errorf("width %d: row %q is wider than the row less its padding", w, row)
			}
		}
		// The status row no longer repeats the message.
		var text strings.Builder
		for _, r := range l.status {
			text.WriteString(r.text)
		}
		if strings.Contains(text.String(), "cannot represent") {
			t.Errorf("width %d: the status row still holds the message: %q", w, text.String())
		}
		// A click on a band row reaches nothing.
		if got := l.hit(5, l.noticeY); got.Kind != hitNone {
			t.Errorf("width %d: a click on the notice band hit %v", w, got.Kind)
		}
	}
}

// A message too long for the band loses its middle, so the head and the reason at the end
// both stay. The band still holds at most maxNoticeRows rows.
func TestAVeryLongMessageKeepsItsEndAndCutsTheMiddle(t *testing.T) {
	t.Parallel()
	msg := "head words " + strings.Repeat("filler words ", 60) + "the final reason"
	l := computeLayout(80, 30, noticeInput(msg))
	if len(l.notice) != maxNoticeRows {
		t.Fatalf("band has %d rows, want %d", len(l.notice), maxNoticeRows)
	}
	joined := strings.Join(l.notice, " ")
	if !strings.Contains(joined, "…") {
		t.Errorf("band %q has no ellipsis where the middle was cut", joined)
	}
	if !strings.Contains(joined, "head words") || !strings.HasSuffix(joined, "the final reason") {
		t.Errorf("band %q lost the head or the final reason", joined)
	}
}

// At 60x20 the message is longer than the three band rows. The reason is the part a user
// needs, so it must be on screen, not only in Show Warnings.
func TestTheReasonSurvivesANarrowBand(t *testing.T) {
	t.Parallel()
	msg := "save /tmp/project/readonly-file-with-a-rather-long-name-to-see-the-message.txt: open /tmp/project/.readonly-file-with-a-rather-long-name-to-see-the-message.txt.gvim-4062212207: permission denied"
	for _, size := range [][2]int{{60, 20}, {40, 20}, {80, 24}} {
		l := computeLayout(size[0], size[1], noticeInput(msg))
		if len(l.notice) == 0 {
			t.Fatalf("%dx%d: no band for a long message", size[0], size[1])
		}
		if joined := strings.Join(l.notice, " "); !strings.Contains(joined, "permission denied") {
			t.Errorf("%dx%d: band %q lost the reason", size[0], size[1], joined)
		}
		for _, row := range l.notice {
			if ansi.StringWidth(row) > size[0]-2*space.PadX {
				t.Errorf("%dx%d: row %q is wider than the band", size[0], size[1], row)
			}
		}
	}
}

func TestNoBandWhileTheStatusRowIsHiddenOrTheFrameIsTiny(t *testing.T) {
	t.Parallel()
	in := noticeInput(longNotice)
	in.hideStatus = true
	if l := computeLayout(80, 24, in); len(l.notice) != 0 {
		t.Errorf("a hidden status row got a band: %q", l.notice)
	}
	for h := gateMinHeight; h <= 10; h++ {
		for w := gateMinWidth; w <= 30; w += 2 {
			l := computeLayout(w, h, noticeInput(longNotice))
			if l.gated {
				continue
			}
			if len(l.notice) > 0 && l.noticeY <= l.bodyTop {
				t.Errorf("%dx%d: the band at row %d covers the first body row %d", w, h, l.noticeY, l.bodyTop)
			}
		}
	}
}

// With the explorer open the band starts at the editor's left edge, so the explorer keeps
// its border and its last rows. Its text fits the editor columns.
func TestTheBandStaysBesideTheExplorer(t *testing.T) {
	t.Parallel()
	in := noticeInput(longNotice)
	in.explorerOpen = true
	l := computeLayout(80, 24, in)
	if len(l.notice) == 0 {
		t.Fatal("a long notice got no band at 80x24")
	}
	if l.ex == 0 || l.noticeX != l.ex {
		t.Fatalf("band starts at column %d, want the editor edge %d", l.noticeX, l.ex)
	}
	if l.onNotice(l.ex-1, l.noticeY) {
		t.Errorf("the band covers the explorer border column %d", l.ex-1)
	}
	if !l.onNotice(l.ex, l.noticeY) {
		t.Errorf("the band does not take the first editor column %d", l.ex)
	}
	for _, row := range l.notice {
		if ansi.StringWidth(row) > l.w-l.ex-2*space.PadX {
			t.Errorf("row %q is wider than the editor columns", row)
		}
	}
}
