package prompt

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

var (
	keyUp   = tea.KeyPressMsg{Code: tea.KeyUp}
	keyDown = tea.KeyPressMsg{Code: tea.KeyDown}
)

// Up walks back through the history, Down forward; what was being typed comes back below the
// newest entry.
func TestLineHistoryWalk(t *testing.T) {
	t.Parallel()
	l := NewLine(testStyles(), LineOptions{Label: ":", History: []string{"s/one/X/", "g/two/d", "w"}})
	typeText(l, "dra")
	steps := []struct {
		key  tea.KeyPressMsg
		want string
	}{
		{keyUp, "w"},
		{keyUp, "g/two/d"},
		{keyUp, "s/one/X/"},
		{keyUp, "s/one/X/"}, // the oldest stays
		{keyDown, "g/two/d"},
		{keyDown, "w"},
		{keyDown, "dra"}, // the draft is back
		{keyDown, "dra"},
		{keyUp, "w"},
	}
	for i, st := range steps {
		l.Update(st.key)
		if got := l.Value(); got != st.want {
			t.Fatalf("step %d: value = %q, want %q", i, got, st.want)
		}
	}
	// the entry can be edited and submitted
	typeText(l, "q")
	_, cmd := l.Update(key("enter"))
	if d, ok := emitted(cmd); !ok || d.Text != "wq" {
		t.Fatalf("submitted %+v", d)
	}
}

// An empty history leaves Up and Down alone, and a prefilled text is replaced by an entry.
func TestLineHistoryEdges(t *testing.T) {
	t.Parallel()
	l := NewLine(testStyles(), LineOptions{Label: ":", Input: "keep"})
	l.Update(keyUp)
	l.Update(keyDown)
	if got := l.Value(); got != "keep" {
		t.Fatalf("value = %q with no history", got)
	}
	l = NewLine(testStyles(), LineOptions{Label: "/", Input: "prefill", SelectAll: true, History: []string{"old"}})
	l.Update(keyUp)
	if got := l.Value(); got != "old" {
		t.Fatalf("value = %q, want the entry over the prefill", got)
	}
	l.Update(keyDown)
	if got := l.Value(); got != "prefill" {
		t.Fatalf("value = %q, want the prefill back", got)
	}
	// a change through the history is an edit for OnChange
	var seen []string
	l = NewLine(testStyles(), LineOptions{Label: "/", History: []string{"a", "b"}, OnChange: func(s string) { seen = append(seen, s) }})
	l.Update(keyUp)
	l.Update(keyUp)
	if len(seen) != 2 || seen[0] != "b" || seen[1] != "a" {
		t.Fatalf("OnChange saw %v", seen)
	}
}

func TestLineHistoryReplacesThePrefill(t *testing.T) {
	t.Parallel()
	l := NewLine(testStyles(), LineOptions{Label: ":", Input: "'<,'>", SelectAll: true, History: []string{"s/a/b/"}})
	l.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if got := l.Value(); got != "s/a/b/" {
		t.Fatalf("Value = %q", got)
	}
	l.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if got := l.Value(); got != "'<,'>" {
		t.Fatalf("Value after Down = %q, want the prefill back", got)
	}
}
