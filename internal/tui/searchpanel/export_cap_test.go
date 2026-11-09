package searchpanel

import "testing"

// TestExportItemsCapped bounds the quickfix list independently of the project search's
// 10000-match DefaultLimit and reports the omitted match count.
func TestExportItemsCapped(t *testing.T) {
	p := &Panel{fileMatches: []FileMatches{
		{Path: "/w/a.go", Name: "a.go", Matches: []Match{
			{Line: 1, Col: 1, Len: 3, Text: "foo one"},
			{Line: 2, Col: 1, Len: 3, Text: "foo two"},
		}},
		{Path: "/w/b.go", Name: "b.go", Matches: []Match{
			{Line: 3, Col: 1, Len: 3, Text: "foo three"},
		}},
	}}
	items, omitted := p.ExportItemsCapped(2)
	if omitted != 1 {
		t.Fatalf("omitted = %d, want 1", omitted)
	}
	if len(items) != 3 { // one header + two match rows (cap counts matches)
		t.Fatalf("items = %+v, want one header and two matches", items)
	}
	matchRows := 0
	for _, it := range items {
		if !it.Header {
			matchRows++
		}
	}
	if matchRows != 2 {
		t.Fatalf("match rows = %d, want 2", matchRows)
	}
	all := p.ExportItems()
	if len(all) != 5 {
		t.Fatalf("unbounded export = %d rows, want 5", len(all))
	}
	if again, omitted := p.ExportItemsCapped(0); len(again) != 5 || omitted != 0 {
		t.Fatalf("limit 0 = %d rows omitted %d, want all 5", len(again), omitted)
	}
}
