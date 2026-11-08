package pluginapi

// The compare/diff surface (parity backlog #15): compareBuffers and replaceRange.
// It mirrors Fresh's rich CompareResult so the compare_files plugin port can paint
// aligned, editable diff panels from host-computed geometry, and edit one hunk as
// one undo step without copying file text through Lua.
//
// The byte model is gvim's: offsets count one byte per line break (the document is
// LF-normalised, as buffer.ByteOffset and gvim.buffer.slice already are), so every
// offset a plugin gets here can be used with replace_range or the buffer API as it
// stands.

// CompareSide is one side of a CompareHunk: where the hunk's lines are, in the
// lines of the side and in its bytes.
type CompareSide struct {
	// Start is the first line of the hunk (0-based), Count its lines (0 when the
	// other side alone has lines here).
	Start, Count int
	// From and To are the byte offsets of the hunk's first line and just past its
	// last line, terminator included. start..to is what replacing this side's lines
	// of the hunk replaces.
	From, To int
	// Jump is where a jump to the hunk puts this side's cursor: From, or, for an
	// empty side, the start of the line before the gap, so the cursor sits at the
	// change rather than past it. At the very top of the text, From.
	Jump int
	// Lines holds each line's text as [start, end) byte offsets, without the line
	// break.
	Lines [][2]int
	// Spans has one entry per line paired with the other side (the first
	// min(Count, other.Count) lines): the part that differs, as absolute byte
	// offsets, or Set false when the lines are equal (in gvim's model they then
	// differ only by a line ending, which the editor normalises before the diff).
	Spans []CompareSpan
}

// CompareSpan is the differing part of one paired line: the absolute byte range in
// the side's buffer. Set false means the pair is equal and has no span.
type CompareSpan struct {
	Start, End int
	Set        bool
}

// CompareHunk is one hunk of a CompareResult, with the geometry a side-by-side
// view paints from.
type CompareHunk struct {
	Left, Right CompareSide
	// EOLOnly says the two sides have the same lines but for their terminators.
	// gvim normalises line endings when a file is read, so this is always false
	// here; the field mirrors Fresh and leaves room for a future byte-exact model.
	EOLOnly bool
}

// CompareResult is the answer to compare_buffers: the line diff of two buffers,
// measured in their own bytes.
type CompareResult struct {
	Hunks                 []CompareHunk // increasing order; empty when equal
	LeftLines, RightLines int           // lines in each buffer
	LeftLen, RightLen     int           // each buffer's length in bytes
	LeftVersion           Version       // the content version when it was read
	RightVersion          Version
}

// DiffSource is one buffer read for compare_buffers, without its text crossing into
// Go: the lines the diff compares, each line's byte start, the byte length of the
// buffer and its version.
type DiffSource struct {
	Lines      []string
	Starts     []int // absolute byte offset of each line's first byte
	Len        int   // the buffer's length in bytes
	LineEnding string
	Version    Version
}

// DiffHost is the compare/diff surface: the two reads compare_buffers builds from,
// and replace_range's conditional edit.
type DiffHost interface {
	// DiffSource returns one buffer's lines, their byte starts, the byte length and
	// the content version. It refuses a binary buffer with CodeInvalid and one in
	// large-file mode (or over the compare size cap) with CodeTooLarge, so a
	// side-by-side view never silently falls back to partial text.
	DiffSource(id BufferID) (DiffSource, error)
	// ReplaceRange replaces the bytes start..end of buffer id with text, as ONE
	// edit and one undo step. It returns false, changing nothing, when the buffer
	// is read-only or binary, the range is outside the buffer, or a non-nil expect
	// names another content version. A missing buffer or another real failure is an
	// error. Text is EOL-normalised for the document before it is inserted.
	ReplaceRange(id BufferID, start, end int, text string, expect *Version) (bool, error)
}
