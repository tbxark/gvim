package explorer

import (
	"reflect"
	"testing"

	"github.com/tbxark/gvim/internal/ui/style"
)

func TestSelectAtSelectsTheRowWithoutOpeningIt(t *testing.T) {
	t.Parallel()
	tr := opsTree(2) // the cursor is on b.txt
	p := New(tr, style.Styles{}, nil)
	// Row 0 is the folder a/. A press would toggle it, and a right press must not.
	before := len(tr.rows)
	got, ok := p.SelectAt(testGeometry, 3, bodyTop)
	if !ok || !reflect.DeepEqual(got.Paths, []string{"/r/a"}) || got.Root {
		t.Fatalf("SelectAt on a folder = %+v, %v", got, ok)
	}
	if sel, _ := tr.Selected(); sel.Path != "/r/a" {
		t.Errorf("the cursor is on %q, want /r/a", sel.Path)
	}
	if len(tr.rows) != before {
		t.Errorf("the right press toggled the folder: %d rows, was %d", len(tr.rows), before)
	}
}

func TestSelectAtTheBlankAreaIsTheRoot(t *testing.T) {
	t.Parallel()
	p := New(opsTree(1), style.Styles{}, nil)
	got, ok := p.SelectAt(testGeometry, 3, bodyTop+5)
	if !ok || !got.Root || len(got.Paths) != 0 {
		t.Fatalf("SelectAt on the blank area = %+v, %v", got, ok)
	}
}

func TestSelectAtOutsideThePanelIsNotATarget(t *testing.T) {
	t.Parallel()
	p := New(opsTree(1), style.Styles{}, nil)
	for _, pt := range [][2]int{{testGeometry.Width + 3, 4}, {3, 0}} {
		if got, ok := p.SelectAt(testGeometry, pt[0], pt[1]); ok {
			t.Errorf("SelectAt(%d, %d) = %+v, want no target", pt[0], pt[1], got)
		}
	}
}

func TestSelectAtKeepsSeveralPicksOnlyForAPickedRow(t *testing.T) {
	t.Parallel()
	p := New(opsTree(0), style.Styles{}, nil)
	p, _ = doAction(t, p, "file_explorer_toggle_select")
	p, _ = doAction(t, p, "file_explorer_down")
	p, _ = doAction(t, p, "file_explorer_down")
	p, _ = doAction(t, p, "file_explorer_toggle_select")
	// A press on a picked row keeps both picks, in display order.
	got, ok := p.SelectAt(testGeometry, 3, bodyTop+2)
	if !ok || !reflect.DeepEqual(got.Paths, []string{"/r/a", "/r/b.txt"}) {
		t.Fatalf("SelectAt on a picked row = %+v, %v", got, ok)
	}
	// A press on a row that is not picked drops them.
	got, ok = p.SelectAt(testGeometry, 3, bodyTop+1)
	if !ok || !reflect.DeepEqual(got.Paths, []string{"/r/a/x.txt"}) {
		t.Fatalf("SelectAt on another row = %+v, %v", got, ok)
	}
	if n := len(p.targets()); n != 1 {
		t.Errorf("%d targets after the picks were dropped, want 1", n)
	}
}
