package space

// Rect is a rectangle of frame cells: its first column X and first row Y, and
// its width W and height H. The layout, the hit map and the overlay kits place
// their boxes in this one geometry type.
type Rect struct{ X, Y, W, H int }

// Contains reports whether cell (x, y) is inside r.
func (r Rect) Contains(x, y int) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}
