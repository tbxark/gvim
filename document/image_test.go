package document

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pngBytes returns a w by h RGBA PNG.
func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 0x80, A: 0xff})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// gifBytes returns a w by h GIF.
func gifBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewPaletted(image.Rect(0, 0, w, h), color.Palette{
		color.Black, color.White,
	})
	var buf bytes.Buffer
	if err := gif.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode gif: %v", err)
	}
	return buf.Bytes()
}

// jpegBytes returns a w by h JPEG.
func jpegBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: 0x40, G: 0x60, B: 0x80, A: 0xff})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

// webpBytes is a 1x1 lossless WEBP, the fixture for the format the standard
// library does not ship an encoder for. The bytes were produced by libwebp and are
// small enough to keep inline, so the test needs no file on disk.
const webp1x1 = "UklGRhwAAABXRUJQVlA4TA8AAAAvAAAAAAcQ/Y/+ByKi/wEA"

// webpBytes decodes the inline WEBP fixture.
func webpBytes(t *testing.T) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(webp1x1)
	if err != nil {
		t.Fatalf("decode the webp fixture: %v", err)
	}
	return data
}

// TestImageInfoOfRecognizesFormats checks the header read of each supported
// format: the format name, the pixel size and the color model name.
func TestImageInfoOfRecognizesFormats(t *testing.T) {
	tests := []struct {
		name   string
		data   []byte
		format string
		w, h   int
		color  string
	}{
		{"png", pngBytes(t, 24, 16), "png", 24, 16, "RGBA"},
		{"jpeg", jpegBytes(t, 20, 10), "jpeg", 20, 10, "YCbCr"},
		{"gif", gifBytes(t, 12, 8), "gif", 12, 8, "Palette"},
		{"webp", webpBytes(t), "webp", 1, 1, "RGBA"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, ok := imageInfoOf(tt.data)
			if !ok {
				t.Fatal("imageInfoOf = false, want true")
			}
			if info.Format != tt.format || info.Width != tt.w || info.Height != tt.h || info.Color != tt.color {
				t.Fatalf("info = %+v, want %s %dx%d %s", info, tt.format, tt.w, tt.h, tt.color)
			}
			if info.Size != int64(len(tt.data)) {
				t.Errorf("size = %d, want %d", info.Size, len(tt.data))
			}
		})
	}
}

// TestImageInfoOfRejectsNonImages checks that text, a truncated header and an
// unknown format fall through to the text/binary paths.
func TestImageInfoOfRejectsNonImages(t *testing.T) {
	png := pngBytes(t, 4, 4)
	tests := []struct {
		name string
		data []byte
	}{
		{"text", []byte("hello, world\n")},
		{"empty", nil},
		{"truncated png", png[:8]},
		{"garbage", bytes.Repeat([]byte{0x00, 0x01, 0x02, 0x03}, 16)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if info, ok := imageInfoOf(tt.data); ok {
				t.Fatalf("imageInfoOf = %+v, want false", info)
			}
		})
	}
}

// TestOpenImageFileGivesImageDocument checks the open path: a PNG on disk becomes
// a read-only image document carrying the header's facts, and its summary text is
// what the tab sees.
func TestOpenImageFileGivesImageDocument(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pic.png")
	data := pngBytes(t, 30, 20)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := readDocument(path, loadSpec{Eol: eolLF})
	if err != nil {
		t.Fatalf("readDocument: %v", err)
	}
	info, ok := d.Image()
	if !ok {
		t.Fatal("Image() = false, want true")
	}
	if info.Format != "png" || info.Width != 30 || info.Height != 20 {
		t.Fatalf("info = %+v", info)
	}
	if !d.ReadOnly() {
		t.Error("image document is not read-only")
	}
	if d.Encoding() != "PNG" {
		t.Errorf("encoding = %q, want PNG", d.Encoding())
	}
	if got := d.Buffer.Line(0); got != "pic.png" {
		t.Errorf("line 0 = %q, want the base name", got)
	}
	if got := d.Buffer.Lines(); got < 2 {
		t.Errorf("lines = %d, want the summary block", got)
	}
}

// TestOpenLargeImageStaysAnImage checks the large-file path: an image over the
// threshold is still an image document, not a windowed binary file.
func TestOpenLargeImageStaysAnImage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.png")
	data := pngBytes(t, 64, 64)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	// A threshold below the file size forces the large path.
	spec := loadSpec{Eol: eolLF, LargeConf: largeSpec{Threshold: 16, FullMax: maxFullLoad}}
	d, err := readDocument(path, spec)
	if err != nil {
		t.Fatalf("readDocument: %v", err)
	}
	info, ok := d.Image()
	if !ok {
		t.Fatal("Image() = false for a large image, want true")
	}
	if info.Width != 64 || info.Height != 64 {
		t.Fatalf("info = %+v", info)
	}
	if d.Large() {
		t.Error("an image document must not enter large file mode")
	}
	if !d.ReadOnly() || !d.Binary() {
		t.Errorf("read-only = %v, binary = %v, want both true", d.ReadOnly(), d.Binary())
	}
}

// TestOpenWebpFileGivesImageDocument checks the whole open path for WEBP, whose
// decoder is the extension package rather than a standard one: the file must become
// an image document, not a binary one.
func TestOpenWebpFileGivesImageDocument(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pic.webp")
	if err := os.WriteFile(path, webpBytes(t), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := readDocument(path, loadSpec{Eol: eolLF})
	if err != nil {
		t.Fatalf("readDocument: %v", err)
	}
	info, ok := d.Image()
	if !ok {
		t.Fatal("Image() = false for a WEBP, want true")
	}
	if info.Format != "webp" || info.Width != 1 || info.Height != 1 {
		t.Fatalf("info = %+v", info)
	}
	if !d.ReadOnly() {
		t.Error("a WEBP document is not read-only")
	}
	if d.Encoding() != "WEBP" {
		t.Errorf("encoding = %q, want WEBP", d.Encoding())
	}
}

// TestOpenBinaryFileIsNotAnImage checks that a non-image binary file still opens
// as a binary summary, so the image branch does not swallow it.
func TestOpenBinaryFileIsNotAnImage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blob.bin")
	if err := os.WriteFile(path, bytes.Repeat([]byte{0x00, 0x01, 0x02, 0x03}, 32), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := readDocument(path, loadSpec{Eol: eolLF})
	if err != nil {
		t.Fatalf("readDocument: %v", err)
	}
	if _, ok := d.Image(); ok {
		t.Fatal("Image() = true for a binary blob, want false")
	}
	if !d.Binary() || !d.ReadOnly() {
		t.Errorf("binary = %v, read-only = %v, want both true", d.Binary(), d.ReadOnly())
	}
	if d.Encoding() != "Binary" {
		t.Errorf("encoding = %q, want Binary", d.Encoding())
	}
	if strings.Contains(d.Buffer.Text(), "\x00") {
		t.Error("the binary summary contains a NUL byte")
	}
}

// jpegWithMetadata inserts APP1 segments holding n payload bytes in all after the SOI
// marker of a JPEG, as EXIF, XMP and ICC blocks do. A segment length is 16 bits, so the
// payload is split into segments of at most 60000 bytes. The frame header is pushed
// past the head sample.
func jpegWithMetadata(t *testing.T, w, h, n int) []byte {
	t.Helper()
	src := jpegBytes(t, w, h)
	out := append([]byte{}, src[:2]...)
	for left := n; left > 0; {
		size := min(left, 60000)
		out = append(out, 0xff, 0xe1, byte((size+2)>>8), byte(size+2))
		out = append(out, make([]byte, size)...)
		left -= size
	}
	return append(out, src[2:]...)
}

// TestOpenJPEGWithLargeMetadataIsAnImage: a JPEG whose metadata is longer than the 64 KiB
// head sample still opens as an image on every open path, reading no more than the
// bounded head.
func TestOpenJPEGWithLargeMetadataIsAnImage(t *testing.T) {
	t.Parallel()
	data := jpegWithMetadata(t, 40, 30, 100<<10)
	if _, ok := imageInfoOf(data[:largeSampleSize]); ok {
		t.Fatal("fixture: the sample alone decodes, so the metadata is too short to test")
	}
	path := filepath.Join(t.TempDir(), "photo.jpg")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	check := func(t *testing.T, d *Document) {
		t.Helper()
		info, ok := d.Image()
		if !ok || info.Format != "jpeg" || info.Width != 40 || info.Height != 30 || info.Size != int64(len(data)) {
			t.Fatalf("Image() = %+v, %v; want a 40x30 jpeg of %d bytes", info, ok, len(data))
		}
		if d.Large() || !d.Binary() {
			t.Fatalf("Large = %v, Binary = %v", d.Large(), d.Binary())
		}
	}
	t.Run("local", func(t *testing.T) {
		d, err := readDocument(path, loadSpec{Eol: eolLF})
		if err != nil {
			t.Fatal(err)
		}
		check(t, d)
	})
	t.Run("local large", func(t *testing.T) {
		d, err := readDocument(path, loadSpec{Eol: eolLF, LargeConf: largeSpec{Threshold: 16, FullMax: maxFullLoad}})
		if err != nil {
			t.Fatal(err)
		}
		check(t, d)
	})
	for name, conf := range map[string]largeSpec{"remote": {}, "remote large": {Threshold: 16, FullMax: maxFullLoad}} {
		t.Run(name, func(t *testing.T) {
			access := &countingRangeAccess{data: data}
			d, err := readDocument("/remote/photo.jpg", loadSpec{Eol: eolLF, Access: access, LargeConf: conf})
			if err != nil {
				t.Fatal(err)
			}
			check(t, d)
			if access.bytes > jpegMaxHead+largeSampleSize {
				t.Fatalf("read %d bytes, want at most the %d-byte JPEG head bound", access.bytes, jpegMaxHead)
			}
		})
	}
}

// TestJPEGFrameBeyondTheBoundIsNotAnImage: metadata past jpegMaxHead stops the scan, and
// a non-JPEG head never triggers a longer read.
func TestJPEGFrameBeyondTheBoundIsNotAnImage(t *testing.T) {
	t.Parallel()
	reads := 0
	read := func(n int64) ([]byte, error) {
		reads++
		return make([]byte, n), nil
	}
	if _, ok := imageInfoOfHead(append([]byte{0x00, 0x01}, make([]byte, 100)...), 1<<30, read); ok || reads != 0 {
		t.Fatalf("a non-JPEG head: ok = %v, reads = %d", ok, reads)
	}
	// Segments of 64 KiB chain up to the bound without ever reaching a frame header.
	seg := append([]byte{0xff, 0xe1, 0xff, 0xff}, make([]byte, 0xffff-2)...)
	var data []byte
	data = append(data, 0xff, 0xd8)
	for len(data) < jpegMaxHead+len(seg) {
		data = append(data, seg...)
	}
	more := func(n int64) ([]byte, error) { return data[:n], nil }
	if _, ok := imageInfoOfHead(data[:largeSampleSize], int64(len(data)), more); ok {
		t.Fatal("a frame header beyond the bound was accepted")
	}
}

// TestMirrorKeepsBinaryAndImage: a cross-workspace mirror of an image or binary
// document is still not text, so it takes the same gates as its owner.
func TestMirrorKeepsBinaryAndImage(t *testing.T) {
	t.Parallel()
	img := newImageDocument("/w/a.png", diskState{}, ImageInfo{Format: "png", Width: 3, Height: 2, Size: 9, Color: "RGBA"})
	bin := binaryDocument("/w/a.bin", diskState{})
	for name, owner := range map[string]*Document{"image": img, "binary": bin} {
		m, err := NewMirrorDocument(owner)
		if err != nil {
			t.Fatal(name, err)
		}
		if m.Binary() != owner.Binary() || m.IsImage() != owner.IsImage() {
			t.Errorf("%s mirror: Binary = %v, IsImage = %v", name, m.Binary(), m.IsImage())
		}
		if got, ok := m.Image(); ok != owner.IsImage() || (ok && got != *owner.ImagePtr()) {
			t.Errorf("%s mirror image = %+v, %v", name, got, ok)
		}
		stale := newDocument("/w/a", "text")
		stale.sharedMirror = true
		if err := stale.SyncMirrorFrom(owner); err != nil {
			t.Fatal(err)
		}
		if stale.Binary() != owner.Binary() || stale.IsImage() != owner.IsImage() {
			t.Errorf("%s sync: Binary = %v, IsImage = %v", name, stale.Binary(), stale.IsImage())
		}
	}
}
