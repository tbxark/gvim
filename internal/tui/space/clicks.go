package space

import "time"

// DoubleClickWindow is the time within which presses in one cell count as a
// double or triple press (blueprint section 3.4).
const DoubleClickWindow = 500 * time.Millisecond

// ClickCounter counts presses in one cell. The count runs 1, 2, 3, then 1, and a
// press restarts it when it is outside the cell or the window.
type ClickCounter struct {
	at   time.Time
	x, y int
	n    int
}

// Press records a left press at (x, y) at time at using the default window and
// returns its count.
func (c *ClickCounter) Press(at time.Time, x, y int) int {
	return c.PressWindow(at, x, y, DoubleClickWindow)
}

// PressWindow records a left press at (x, y) at time at with an explicit window
// and returns its count. A window <= 0 uses the default.
func (c *ClickCounter) PressWindow(at time.Time, x, y int, window time.Duration) int {
	if window <= 0 {
		window = DoubleClickWindow
	}
	if c.n > 0 && at.Sub(c.at) <= window && c.x == x && c.y == y {
		c.n = c.n%3 + 1
	} else {
		c.n = 1
	}
	c.at, c.x, c.y = at, x, y
	return c.n
}

// Thumb sizes a scrollbar thumb for lines rows in a window of h rows, with the
// first window row top. The thumb is at least one row long and at most the
// window. Its start runs over 0..h-length as top runs over 0..lines-h, so it
// reaches the bottom of the track when the last page is shown.
func Thumb(lines, h, top int) (start, length int) {
	return ThumbMax(lines, h, top, lines-h)
}

// ThumbMax is Thumb for a view whose last page starts at row maxTop, as when the
// rows are wrapped and fewer than lines-h of them scroll.
func ThumbMax(lines, h, top, maxTop int) (start, length int) {
	if lines <= 0 || h <= 0 {
		return 0, 0
	}
	length = min(h, max(1, int((int64(h)*int64(h)+int64(lines)/2)/int64(lines))))
	if maxTop > 0 {
		start = int(int64(min(max(top, 0), maxTop)) * int64(h-length) / int64(maxTop))
	}
	return start, length
}

// Checkbox is the box before an option name.
func Checkbox(on bool) string {
	if on {
		return "[x] "
	}
	return "[ ] "
}
