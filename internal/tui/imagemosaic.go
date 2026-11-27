package tui

import (
	"bytes"
	"errors"
	"image"
	"image/draw"
	"os"
	"slices"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/mosaic"
	xdraw "golang.org/x/image/draw"

	"github.com/tbxark/gvim/document"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/split"
	"github.com/tbxark/gvim/internal/tui/style"
)

// The half-block picture fallback. When the capability probe found no graphics
// support (mosaicEnabled), the pane draws the picture itself as '▀' cells built
// by github.com/charmbracelet/x/mosaic instead of the text summary. The pixels
// never leave the frame: there is no upload, no placement and no terminal
// resource to release.

const (
	// mosaicMaxSide bounds the decoded picture the cache keeps, on its longer
	// side. A mosaic cell is one column wide and two pixel-rows tall, so a decode
	// far bigger than the pane is only memory.
	mosaicMaxSide = 1024
	// mosaicCacheMax bounds the decoded pictures the state holds.
	mosaicCacheMax = 4
)

// mosaicEntry is the state's record of one picture's decoded pixels: the file's
// stat when it was read, the decoded image, and the last cell rendering for a
// pane size. A failed entry keeps the summary and stops the file being read
// again until its stat changes, the same philosophy as imageFailed.
type mosaicEntry struct {
	size, mtime int64
	img         image.Image
	failed      bool
	pending     bool
	render      mosaicRender
}

// mosaicRender is a finished cell rendering: w by h cells in row-major order.
type mosaicRender struct {
	w, h  int
	cells []grid.Cell
}

// mosaicEnabled reports whether the probe found no graphics support, so the
// pane draws the picture as half-block cells instead of the summary.
func (s imageState) mosaicEnabled() bool {
	return s.probed && !s.kitty && !s.forced && !s.off && !s.localOnly
}

// mosaicPut stores entry for path and drops the oldest entries past
// mosaicCacheMax. Replacing an entry moves its path to the back of the order.
func (s *imageState) mosaicPut(path string, e *mosaicEntry) {
	if s.mosaic == nil {
		s.mosaic = make(map[string]*mosaicEntry)
	}
	if _, ok := s.mosaic[path]; ok {
		s.mosaicOrder = slices.DeleteFunc(s.mosaicOrder, func(p string) bool { return p == path })
	}
	s.mosaic[path] = e
	s.mosaicOrder = append(s.mosaicOrder, path)
	for len(s.mosaicOrder) > mosaicCacheMax {
		delete(s.mosaic, s.mosaicOrder[0])
		s.mosaicOrder = s.mosaicOrder[1:]
	}
}

// mosaicLoadedMsg carries a picture decoded for the half-block fallback. The
// work runs in a command, so a large file never stalls the update loop, the
// same as the kitty read.
type mosaicLoadedMsg struct {
	path        string
	size, mtime int64
	img         image.Image
	err         error
}

// errImageUndecodable marks a file whose header parsed but whose dimensions are
// not a picture.
var errImageUndecodable = errors.New("image has no dimensions")

// mosaicLoadCmd reads and decodes path's picture for the half-block fallback,
// under the same guards the kitty read applies. The decoded image is downscaled
// to mosaicMaxSide on its long side so the cache stays bounded; it is never
// upscaled.
func mosaicLoadCmd(path string, size, mtime int64) tea.Cmd {
	return func() tea.Msg {
		st, err := os.Stat(path)
		if err != nil {
			return mosaicLoadedMsg{path: path, size: size, mtime: mtime, err: err}
		}
		if !st.Mode().IsRegular() {
			return mosaicLoadedMsg{path: path, size: size, mtime: mtime, err: errImageNotRegular}
		}
		if st.Size() > imageMaxBytes {
			return mosaicLoadedMsg{path: path, size: size, mtime: mtime, err: errImageTooLarge}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return mosaicLoadedMsg{path: path, size: size, mtime: mtime, err: err}
		}
		if len(data) > imageMaxBytes {
			return mosaicLoadedMsg{path: path, size: size, mtime: mtime, err: errImageTooLarge}
		}
		img, _, err := decodeMosaic(data, imageMaxPixels)
		return mosaicLoadedMsg{path: path, size: size, mtime: mtime, img: img, err: err}
	}
}

func decodeMosaic(data []byte, maxPixels int) (image.Image, image.Point, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, image.Point{}, err
	}
	size := image.Pt(cfg.Width, cfg.Height)
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, size, errImageUndecodable
	}
	if cfg.Height > maxPixels/cfg.Width {
		return nil, size, errImageTooLarge
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, size, err
	}
	return mosaicBound(img), size, nil
}

// mosaicBound downscales img so its longer side is at most mosaicMaxSide, never
// upscaling. The mosaic path holds the decode in memory for the whole session,
// so it keeps a bounded copy rather than the file's pixels.
func mosaicBound(img image.Image) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= mosaicMaxSide && h <= mosaicMaxSide {
		return img
	}
	var nw, nh int
	if w >= h {
		nw = mosaicMaxSide
		nh = max(h*mosaicMaxSide/w, 1)
	} else {
		nh = mosaicMaxSide
		nw = max(w*mosaicMaxSide/h, 1)
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
	return dst
}

// mosaicPane returns the half-block rendering of doc's picture when the probe
// found no graphics support. It reports false while the decode is missing,
// stale or failed, so the caller keeps the text summary. The fit uses the
// picture's original size and the same cell size as the kitty path, so the two
// paths place the picture in the same cells.
func (m *Model) mosaicPane(p split.Pane, doc Document, info document.ImageInfo) (*grid.Grid, bool) {
	if !m.img.mosaicEnabled() {
		return nil, false
	}
	path, ok := m.imagePathOf(doc)
	if !ok || path == "" {
		return nil, false
	}
	size, mtime := m.imageStatOf(doc)
	e, ok := m.img.mosaic[path]
	if !ok || e.size != size || e.mtime != mtime || e.failed || e.img == nil {
		return nil, false
	}
	area := imageRegion{x: 0, y: 0, w: p.W, h: p.H}
	cellW, cellH := m.img.cellSize()
	fit := imageFit(area, info.Width, info.Height, cellW, cellH)
	pg := grid.New(area.w, area.h)
	fg := style.Cell(m.styles.Editor)
	for y := range pg.Height() {
		pg.Fill(0, pg.Width(), y, fg)
	}
	if fit.w <= 0 || fit.h <= 0 {
		return pg, true
	}
	cells := e.render.pictureCells(e.img, fit.w, fit.h)
	for row := range fit.h {
		for col := range fit.w {
			c := cells[row*fit.w+col]
			pg.Set(fit.x-area.x+col, fit.y-area.y+row, c.Text, c.Style)
		}
	}
	return pg, true
}

// mosaicCells renders img into w by h cells. mosaic scales the source to
// width*scale by height*scale pixels and emits one cell per 2x2 pixel block, so
// Width(w).Height(h).Scale(2) makes exactly w columns by h rows. With
// IgnoreBlockSymbols every cell is the upper half block carrying the true
// colors instead of a luminance-thresholded symbol, but it puts the lower
// pixel's color in the foreground while '▀' paints its upper half with it; the
// two colors are swapped back here so the top pixel is the foreground.
func mosaicCells(img image.Image, w, h int) []grid.Cell {
	m := mosaic.New().Width(w).Height(h).Scale(2).Symbol(mosaic.Half).IgnoreBlockSymbols(true)
	out := m.Render(img)
	buf := uv.NewScreenBuffer(w, h)
	uv.NewStyledString(out).Draw(buf, buf.Bounds())
	cells := make([]grid.Cell, 0, w*h)
	for y := range h {
		for x := range w {
			c := buf.CellAt(x, y)
			if c == nil || c.Content != "▀" {
				cells = append(cells, grid.Cell{Text: " "})
				continue
			}
			cells = append(cells, grid.Cell{
				Text:  "▀",
				Style: grid.Style{FG: c.Style.Bg, BG: c.Style.Fg},
			})
		}
	}
	return cells
}
