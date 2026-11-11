package frame

// Style is one look of the frame: the decoration it reserves around the workspace and
// the glyphs the painter draws in it. ComputeLayout reserves the cells a Style asks
// for and the root paints the same glyphs, so another look is another Style rather
// than another set of branches in the geometry.
type Style interface {
	// Chrome is the decoration the style draws around the workspace.
	Chrome() Chrome
}

// Chrome is the box a Style draws around the workspace: the border cells the layout
// reserves and the glyphs the painter puts in them. The zero Chrome draws nothing.
type Chrome struct {
	Border bool

	Horizontal  string
	Vertical    string
	TopLeft     string
	TopRight    string
	BottomLeft  string
	BottomRight string
	// TopJoin and BottomJoin sit over the columns of the vertical separators inside
	// the box: the explorer's edge and the borders between the panes.
	TopJoin    string
	BottomJoin string
	// LeftJoin and RightJoin sit on the side border where the explorer's section
	// divider runs into it.
	LeftJoin  string
	RightJoin string
}

// Classic is the unified workbench layout: the whole workspace in one character-cell
// border, after Lazygit and Turbo Vision.
var Classic Style = classic{}

type classic struct{}

func (classic) Chrome() Chrome {
	return Chrome{
		Border:      true,
		Horizontal:  "─",
		Vertical:    "│",
		TopLeft:     "┌",
		TopRight:    "┐",
		BottomLeft:  "└",
		BottomRight: "┘",
		TopJoin:     "┬",
		BottomJoin:  "┴",
		LeftJoin:    "├",
		RightJoin:   "┤",
	}
}

// chrome is the decoration in draws: its Style's, or Classic's when it names none.
func (in Input) chrome() Chrome {
	if in.Style == nil {
		return Classic.Chrome()
	}
	return in.Style.Chrome()
}
