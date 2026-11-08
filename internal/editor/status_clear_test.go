package editor

import "testing"

func TestClearStatusDropsTheActiveTabsMessageOnly(t *testing.T) {
	t.Parallel()
	e := New(Options{Rows: 10, Cols: 40})
	e.Do("new", nil)
	first := e.Status()
	if first == "" {
		t.Fatal("setup: a new buffer sets no status")
	}
	e.Do("new", nil)
	e.ClearStatus()
	if got := e.Status(); got != "" {
		t.Errorf("status after ClearStatus = %q, want none", got)
	}
	e.ActivateTab(0)
	if got := e.Status(); got != first {
		t.Errorf("the other tab's status = %q, want %q kept", got, first)
	}
}

func TestClearStatusOfTheEmptyEditor(t *testing.T) {
	t.Parallel()
	e := New(Options{Empty: true})
	e.setStatus("No open document")
	e.ClearStatus()
	if got := e.Status(); got != "" {
		t.Errorf("empty editor status = %q after ClearStatus, want none", got)
	}
}
