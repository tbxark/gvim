package app

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"

	"github.com/tbxark/gvim/internal/tui/palette"
)

// previewReader is the palette's Previewer over the filesystem: it reads the head of a
// file, at most the byte count the palette asks for, so a preview never loads a whole
// file and never runs on the update loop.
type previewReader struct{}

// ReadHead returns at most maxBytes of the file at path. truncated reports that the
// file continues past them. A directory or an unreadable file gives an error.
func (previewReader) ReadHead(path string, maxBytes int) ([]byte, bool, error) {
	if maxBytes <= 0 {
		maxBytes = 1 << 10
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, maxBytes+1)
	n, err := io.ReadFull(f, buf)
	switch {
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return buf[:n], false, nil
	case err != nil:
		return nil, false, err
	}
	// One byte more than asked for is the read marker: the file continues.
	return buf[:maxBytes], true, nil
}

// ReadWindow returns complete lines around the 1-based line, at most maxBytes, for a
// Live Grep row's preview. It reads sequentially and is bounded by the context, a
// before-window scan limit, a single-line buffer limit, a line count and the byte cap;
// it never loads the whole file. truncated says the file has more lines outside the
// returned window, and firstLine is the 1-based line of data[0].
func (previewReader) ReadWindow(ctx context.Context, path string, line, maxBytes int) ([]byte, int, bool, error) {
	if maxBytes <= 0 {
		maxBytes = 1 << 10
	}
	if line < 1 {
		line = 1
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, false, err
	}
	defer func() { _ = f.Close() }()
	const (
		beforeLines = 5
		maxLines    = 12
		// The lines before the window are scanned only up to this budget; a hit far
		// into a large file is refused rather than turning the preview into an
		// unbounded read. A longer single line is an error from the scanner below.
		scanBudget = 8 << 20
		maxLine    = 256 << 10
	)
	first := max(line-beforeLines, 1)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), maxLine)
	var out bytes.Buffer
	scanned := 0
	n := 0
	for sc.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, 0, false, err
		}
		n++
		ln := sc.Bytes()
		if n < first {
			scanned += len(ln) + 1
			if scanned > scanBudget {
				return nil, 0, false, errors.New("preview window is too far into the file")
			}
			continue
		}
		if out.Len() > 0 && out.Len()+len(ln)+1 > maxBytes {
			return out.Bytes(), first, true, nil
		}
		if out.Len() == 0 && len(ln)+1 > maxBytes {
			// A single over-cap line: keep at most the byte budget; newPreview drops
			// the partial last line because truncated is set.
			if maxBytes > 0 && len(ln) >= maxBytes {
				ln = ln[:maxBytes]
			}
			out.Write(ln)
			return out.Bytes(), first, true, nil
		}
		out.Write(ln)
		out.WriteByte('\n')
		if n-first+1 >= maxLines {
			return out.Bytes(), first, true, nil
		}
	}
	if err := sc.Err(); err != nil {
		return nil, 0, false, err
	}
	return out.Bytes(), first, false, nil
}

// The palette's previewer, wired in run.go.
var _ palette.Previewer = previewReader{}
var _ palette.WindowPreviewer = previewReader{}
