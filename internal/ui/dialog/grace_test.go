package dialog

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// enterAt opens a dialog at time zero, moves the clock to each offset in turn,
// presses Enter at each one, and reports whether it was accepted (the dialog
// closed).
func enterAt(t *testing.T, mk func(Clock) Overlay, c *fakeClock, offsets ...time.Duration) []bool {
	t.Helper()
	d := mk(c.Now)
	var accepted []bool
	start := c.now
	for _, off := range offsets {
		c.now = start.Add(off)
		next, cmd := d.Update(key("enter"))
		_, answered := answer(cmd)
		accepted = append(accepted, answered)
		if next == nil {
			break
		}
	}
	return accepted
}

func TestGraceIgnoresEnterAtOpen(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	got := enterAt(t, func(clk Clock) Overlay { return NewUnsavedChanges(clk, "a", nil, testOv) }, c, 0)
	if len(got) != 1 || got[0] {
		t.Fatalf("Enter at open was accepted, want ignored")
	}
}

func TestGraceEndsAt425Milliseconds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		offset   time.Duration
		accepted bool
	}{
		{"one millisecond before the end is ignored", 424 * time.Millisecond, false},
		{"exactly 425 milliseconds is accepted", 425 * time.Millisecond, true},
		{"well after is accepted", 2 * time.Second, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := newFakeClock()
			got := enterAt(t, func(clk Clock) Overlay { return NewUnsavedChanges(clk, "a", nil, testOv) }, c, tt.offset)
			if got[0] != tt.accepted {
				t.Fatalf("accepted = %v at %v, want %v", got[0], tt.offset, tt.accepted)
			}
		})
	}
}

func TestGraceIgnoredEnterExtendsWindow(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	// Opened at 0, the window ends at 425ms. Enter at 100ms is ignored and
	// extends it to 525ms. Enter at 500ms is ignored and extends it to 925ms.
	// Enter at 900ms is ignored and extends it to 1325ms. Enter at 1325ms is
	// accepted.
	got := enterAt(t, func(clk Clock) Overlay { return NewUnsavedChanges(clk, "a", nil, testOv) }, c,
		100*time.Millisecond, 500*time.Millisecond, 900*time.Millisecond, 1325*time.Millisecond)
	want := []bool{false, false, false, true}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Enter %d accepted = %v, want %v (all %v)", i, got[i], want[i], got)
		}
	}
}

func TestGraceExtensionCapsAt1500Milliseconds(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	// Repeated ignored Enters keep pushing the window out, but it never runs past
	// 1500ms after opening. Every Enter from 0ms to 1400ms is ignored, and the
	// one at 1500ms is accepted.
	var offsets []time.Duration
	for ms := 0; ms <= 1450; ms += 100 {
		offsets = append(offsets, time.Duration(ms)*time.Millisecond)
	}
	offsets = append(offsets, 1500*time.Millisecond)
	got := enterAt(t, func(clk Clock) Overlay { return NewUnsavedChanges(clk, "a", nil, testOv) }, c, offsets...)
	for i, ok := range got[:len(got)-1] {
		if ok {
			t.Fatalf("Enter %d at %v was accepted inside the grace window", i, offsets[i])
		}
	}
	if !got[len(got)-1] {
		t.Fatalf("Enter at 1500ms was ignored, the window should end at the cap")
	}
}

func TestGraceDoesNotAffectLettersEscAndClicks(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	d := NewUnsavedChanges(c.Now, "a", nil, testOv)
	_, cmd := d.Update(key("d"))
	r, ok := answer(cmd)
	if !ok || r.Choice != ChoiceDiscard {
		t.Fatalf("a letter inside the grace window answered %+v, want Discard", r)
	}

	e := NewUnsavedChanges(c.Now, "a", nil, testOv)
	_, cmd = e.Update(key("esc"))
	if r, _ := answer(cmd); r.Choice != ChoiceCancel {
		t.Fatalf("Esc inside the grace window answered %v, want Cancel", r.Choice)
	}

	f := NewUnsavedChanges(c.Now, "a", nil, testOv)
	g := draw(f, 80, 24)
	x, y := mustFind(t, g, "Discard")
	_, cmd = f.Update(click(x, y))
	if r, _ := answer(cmd); r.Choice != ChoiceDiscard {
		t.Fatalf("a click inside the grace window answered %v, want Discard", r.Choice)
	}
}

func TestGraceAppliesToEveryDialog(t *testing.T) {
	t.Parallel()
	makers := map[string]func(Clock) Overlay{
		"unsaved": func(clk Clock) Overlay { return NewUnsavedChanges(clk, "a", nil, testOv) },
		"trash":   func(clk Clock) Overlay { return NewConfirmTrash(clk, "a", nil, testOv) },
		"saveas":  func(clk Clock) Overlay { return NewSaveAs(clk, "a.txt", nil, PathChecks{}, testOv) },
		"open":    func(clk Clock) Overlay { return NewOpenFolder(clk, []string{"/x"}, "/x", nil, nil, testOv) },
		"error":   func(clk Clock) Overlay { return NewErrorDialog(clk, "boom", nil, testOv) },
	}
	for name, mk := range makers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := newFakeClock()
			got := enterAt(t, mk, c, 0, 425*time.Millisecond)
			if got[0] {
				t.Fatalf("Enter at open was accepted")
			}
			if !got[1] {
				t.Fatalf("Enter after the grace window was ignored")
			}
		})
	}
}

func TestNilClockUsesRealTime(t *testing.T) {
	t.Parallel()
	d := NewErrorDialog(nil, "boom", nil, testOv)
	// A dialog opened just now is still inside its grace window.
	if _, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatalf("Enter on a dialog opened with the real clock was accepted at once")
	}
}
