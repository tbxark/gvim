// Package outline scans Markdown text for its headings: the data behind the
// Contents section of the sidebar. It is a TUI-free engine package: it does no
// IO and holds no display state, so it can be tested directly and read from a
// Bubble Tea update without importing the UI tree.
//
// The scan reads two heading forms, both of which an outline lists:
//
//   - ATX headings, a run of one to six "#" followed by a space and the text;
//   - setext headings, a paragraph line underlined with "===" (level 1) or
//     "---" (level 2).
//
// It skips fenced code blocks: a "# comment" inside a shell block is code, not
// a heading. Fences are matched by their own character and length, so a line
// of three backticks inside a tilde fence is content.
package outline

import (
	"strings"
	"unicode/utf8"
)

// MaxScanBytes is the most Markdown one scan reads. A larger document is not
// scanned: the caller keeps the outline it last showed and says the file is too
// large for one. It is the same order as the editor's large-file threshold, so
// a document the editor treats as large does not cost the outline a full walk.
const MaxScanBytes = 1 << 20

// MaxTitleBytes is the most of a heading's own text a Heading keeps. A heading
// line can be arbitrarily long; the outline draws a few dozen cells of it at
// most, so the rest is cut at a rune boundary.
const MaxTitleBytes = 1024

// Lines is the view of a document a bounded scan reads: its line count and one
// line of text at a time. It is a port, so the scan stays TUI-free; the root
// model satisfies it with the active document.
type Lines interface {
	LineCount() int
	Line(i int) string
}

// Heading is one heading of a Markdown document.
type Heading struct {
	// Byte is the offset of the first byte of the heading's line. Two headings
	// never share it, so it identifies a heading across a rescan, and it keys
	// the tree's fold state.
	Byte int
	// Line is the zero-based line of the heading.
	Line int
	// Level is 1 for "#" or for a "===" setext line, 2 for "##" or a "---"
	// setext line, and up to 6 for the others.
	Level int
	// Title is the heading text without its markers, trimmed.
	Title string
	// HasChildren reports that the next heading is deeper than this one.
	HasChildren bool
	// EndLine is the last line of the heading's section: the line before the
	// next heading of the same or a higher level, or the document's last line.
	// A section is empty when EndLine equals Line.
	EndLine int
	// BodyStart is the offset of the first byte after the heading's own line,
	// and EndByte the offset just past the last byte of the section's last line.
	// Together they are the range a fold over the section hides.
	BodyStart int
	EndByte   int
}

// Scan returns the headings of lines, in line order, with their section extents
// and HasChildren from the same walk.
func Scan(lines []string) []Heading {
	heads := make([]Heading, 0, 8)
	starts, ends := make([]int, len(lines)), make([]int, len(lines))
	offset := 0
	for i, line := range lines {
		starts[i] = offset
		ends[i] = offset + len(line)
		offset = ends[i] + 1 // the newline a split consumed
	}

	inFence, fenceChar, fenceLen := false, byte(0), 0
	for i, line := range lines {
		if inFence {
			if closesFence(line, fenceChar, fenceLen) {
				inFence = false
			}
			continue
		}
		if char, n, ok := openFence(line); ok {
			inFence, fenceChar, fenceLen = true, char, n
			continue
		}
		if level, title, ok := atxHeading(line); ok {
			heads = append(heads, Heading{Byte: starts[i], Line: i, Level: level, Title: title})
			continue
		}
		// A setext heading is written as its text line followed by an
		// underline, so it is found at the underline, one line after its text.
		if level, ok := setextUnderline(line); ok && i > 0 && setextText(lines[i-1]) {
			if n := len(heads); n > 0 && heads[n-1].Line == i-1 {
				continue // the line above is an ATX heading, not a paragraph
			}
			heads = append(heads, Heading{Byte: starts[i-1], Line: i - 1, Level: level, Title: clipTitle(strings.TrimSpace(lines[i-1]))})
		}
	}

	for i := range heads {
		h := &heads[i]
		h.EndLine = len(lines) - 1
		for j := i + 1; j < len(heads); j++ {
			if heads[j].Level <= h.Level {
				h.EndLine = heads[j].Line - 1
				break
			}
		}
		h.BodyStart = ends[h.Line] + 1
		h.EndByte = ends[h.EndLine]
		h.HasChildren = i+1 < len(heads) && heads[i+1].Level > h.Level
	}
	return heads
}

// ScanDoc scans doc, bounded to maxBytes of text (MaxScanBytes when maxBytes is
// zero or negative). complete is false when the document is larger than the
// bound; headings is nil then, and the caller keeps the outline it has.
func ScanDoc(doc Lines, maxBytes int) (headings []Heading, complete bool) {
	if maxBytes <= 0 {
		maxBytes = MaxScanBytes
	}
	n := doc.LineCount()
	if n < 0 {
		n = 0
	}
	lines := make([]string, 0, n)
	total := 0
	for i := 0; i < n; i++ {
		line := doc.Line(i)
		total += len(line) + 1
		if total > maxBytes {
			return nil, false
		}
		lines = append(lines, line)
	}
	return Scan(lines), true
}

// atxHeading returns the level and the title of an ATX heading line, or false
// when the line is not one. A heading is up to three spaces, one to six "#",
// whitespace, and a non-empty text; an optional closing run of "#" is stripped
// when a space precedes it ("## Title ##" is "Title", "## C#" is "C#").
func atxHeading(line string) (level int, title string, ok bool) {
	i := 0
	for i < len(line) && i < 3 && (line[i] == ' ' || line[i] == '	') {
		i++
	}
	start := i
	for i < len(line) && line[i] == '#' {
		i++
	}
	level = i - start
	if level < 1 || level > 6 {
		return 0, "", false
	}
	if i >= len(line) || (line[i] != ' ' && line[i] != '	') {
		return 0, "", false
	}
	title = strings.TrimSpace(line[i:])
	if title == "" {
		return 0, "", false
	}
	// A closing run counts only after whitespace, and not when it is the whole
	// title (the title was required to be non-empty above).
	trimmed := strings.TrimRight(title, " 	")
	hashes := len(trimmed) - len(strings.TrimRight(trimmed, "#"))
	if hashes > 0 && hashes < len(trimmed) {
		if before := trimmed[len(trimmed)-hashes-1]; before == ' ' || before == '	' {
			title = strings.TrimSpace(trimmed[:len(trimmed)-hashes])
		}
	}
	if title == "" {
		return 0, "", false
	}
	return level, clipTitle(title), true
}

// setextUnderline reports the level an underline line gives its text: 1 for a
// run of "=", 2 for a run of "-". Up to three spaces may precede it and
// trailing white space is allowed.
func setextUnderline(line string) (level int, ok bool) {
	i := 0
	for i < len(line) && i < 3 && (line[i] == ' ' || line[i] == '	') {
		i++
	}
	if i >= len(line) {
		return 0, false
	}
	char := line[i]
	if char != '=' && char != '-' {
		return 0, false
	}
	for i < len(line) && line[i] == char {
		i++
	}
	if strings.TrimSpace(line[i:]) != "" {
		return 0, false
	}
	if char == '=' {
		return 1, true
	}
	return 2, true
}

// setextText reports whether line can be the text of a setext heading: a
// paragraph line, not blank, not a heading or a fence, and not a block marker
// that a following rule line would be read as a break of.
func setextText(line string) bool {
	if strings.TrimSpace(line) == "" {
		return false
	}
	if _, _, ok := atxHeading(line); ok {
		return false
	}
	if _, _, ok := openFence(line); ok {
		return false
	}
	if _, ok := setextUnderline(line); ok {
		return false
	}
	trimmed := strings.TrimLeft(line, " 	")
	switch {
	case strings.HasPrefix(trimmed, ">"), strings.HasPrefix(trimmed, "- "),
		strings.HasPrefix(trimmed, "* "), strings.HasPrefix(trimmed, "+ "),
		strings.HasPrefix(trimmed, "|"):
		return false
	}
	// An ordered list item: "1. text" or "1) text".
	digits := 0
	for digits < len(trimmed) && trimmed[digits] >= '0' && trimmed[digits] <= '9' {
		digits++
	}
	if digits > 0 && digits < len(trimmed) && (trimmed[digits] == '.' || trimmed[digits] == ')') && digits+1 < len(trimmed) && trimmed[digits+1] == ' ' {
		return false
	}
	return true
}

// openFence reports a fence opener: up to three spaces, then at least three
// backticks or tildes. It returns the fence character and its length.
func openFence(line string) (char byte, n int, ok bool) {
	i := 0
	for i < len(line) && i < 3 && line[i] == ' ' {
		i++
	}
	if i >= len(line) || (line[i] != '`' && line[i] != '~') {
		return 0, 0, false
	}
	char = line[i]
	for i < len(line) && line[i] == char {
		i++
		n++
	}
	if n < 3 {
		return 0, 0, false
	}
	// A backtick fence's info string may not contain a backtick.
	if char == '`' && strings.ContainsRune(line[i:], '`') {
		return 0, 0, false
	}
	return char, n, true
}

// closesFence reports whether line closes the fence char long n: up to three
// spaces, at least n of the same character, and nothing else.
func closesFence(line string, char byte, n int) bool {
	i := 0
	for i < len(line) && i < 3 && line[i] == ' ' {
		i++
	}
	got := 0
	for i < len(line) && line[i] == char {
		i++
		got++
	}
	return got >= n && strings.TrimSpace(line[i:]) == ""
}

// clipTitle cuts title to MaxTitleBytes at a rune boundary.
func clipTitle(title string) string {
	if len(title) <= MaxTitleBytes {
		return title
	}
	cut := MaxTitleBytes
	for cut > 0 && !utf8.RuneStart(title[cut]) {
		cut--
	}
	return title[:cut]
}
