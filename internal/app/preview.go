package app

import (
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

// The palette's previewer, wired in run.go.
var _ palette.Previewer = previewReader{}
