package explorer

// Width is the configured width of the explorer column: a percentage of the
// terminal width or a count of columns. It is Fresh's ExplorerWidth
// (crates/fresh-editor-core/src/config.rs) and the same two forms a config
// writes ("30%" or "24").
type Width struct {
	Mode  WidthMode
	Value int
}

// WidthMode is how a Width is measured.
type WidthMode int

const (
	// WidthAutomatic is the zero Width: no width is configured, so the frame
	// keeps its own automatic width.
	WidthAutomatic WidthMode = iota
	// WidthPercent measures Value percent of the terminal width.
	WidthPercent
	// WidthColumns measures Value terminal columns.
	WidthColumns
)

// PercentWidth returns a width of p percent of the terminal, clamped to Fresh's
// 0..100 range. A negative percentage reads as 0.
func PercentWidth(p int) Width {
	return Width{Mode: WidthPercent, Value: min(max(p, 0), 100)}
}

// ColumnsWidth returns a width of n columns. A negative count reads as 0.
func ColumnsWidth(n int) Width {
	return Width{Mode: WidthColumns, Value: max(n, 0)}
}

// Cols is the width in columns at a frame frameWidth columns wide, before the
// frame's own clamps: a percentage (integer division, as Fresh's to_cols), the
// column count, or 0 for the automatic width.
func (w Width) Cols(frameWidth int) int {
	switch w.Mode {
	case WidthPercent:
		return max(frameWidth, 0) * w.Value / 100
	case WidthColumns:
		return w.Value
	}
	return 0
}

// WithCols is the width of the same form that renders as close to cols as it can
// at a frame frameWidth columns wide: Fresh's ExplorerWidth::with_cols. A column
// count is exact; a percentage keeps its form, choosing whichever of the two
// percents bracketing cols renders nearer it (a tie goes to the wider). An
// automatic width becomes a column count, since there is no configured form to
// preserve.
func (w Width) WithCols(cols, frameWidth int) Width {
	cols = min(max(cols, 0), max(frameWidth, 0))
	switch w.Mode {
	case WidthPercent:
		if frameWidth <= 0 {
			return w
		}
		lo := min(cols*100/frameWidth, 100)
		hi := min((cols*100+frameWidth-1)/frameWidth, 100)
		miss := func(pct int) int { return absValue(PercentWidth(pct).Cols(frameWidth) - cols) }
		if miss(lo) < miss(hi) {
			return PercentWidth(lo)
		}
		return PercentWidth(hi)
	}
	return ColumnsWidth(cols)
}

// absValue is the absolute value of n.
func absValue(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// Side is the frame side the explorer column sits on.
type Side int

const (
	// SideLeft is the left edge of the frame: the default.
	SideLeft Side = iota
	// SideRight is the right edge of the frame.
	SideRight
)

// String is the name a config writes for the side.
func (s Side) String() string {
	if s == SideRight {
		return "right"
	}
	return "left"
}

// ParseSide parses a config side name. ok is false for a name Fresh does not
// know; the caller reports it as a config diagnostic.
func ParseSide(name string) (Side, bool) {
	switch name {
	case "left":
		return SideLeft, true
	case "right":
		return SideRight, true
	}
	return SideLeft, false
}

// Other returns the opposite side: what toggle_file_explorer_side switches to.
func (s Side) Other() Side {
	if s == SideRight {
		return SideLeft
	}
	return SideRight
}

// Options are the explorer's navigation and display settings, as Fresh's
// file_explorer section defines them. The zero value keeps the frame's own
// automatic width, the left side, and no navigation behaviour turned on; the
// app fills it from the config, whose defaults are Fresh's
// (DefaultOptions).
type Options struct {
	// Width is file_explorer.width. WidthAutomatic keeps the frame's automatic
	// width.
	Width Width
	// Side is file_explorer.side: the frame edge the column sits on.
	Side Side
	// FollowActiveBuffer is file_explorer.follow_active_buffer: reveal the
	// active file as tabs change.
	FollowActiveBuffer bool
	// CompactDirectories is file_explorer.compact_directories: draw a chain of
	// single-child folders as one row.
	CompactDirectories bool
	// AutoOpenOnLastBufferClose is
	// file_explorer.auto_open_on_last_buffer_close: focus the explorer when the
	// last buffer closes.
	AutoOpenOnLastBufferClose bool
}

// DefaultOptions returns Fresh's defaults for the non-width explorer options:
// compact directories on, auto-open on the last buffer close on, following off,
// left side. The width stays automatic, so a frame built without config keeps
// its own default width (the app always passes the configured width).
func DefaultOptions() Options {
	return Options{
		Side:                      SideLeft,
		CompactDirectories:        true,
		AutoOpenOnLastBufferClose: true,
	}
}
