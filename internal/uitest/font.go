package uitest

import (
	"fmt"
	"os"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/opentype"
)

// fontPaths lists the monospace fonts tried in order when no font is given and
// UITEST_FONT is unset. The embedded Go Mono font is the last resort.
var fontPaths = []string{
	"/System/Library/Fonts/Menlo.ttc",
	"/System/Library/Fonts/SFNSMono.ttf",
	"/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
}

var (
	defaultFontOnce sync.Once
	defaultFont     *opentype.Font
	defaultFontErr  error
)

// loadFont returns the font for the renderer. A non-empty path names the font
// file; otherwise the default font is used, found once per process.
func loadFont(path string) (*opentype.Font, error) {
	if path != "" {
		return parseFontFile(path)
	}
	defaultFontOnce.Do(func() {
		defaultFont, defaultFontErr = findDefaultFont()
	})
	return defaultFont, defaultFontErr
}

// findDefaultFont uses UITEST_FONT when it is set, and otherwise the first
// readable file in fontPaths, and otherwise the embedded Go Mono font.
func findDefaultFont() (*opentype.Font, error) {
	if p := os.Getenv("UITEST_FONT"); p != "" {
		return parseFontFile(p)
	}
	for _, p := range fontPaths {
		if f, err := parseFontFile(p); err == nil {
			return f, nil
		}
	}
	f, err := opentype.Parse(gomono.TTF)
	if err != nil {
		return nil, fmt.Errorf("uitest: parse embedded Go Mono: %w", err)
	}
	return f, nil
}

// parseFontFile reads a TrueType or OpenType file, or a collection, and returns
// its first font.
func parseFontFile(path string) (*opentype.Font, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("uitest: read font: %w", err)
	}
	coll, err := opentype.ParseCollection(data)
	if err != nil {
		return nil, fmt.Errorf("uitest: parse font %s: %w", path, err)
	}
	f, err := coll.Font(0)
	if err != nil {
		return nil, fmt.Errorf("uitest: font %s: %w", path, err)
	}
	return f, nil
}

// newFace returns a face whose '0' advances exactly cellW pixels. A font whose
// line is taller than cellH is scaled down to fit. The second result is the
// baseline's distance below the top of a cell.
func newFace(f *opentype.Font, cellW, cellH int) (font.Face, int, error) {
	const probeSize = 100.0
	probe, err := opentype.NewFace(f, &opentype.FaceOptions{Size: probeSize, DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		return nil, 0, fmt.Errorf("uitest: font face: %w", err)
	}
	adv, ok := probe.GlyphAdvance('0')
	if !ok || adv <= 0 {
		return nil, 0, fmt.Errorf("uitest: font has no advance for '0'")
	}
	size := probeSize * float64(cellW) * 64 / float64(adv)
	m := probe.Metrics()
	line := float64(m.Ascent+m.Descent) / 64 * size / probeSize
	if line > float64(cellH) {
		size *= float64(cellH) / line
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		return nil, 0, fmt.Errorf("uitest: font face: %w", err)
	}
	m = face.Metrics()
	asc, desc := m.Ascent.Ceil(), m.Descent.Ceil()
	base := (cellH-(asc+desc))/2 + asc
	base = min(max(base, 0), cellH-1)
	return face, base, nil
}
