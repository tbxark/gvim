package decorations

import (
	"testing"
)

// TestTier1CWindow checks the new decoration kinds reach the per-line window with their
// fields, that a conceal range spanning lines reports per-line spans, and that ClearRange
// removes only what intersects the range.
func TestTier1CWindow(t *testing.T) {
	s := New()
	if _, err := s.Add(1, "p", "ns", Item{
		Kind: VirtualLineAbove, FromLine: 2, Text: "above",
		Style: Style{FG: Color{Key: "error"}}, GutterText: "-",
		TextOverlays:     [8]TextOverlay{{Start: 0, End: 3, Style: Style{Bold: true}}},
		TextOverlayCount: 1,
		Priority:         5,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(1, "p", "ns", Item{Kind: VirtualLineBelow, FromLine: 2, Text: "below"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(1, "p", "ns", Item{Kind: Conceal, FromLine: 1, FromCol: 2, ToLine: 3, ToCol: 4, Text: "…"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(1, "p", "ns", Item{Kind: SoftBreak, FromLine: 2, FromCol: 4, Indent: 2, PrefixText: ">"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(1, "p", "ns", Item{Kind: CursorLine, Style: Style{BG: Color{Set: true, R: 1}}}); err != nil {
		t.Fatal(err)
	}

	l := s.Line(1, 2)
	if len(l.Above) != 1 || l.Above[0].Text != "above" || l.Above[0].GutterText != "-" ||
		len(l.Above[0].Overlays) != 1 || !l.Above[0].Overlays[0].Style.Bold {
		t.Fatalf("above = %+v", l.Above)
	}
	if len(l.Below) != 1 || l.Below[0].Text != "below" {
		t.Fatalf("below = %+v", l.Below)
	}
	if len(l.SoftBreaks) != 1 || l.SoftBreaks[0].Indent != 2 || l.SoftBreaks[0].PrefixText != ">" {
		t.Fatalf("soft = %+v", l.SoftBreaks)
	}
	// The cursor-line overlay is anchored on line 0 (the caret line is dynamic), so the
	// pane reads it through the line-0 window.
	if c := s.Line(1, 0).CursorLine; len(c) != 1 || c[0].Style.BG.Set != true {
		t.Fatalf("cursor = %+v", c)
	}
	// The conceal range covers lines 1..3: line 1 starts at its from column, line 2 fully,
	// line 3 up to its to column.
	if c := s.Line(1, 1).Conceals; len(c) != 1 || c[0].Start != 2 || c[0].End != -1 {
		t.Fatalf("conceal line 1 = %+v", c)
	}
	if c := s.Line(1, 3).Conceals; len(c) != 1 || c[0].Start != 0 || c[0].End != 4 {
		t.Fatalf("conceal line 3 = %+v", c)
	}

	// Clear by range removes the conceal (it intersects) and the virtual rows on line 2,
	// and keeps the cursor-line overlay (it sits on line 0).
	s.ClearRange(1, "p", "ns", 1, 3)
	l = s.Line(1, 2)
	if len(l.Above) != 0 || len(l.Below) != 0 || len(l.Conceals) != 0 || len(l.SoftBreaks) != 0 {
		t.Fatalf("after clear = %+v", l)
	}
	if len(s.Line(1, 0).CursorLine) != 1 {
		t.Fatal("the cursor-line overlay was cleared with the range")
	}
}

// TestTier1CApplyMovesAnchors checks the new kinds follow the text: a virtual line moves with
// its line, a conceal range moves like a highlight, and a new line inserted above shifts both.
func TestTier1CApplyMovesAnchors(t *testing.T) {
	s := New()
	if _, err := s.Add(1, "p", "ns", Item{Kind: VirtualLineAbove, FromLine: 2, Text: "v"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(1, "p", "ns", Item{Kind: Conceal, FromLine: 1, FromCol: 0, ToLine: 1, ToCol: 3}); err != nil {
		t.Fatal(err)
	}
	s.Apply(1, []Edit{{FromLine: 0, FromCol: 0, ToLine: 0, ToCol: 0, Text: "x\n"}}, false)
	if got := s.Line(1, 3).Above; len(got) != 1 {
		t.Fatalf("virtual line did not move: %+v", got)
	}
	if got := s.Line(1, 2).Conceals; len(got) != 1 || got[0].Start != 0 || got[0].End != 3 {
		t.Fatalf("conceal did not move: %+v", got)
	}
}

// TestTier1CRemainingRegressions checks SoftBreak shifting across single and composed edits,
// CursorLine visibility in windows >= 64, multi-line Conceal ToCol==0 and ClearRange, and
// TextOverlayCount clamping.
func TestTier1CRemainingRegressions(t *testing.T) {
	t.Run("SoftBreak shifts on earlier-line and same-line edits", func(t *testing.T) {
		s := New()
		if _, err := s.Add(1, "p", "ns", Item{Kind: SoftBreak, FromLine: 2, FromCol: 5, Indent: 2}); err != nil {
			t.Fatal(err)
		}
		// Earlier-line edit inserts a newline: SoftBreak moves down and preserves FromCol.
		s.Apply(1, []Edit{{FromLine: 0, FromCol: 0, ToLine: 0, ToCol: 0, Text: "a\n"}}, false)
		if sb := s.Line(1, 3).SoftBreaks; len(sb) != 1 || sb[0].Col != 5 {
			t.Fatalf("after earlier-line insert = %+v, want Col=5 on line 3", sb)
		}
		// Same-line insert before FromCol shifts Col right.
		s.Apply(1, []Edit{{FromLine: 3, FromCol: 1, ToLine: 3, ToCol: 1, Text: "xyz"}}, false)
		if sb := s.Line(1, 3).SoftBreaks; len(sb) != 1 || sb[0].Col != 8 {
			t.Fatalf("after same-line insert = %+v, want Col=8", sb)
		}
		// Composed bulk edits also shift SoftBreak accurately.
		s.Queue(1, Edit{FromLine: 0, FromCol: 0, ToLine: 0, ToCol: 0, Text: "1\n"})
		s.Queue(1, Edit{FromLine: 4, FromCol: 2, ToLine: 4, ToCol: 2, Text: "AB"})
		if sb := s.Line(1, 4).SoftBreaks; len(sb) != 1 || sb[0].Col != 10 {
			t.Fatalf("after composed edits = %+v, want Col=10 on line 4", sb)
		}
		// Deleting across FromCol removes the SoftBreak.
		s.Apply(1, []Edit{{FromLine: 4, FromCol: 9, ToLine: 4, ToCol: 12, Text: ""}}, false)
		if sb := s.Line(1, 4).SoftBreaks; len(sb) != 0 {
			t.Fatalf("after delete across anchor = %+v, want removed", sb)
		}
	})

	t.Run("CursorLine works on lines >= 64 and survives composed edits", func(t *testing.T) {
		s := New()
		if _, err := s.Add(1, "p", "ns", Item{Kind: CursorLine, FromLine: 15, Style: Style{BG: Color{Set: true, R: 7}}}); err != nil {
			t.Fatal(err)
		}
		if c := s.Line(1, 0).CursorLine; len(c) != 1 || c[0].Style.BG.R != 7 {
			t.Fatalf("line 0 CursorLine = %+v", c)
		}
		if c := s.Line(1, 64).CursorLine; len(c) != 1 || c[0].Style.BG.R != 7 {
			t.Fatalf("line 64 CursorLine = %+v", c)
		}
		if c := s.Line(1, 130).CursorLine; len(c) != 1 || c[0].Style.BG.R != 7 {
			t.Fatalf("line 130 CursorLine = %+v", c)
		}
		// Composed edits deleting line 0 must not remove CursorLine.
		s.Queue(1, Edit{FromLine: 0, FromCol: 0, ToLine: 1, ToCol: 0, Text: ""})
		s.Queue(1, Edit{FromLine: 5, FromCol: 0, ToLine: 5, ToCol: 0, Text: "x\n"})
		if c := s.Line(1, 70).CursorLine; len(c) != 1 {
			t.Fatalf("after composed edits CursorLine = %+v, want kept", c)
		}
	})

	t.Run("Multi-line Conceal ToCol 0 and ClearRange", func(t *testing.T) {
		s := New()
		if _, err := s.Add(1, "p", "ns", Item{Kind: Conceal, FromLine: 2, FromCol: 1, ToLine: 4, ToCol: 0, Text: "…"}); err != nil {
			t.Fatal(err)
		}
		if c := s.Line(1, 2).Conceals; len(c) != 1 || c[0].Start != 1 || c[0].End != -1 {
			t.Fatalf("line 2 conceal = %+v", c)
		}
		if c := s.Line(1, 3).Conceals; len(c) != 1 || c[0].Start != 0 || c[0].End != -1 {
			t.Fatalf("line 3 conceal = %+v", c)
		}
		if c := s.Line(1, 4).Conceals; len(c) != 0 {
			t.Fatalf("line 4 conceal = %+v, want empty when ToCol == 0", c)
		}
		// ClearRange intersecting only the end of the multi-line Conceal removes it.
		s.ClearRange(1, "p", "ns", 4, 5)
		if c := s.Line(1, 2).Conceals; len(c) != 0 {
			t.Fatalf("after ClearRange(4,5) = %+v, want cleared", c)
		}
	})

	t.Run("TextOverlayCount clamped to [0, max]", func(t *testing.T) {
		s := New()
		if _, err := s.Add(1, "p", "ns", Item{Kind: VirtualLineAbove, FromLine: 1, Text: "a", TextOverlayCount: -3}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Add(1, "p", "ns", Item{Kind: VirtualLineBelow, FromLine: 1, Text: "b", TextOverlayCount: MaxTextOverlaysPerLine() + 10}); err != nil {
			t.Fatal(err)
		}
		ld := s.Line(1, 1)
		if len(ld.Above) != 1 || len(ld.Above[0].Overlays) != 0 {
			t.Fatalf("negative TextOverlayCount = %+v", ld.Above)
		}
		if len(ld.Below) != 1 || len(ld.Below[0].Overlays) != MaxTextOverlaysPerLine() {
			t.Fatalf("oversized TextOverlayCount = %d, want %d", len(ld.Below[0].Overlays), MaxTextOverlaysPerLine())
		}
	})
}
