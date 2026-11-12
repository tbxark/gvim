package tui

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"
	_ "golang.org/x/image/webp"

	"github.com/tbxark/gvim/document"
	"github.com/tbxark/gvim/internal/tui/grid"
)

// imageDoc is the optional view of a document that holds an image. The pane for
// such a document draws the picture instead of the text when the terminal can.
type imageDoc interface {
	Image() (document.ImageInfo, bool)
}

// imagePathDoc is implemented by a Document that holds an image and knows its
// file path, so the viewer can read the picture's bytes to send them. The text
// summary in Document occupies its lines; the pixels are read from the path.
// pathEditor (model.go) already has FilePath, so an image document needs both.
type imagePathDoc interface {
	imageDoc
	pathEditor
}

// imageLocalDoc is implemented by an image document whose file is on this
// machine. A remote image document answers false, so the viewer never falls
// back to os.ReadFile for a path that belongs to another host.
type imageLocalDoc interface {
	LocalImage() bool
}

// imageStatDoc is implemented by an image Document that reports the file's size
// and modification time at the time its tab opened. The viewer records them with
// the transmission; a file changed on disk does not refresh the picture, and a
// close and reopen is what reads it again.
type imageStatDoc interface {
	ImageStat() (size, mtime int64)
}

// imageRegion is a rectangle of pane cells.
type imageRegion struct{ x, y, w, h int }

// imageCellSize is the assumed pixel size of one cell when the terminal did not
// report it. A 16x32 cell is the common default for a monospace font.
var imageCellSize = struct{ w, h int }{w: 16, h: 32}

const (
	// imageMaxBytes bounds the picture file the viewer reads into memory. A
	// larger file keeps the text summary.
	imageMaxBytes = 32 << 20
	// imageMaxPixels bounds the decoded picture. A larger picture keeps the text
	// summary: decoding it would cost memory and time out of proportion to a
	// terminal's cells.
	imageMaxPixels = 64_000_000
)

// imagePathOf returns the file path of an image document, and whether doc is one.
// The type assertion alone is not enough: the document adapter has Image, FilePath
// and ImageStat for every document, so a text document also satisfies the
// interface. The Image method's ok is what says the document is really an image.
func (m *Model) imagePathOf(doc Document) (string, bool) {
	pd, ok := doc.(imagePathDoc)
	if !ok {
		return "", false
	}
	if !isImageDoc(doc) {
		return "", false
	}
	if ld, ok := doc.(imageLocalDoc); ok && !ld.LocalImage() {
		return "", false
	}
	return pd.FilePath(), true
}

// isImageDoc reports whether doc is an image document. The document adapter has
// the image methods on every document, so its Image ok is what decides.
func isImageDoc(doc Document) bool {
	if i, ok := doc.(interface{ IsImage() bool }); ok {
		return i.IsImage()
	}
	img, ok := doc.(imageDoc)
	if !ok {
		return false
	}
	_, isImage := img.Image()
	return isImage
}

// imageStatOf is doc's file size and modification time, or zero when the document
// does not report them.
func (m *Model) imageStatOf(doc Document) (size, mtime int64) {
	if d, ok := doc.(imageStatDoc); ok {
		return d.ImageStat()
	}
	return 0, 0
}

// imageIDFor returns the id doc's picture is transmitted under, and whether it has
// reached the terminal. It only reads the state: a paint never allocates an id, so
// a background tab's paint cannot shift the transmissions the active tab owns.
func (m *Model) imageIDFor(doc Document) (id int, uploaded bool) {
	path, ok := m.imagePathOf(doc)
	if !ok || path == "" {
		return 0, false
	}
	size, mtime := m.imageStatOf(doc)
	return m.img.imageUploaded(path, size, mtime)
}

// imageSendMsg carries the picture bytes a tab needs transmitted. The root reads
// the file in a command, so a big picture does not block the update loop.
type imageSendMsg struct {
	path string
	id   int
	data []byte
	err  error
}

// imageUploadedMsg means the Raw message of an upload has reached the program.
// The viewer marks the picture drawable then, never before.
type imageUploadedMsg struct {
	path string
	id   int
}

// imageSendCmd reads the picture of path and returns the bytes to upload under id.
// The read is a command, so opening a large image does not stall the frame. A read
// failure is reported to imageMessage, which records the failure so the pane keeps
// the summary and the same file is not read again every frame.
func imageSendCmd(path string, id int) tea.Cmd {
	return func() tea.Msg {
		st, err := os.Stat(path)
		if err != nil {
			return imageSendMsg{path: path, id: id, err: err}
		}
		if !st.Mode().IsRegular() {
			return imageSendMsg{path: path, id: id, err: errImageNotRegular}
		}
		if st.Size() > imageMaxBytes {
			return imageSendMsg{path: path, id: id, err: errImageTooLarge}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return imageSendMsg{path: path, id: id, err: err}
		}
		if len(data) > imageMaxBytes {
			return imageSendMsg{path: path, id: id, err: errImageTooLarge}
		}
		return imageSendMsg{path: path, id: id, data: data}
	}
}

// errImageTooLarge marks a picture that is over a render limit. It stays a text
// summary, and the same file is not read again until it changes or is reopened.
var errImageTooLarge = errors.New("image is too large to render")

// errImageNotRegular marks a path that is no longer a regular file, such as a pipe
// or a device that replaced the picture after it was opened.
var errImageNotRegular = errors.New("image path is not a regular file")

// imageUploadSequence is the bytes a picture send hands to the terminal: the
// upload alone. The virtual placement is created every frame by the pane, because
// its size follows the pane's size; a placement made once here would keep the
// cells it was created for after a resize.
//
// The terminal understands PNG among the compressed formats, so a picture that is
// not already PNG (a JPEG or a GIF) is re-encoded first. A picture that cannot be
// decoded returns "" and the caller leaves the summary text up.
func imageUploadSequence(data []byte, id int) string {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return ""
	}
	if cfg.Height > imageMaxPixels/cfg.Width {
		return ""
	}
	png, ok := asPNG(data)
	if !ok {
		return ""
	}
	return imageTransmit(png, id)
}

// asPNG returns data as PNG bytes. A PNG file is passed through unchanged; any
// other registered format is decoded and re-encoded, so a JPEG or a GIF reaches a
// terminal that only understands the protocol's three formats.
func asPNG(data []byte) ([]byte, bool) {
	if bytes.HasPrefix(data, pngSignature) {
		return data, true
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, false
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, false
	}
	return buf.Bytes(), true
}

// pngSignature is the eight-byte signature every PNG file starts with. Checking
// the whole signature keeps a text or a corrupted file from being sent as a PNG.
var pngSignature = []byte("\x89PNG\r\n\x1a\n")

// imageFit returns the cell rectangle inside area that holds the picture at the
// largest size keeping its aspect ratio, centered. The image is never stretched:
// a tall picture keeps its proportions and leaves side margins.
func imageFit(area imageRegion, pxW, pxH, cellW, cellH int) imageRegion {
	if area.w <= 0 || area.h <= 0 || pxW <= 0 || pxH <= 0 {
		return imageRegion{x: area.x, y: area.y}
	}
	cellW, cellH = max(cellW, 1), max(cellH, 1)
	// The natural size in cells, rounding a partial cell up.
	cols := (pxW + cellW - 1) / cellW
	rows := (pxH + cellH - 1) / cellH
	if cols > area.w {
		rows = max(rows*area.w/cols, 1)
		cols = area.w
	}
	if rows > area.h {
		cols = max(cols*area.h/rows, 1)
		rows = area.h
	}
	return imageRegion{
		x: area.x + (area.w-cols)/2,
		y: area.y + (area.h-rows)/2,
		w: cols,
		h: rows,
	}
}

// paintImage fills g (an area.w by area.h grid) with the placeholder cells of
// doc's picture. The picture's own data is not in the grid: the cell text is the
// Unicode placeholder U+10EEEE with the row and column diacritics, and the cell's
// foreground color is the image id. The terminal draws the image over those cells
// and moves it with the text, so scrolling and resizing need no extra work.
//
// The first cell also carries the virtual placement that tells the terminal how
// big the placeholder grid is. It is re-created every frame, so a resize or a
// change of the fitted size takes effect; the placement id makes each frame
// replace the previous one instead of stacking.
//
// fg is the style of the surrounding body; each placeholder cell takes its color
// and the id in the foreground.
func paintImage(g *grid.Grid, area imageRegion, info document.ImageInfo, id, placement int, cellW, cellH int, fg grid.Style) {
	for y := range g.Height() {
		g.Fill(0, g.Width(), y, fg)
	}
	fit := imageFit(area, info.Width, info.Height, cellW, cellH)
	if fit.w <= 0 || fit.h <= 0 {
		return
	}
	for row := 0; row < fit.h; row++ {
		for col := 0; col < fit.w; col++ {
			text := placeholderCell(row, col)
			if row == 0 && col == 0 {
				// The placement must exist before the cells that name it are drawn, and
				// this cell is the first of them in the row-major paint order.
				text = imageVirtualPlacement(id, placement, fit.w, fit.h) + text
			}
			st := fg
			st.FG = imageIDColor(id)
			// SGR 58 names the placement; SGR 4 would draw a visible line under
			// every placeholder row.
			st.Underline = false
			st.UnderlineColor = imageIDColor(placement)
			g.Set(fit.x-area.x+col, fit.y-area.y+row, text, st)
		}
	}
}

// placeholderCell is the text of the placeholder at (row, col): the private-use
// character followed by the diacritics that name the row and the column. The row
// and column diacritics are what make each cell of the image distinct; without
// them the terminal would repeat one cell of the picture everywhere.
func placeholderCell(row, col int) string {
	var b strings.Builder
	b.WriteRune(kitty.Placeholder)
	b.WriteRune(kitty.Diacritic(row))
	b.WriteRune(kitty.Diacritic(col))
	return b.String()
}

// imageIDColor is the foreground color that carries image id to the terminal: the
// placeholder protocol reads the id from the foreground color, so an id that fits
// a byte is sent as an ANSI-256 color and a larger one as a true color.
func imageIDColor(id int) ansi.Color {
	if id < 256 {
		return ansi.IndexedColor(id)
	}
	return ansi.RGBColor{R: uint8(id >> 16), G: uint8(id >> 8), B: uint8(id)}
}

// imageTransmit is the APC that uploads a PNG to the terminal under id, without
// displaying it: the placeholders are what display it. It is quiet (q=2) so the
// terminal's acknowledgement cannot reach the program as input, and chunked so a
// large picture does not exceed the escape-code limit.
func imageTransmit(data []byte, id int) string {
	var b strings.Builder
	writeImageChunks(&b, data, fmt.Sprintf("a=t,f=100,q=2,i=%d", id))
	return b.String()
}

// imageVirtualPlacement is the APC that creates the virtual placement the
// placeholders refer to: U=1 makes it invisible until a placeholder names it, and
// c/r are the cells the picture is fitted into.
func imageVirtualPlacement(id, placement, cols, rows int) string {
	return ansi.KittyGraphics(nil, fmt.Sprintf("a=p,U=1,q=2,i=%d,p=%d,c=%d,r=%d", id, placement, cols, rows))
}

// writeImageChunks writes data as base64 chunks of the protocol's maximum size,
// the first carrying opts and the rest only the continuation keys. The payload of
// each APC is the base64 text itself; the protocol wants base64, and ansi's
// KittyGraphics only frames it.
func writeImageChunks(b *strings.Builder, data []byte, opts string) {
	encoded := base64.StdEncoding.EncodeToString(data)
	mid := false
	for {
		n := min(len(encoded), kitty.MaxChunkSize)
		chunk, rest := encoded[:n], encoded[n:]
		last := len(rest) == 0
		var o string
		switch {
		case !mid:
			o = opts
			if !last {
				o += ",m=1"
			}
		default:
			o = "q=2"
			if last {
				o += ",m=0"
			} else {
				o += ",m=1"
			}
		}
		b.WriteString(ansi.KittyGraphics([]byte(chunk), o))
		encoded, mid = rest, true
		if last {
			return
		}
	}
}
