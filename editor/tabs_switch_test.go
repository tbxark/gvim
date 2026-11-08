package editor

import (
	"errors"
	"testing"
)

func TestSwitchToPreviousTabToggles(t *testing.T) {
	t.Parallel()
	e := withTabs("a.txt", "b.txt", "c.txt")
	run(t, e, "next_buffer") // a -> b, previous is a
	run(t, e, "next_buffer") // b -> c, previous is b
	run(t, e, "switch_to_previous_tab")
	if got := e.Active().Name(); got != "b.txt" {
		t.Fatalf("after one switch back active = %s, want b.txt", got)
	}
	run(t, e, "switch_to_previous_tab")
	if got := e.Active().Name(); got != "c.txt" {
		t.Fatalf("after a second switch active = %s, want c.txt", got)
	}
	if want := "Switched to c.txt"; e.Status() != want {
		t.Fatalf("status = %q, want %q", e.Status(), want)
	}
}

func TestSwitchToPreviousTabWithNoOtherTabFails(t *testing.T) {
	t.Parallel()
	e := withTabs("a.txt")
	if r := e.Do("switch_to_previous_tab", nil); !errors.Is(r.Err, ErrNoPreviousTab) {
		t.Fatalf("err = %v, want ErrNoPreviousTab", r.Err)
	}
	if got := e.Active().Name(); got != "a.txt" {
		t.Fatalf("active = %s, want a.txt", got)
	}
}

func TestSwitchToPreviousTabAfterItWasClosedFails(t *testing.T) {
	t.Parallel()
	e := withTabs("a.txt", "b.txt")
	run(t, e, "next_buffer", "prev_buffer") // now on a, previous is b
	if err := e.CloseTab(1, false); err != nil {
		t.Fatalf("CloseTab: %v", err)
	}
	if r := e.Do("switch_to_previous_tab", nil); !errors.Is(r.Err, ErrNoPreviousTab) {
		t.Fatalf("err = %v, want ErrNoPreviousTab", r.Err)
	}
}

func TestSwitchToTabByNameActivatesTheFirstMatch(t *testing.T) {
	t.Parallel()
	e := withTabs("a.txt", "b.txt", "b.txt")
	r := e.Do("switch_to_tab_by_name", map[string]any{"name": "b.txt"})
	if r.Err != nil {
		t.Fatalf("switch_to_tab_by_name: %v", r.Err)
	}
	if e.Active() != e.docs[1] {
		t.Fatal("the first b.txt tab is not active")
	}
	if want := "Switched to b.txt"; e.Status() != want {
		t.Fatalf("status = %q, want %q", e.Status(), want)
	}
}

func TestSwitchToTabByNameFailures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name string
		Args map[string]any
	}{
		{Name: "no name given", Args: nil},
		{Name: "a name that is not open", Args: map[string]any{"name": "zzz.txt"}},
		{Name: "a name that is not a string", Args: map[string]any{"name": 3}},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := withTabs("a.txt", "b.txt")
			run(t, e, "next_buffer") // on b.txt
			r := e.Do("switch_to_tab_by_name", tt.Args)
			if r.Err == nil || errors.Is(r.Err, ErrUnknownAction) {
				t.Fatalf("err = %v, want a failure that is not ErrUnknownAction", r.Err)
			}
			if got := e.Active().Name(); got != "b.txt" {
				t.Fatalf("active = %s, want b.txt unchanged", got)
			}
		})
	}
}
