package explorer

import "time"

// doubleClickWindow is the time within which presses in one cell count as a
// double or triple press (blueprint section 3.4).
const doubleClickWindow = 500 * time.Millisecond

// clickCounter counts presses in one cell. The count runs 1, 2, 3, then 1, and a
// press restarts it when it is outside the cell or the window. It is the same
// rule as the root's, kept here so the panel does not import the root.
type clickCounter struct {
	at   time.Time
	x, y int
	n    int
}

// press records a left press at (x, y) at time at and returns its count.
func (c *clickCounter) press(at time.Time, x, y int) int {
	if c.n > 0 && at.Sub(c.at) <= doubleClickWindow && c.x == x && c.y == y {
		c.n = c.n%3 + 1
	} else {
		c.n = 1
	}
	c.at, c.x, c.y = at, x, y
	return c.n
}
