package bundled_test

import (
	"fmt"
	"slices"
	"testing"
)

// history reads the history the open prompt was given.
func (v *vi) history() []string {
	v.t.Helper()
	o := v.host.UI().Last("input")
	if o == nil || !o.Open {
		v.t.Fatal("no prompt is open")
	}
	return o.Input.History
}

// The : and / prompts offer what was typed there before: Up and Down walk it.
func TestViPromptHistory(t *testing.T) {
	t.Parallel()
	v := newVi(t, "one two one\nthree\n")
	v.keys(":«s/one/X/»")
	v.keys("/«th»")
	v.keys(":«2»")
	v.keys(":«s/one/X/»") // typed again: it moves to the end

	v.keys(":")
	if got, want := v.history(), []string{"2", "s/one/X/"}; !slices.Equal(got, want) {
		t.Fatalf("ex history = %q, want %q", got, want)
	}
	v.keys("<in!>")
	v.keys("/")
	if got, want := v.history(), []string{"th"}; !slices.Equal(got, want) {
		t.Fatalf("search history = %q, want %q", got, want)
	}
	v.keys("<in!>")
	v.keys("?")
	if got, want := v.history(), []string{"th"}; !slices.Equal(got, want) {
		t.Fatalf("backward search history = %q, want the search history", got)
	}
	v.keys("<in!>")
	// a cancelled prompt and an empty line leave nothing
	v.keys(":«»")
	v.keys(":")
	if got := v.history(); !slices.Equal(got, []string{"2", "s/one/X/"}) {
		t.Fatalf("ex history = %q after an empty line", got)
	}
}

// The history keeps the last hundred lines.
func TestViPromptHistoryIsBounded(t *testing.T) {
	t.Parallel()
	v := newVi(t, "a\n")
	for i := range 130 {
		v.keys(fmt.Sprintf(":«%d»", i+1))
	}
	v.keys(":")
	h := v.history()
	if len(h) != 100 || h[0] != "31" || h[99] != "130" {
		t.Fatalf("history has %d lines, from %q to %q", len(h), h[0], h[len(h)-1])
	}
}
