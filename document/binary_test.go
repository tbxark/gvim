package document

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBinaryDocumentIsASummary checks the default view of a non-image binary:
// a short read-only summary, not a Latin-1 byte view.
func TestBinaryDocumentIsASummary(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "blob.bin")
	data := []byte("head\x00\x01\x02\x1b[31mtail")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := readDocument(path, loadSpec{Eol: eolLF})
	if err != nil {
		t.Fatalf("readDocument: %v", err)
	}
	if !d.Binary() || !d.ReadOnly() || d.IsImage() {
		t.Fatalf("Binary = %v, ReadOnly = %v, IsImage = %v", d.Binary(), d.ReadOnly(), d.IsImage())
	}
	if d.Encoding() != "Binary" || d.Codec != "binary" {
		t.Errorf("encoding = %q, codec = %q", d.Encoding(), d.Codec)
	}
	text := d.Buffer.Text()
	for _, bad := range []string{"\x00", "\x01", "\x02", "\x1b"} {
		if strings.Contains(text, bad) {
			t.Errorf("summary contains the raw byte %q", bad)
		}
	}
	if !strings.Contains(text, "Binary file: read-only.") || !strings.Contains(text, "Size: 16 B") {
		t.Errorf("summary = %q", text)
	}
	if size, _ := d.BinaryStat(); size != int64(len(data)) {
		t.Errorf("BinaryStat size = %d, want %d", size, len(data))
	}
}

// TestBinaryOpenReadsOnlyTheHead checks that a large binary file is not read
// whole when it opens. The file has no line index and no large-file mode.
func TestBinaryOpenReadsOnlyTheHead(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "big.bin")
	data := append([]byte{0x00, 0x01, 0x02}, make([]byte, 2<<20)...)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := readDocument(path, loadSpec{Eol: eolLF, LargeConf: largeSpec{Threshold: 4096, FullMax: maxFullLoad}})
	if err != nil {
		t.Fatalf("readDocument: %v", err)
	}
	if d.Large() || !d.Binary() || !d.ReadOnly() {
		t.Fatalf("Large = %v, Binary = %v, ReadOnly = %v", d.Large(), d.Binary(), d.ReadOnly())
	}
	if got := d.Status; got != "" {
		t.Logf("status before open = %q", got)
	}
	if got := d.OpenStatus(); got != "Opened big.bin (binary file, read-only)" {
		t.Errorf("OpenStatus = %q", got)
	}
}

// countingRangeAccess counts every byte the head probe reads. The open path
// must inspect no more than one bounded sample even for a large binary.
type countingRangeAccess struct {
	data  []byte
	bytes int64
}

func (a *countingRangeAccess) ReadFile(string, int64) ([]byte, error) {
	return nil, errors.New("whole-file read must not be used for a binary head probe")
}

func (a *countingRangeAccess) Stat(string) (AccessStat, error) {
	return AccessStat{Exists: true, Size: int64(len(a.data))}, nil
}

func (a *countingRangeAccess) WriteFileAtomic(string, []byte) error { return nil }

func (a *countingRangeAccess) ReadRange(_ context.Context, _ string, off, n int64) ([]byte, error) {
	if off < 0 || off > int64(len(a.data)) {
		return nil, io.EOF
	}
	end := min(off+n, int64(len(a.data)))
	a.bytes += end - off
	out := append([]byte(nil), a.data[off:end]...)
	if end < off+n {
		return out, io.EOF
	}
	return out, nil
}

// TestBinaryOpenDoesNotReadTheWholeFile checks the byte budget at the read seam
// below the large-file threshold: the open probes one sample, not the file. The
// result alone cannot tell the two apart, so the access counts the bytes.
func TestBinaryOpenDoesNotReadTheWholeFile(t *testing.T) {
	t.Parallel()
	size := int64(largeSampleSize * 8)
	data := append([]byte{0x00, 0x01, 0x02}, make([]byte, size-3)...)
	access := &countingRangeAccess{data: data}
	d, err := readDocument("/remote/big.bin", loadSpec{
		Eol:    eolLF,
		Access: access,
	})
	if err != nil {
		t.Fatalf("readDocument: %v", err)
	}
	if !d.Binary() {
		t.Fatal("the file did not open as a binary summary")
	}
	if access.bytes > largeSampleSize {
		t.Fatalf("the open read %d bytes, want at most the %d-byte sample", access.bytes, largeSampleSize)
	}
}

// TestBinaryOpenCatchesALateMarker checks a binary marker after the 64 KiB head
// sample. The large path still sees it in its wider source sample before building
// a window, so the file does not become a text document with a line index.
func TestBinaryOpenCatchesALateMarker(t *testing.T) {
	t.Parallel()
	data := append(make([]byte, largeSampleSize+1024), 0x00, 0x01, 0x02)
	access := &countingRangeAccess{data: data}
	d, err := readDocument("/remote/late.bin", loadSpec{
		Eol:       eolLF,
		Access:    access,
		LargeConf: largeSpec{Threshold: 1, FullMax: maxFullLoad},
	})
	if err != nil {
		t.Fatalf("readDocument: %v", err)
	}
	if !d.Binary() || d.Large() || d.AsyncLarge() {
		t.Fatalf("Binary = %v, Large = %v, AsyncLarge = %v; want a binary summary", d.Binary(), d.Large(), d.AsyncLarge())
	}
	if access.bytes > largeSampleSize {
		t.Fatalf("the open read %d bytes, want at most the %d-byte head sample", access.bytes, largeSampleSize)
	}
}

// TestReadBinaryRangeLocal checks bounded local reads, EOF and short files.
func TestReadBinaryRangeLocal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "blob.bin")
	data := []byte("\x00\x01\x020123456789abcdef")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := readDocument(path, loadSpec{Eol: eolLF})
	if err != nil {
		t.Fatal(err)
	}
	got, err := d.ReadBinaryRange(context.Background(), 4, 6)
	if err != nil || string(got) != "123456" {
		t.Fatalf("ReadBinaryRange = %q, %v", got, err)
	}
	got, err = d.ReadBinaryRange(context.Background(), 16, 16)
	if err != nil || string(got) != "def" {
		t.Fatalf("tail read = %q, %v; want the remaining bytes", got, err)
	}
	got, err = d.ReadBinaryRange(context.Background(), int64(len(data)), 1)
	if !errors.Is(err, io.EOF) || len(got) != 0 {
		t.Fatalf("EOF read = %q, %v", got, err)
	}
}

// TestReadBinaryRangeRefusesTextAndImage checks that the range reader is not a
// general file reader: only a non-image binary document may use it.
func TestReadBinaryRangeRefusesTextAndImage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	textPath := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(textPath, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	text, err := readDocument(textPath, loadSpec{Eol: eolLF})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := text.ReadBinaryRange(context.Background(), 0, 1); !errors.Is(err, ErrNotBinary) {
		t.Errorf("text ReadBinaryRange = %v, want ErrNotBinary", err)
	}
	imagePath := filepath.Join(dir, "pic.png")
	if err := os.WriteFile(imagePath, pngBytes(t, 2, 2), 0o644); err != nil {
		t.Fatal(err)
	}
	imageDoc, err := readDocument(imagePath, loadSpec{Eol: eolLF})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := imageDoc.ReadBinaryRange(context.Background(), 0, 1); !errors.Is(err, ErrNotBinary) {
		t.Errorf("image ReadBinaryRange = %v, want ErrNotBinary", err)
	}
}

// TestReadBinaryRangeRemote checks that remote binaries use RangeAccess and
// never the local filesystem or a whole-file read.
func TestReadBinaryRangeRemote(t *testing.T) {
	t.Parallel()
	access := &fakeRangeAccess{data: []byte("\x00\x01abcdefghij")}
	d, err := readDocument("/remote/blob.bin", loadSpec{
		Eol:       eolLF,
		Access:    access,
		LargeConf: largeSpec{Threshold: 4, FullMax: maxFullLoad},
	})
	if err != nil {
		t.Fatalf("readDocument: %v", err)
	}
	if !d.Binary() || d.Large() {
		t.Fatalf("Binary = %v, Large = %v", d.Binary(), d.Large())
	}
	got, err := d.ReadBinaryRange(context.Background(), 0, 4)
	if err != nil || string(got) != "\x00\x01ab" {
		t.Fatalf("ReadBinaryRange = %q, %v", got, err)
	}
	reads, _ := access.counts()
	if reads == 0 {
		t.Fatal("the remote range reader was not used")
	}
}

// TestReadBinaryRangeRemoteUnsupported checks the clear refusal for an access
// that only reads whole files.
func TestReadBinaryRangeRemoteUnsupported(t *testing.T) {
	t.Parallel()
	access := &wholeFileOnlyAccess{data: []byte("\x00\x01")}
	d, err := readDocument("/remote/blob.bin", loadSpec{Eol: eolLF, Access: access})
	if err != nil {
		t.Fatalf("readDocument: %v", err)
	}
	if _, err := d.ReadBinaryRange(context.Background(), 0, 1); !errors.Is(err, ErrBinaryRangeUnsupported) {
		t.Fatalf("ReadBinaryRange = %v, want ErrBinaryRangeUnsupported", err)
	}
}

// wholeFileOnlyAccess is a FileAccess without RangeAccess.
type wholeFileOnlyAccess struct{ data []byte }

func (a *wholeFileOnlyAccess) ReadFile(string, int64) ([]byte, error) {
	return append([]byte(nil), a.data...), nil
}

func (a *wholeFileOnlyAccess) Stat(string) (AccessStat, error) {
	return AccessStat{Exists: true, Size: int64(len(a.data))}, nil
}

func (a *wholeFileOnlyAccess) WriteFileAtomic(string, []byte) error { return nil }
