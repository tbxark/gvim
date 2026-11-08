package plugin

import (
	"unicode/utf8"

	"github.com/tbxark/gvim/pluginapi"
)

// buildCompareResult assembles compare_buffers' answer from the two buffer reads and
// the line hunks between them. All offsets are absolute bytes of the respective
// buffer in gvim's byte model. hunk sides never overlap and are in order, which the
// side-by-side painter relies on.
func buildCompareResult(left, right pluginapi.DiffSource, hunks []pluginapi.Hunk) pluginapi.CompareResult {
	res := pluginapi.CompareResult{
		Hunks:        make([]pluginapi.CompareHunk, 0, len(hunks)),
		LeftLines:    len(left.Lines),
		RightLines:   len(right.Lines),
		LeftLen:      left.Len,
		RightLen:     right.Len,
		LeftVersion:  left.Version,
		RightVersion: right.Version,
	}
	for _, h := range hunks {
		l := compareSide(left, h.OldStart, h.OldCount)
		r := compareSide(right, h.NewStart, h.NewCount)
		compareSpans(left, right, &l, &r)
		res.Hunks = append(res.Hunks, pluginapi.CompareHunk{Left: l, Right: r})
	}
	return res
}

// compareSide measures one side of a hunk: its lines, their byte ranges and the byte
// range the whole hunk replaces. A side with no lines sits between lines: from and to
// are the gap, and jump points at the line before it so a view's filler rows for the
// other side hang under that line.
func compareSide(src pluginapi.DiffSource, start, count int) pluginapi.CompareSide {
	start = min(max(start, 0), len(src.Lines))
	count = min(max(count, 0), len(src.Lines)-start)
	side := pluginapi.CompareSide{Start: start, Count: count}
	side.From = lineByteStart(src, start)
	side.To = lineByteStart(src, start+count)
	side.Jump = side.From
	if count == 0 && start > 0 {
		side.Jump = lineByteStart(src, start-1)
	}
	side.Lines = make([][2]int, count)
	for i := range count {
		line := start + i
		at := lineByteStart(src, line)
		side.Lines[i] = [2]int{at, at + len(src.Lines[line])}
	}
	return side
}

// lineByteStart is the byte offset of a line's first byte. len(Lines) is the end of
// the text.
func lineByteStart(src pluginapi.DiffSource, line int) int {
	if line >= len(src.Lines) {
		return src.Len
	}
	if line < 0 {
		return 0
	}
	return src.Starts[line]
}

// compareSpans fills the differing span of every line pair in a hunk: the lines are
// paired positionally (Fresh's fallback pairing), the shared prefix and suffix are
// skipped, and the span is snapped to UTF-8 boundaries so a painted highlight never
// cuts a character.
func compareSpans(left, right pluginapi.DiffSource, l, r *pluginapi.CompareSide) {
	pairs := min(l.Count, r.Count)
	l.Spans = make([]pluginapi.CompareSpan, pairs)
	r.Spans = make([]pluginapi.CompareSpan, pairs)
	for i := range pairs {
		a, b := left.Lines[l.Start+i], right.Lines[r.Start+i]
		start, aEnd, bEnd := differSpan(a, b)
		if start >= aEnd && start >= bEnd {
			continue // the same text: no span
		}
		aBase, bBase := l.Lines[i][0], r.Lines[i][0]
		if aEnd > start {
			l.Spans[i] = pluginapi.CompareSpan{Start: aBase + start, End: aBase + aEnd, Set: true}
		}
		if bEnd > start {
			r.Spans[i] = pluginapi.CompareSpan{Start: bBase + start, End: bBase + bEnd, Set: true}
		}
	}
}

// differSpan returns the common byte start and the two differing ends of a pair of
// lines: [start, aEnd) of a and [start, bEnd) of b differ, with start, aEnd and bEnd
// on UTF-8 boundaries of their line.
func differSpan(a, b string) (start, aEnd, bEnd int) {
	common := min(len(a), len(b))
	prefix := 0
	for prefix < common && a[prefix] == b[prefix] {
		prefix++
	}
	for prefix > 0 && (!runeBoundary(a, prefix) || !runeBoundary(b, prefix)) {
		prefix--
	}
	suffix := 0
	for suffix < common-prefix && a[len(a)-1-suffix] == b[len(b)-1-suffix] {
		suffix++
	}
	for suffix > 0 && (!runeBoundary(a, len(a)-suffix) || !runeBoundary(b, len(b)-suffix)) {
		suffix--
	}
	return prefix, len(a) - suffix, len(b) - suffix
}

// runeBoundary reports whether at is the start of a rune in s (the end counts).
func runeBoundary(s string, at int) bool {
	return at <= 0 || at >= len(s) || utf8.RuneStart(s[at])
}
