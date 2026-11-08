package editorpane

import (
	"image/color"
	"math"
	"sync"

	"github.com/charmbracelet/x/ansi"
)

// minContrast is the WCAG contrast ratio a foreground must keep over the occurrence
// background, as in Fresh: 3.0, below the 4.5 of normal text because terminal fonts
// are large.
const minContrast = 3.0

// cubeLevels are the channel values of the 6x6x6 color cube of the 256-color palette.
var cubeLevels = [6]uint8{0x00, 0x5f, 0x87, 0xaf, 0xd7, 0xff}

// paletteRGB is the 240 colors the repair picks from: the color cube and the gray
// ramp (palette entries 16 to 255), with their relative luminance.
var paletteRGB = sync.OnceValue(func() (p [240]struct {
	rgb [3]uint8
	lum float64
},
) {
	for i := range p {
		idx := i + 16
		var c [3]uint8
		if idx < 232 {
			n := idx - 16
			c = [3]uint8{cubeLevels[n/36], cubeLevels[n%36/6], cubeLevels[n%6]}
		} else {
			g := uint8((idx-232)*10 + 8)
			c = [3]uint8{g, g, g}
		}
		p[i].rgb, p[i].lum = c, luminance(c)
	}
	return p
})

func srgbLinear(c uint8) float64 {
	s := float64(c) / 255
	if s <= 0.04045 {
		return s / 12.92
	}
	return math.Pow((s+0.055)/1.055, 2.4)
}

// luminance is the WCAG relative luminance of an sRGB color.
func luminance(c [3]uint8) float64 {
	return 0.2126*srgbLinear(c[0]) + 0.7152*srgbLinear(c[1]) + 0.0722*srgbLinear(c[2])
}

func ratioOf(l1, l2 float64) float64 {
	if l1 < l2 {
		l1, l2 = l2, l1
	}
	return (l1 + 0.05) / (l2 + 0.05)
}

// contrastRatio is the WCAG contrast ratio of two sRGB colors.
func contrastRatio(a, b [3]uint8) float64 { return ratioOf(luminance(a), luminance(b)) }

// rgbOf is the RGB of c, or false for a missing color and for a named terminal color,
// whose real value belongs to the terminal.
func rgbOf(c color.Color) ([3]uint8, bool) {
	if c == nil {
		return [3]uint8{}, false
	}
	if _, named := c.(ansi.BasicColor); named {
		return [3]uint8{}, false
	}
	r, g, b, _ := c.RGBA()
	return [3]uint8{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)}, true
}

// repairedFG is the foreground to paint so that text stays legible over bg, or nil to
// keep fg. Only a pair under minContrast is repaired, and then fg moves to the nearest
// color of the 256-color palette that clears the ratio, so a dim comment stays
// recognizably a comment. This is Fresh's fg_on_low_contrast for the occurrence band.
// Unlike Fresh it looks on both sides of the background: on a mid-tone band white may
// not reach the ratio while a dark color does.
func repairedFG(fg, bg color.Color) color.Color {
	f, ok := rgbOf(fg)
	if !ok {
		return nil
	}
	b, ok := rgbOf(bg)
	if !ok {
		return nil
	}
	if contrastRatio(f, b) >= minContrast {
		return nil
	}
	bgLum := luminance(b)
	best, bestDist := -1, math.MaxInt
	pal := paletteRGB()
	for i, c := range pal {
		if ratioOf(c.lum, bgLum) < minContrast {
			continue
		}
		// The weighted distance of compuphase, closer to the eye than the Euclidean one.
		rMean := (int(f[0]) + int(c.rgb[0])) / 2
		dr, dg, db := int(f[0])-int(c.rgb[0]), int(f[1])-int(c.rgb[1]), int(f[2])-int(c.rgb[2])
		if d := (1024+rMean)*dr*dr + 1024*dg*dg + (1534-rMean)*db*db; d < bestDist {
			best, bestDist = i, d
		}
	}
	if best < 0 {
		return nil
	}
	c := pal[best].rgb
	return color.RGBA{R: c[0], G: c[1], B: c[2], A: 255}
}
