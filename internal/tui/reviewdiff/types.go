package reviewdiff

import (
	"image"

	"github.com/tbxark/gvim/diff"
)

// Scope selects the two Git snapshots compared in Review Diff Mode.
type Scope uint8

const (
	ScopeWorkingTree Scope = iota
	ScopeStaged
	ScopeUnstaged
)

func (s Scope) Label() string {
	switch s {
	case ScopeStaged:
		return "Staged"
	case ScopeUnstaged:
		return "Unstaged"
	default:
		return "Working Tree"
	}
}

// ImageSide holds the bounded raster and original dimensions of one revision.
type ImageSide struct {
	Pixels image.Image
	Size   image.Point
	Error  string
}

// FileStatus classifies a changed path shown in the Changes sidebar.
type FileStatus uint8

const (
	StatusModified FileStatus = iota + 1
	StatusAdded
	StatusDeleted
	StatusRenamed
	StatusUntracked
	StatusConflicted
)

// Badge returns the single-character status glyph shown beside a changed file.
func (s FileStatus) Badge() string {
	switch s {
	case StatusModified:
		return "M"
	case StatusAdded:
		return "A"
	case StatusDeleted:
		return "D"
	case StatusRenamed:
		return "R"
	case StatusUntracked:
		return "?"
	case StatusConflicted:
		return "!"
	default:
		return " "
	}
}

// FileEntry is one changed file listed in the Changes sidebar.
type FileEntry struct {
	Path     string // repository-relative slash path
	OldPath  string // original path for renames, empty otherwise
	Status   FileStatus
	Added    int
	Deleted  int
	Binary   bool
	StatOnly bool // true when line counts are unknown (e.g. binary or huge untracked)
	Scope    Scope
}

// FileDiff is the old/new content and diff metadata of one changed file.
// Holding both sides allows instant switching between Unified and Split views,
// instant hunk-context expansion and full-file toggles, and accurate multi-line
// syntax highlighting without extra git round-trips.
type FileDiff struct {
	Path      string
	OldPath   string
	Status    FileStatus
	BaseRef   string
	OldLines  []string
	NewLines  []string
	Hunks     []diff.Hunk
	Binary    bool
	Truncated bool
	Message   string // non-empty for binary, too-large, or empty-diff notes
	Scope     Scope
	Images    *[2]ImageSide // old and new; an empty side denotes a missing file
}

// ViewMode selects how the right-hand diff pane renders changes.
type ViewMode uint8

const (
	ViewUnified ViewMode = iota
	ViewSplit
)

// Label returns the short button label for the diff header bar.
func (v ViewMode) Label() string {
	if v == ViewSplit {
		return "Split"
	}
	return "Unified"
}

// PaneFocus identifies which pane of Review Diff Mode owns keyboard navigation.
type PaneFocus uint8

const (
	FocusFiles PaneFocus = iota
	FocusDiff
)

// RowKind classifies a rendered diff row or split-side cell.
type RowKind uint8

const (
	RowContext RowKind = iota
	RowAdd
	RowDelete
	RowModify
	RowHunkHeader
	RowInfo
	RowFiller
)

// ByteRange is a half-open [Start, End) byte interval inside one line's text.
type ByteRange struct {
	Start int
	End   int
}

// SearchMatch identifies one substring match inside a logical diff row.
type SearchMatch struct {
	RowIdx    int // logical row index in unified or split slice
	Side      int // 0 for unified or split-left, 1 for split-right
	ByteStart int
	ByteEnd   int
}

// DefaultContextLines is the number of unchanged lines shown above and below
// each hunk in hunk-only mode (matching `git diff -U3`).
const DefaultContextLines = 3

// ExpandStep is the number of hidden lines revealed per expand gesture on a
// hunk separator row.
const ExpandStep = 20

// MinSplitWidth is the minimum diff-pane width (in terminal cells) required to
// render Side-by-Side mode. Narrower viewports fall back to Unified mode.
const MinSplitWidth = 60
