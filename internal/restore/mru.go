package restore

import (
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/session"
)

// applySessionMru restores the saved most-recently-used order after the files are
// open. restoredByFile maps each saved Files entry to its restored tab, or -1 when
// the entry was skipped. A saved reference that no longer names an opened file (missing,
// renamed away, closed, or hand-edited) is skipped, and the remaining references keep
// their relative order; an old session without MRU keeps the activation-derived order.
func applySessionMru(ed *editor.Editor, s session.Session, restoredByFile []int) {
	if len(s.MRU) == 0 {
		return
	}
	used := make([]bool, len(s.Files))
	order := make([]int, 0, len(s.MRU))
	for _, ref := range s.MRU {
		fileIndex := matchMruFile(s.Files, used, ref)
		if fileIndex < 0 {
			continue
		}
		used[fileIndex] = true
		if tab := restoredByFile[fileIndex]; tab >= 0 {
			order = append(order, tab)
		}
	}
	if len(order) > 0 {
		ed.SetRecentTabIndices(order)
	}
}

// matchMruFile returns the first saved file not used yet that ref names, or -1. A
// named reference matches by path; a pathless one by label, so an untitled buffer does
// not adopt a named tab (or the reverse).
func matchMruFile(files []session.FileState, used []bool, ref session.MruFile) int {
	for i, f := range files {
		if used[i] {
			continue
		}
		if f.Path != ref.Path {
			continue
		}
		if f.Path == "" && f.Label != ref.Label {
			continue
		}
		return i
	}
	return -1
}
