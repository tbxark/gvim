package ui

import "testing"

// TestClearSearchEmptiesTheFindBar checks that clear_search, which the editor runs on the
// document, also clears the search text of the find bar, so its highlights go with it.
func TestClearSearchEmptiesTheFindBar(t *testing.T) {
	m, _ := newSearchModel(t, "foo bar")
	runAction(t, m, "find")
	typeText(m, "foo")
	if q := m.findBar.Query(); q.Text != "foo" {
		t.Fatalf("setup: find bar query %q, want foo", q.Text)
	}
	runAction(t, m, "clear_search")
	if q := m.findBar.Query(); q.Text != "" {
		t.Errorf("find bar query after clear_search = %q, want none", q.Text)
	}
}
