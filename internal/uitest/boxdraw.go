package uitest

import (
	"image"
	"image/color"
)

// boxArms gives, for U+2500 to U+257F, the weight of the line that leaves the
// cell centre towards up, down, left and right. Each entry is four digits in
// that order: 0 none, 1 light, 2 heavy, 3 double. The dashed and rounded
// variants use the solid or light weight of their line, so their shapes match
// the plain ones. U+2571 to U+2573 are diagonals and are drawn separately.
var boxArms = [128]string{
	// U+2500 to U+250F
	"0011", "0022", "1100", "2200", "0011", "0022", "1100", "2200",
	"0011", "0022", "1100", "2200", "0101", "0102", "0201", "0202",
	// U+2510 to U+251F
	"0110", "0120", "0210", "0220", "1001", "1002", "2001", "2002",
	"1010", "1020", "2010", "2020", "1101", "1102", "2101", "1201",
	// U+2520 to U+252F
	"2201", "2102", "1202", "2202", "1110", "1120", "2110", "1210",
	"2210", "2120", "1220", "2220", "0111", "0121", "0112", "0122",
	// U+2530 to U+253F
	"0211", "0221", "0212", "0222", "1011", "1021", "1012", "1022",
	"2011", "2021", "2012", "2022", "1111", "1121", "1112", "1122",
	// U+2540 to U+254F
	"2111", "1211", "2211", "2121", "2112", "1221", "1212", "2122",
	"1222", "2221", "2212", "2222", "0011", "0022", "1100", "2200",
	// U+2550 to U+255F
	"0033", "3300", "0103", "0301", "0303", "0130", "0310", "0330",
	"1003", "3001", "3003", "1030", "3010", "3030", "1103", "3301",
	// U+2560 to U+256F
	"3303", "1130", "3310", "3330", "0133", "0311", "0333", "1033",
	"3011", "3033", "1133", "3311", "3333", "0101", "0110", "1010",
	// U+2570 to U+257F
	"1001", "0000", "0000", "0000", "0010", "1000", "0001", "0100",
	"0020", "2000", "0002", "0200", "0012", "1200", "0021", "2100",
}

// quadrants gives, for U+2596 to U+259F, the quadrants of the cell that are
// filled. The bits are upper-left 1, upper-right 2, lower-left 4, lower-right 8.
var quadrants = [10]uint8{4, 8, 1, 13, 9, 7, 11, 2, 6, 14}

// drawProcedural draws the box-drawing and block characters of the cell at
// (x0, y0) procedurally, so their lines join up whatever the font. It reports
// false for any other rune. bg is the cell background, used for the shade
// blocks.
func drawProcedural(img *image.RGBA, x0, y0, cw, ch int, r rune, fg, bg color.RGBA) bool {
	switch {
	case r >= 0x2500 && r <= 0x257F:
		if r >= 0x2571 && r <= 0x2573 {
			drawDiagonals(img, x0, y0, cw, ch, r, fg)
		} else {
			drawBoxArms(img, x0, y0, cw, ch, boxArms[r-0x2500], fg)
		}
		return true
	case r >= 0x2580 && r <= 0x259F:
		drawBlock(img, x0, y0, cw, ch, r, fg, bg)
		return true
	}
	return false
}

// drawBoxArms draws the arms of one box character from the cell centre. The
// arm order in the string is up, down, left, right.
func drawBoxArms(img *image.RGBA, x0, y0, cw, ch int, arms string, fg color.RGBA) {
	light := max(1, cw/10)
	heavy := 2 * light
	cx, cy := cw/2, ch/2
	for i, arm := range arms {
		w := arm - '0'
		if w == 0 {
			continue
		}
		stroke := light
		if w == 2 {
			stroke = heavy
		}
		if i < 2 { // vertical arm
			from, to := cy, ch
			if i == 0 {
				from, to = 0, cy+1
			}
			if w == 3 {
				fillBox(img, x0, y0, cx-2, from, 1, to-from, fg)
				fillBox(img, x0, y0, cx+2, from, 1, to-from, fg)
			} else {
				fillBox(img, x0, y0, cx-stroke/2, from, stroke, to-from, fg)
			}
			continue
		}
		from, to := cx, cw // horizontal arm
		if i == 2 {
			from, to = 0, cx+1
		}
		if w == 3 {
			fillBox(img, x0, y0, from, cy-2, to-from, 1, fg)
			fillBox(img, x0, y0, from, cy+2, to-from, 1, fg)
		} else {
			fillBox(img, x0, y0, from, cy-stroke/2, to-from, stroke, fg)
		}
	}
}

// drawDiagonals draws U+2571 (rising), U+2572 (falling) and U+2573 (both) as
// one-pixel lines corner to corner.
func drawDiagonals(img *image.RGBA, x0, y0, cw, ch int, r rune, fg color.RGBA) {
	if r == 0x2571 || r == 0x2573 {
		for y := range ch {
			x := (ch - 1 - y) * (cw - 1) / max(1, ch-1)
			fillBox(img, x0, y0, x, y, 1, 1, fg)
		}
	}
	if r == 0x2572 || r == 0x2573 {
		for y := range ch {
			x := y * (cw - 1) / max(1, ch-1)
			fillBox(img, x0, y0, x, y, 1, 1, fg)
		}
	}
}

// drawBlock draws the block elements U+2580 to U+259F. Fractional blocks are
// rounded to whole pixels, with at least one pixel in each dimension.
func drawBlock(img *image.RGBA, x0, y0, cw, ch int, r rune, fg, bg color.RGBA) {
	hw, hh := cw/2, ch/2
	switch {
	case r == 0x2580: // upper half
		fillBox(img, x0, y0, 0, 0, cw, hh, fg)
	case r >= 0x2581 && r <= 0x2587: // lower eighths
		n := int(r - 0x2580)
		h := max(1, (ch*n+4)/8)
		fillBox(img, x0, y0, 0, ch-h, cw, h, fg)
	case r == 0x2588: // full block
		fillBox(img, x0, y0, 0, 0, cw, ch, fg)
	case r >= 0x2589 && r <= 0x258F: // left sevenths and eighths
		n := int(0x2590 - r)
		w := max(1, (cw*n+4)/8)
		fillBox(img, x0, y0, 0, 0, w, ch, fg)
	case r == 0x2590: // right half
		fillBox(img, x0, y0, hw, 0, cw-hw, ch, fg)
	case r >= 0x2591 && r <= 0x2593: // light, medium and dark shade
		n := int(r-0x2590) * 2 // 2, 4 or 6 out of 8 parts of fg
		shade := mix(fg, bg, n, 8)
		fillBox(img, x0, y0, 0, 0, cw, ch, shade)
	case r == 0x2594: // upper eighth
		fillBox(img, x0, y0, 0, 0, cw, max(1, ch/8), fg)
	case r == 0x2595: // right eighth
		w := max(1, cw/8)
		fillBox(img, x0, y0, cw-w, 0, w, ch, fg)
	default: // U+2596 to U+259F: quadrants
		mask := quadrants[r-0x2596]
		if mask&1 != 0 {
			fillBox(img, x0, y0, 0, 0, hw, hh, fg)
		}
		if mask&2 != 0 {
			fillBox(img, x0, y0, hw, 0, cw-hw, hh, fg)
		}
		if mask&4 != 0 {
			fillBox(img, x0, y0, 0, hh, hw, ch-hh, fg)
		}
		if mask&8 != 0 {
			fillBox(img, x0, y0, hw, hh, cw-hw, ch-hh, fg)
		}
	}
}

// mix blends fg over bg with weight num/den on fg.
func mix(fg, bg color.RGBA, num, den int) color.RGBA {
	blend := func(a, b uint8) uint8 {
		return uint8((int(a)*num + int(b)*(den-num)) / den)
	}
	return color.RGBA{R: blend(fg.R, bg.R), G: blend(fg.G, bg.G), B: blend(fg.B, bg.B), A: 0xff}
}

// fillBox paints the w by h rectangle at (dx, dy) inside the cell at (x0, y0).
func fillBox(img *image.RGBA, x0, y0, dx, dy, w, h int, c color.RGBA) {
	r := image.Rect(x0+dx, y0+dy, x0+dx+w, y0+dy+h).Intersect(img.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			img.SetRGBA(x, y, c)
		}
	}
}
