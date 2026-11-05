package uitest

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// Default cell size in pixels. A cell is 10 pixels wide, which is the advance
// of the default font's glyphs, and 20 pixels high, which holds the line.
const (
	defaultCellW = 10
	defaultCellH = 20
)

// PNGOption configures Screen.PNG.
type PNGOption func(*pngConfig)

type pngConfig struct {
	cellW, cellH int
	fontFile     string
}

// WithCellSize sets the size of one cell in pixels. The default is 10 by 20.
// The font is scaled so that its digits fill the cell width.
func WithCellSize(w, h int) PNGOption {
	return func(c *pngConfig) { c.cellW, c.cellH = w, h }
}

// WithFontFile draws the text with the font file at path (a TrueType or
// OpenType file, or a collection). Without it, UITEST_FONT names the font, then
// a macOS or Linux monospace font, then the embedded Go Mono font.
func WithFontFile(path string) PNGOption {
	return func(c *pngConfig) { c.fontFile = path }
}

// PNG writes the screen as a PNG image, with one cell of the configured size
// per character cell. Colours, bold and underline come from the cells, so the
// image shows what Screen shows. Box-drawing and block characters are drawn
// procedurally. Other characters the font lacks are drawn as an outlined box.
func (sc Screen) PNG(w io.Writer, opt ...PNGOption) error {
	cfg := pngConfig{cellW: defaultCellW, cellH: defaultCellH}
	for _, o := range opt {
		o(&cfg)
	}
	if cfg.cellW < 1 || cfg.cellH < 1 {
		return fmt.Errorf("uitest: cell size %dx%d is not positive", cfg.cellW, cfg.cellH)
	}
	f, err := loadFont(cfg.fontFile)
	if err != nil {
		return err
	}
	face, base, err := newFace(f, cfg.cellW, cfg.cellH)
	if err != nil {
		return err
	}
	defer func() { _ = face.Close() }()

	img := image.NewRGBA(image.Rect(0, 0, sc.w*cfg.cellW, sc.h*cfg.cellH))
	r := &pngRenderer{img: img, face: face, cellW: cfg.cellW, cellH: cfg.cellH, base: base}
	r.draw(sc)
	return png.Encode(w, img)
}

// pngRenderer paints the cells of one screen into an image.
type pngRenderer struct {
	img          *image.RGBA
	face         font.Face
	cellW, cellH int
	base         int // baseline distance below the top of a cell
}

// draw paints every background first, then the glyphs and underlines. A wide
// character's glyph can spill into the next cell, so that cell's background
// must already be down.
func (r *pngRenderer) draw(sc Screen) {
	for y := range sc.h {
		for x := range sc.w {
			r.fill(x, y, sc.Cell(x, y).Bg)
		}
	}
	for y := range sc.h {
		for x := range sc.w {
			c := sc.Cell(x, y)
			r.glyph(x, y, c)
			if c.Underline {
				r.underline(x, y, c.Fg)
			}
		}
	}
}

// origin returns the pixel position of the top-left corner of a cell.
func (r *pngRenderer) origin(x, y int) (int, int) {
	return x * r.cellW, y * r.cellH
}

// fill paints the whole cell at (x, y) with colour c.
func (r *pngRenderer) fill(x, y int, c color.RGBA) {
	x0, y0 := r.origin(x, y)
	fillBox(r.img, x0, y0, 0, 0, r.cellW, r.cellH, c)
}

// glyph draws the cell's character in its foreground colour. Bold draws the
// glyph a second time, one pixel to the right.
func (r *pngRenderer) glyph(x, y int, c Cell) {
	runes := []rune(c.Char)
	if len(runes) == 0 || runes[0] == ' ' {
		return
	}
	x0, y0 := r.origin(x, y)
	ch := runes[0]
	if drawProcedural(r.img, x0, y0, r.cellW, r.cellH, ch, c.Fg, c.Bg) {
		return
	}
	if _, ok := r.face.GlyphAdvance(ch); !ok {
		r.missing(x0, y0, ansi.StringWidth(string(ch)), c.Fg)
		return
	}
	d := font.Drawer{
		Dst:  r.img,
		Src:  image.NewUniform(c.Fg),
		Face: r.face,
		Dot:  fixed.P(x0, y0+r.base),
	}
	d.DrawString(string(ch))
	if c.Bold {
		d.Dot = fixed.P(x0+1, y0+r.base)
		d.DrawString(string(ch))
	}
}

// underline draws a one-pixel line two pixels below the baseline of the cell,
// across its full width. The line stays inside the cell.
func (r *pngRenderer) underline(x, y int, c color.RGBA) {
	x0, y0 := r.origin(x, y)
	dy := min(r.base+2, r.cellH-1)
	fillBox(r.img, x0, y0, 0, dy, r.cellW, 1, c)
}

// missing draws an outlined box in place of a character the font lacks. It
// spans width cells, which is 2 for a wide character.
func (r *pngRenderer) missing(x0, y0, width int, c color.RGBA) {
	w := max(1, width) * r.cellW
	dx, dy := 2, 3
	inW, inH := w-2*dx, r.cellH-2*dy
	if inW < 1 || inH < 1 {
		return
	}
	fillBox(r.img, x0, y0, dx, dy, inW, 1, c)
	fillBox(r.img, x0, y0, dx, dy+inH-1, inW, 1, c)
	fillBox(r.img, x0, y0, dx, dy, 1, inH, c)
	fillBox(r.img, x0, y0, dx+inW-1, dy, 1, inH, c)
}
