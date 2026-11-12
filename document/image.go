package document

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"path/filepath"
	"strings"

	_ "golang.org/x/image/webp"
)

// ErrImage is returned by a revert of an image document. The picture is read when
// its tab opens, so closing and reopening is what loads a new version.
var ErrImage = errors.New("image file: close and reopen the tab to reload it")

// ImageInfo describes an image file: what it is and how big it is. It is read
// from the file header alone, so opening a large image does not decode it.
type ImageInfo struct {
	// Format is the file format as the image package names it: "png", "jpeg",
	// "gif" or "webp".
	Format string
	// Width and Height are the image size in pixels.
	Width, Height int
	// Size is the file size in bytes.
	Size int64
	// Color is the color model name: "RGBA", "RGB", "Gray", "YCbCr" or "Palette".
	Color string
}

// Label is the format name as the status row shows it: "PNG", "JPEG", "GIF" or
// "WEBP".
func (i ImageInfo) Label() string { return strings.ToUpper(i.Format) }

// String describes the image in one line: "PNG 2.3 MB (1920x1080) · RGBA".
func (i ImageInfo) String() string {
	return fmt.Sprintf("%s %s (%dx%d) · %s", i.Label(), formatSize(i.Size), i.Width, i.Height, i.Color)
}

// imageFormats are the image formats an image document holds. A format the image
// package knows but this list does not opens as a binary file, so the viewer only
// claims what it can show.
var imageFormats = map[string]bool{
	"png":  true,
	"jpeg": true,
	"gif":  true,
	"webp": true,
}

// imageInfoOf reads the header of data and reports what image it is. ok is false
// for data that is not an image of a supported format, which then opens as text
// or as a binary file, the way it did before.
func imageInfoOf(data []byte) (ImageInfo, bool) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || !imageFormats[format] {
		return ImageInfo{}, false
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return ImageInfo{}, false
	}
	return ImageInfo{
		Format: format,
		Width:  cfg.Width,
		Height: cfg.Height,
		Size:   int64(len(data)),
		Color:  colorModelName(cfg.ColorModel),
	}, true
}

// colorModelName names a color model for the summary. A palette is the model of
// GIF and of an indexed PNG; the rest are matched by identity.
func colorModelName(m color.Model) string {
	switch m {
	case color.RGBAModel, color.NRGBAModel, color.RGBA64Model, color.NRGBA64Model:
		return "RGBA"
	case color.GrayModel, color.Gray16Model:
		return "Gray"
	case color.YCbCrModel:
		return "YCbCr"
	case color.CMYKModel:
		return "CMYK"
	}
	if _, ok := m.(color.Palette); ok {
		return "Palette"
	}
	return "RGB"
}

// imageDocument is the read-only document for an image file. Its text is a short
// summary, so the tab, the status row and the pane geometry need no special case;
// the image itself is drawn by the UI from Image().
func imageDocument(path string, data []byte, disk diskState, info ImageInfo) *Document {
	info.Size = int64(len(data))
	return newImageDocument(path, disk, info)
}

// newImageDocument builds the read-only image document for path from the
// header's facts. data is not kept: the UI reads the file again only when it can
// draw pictures.
func newImageDocument(path string, disk diskState, info ImageInfo) *Document {
	text := strings.Join([]string{
		filepath.Base(path),
		"",
		info.String(),
		"",
		imageSummaryHint,
	}, "\n")
	d := newDocument(path, text)
	d.Disk = disk
	d.Codec = "binary"
	d.encoding = info.Label()
	d.image = &info
	d.readOnly = true
	// An image is not text, so it takes the binary document's gates: no edit, no
	// save, no diff. The flag only says "not text"; the viewer is what shows it, and
	// the byte view of binary.go is never used because Open checks for an image first.
	d.binary = true
	return d
}

// imageSummaryHint is the last line of an image document's text: what the reader
// can do with it. It is shown whether or not the terminal draws images, so the
// text view says the same thing in both cases.
const imageSummaryHint = "Image file: read-only."

// Image returns the image the document holds. ok is false for a document that is
// not an image file.
func (d *Document) Image() (ImageInfo, bool) {
	if d.image == nil {
		return ImageInfo{}, false
	}
	return *d.image, true
}

// IsImage reports whether d is an image file. Other packages use it instead of
// comparing Image, whose zero value is a valid-looking ImageInfo.
func (d *Document) IsImage() bool { return d.image != nil }

// ImagePtr returns the image facts of the document for a swap, or nil for a
// document that is not an image. The caller must not change the copy.
func (d *Document) ImagePtr() *ImageInfo {
	if d.image == nil {
		return nil
	}
	info := *d.image
	return &info
}

// ImageStat is the size in bytes and the modification time in nanoseconds the
// document's file had when its tab opened. The viewer records them with the
// transmission; the picture is read again only when the tab is closed and
// reopened, not when the file changes underneath it. Both are zero for a
// document without a disk state.
func (d *Document) ImageStat() (size, mtime int64) {
	return d.Disk.Size, mtimeNano(d.Disk)
}
