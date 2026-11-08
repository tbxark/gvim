package buffer

import (
	"strings"
	"unicode/utf8"
)

// This file holds the methods the rest of the editor can call on any Buffer,
// whether it is a memory buffer or (in later stages) a large file buffer. A
// memory buffer answers all of them from its lines, with no behaviour change.

// IndexInfo describes how much of a document's line index exists.
type IndexInfo struct {
	// Size is the document's size in bytes and Indexed the prefix of it that
	// has been scanned. A memory buffer leaves both zero: it has no source.
	Size, Indexed int64
	// Lines equals Lines().
	Lines int
	// ApproxLines is Lines() when Complete, else an estimate of the final count.
	ApproxLines int
	// Complete reports whether every line is known.
	Complete bool
	// HugeLine reports that a line longer than the large-mode line cap exists.
	HugeLine bool
}

// Stats counts the work a buffer has done, so tests can assert bounded
// reads instead of timing. A memory buffer reports zeros.
type Stats struct {
	Large                      bool
	Pieces                     int
	ReadBytes, CacheBytes      int64 // bytes read from the source; bytes in pages
	MaterializedBytes          int64 // bytes returned by Text and Range
	OverCap                    int   // Text and Range calls over MaxMaterialize
	DecodedRunes               int64 // runes decoded from source bytes
	LineCalls                  int64 // base lines read from the source or its page cache
	OverlayBytes, HistoryBytes int64
	IndexedBytes, TotalBytes   int64
	HistoryGroups              int
}

// Version returns the version number of the current content. It changes with
// every edit, Undo and Redo, and returns to an earlier value when Undo
// restores earlier content.
func (b *Buffer) Version() uint64 {
	return b.ver
}

// Empty reports whether the document has no text.
func (b *Buffer) Empty() bool {
	if b.big != nil {
		return b.big.empty()
	}
	return len(b.lines) == 1 && len(b.lines[0]) == 0
}

// TextPrefix returns the first maxBytes bytes of Text, cut back to a whole
// rune. It never builds more than it returns.
func (b *Buffer) TextPrefix(maxBytes int) string {
	if b.big != nil {
		return b.big.textPrefix(maxBytes)
	}
	var sb strings.Builder
	var tmp [utf8.UTFMax]byte
	for i, l := range b.lines {
		if i > 0 {
			if sb.Len()+1 > maxBytes {
				break
			}
			sb.WriteByte('\n')
		}
		for _, r := range l {
			n := utf8.EncodeRune(tmp[:], r)
			if sb.Len()+n > maxBytes {
				return sb.String()
			}
			sb.Write(tmp[:n])
		}
	}
	return sb.String()
}

// TextChecked is Text with an error result. A memory buffer never fails. A
// large buffer returns ErrOverCap, and no text, when the document is over
// MaxMaterialize, or the source error when it cannot be read. Unlike Text it
// does not record a refusal.
func (b *Buffer) TextChecked() (string, error) {
	if b.big != nil {
		return b.big.text()
	}
	return b.Text(), nil
}

// RangeChecked is Range with an error result. A memory buffer never fails.
// See TextChecked for a large buffer.
func (b *Buffer) RangeChecked(from, to Pos) (string, error) {
	if b.big != nil {
		return b.big.rangeText(from, to)
	}
	return b.Range(from, to), nil
}

// LineBytes returns the UTF-8 length of line i without its newline.
// Out-of-range indexes are clamped. A large buffer answers from its index
// without reading the line, so an invalid UTF-8 byte counts as one byte and a
// line cut at MaxLine counts as MaxLine.
func (b *Buffer) LineBytes(i int) int {
	if b.big != nil {
		return b.big.lineBytesLen(i)
	}
	return runesLen(b.lineRunes(i))
}

// LineCut reports whether line i is shown cut by Line: a large buffer's line that is
// longer than the line cap, or the still unterminated first line of a source
// whose index has not found a line break within the cap. Line gives only a prefix of
// such a line, so copying a line through it, or re-inserting its text, would lose the
// rest. Range and LineSlice read a line over the cap to its end.
// A memory buffer never cuts a line. Out-of-range indexes are clamped.
func (b *Buffer) LineCut(i int) bool {
	return b.big != nil && b.big.lineCut(i)
}

// LineUnfinished reports whether line i is shown cut because the index of a large buffer has not
// found its end yet. A line longer than the line cap is cut for Line, but is read to its end by
// Range and LineSlice, so it is not unfinished. A memory buffer has no such line. Out-of-range
// indexes are clamped.
func (b *Buffer) LineUnfinished(i int) bool {
	return b.big != nil && b.big.lineUnfinished(i)
}

// LineSlice returns runes [col, col+n) of line i, clamped to the line.
// Out-of-range indexes are clamped.
func (b *Buffer) LineSlice(i, col, n int) string {
	if b.big != nil {
		return b.big.lineSlice(i, col, n)
	}
	line := b.lineRunes(i)
	col = min(max(col, 0), len(line))
	end := len(line)
	if n < end-col {
		end = col + max(n, 0)
	}
	return string(line[col:end])
}

// Refusal returns and clears the reason the last read or edit was refused. A
// memory buffer never refuses.
func (b *Buffer) Refusal() error {
	if b.big == nil {
		return nil
	}
	err := b.big.refusal
	b.big.refusal = nil
	return err
}

// LineDirty reports whether line i is a modified line that has not been
// saved. Only large buffers track it; a memory buffer returns false.
func (b *Buffer) LineDirty(i int) bool {
	return b.big != nil && b.Modified() && b.big.lineDirty(i)
}

// SourceErr returns the sticky error of a large buffer's file source.
func (b *Buffer) SourceErr() error {
	if b.big == nil {
		return nil
	}
	return b.big.err
}

// SourceGrew reports whether a large buffer's file grew since it was opened.
func (b *Buffer) SourceGrew() bool { return b.big != nil && b.big.grew }

// Close releases the resources behind the buffer. It is a no-op for a memory
// buffer and safe to call more than once.
func (b *Buffer) Close() error {
	if b.big != nil {
		b.big.close()
	}
	return nil
}

// LinesBytesOver reports whether the text of lines i..j (inclusive, joined by newlines) is
// certainly longer than limit bytes, and a number of bytes that it is at least. It answers
// from the line index and the edit overlay alone and reads no line, in time that does not
// depend on the number of lines, so a caller can refuse a request that is too large
// before it does the work of finding out. A false answer says nothing: the text may be
// longer all the same. A memory buffer, which a caller measures line by line at no
// cost, and a large buffer that holds a line cut at the line cap, whose length the index
// does not know, always answer false.
func (b *Buffer) LinesBytesOver(i, j int, limit int64) (n int64, over bool) {
	s := b.big
	if s == nil || s.hugeSeen() || i < 0 || j >= s.lines() || i > j {
		return 0, false
	}
	// The estimate counts a CRLF as two bytes, but the text joins lines with one newline:
	// a byte per terminator comes off so it stays a lower bound.
	joins := int64(j - i)
	est, err := s.spanBytes(i, j, limit+joins)
	if err != nil {
		return 0, false
	}
	return est - joins, est-joins > limit
}

// Stats reports the buffer's work counters.
func (b *Buffer) Stats() Stats {
	s := b.big
	if s == nil {
		return Stats{}
	}
	return Stats{
		Large:             true,
		ReadBytes:         s.stats.readBytes.Load(),
		CacheBytes:        s.stats.cacheBytes.Load(),
		MaterializedBytes: s.stats.materialized.Load(),
		OverCap:           int(s.stats.overCap.Load()),
		DecodedRunes:      s.stats.decodedRunes.Load(),
		LineCalls:         s.stats.lineCalls.Load(),
		IndexedBytes:      min(int64(s.g.scanned)*s.g.chunk, s.g.size),
		TotalBytes:        s.g.size,
		Pieces:            s.st.root.pieceCount(),
		OverlayBytes:      overlayBytes(s.st),
		HistoryBytes:      b.historyBytes(),
		HistoryGroups:     len(b.undo) + len(b.redo),
	}
}

// lineRunes returns line i as runes without copying. Callers must not modify
// the result. Out-of-range indexes are clamped.
func (b *Buffer) lineRunes(i int) []rune {
	if b.big != nil {
		return b.big.runesOf(i)
	}
	return b.lines[b.clampLine(i)]
}
