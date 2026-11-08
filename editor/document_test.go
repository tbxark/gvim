package editor

import "testing"

// TestMemoChangesComputesOncePerTextAndSavedText checks that the change marks of a
// repaint are found again only after the text or the saved text changed.
func TestMemoChangesComputesOncePerTextAndSavedText(t *testing.T) {
	t.Parallel()
	e, _ := openFixture(t, Options{}, "one\ntwo\n")
	d := e.Active()
	calls := 0
	marks := func() (bar, scroll map[int]bool) {
		calls++
		return map[int]bool{calls: true}, nil
	}
	steps := []struct {
		name      string
		do        func()
		wantCalls int
	}{
		{"first call computes", func() {}, 1},
		{"unchanged text is kept", func() {}, 1},
		{"an edit recomputes", func() {
			if r := e.Do("insert_char", map[string]any{"text": "x"}); r.Err != nil {
				t.Fatal(r.Err)
			}
		}, 2},
		{"unchanged again is kept", func() {}, 2},
		{"undo recomputes", func() { run(t, e, "undo") }, 3},
		{"redo and a save changes the saved text", func() { run(t, e, "redo", "save") }, 4},
	}
	for _, s := range steps {
		s.do()
		bar, _ := d.MemoChanges(marks)
		if calls != s.wantCalls || !bar[calls] {
			t.Fatalf("%s: compute ran %d times, want %d (bar %v)", s.name, calls, s.wantCalls, bar)
		}
	}
}
