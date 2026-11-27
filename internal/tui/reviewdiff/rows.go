package reviewdiff

import (
	"fmt"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"

	"github.com/tbxark/gvim/diff"
	"github.com/tbxark/gvim/internal/tui/textsafe"
	"github.com/tbxark/gvim/syntax"
)

// UnifiedRow is one logical row of the Unified diff view.
type UnifiedRow struct {
	Kind        RowKind
	HunkIdx     int // 0-based diff.Hunk index, -1 outside hunks
	GapID       int // expandable gap identifier on RowHunkHeader, -1 when not expandable
	HiddenCount int // hidden lines remaining in GapID
	OldLine     int // 1-based line number on the old side, 0 if absent
	NewLine     int // 1-based line number on the new side, 0 if absent
	EditLine    int // 1-based working-tree target line for Enter-to-edit (0 if deleted file)
	Text        string
	Spans       []syntax.Span
	Highlights  []ByteRange
}

// SplitCell is one half (left or right) of a Side-by-Side diff row.
type SplitCell struct {
	Kind       RowKind
	Line       int // 1-based line number, 0 for filler/header/info
	Text       string
	Spans      []syntax.Span
	Highlights []ByteRange
}

// SplitRow is one logical row of the Side-by-Side diff view.
type SplitRow struct {
	Kind        RowKind
	HunkIdx     int
	GapID       int
	HiddenCount int
	HeaderText  string // populated for RowHunkHeader and RowInfo
	EditLine    int
	Left        SplitCell
	Right       SplitCell
}

// visualLine maps one wrapped screen line in Unified mode back to a slice of a
// logical UnifiedRow.
type visualLine struct {
	rowIdx    int
	startByte int
	endByte   int
	cont      bool
}

type hunkGroup struct {
	firstHunk int
	lastHunk  int
	oldStart  int
	oldEnd    int
	newStart  int
	newEnd    int
}

// lexSide runs Chroma once over lines for pathName so multi-line constructs
// (block comments, raw strings) highlight accurately on both diff sides.
func lexSide(pathName string, lines []string) [][]syntax.Span {
	if len(lines) == 0 {
		return nil
	}
	var head strings.Builder
	for i := 0; i < len(lines) && head.Len() < 4096; i++ {
		if i > 0 {
			head.WriteByte('\n')
		}
		head.WriteString(lines[i])
	}
	lexer := syntax.Detect(path.Base(pathName), []byte(head.String()))
	return syntax.NewHighlighter(lexer).Lines(1, lines)
}

// sanitizeLines cleans untrusted lines once so syntax spans, word-diff byte
// ranges, and search offsets align identically with the painted text.
func sanitizeLines(lines []string) []string {
	if len(lines) == 0 {
		return nil
	}
	out := make([]string, len(lines))
	for i, s := range lines {
		out[i] = textsafe.Clean(s)
	}
	return out
}

func spanAt(spans [][]syntax.Span, idx int) []syntax.Span {
	if idx < 0 || idx >= len(spans) {
		return nil
	}
	return spans[idx]
}

// buildRows constructs both Unified and Split row tables and hunk-jump indices
// from d, its cached syntax spans, fullFile mode, and per-gap expansion counts.
func buildRows(
	d *FileDiff,
	oldLines, newLines []string,
	oldSpans, newSpans [][]syntax.Span,
	fullFile bool,
	gaps map[int]int,
) (unified []UnifiedRow, split []SplitRow, uHunkRows []int, sHunkRows []int) {
	if d == nil {
		return nil, nil, nil, nil
	}
	if d.Binary {
		msg := d.Message
		if msg == "" {
			msg = "Binary file changed"
		}
		return []UnifiedRow{{Kind: RowInfo, HunkIdx: -1, GapID: -1, Text: msg}},
			[]SplitRow{{Kind: RowInfo, HunkIdx: -1, GapID: -1, HeaderText: msg}},
			nil, nil
	}
	if len(d.Hunks) == 0 {
		msg := d.Message
		if msg == "" {
			if len(oldLines) == 0 && len(newLines) == 0 {
				msg = "Empty file"
			} else if !fullFile {
				msg = "No text differences"
			}
		}
		if !fullFile || (len(oldLines) == 0 && len(newLines) == 0) {
			return []UnifiedRow{{Kind: RowInfo, HunkIdx: -1, GapID: -1, Text: msg}},
				[]SplitRow{{Kind: RowInfo, HunkIdx: -1, GapID: -1, HeaderText: msg}},
				nil, nil
		}
	}

	canEdit := d.Status != StatusDeleted && len(newLines) > 0
	editLineFor := func(newZero int) int {
		if !canEdit {
			return 0
		}
		if newZero < 0 {
			return 1
		}
		if newZero >= len(newLines) {
			return len(newLines)
		}
		return newZero + 1
	}

	appendContext := func(oldFrom, newFrom, count int) {
		for k := range count {
			oi, ni := oldFrom+k, newFrom+k
			if oi < 0 || oi >= len(oldLines) || ni < 0 || ni >= len(newLines) {
				continue
			}
			line := newLines[ni]
			sp := spanAt(newSpans, ni)
			el := editLineFor(ni)
			unified = append(unified, UnifiedRow{
				Kind:     RowContext,
				HunkIdx:  -1,
				GapID:    -1,
				OldLine:  oi + 1,
				NewLine:  ni + 1,
				EditLine: el,
				Text:     line,
				Spans:    sp,
			})
			split = append(split, SplitRow{
				Kind:     RowContext,
				HunkIdx:  -1,
				GapID:    -1,
				EditLine: el,
				Left:     SplitCell{Kind: RowContext, Line: oi + 1, Text: oldLines[oi], Spans: spanAt(oldSpans, oi)},
				Right:    SplitCell{Kind: RowContext, Line: ni + 1, Text: line, Spans: sp},
			})
		}
	}

	appendHunk := func(hi int, h diff.Hunk) {
		uHunkRows = append(uHunkRows, len(unified))
		sHunkRows = append(sHunkRows, len(split))

		paired := 0
		if h.Kind == diff.Change {
			paired = min(h.OldLines, h.NewLines)
		}
		var oldHi, newHi [][]ByteRange
		if paired > 0 {
			oldHi = make([][]ByteRange, paired)
			newHi = make([][]ByteRange, paired)
			for k := range paired {
				oldHi[k], newHi[k] = WordDiffRanges(oldLines[h.OldStart+k], newLines[h.NewStart+k])
			}
		}

		// Unified rows: all deletes of the hunk, then all inserts.
		delEditLine := editLineFor(h.NewStart)
		for k := range h.OldLines {
			oi := h.OldStart + k
			var hl []ByteRange
			if k < paired {
				hl = oldHi[k]
			}
			unified = append(unified, UnifiedRow{
				Kind:       RowDelete,
				HunkIdx:    hi,
				GapID:      -1,
				OldLine:    oi + 1,
				EditLine:   delEditLine,
				Text:       oldLines[oi],
				Spans:      spanAt(oldSpans, oi),
				Highlights: hl,
			})
		}
		for k := range h.NewLines {
			ni := h.NewStart + k
			var hl []ByteRange
			if k < paired {
				hl = newHi[k]
			}
			unified = append(unified, UnifiedRow{
				Kind:       RowAdd,
				HunkIdx:    hi,
				GapID:      -1,
				NewLine:    ni + 1,
				EditLine:   editLineFor(ni),
				Text:       newLines[ni],
				Spans:      spanAt(newSpans, ni),
				Highlights: hl,
			})
		}

		// Split rows: pair up modified lines side by side, pad shorter side with fillers.
		rows := max(h.OldLines, h.NewLines)
		for k := range rows {
			hasOld := k < h.OldLines
			hasNew := k < h.NewLines
			var sr SplitRow
			sr.HunkIdx = hi
			sr.GapID = -1
			switch {
			case hasOld && hasNew:
				oi, ni := h.OldStart+k, h.NewStart+k
				sr.Kind = RowModify
				sr.EditLine = editLineFor(ni)
				sr.Left = SplitCell{Kind: RowDelete, Line: oi + 1, Text: oldLines[oi], Spans: spanAt(oldSpans, oi), Highlights: oldHi[k]}
				sr.Right = SplitCell{Kind: RowAdd, Line: ni + 1, Text: newLines[ni], Spans: spanAt(newSpans, ni), Highlights: newHi[k]}
			case hasOld:
				oi := h.OldStart + k
				sr.Kind = RowDelete
				sr.EditLine = delEditLine
				sr.Left = SplitCell{Kind: RowDelete, Line: oi + 1, Text: oldLines[oi], Spans: spanAt(oldSpans, oi)}
				sr.Right = SplitCell{Kind: RowFiller}
			default:
				ni := h.NewStart + k
				sr.Kind = RowAdd
				sr.EditLine = editLineFor(ni)
				sr.Left = SplitCell{Kind: RowFiller}
				sr.Right = SplitCell{Kind: RowAdd, Line: ni + 1, Text: newLines[ni], Spans: spanAt(newSpans, ni)}
			}
			split = append(split, sr)
		}
	}

	if d.Truncated && d.Message != "" {
		unified = append(unified, UnifiedRow{Kind: RowInfo, HunkIdx: -1, GapID: -1, Text: d.Message})
		split = append(split, SplitRow{Kind: RowInfo, HunkIdx: -1, GapID: -1, HeaderText: d.Message})
	}

	if fullFile {
		oi, ni := 0, 0
		for hi, h := range d.Hunks {
			ctx := min(h.OldStart-oi, h.NewStart-ni)
			if ctx > 0 {
				appendContext(oi, ni, ctx)
			}
			appendHunk(hi, h)
			oi = h.OldStart + h.OldLines
			ni = h.NewStart + h.NewLines
		}
		tail := min(len(oldLines)-oi, len(newLines)-ni)
		if tail > 0 {
			appendContext(oi, ni, tail)
		}
		return unified, split, uHunkRows, sHunkRows
	}

	// Group hunks within 2*DefaultContextLines of each other.
	groups := groupHunks(d.Hunks, len(oldLines), len(newLines), DefaultContextLines)
	appendHeader := func(gapID, hidden, oldStart, oldCount, newStart, newCount int, trailing bool) {
		var hdr string
		if trailing {
			hdr = fmt.Sprintf("↓ %d hidden lines at end of file", hidden)
		} else {
			oDisp := oldStart + 1
			if oldCount == 0 {
				oDisp = oldStart
			}
			nDisp := newStart + 1
			if newCount == 0 {
				nDisp = newStart
			}
			hdr = fmt.Sprintf("@@ -%d,%d +%d,%d @@", oDisp, oldCount, nDisp, newCount)
			if hidden > 0 {
				hdr += fmt.Sprintf("  ↕ %d hidden lines", hidden)
			}
		}
		if gaps[gapID] > 0 {
			if trailing && hidden == 0 {
				hdr = "End of file context"
			}
			hdr += "  (Enter/click to collapse)"
		} else if gapID >= 0 {
			hdr += "  (Enter/click to expand)"
		}
		unified = append(unified, UnifiedRow{
			Kind:        RowHunkHeader,
			HunkIdx:     -1,
			GapID:       gapID,
			HiddenCount: hidden,
			Text:        hdr,
		})
		split = append(split, SplitRow{
			Kind:        RowHunkHeader,
			HunkIdx:     -1,
			GapID:       gapID,
			HiddenCount: hidden,
			HeaderText:  hdr,
		})
	}

	for gi, g := range groups {
		var gapOldStart, gapNewStart int
		if gi > 0 {
			gapOldStart = groups[gi-1].oldEnd
			gapNewStart = groups[gi-1].newEnd
		}
		gapLen := min(g.oldStart-gapOldStart, g.newStart-gapNewStart)
		if gapLen < 0 {
			gapLen = 0
		}
		exp := 0
		if gaps != nil {
			exp = gaps[gi]
		}
		if exp > gapLen {
			exp = gapLen
		}
		// Compute how many lines of this gap are revealed at the top (after prev
		// group) vs bottom (before this group).
		var topExp, botExp int
		if gi == 0 {
			botExp = exp
		} else {
			topExp = exp / 2
			botExp = exp - topExp
		}
		hidden := gapLen - topExp - botExp

		if topExp > 0 {
			appendContext(gapOldStart, gapNewStart, topExp)
		}

		dispOldStart := g.oldStart - botExp
		dispNewStart := g.newStart - botExp
		dispOldCount := g.oldEnd - dispOldStart
		dispNewCount := g.newEnd - dispNewStart

		if gi == 0 || gapLen > 0 {
			gapID := -1
			if gapLen > 0 {
				gapID = gi
			}
			appendHeader(gapID, hidden, dispOldStart, dispOldCount, dispNewStart, dispNewCount, false)
		}
		if botExp > 0 {
			appendContext(g.oldStart-botExp, g.newStart-botExp, botExp)
		}

		oi, ni := g.oldStart, g.newStart
		for hi := g.firstHunk; hi <= g.lastHunk; hi++ {
			h := d.Hunks[hi]
			ctx := min(h.OldStart-oi, h.NewStart-ni)
			if ctx > 0 {
				appendContext(oi, ni, ctx)
			}
			appendHunk(hi, h)
			oi = h.OldStart + h.OldLines
			ni = h.NewStart + h.NewLines
		}
		post := min(g.oldEnd-oi, g.newEnd-ni)
		if post > 0 {
			appendContext(oi, ni, post)
		}
	}

	// Trailing hidden lines after the last hunk group.
	if len(groups) > 0 {
		last := groups[len(groups)-1]
		trailID := len(groups)
		trailLen := min(len(oldLines)-last.oldEnd, len(newLines)-last.newEnd)
		if trailLen > 0 {
			exp := 0
			if gaps != nil {
				exp = min(gaps[trailID], trailLen)
			}
			if exp > 0 {
				appendContext(last.oldEnd, last.newEnd, exp)
			}
			appendHeader(trailID, trailLen-exp, 0, 0, 0, 0, true)
		}
	}

	return unified, split, uHunkRows, sHunkRows
}

func groupHunks(hunks []diff.Hunk, oldLen, newLen, ctx int) []hunkGroup {
	if len(hunks) == 0 {
		return nil
	}
	var groups []hunkGroup
	for i, h := range hunks {
		os := max(0, h.OldStart-ctx)
		oe := min(oldLen, h.OldStart+h.OldLines+ctx)
		ns := max(0, h.NewStart-ctx)
		ne := min(newLen, h.NewStart+h.NewLines+ctx)
		if len(groups) > 0 {
			prev := &groups[len(groups)-1]
			if os <= prev.oldEnd && ns <= prev.newEnd {
				prev.lastHunk = i
				prev.oldEnd = max(prev.oldEnd, oe)
				prev.newEnd = max(prev.newEnd, ne)
				continue
			}
		}
		groups = append(groups, hunkGroup{
			firstHunk: i,
			lastHunk:  i,
			oldStart:  os,
			oldEnd:    oe,
			newStart:  ns,
			newEnd:    ne,
		})
	}
	return groups
}

// buildVisualLines computes the wrapped visual-line table for Unified mode.
// When wrap is false or textW <= 0, each logical row maps 1:1.
func buildVisualLines(rows []UnifiedRow, wrap bool, textW, tabSize int) []visualLine {
	if len(rows) == 0 {
		return nil
	}
	if !wrap || textW <= 0 {
		out := make([]visualLine, len(rows))
		for i, r := range rows {
			out[i] = visualLine{rowIdx: i, startByte: 0, endByte: len(r.Text)}
		}
		return out
	}
	if tabSize <= 0 {
		tabSize = 4
	}
	out := make([]visualLine, 0, len(rows))
	for i, r := range rows {
		if r.Kind == RowHunkHeader || r.Kind == RowInfo || len(r.Text) == 0 {
			out = append(out, visualLine{rowIdx: i, startByte: 0, endByte: len(r.Text)})
			continue
		}
		segs := wrapLineBytes(r.Text, textW, tabSize)
		for si, seg := range segs {
			out = append(out, visualLine{
				rowIdx:    i,
				startByte: seg.Start,
				endByte:   seg.End,
				cont:      si > 0,
			})
		}
	}
	return out
}

// wrapLineBytes splits s into byte ranges whose expanded display width fits in
// width cells, breaking only on grapheme-cluster boundaries.
func wrapLineBytes(s string, width, tabSize int) []ByteRange {
	if width <= 0 || s == "" {
		return []ByteRange{{Start: 0, End: len(s)}}
	}
	if tabSize <= 0 {
		tabSize = 4
	}
	var out []ByteRange
	segStart := 0
	col := 0
	bytePos := 0
	rest := s
	state := -1
	for len(rest) > 0 {
		cluster, next, _, nextState := uniseg.FirstGraphemeClusterInString(rest, state)
		cw := clusterWidth(cluster, col, tabSize)
		if col > 0 && col+cw > width {
			out = append(out, ByteRange{Start: segStart, End: bytePos})
			segStart = bytePos
			col = 0
			cw = clusterWidth(cluster, col, tabSize)
		}
		col += cw
		bytePos += len(cluster)
		rest = next
		state = nextState
	}
	out = append(out, ByteRange{Start: segStart, End: len(s)})
	return out
}

func clusterWidth(cluster string, col, tabSize int) int {
	if cluster == "\t" {
		rem := tabSize - (col % tabSize)
		if rem <= 0 {
			return tabSize
		}
		return rem
	}
	if len(cluster) == 1 && cluster[0] >= 0x20 && cluster[0] < 0x7f {
		return 1
	}
	w := ansi.StringWidth(cluster)
	if w == 0 && len(cluster) > 0 {
		// Control/zero-width clusters that survived cleaning still advance safely.
		if r, _ := utf8.DecodeRuneInString(cluster); r == '\t' {
			return tabSize - (col % tabSize)
		}
	}
	return w
}
