package uitest

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// blankScreen returns a screen of default cells, to be filled by a test.
func blankScreen(w, h int) Screen {
	sc := Screen{w: w, h: h, cells: make([][]Cell, h)}
	for y := range sc.cells {
		sc.cells[y] = make([]Cell, w)
		for x := range sc.cells[y] {
			sc.cells[y][x] = blankCell()
		}
	}
	return sc
}

// render encodes sc as a PNG, decodes it, and returns the pixels.
func render(t *testing.T, sc Screen, opt ...PNGOption) *image.RGBA {
	t.Helper()
	var buf bytes.Buffer
	if err := sc.PNG(&buf, opt...); err != nil {
		t.Fatalf("PNG: %v", err)
	}
	img, err := png.Decode(&buf)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	out := image.NewRGBA(img.Bounds())
	draw.Draw(out, out.Bounds(), img, img.Bounds().Min, draw.Src)
	return out
}

// pixel returns the colour of the pixel at (x, y) in an image.
func pixel(img *image.RGBA, x, y int) color.RGBA {
	return img.RGBAAt(x, y)
}

// countPixels counts the pixels of colour c inside the cell at column cx, row
// cy, where each cell is cw by ch pixels.
func countPixels(img *image.RGBA, cx, cy, cw, ch int, c color.RGBA) int {
	n := 0
	for y := cy * ch; y < (cy+1)*ch; y++ {
		for x := cx * cw; x < (cx+1)*cw; x++ {
			if pixel(img, x, y) == c {
				n++
			}
		}
	}
	return n
}

// ink sums the red channel of the pixels of the cell at column cx, row cy. It
// grows with the amount of glyph drawn, even where antialiasing leaves no
// pixel at the full colour.
func ink(img *image.RGBA, cx, cy, cw, ch int) int {
	n := 0
	for y := cy * ch; y < (cy+1)*ch; y++ {
		for x := cx * cw; x < (cx+1)*cw; x++ {
			n += int(pixel(img, x, y).R)
		}
	}
	return n
}

// withChar sets the cell at (x, y) to text in the given colours.
func withChar(sc Screen, x, y int, text string, fg, bg color.RGBA) Screen {
	sc.cells[y][x] = Cell{Char: text, Fg: fg, Bg: bg}
	return sc
}

var (
	white = color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	black = color.RGBA{A: 0xff}
	red   = color.RGBA{R: 0xff, A: 0xff}
)

// colourModel writes one truecolour cell, one palette cell and one plain cell.
type colourModel struct{}

func (colourModel) Init() tea.Cmd                       { return nil }
func (colourModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return colourModel{}, nil }
func (colourModel) View() tea.View {
	// A truecolour blank, a palette-red blank, a bold letter, and a plain blank.
	return tea.NewView("\x1b[48;2;200;10;30m \x1b[0m\x1b[41m \x1b[0m\x1b[1mB\x1b[0m")
}

func TestPNGDecodesAndMatchesTheCellGrid(t *testing.T) {
	s := Start(t, &fakeModel{}, 100, 30)
	path := filepath.Join(t.TempDir(), "frame.png")
	if err := s.Screenshot(path); err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	data := readFile(t, path)
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got, want := img.Bounds().Size(), image.Pt(100*defaultCellW, 30*defaultCellH); got != want {
		t.Fatalf("PNG size = %v, want %v", got, want)
	}
}

func TestPNGCellSizeOptionChangesTheGrid(t *testing.T) {
	sc := blankScreen(7, 3)
	img := render(t, sc, WithCellSize(8, 16))
	if got, want := img.Bounds().Size(), image.Pt(7*8, 3*16); got != want {
		t.Fatalf("PNG size = %v, want %v", got, want)
	}
}

func TestPNGRejectsAnEmptyCell(t *testing.T) {
	var buf bytes.Buffer
	if err := blankScreen(2, 2).PNG(&buf, WithCellSize(0, 20)); err == nil {
		t.Fatal("PNG accepted a zero cell width")
	}
}

func TestPNGRejectsAMissingFontFile(t *testing.T) {
	var buf bytes.Buffer
	err := blankScreen(2, 2).PNG(&buf, WithFontFile(filepath.Join(t.TempDir(), "none.ttf")))
	if err == nil || !strings.Contains(err.Error(), "read font") {
		t.Fatalf("PNG with a missing font file: err = %v", err)
	}
}

func TestUnderlinedCellHasAForegroundRow(t *testing.T) {
	sc := blankScreen(3, 1)
	sc.cells[0][1] = Cell{Char: " ", Underline: true, Fg: red, Bg: black}
	img := render(t, sc)
	if got := countPixels(img, 1, 0, defaultCellW, defaultCellH, red); got < defaultCellW {
		t.Fatalf("underlined blank cell has %d foreground pixels, want a full line of at least %d", got, defaultCellW)
	}
	if got := countPixels(img, 0, 0, defaultCellW, defaultCellH, red); got != 0 {
		t.Fatalf("plain blank cell has %d foreground pixels, want none", got)
	}
}

func TestUnderlineIsAFullWidthRow(t *testing.T) {
	sc := blankScreen(1, 1)
	sc.cells[0][0] = Cell{Char: " ", Underline: true, Fg: red, Bg: black}
	img := render(t, sc)
	found := false
	for y := range defaultCellH {
		full := true
		for x := range defaultCellW {
			if pixel(img, x, y) != red {
				full = false
				break
			}
		}
		if full {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("underline is not a row of foreground pixels across the cell")
	}
}

func TestBoxHorizontalHasForegroundOnItsMidline(t *testing.T) {
	sc := withChar(blankScreen(1, 1), 0, 0, "─", white, black)
	img := render(t, sc)
	mid := defaultCellH / 2
	for x := range defaultCellW {
		if pixel(img, x, mid) != white {
			t.Fatalf("horizontal line has no foreground at column %d of its midline", x)
		}
	}
}

func TestBoxVerticalHasForegroundOnItsMidline(t *testing.T) {
	sc := withChar(blankScreen(1, 1), 0, 0, "│", white, black)
	img := render(t, sc)
	mid := defaultCellW / 2
	for y := range defaultCellH {
		if pixel(img, mid, y) != white {
			t.Fatalf("vertical line has no foreground at row %d of its midline", y)
		}
	}
}

func TestBoxCornerJoinsItsArms(t *testing.T) {
	sc := withChar(blankScreen(1, 1), 0, 0, "┌", white, black)
	img := render(t, sc)
	// A down-and-right corner runs from the centre to the bottom and to the right.
	if pixel(img, defaultCellW/2, defaultCellH-1) != white {
		t.Fatal("corner has no foreground at the bottom of its vertical arm")
	}
	if pixel(img, defaultCellW-1, defaultCellH/2) != white {
		t.Fatal("corner has no foreground at the right end of its horizontal arm")
	}
	if pixel(img, 0, 0) != black {
		t.Fatal("corner draws in the top-left, which it should leave blank")
	}
}

func TestFullBlockFillsTheCell(t *testing.T) {
	sc := withChar(blankScreen(1, 1), 0, 0, "█", white, black)
	img := render(t, sc)
	if got := countPixels(img, 0, 0, defaultCellW, defaultCellH, white); got != defaultCellW*defaultCellH {
		t.Fatalf("full block has %d foreground pixels, want all %d", got, defaultCellW*defaultCellH)
	}
}

func TestLeftHalfBlockSplitsTheCell(t *testing.T) {
	// The scrollbar's thumb is drawn with U+258C, so its halves must be exact.
	sc := withChar(blankScreen(1, 1), 0, 0, "▌", white, black)
	img := render(t, sc)
	if got, want := countPixels(img, 0, 0, defaultCellW, defaultCellH, white), defaultCellW/2*defaultCellH; got != want {
		t.Fatalf("left half block has %d foreground pixels, want %d", got, want)
	}
	if pixel(img, 0, 0) != white || pixel(img, defaultCellW-1, 0) != black {
		t.Fatal("left half block is on the wrong side of the cell")
	}
}

func TestBoldDrawsMoreForeground(t *testing.T) {
	regular := withChar(blankScreen(1, 1), 0, 0, "l", white, black)
	bold := blankScreen(1, 1)
	bold.cells[0][0] = Cell{Char: "l", Bold: true, Fg: white, Bg: black}
	n := ink(render(t, regular), 0, 0, defaultCellW, defaultCellH)
	b := ink(render(t, bold), 0, 0, defaultCellW, defaultCellH)
	if b <= n {
		t.Fatalf("bold glyph has ink %d, regular has %d; bold should have more", b, n)
	}
}

func TestMissingGlyphIsAnOutlinedBox(t *testing.T) {
	// U+10FFFF is a noncharacter that no font carries, so the box is drawn.
	sc := withChar(blankScreen(2, 1), 0, 0, "\U0010FFFF", white, black)
	img := render(t, sc)
	if pixel(img, 2, defaultCellH/2) != white {
		t.Fatal("replacement box has no left edge")
	}
	if pixel(img, defaultCellW/2, defaultCellH/2) != black {
		t.Fatal("replacement box is filled, want an outline")
	}
}

func TestEmulatorColoursReachTheImage(t *testing.T) {
	s := Start(t, colourModel{}, 4, 1)
	sc := s.Screen()
	truecolour := color.RGBA{R: 200, G: 10, B: 30, A: 0xff}
	if got := sc.Cell(0, 0).Bg; got != truecolour || sc.Cell(0, 0).DefaultBg {
		t.Fatalf("cell (0,0) bg = %v default=%v, want %v", got, sc.Cell(0, 0).DefaultBg, truecolour)
	}
	if !sc.Cell(3, 0).DefaultBg || !sc.Cell(3, 0).DefaultFg {
		t.Fatal("uncoloured cell is not marked as default")
	}
	if !sc.Cell(3, 0).DefaultBg {
		t.Fatal("blank cell (3,0) has no set background, want default")
	}
	if !sc.Cell(2, 0).Bold {
		t.Fatal("bold cell (2,0) is not marked bold")
	}
	img := render(t, sc)
	if got := countPixels(img, 0, 0, defaultCellW, defaultCellH, truecolour); got != defaultCellW*defaultCellH {
		t.Fatalf("truecolour cell has %d pixels of its background, want all", got)
	}
	palette := sc.Cell(1, 0).Bg
	if palette == defaultBg || sc.Cell(1, 0).DefaultBg {
		t.Fatalf("palette-red cell has the default background %v", palette)
	}
	if got := countPixels(img, 1, 0, defaultCellW, defaultCellH, palette); got != defaultCellW*defaultCellH {
		t.Fatalf("palette-red cell has %d pixels of %v, want all", got, palette)
	}
}

func TestDefaultColoursAreLightOnDark(t *testing.T) {
	sc := blankScreen(1, 1)
	img := render(t, sc)
	if got := pixel(img, 0, 0); got != defaultBg {
		t.Fatalf("default background pixel = %v, want %v", got, defaultBg)
	}
	if defaultFg.R <= defaultBg.R || defaultFg.G <= defaultBg.G {
		t.Fatal("default foreground is not lighter than the default background")
	}
}

// readFile returns the bytes of a file written by a test.
func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
