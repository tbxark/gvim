package document

import (
	"strings"
)

// lineEnds returns the ending of every line break of data, "\n" or "\r\n", when
// the file mixes the two. It returns nil for a file that uses one ending, which
// the document's eol describes.
func lineEnds(data string) []string {
	if strings.IndexByte(data, '\r') == -1 {
		return nil
	}
	crlf := strings.Count(data, "\r\n")
	if crlf == 0 || crlf == strings.Count(data, "\n") {
		return nil
	}
	ends := make([]string, 0, strings.Count(data, "\n"))
	for i := range len(data) {
		switch {
		case data[i] != '\n':
		case i > 0 && data[i-1] == '\r':
			ends = append(ends, "\r\n")
		default:
			ends = append(ends, "\n")
		}
	}
	return ends
}

// breaks returns the ending to write at each line break of text. The record in
// d.ends belongs to d.saved: a line that text shares with it, at the start or the
// end, keeps its ending, and so do the lines in between when the line count has
// not changed. Any other break is a new one and takes the document's ending.
func (d *Document) breaks(text string) []string {
	lines := strings.Split(text, "\n")
	old := strings.Split(d.Saved, "\n")
	own := "\n"
	if d.Eol == eolCRLF {
		own = "\r\n"
	}
	prefix := 0
	for prefix < min(len(lines), len(old)) && lines[prefix] == old[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < min(len(lines), len(old))-prefix && lines[len(lines)-1-suffix] == old[len(old)-1-suffix] {
		suffix++
	}
	recorded := func(i int) string {
		if i < 0 || i >= len(d.Ends) {
			return own
		}
		return d.Ends[i]
	}
	out := make([]string, len(lines)-1)
	for i := range out {
		switch {
		case i < prefix, len(lines) == len(old):
			out[i] = recorded(i)
		case i >= len(lines)-suffix:
			out[i] = recorded(i - (len(lines) - len(old)))
		default:
			out[i] = own
		}
	}
	return out
}

// mixedText is fileText for a document opened from a file with mixed line endings.
func (d *Document) mixedText(text string) string {
	ends := d.breaks(text)
	lines := strings.Split(text, "\n")
	var b strings.Builder
	for i, line := range lines {
		b.WriteString(line)
		if i < len(ends) {
			b.WriteString(ends[i])
		}
	}
	return b.String()
}
