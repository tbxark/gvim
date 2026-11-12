package document

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ErrBinary is returned by an action that would write a binary document, and by
// toggle_read_only, which would make it editable. A binary file is shown, not
// edited: its text is not the bytes of the file.
var ErrBinary = errors.New("binary file")

// ErrNotBinary is returned by ReadBinaryRange for a document that is not a
// non-image binary file.
var ErrNotBinary = errors.New("not a binary file")

// ErrBinaryRangeUnsupported is returned when a remote binary file's access has
// no range reader. The hex view then stays on the summary instead of falling
// back to a whole-file read or to the local filesystem.
var ErrBinaryRangeUnsupported = errors.New("binary range reads are not supported by this file access")

// binaryDocument is the read-only summary of a file that encoding calls binary.
// The bytes stay in the file: the summary can be opened without reading a large
// blob into memory, and Toggle Hex View reads bounded ranges through
// ReadBinaryRange.
func binaryDocument(path string, disk diskState) *Document {
	text := strings.Join([]string{
		filepath.Base(path),
		"",
		"Binary file",
		"Size: " + formatSize(disk.Size),
		"",
		"Binary file: read-only.",
		"",
		"Run Toggle Hex View to inspect the bytes.",
	}, "\n")
	d := newDocument(path, text)
	d.Disk = disk
	d.Codec = "binary"
	d.encoding = "Binary"
	d.readOnly = true
	d.binary = true
	return d
}

// Binary reports whether the document is a binary file.
func (d *Document) Binary() bool { return d.binary }

// BinaryStat is the size and modification time the binary file had when it was
// opened. It is zero for a document without a disk state.
func (d *Document) BinaryStat() (size, mtime int64) {
	return d.Disk.Size, mtimeNano(d.Disk)
}

// ReadBinaryRange reads at most n bytes at off from a binary document. It reads
// a bounded window on demand: local files use os.File.ReadAt, and remote files
// use their RangeAccess. A read at or beyond the end returns io.EOF, and a file
// that shrank since it was opened may return a short result with io.EOF.
//
// The method refuses an image document even though images also report Binary:
// their bytes are for the image viewer, not for a hex dump.
func (d *Document) ReadBinaryRange(ctx context.Context, off, n int64) ([]byte, error) {
	if !d.binary || d.image != nil {
		return nil, ErrNotBinary
	}
	if d.Path == "" {
		return nil, ErrNotBinary
	}
	if off < 0 || n <= 0 {
		return nil, fmt.Errorf("binary range: invalid offset %d or length %d", off, n)
	}
	want := n
	if d.Disk.Exists && d.Disk.Size >= 0 {
		if off >= d.Disk.Size {
			return nil, io.EOF
		}
		want = min(want, d.Disk.Size-off)
	}
	if want <= 0 {
		return nil, io.EOF
	}
	if d.access != nil {
		ranges, ok := d.access.(RangeAccess)
		if !ok {
			return nil, ErrBinaryRangeUnsupported
		}
		data, err := ranges.ReadRange(ctx, d.Path, off, want)
		if int64(len(data)) > want {
			data = data[:want]
		}
		if err == nil && int64(len(data)) < want {
			err = io.EOF
		}
		return data, err
	}
	f, err := os.Open(d.Path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data := make([]byte, want)
	read, err := f.ReadAt(data, off)
	if errors.Is(err, io.EOF) {
		return data[:read], io.EOF
	}
	if err != nil {
		return nil, err
	}
	return data[:read], nil
}
