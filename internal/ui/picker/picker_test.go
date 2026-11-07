package picker

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/theme"
	"github.com/tbxark/gvim/internal/ui/grid"
	"github.com/tbxark/gvim/internal/ui/style"
)

func items(texts ...string) []Item {
	out := make([]Item, len(texts))
	for i, t := range texts {
		out[i] = Item{ID: 100 + i, Text: t}
	}
	return out
}

func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	}
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

// typeText sends the characters one by one and returns the last ranking command.
func typeText(p *Picker, s string) (events []Event, rank tea.Cmd) {
	for _, c := range s {
		r := p.Update(key(string(c)))
		events = append(events, r.Events...)
		if r.Rank != nil {
			rank = r.Rank
		}
	}
	return events, rank
}

// apply runs a ranking command and gives its result to the picker.
func apply(p *Picker, rank tea.Cmd) Result {
	if rank == nil {
		return Result{}
	}
	return p.Update(rank())
}

func ids(p *Picker) []int { return p.RowIDs() }

func TestRankItems(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		query string
		items []Item
		want  []int
	}{
		{"subsequence match keeps matching items only", "abc", items("a-b-c", "xyz", "abd", "cba"), []int{100}},
		{"better score first", "main", items("domain.go", "main.go", "remains"), []int{101, 100, 102}},
		{"equal scores keep their order", "a", items("a1", "a2", "a3"), []int{100, 101, 102}},
		{"no match", "zzz", items("a", "b"), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got []int
			for _, r := range rankItems(tt.query, tt.items) {
				got = append(got, r.it.ID)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("rankItems(%q) = %v, want %v", tt.query, got, tt.want)
			}
		})
	}
	t.Run("highlight indexes are the matched runes", func(t *testing.T) {
		t.Parallel()
		rows := rankItems("ac", items("abc"))
		if len(rows) != 1 || !slices.Equal(rows[0].hl, []int{0, 2}) {
			t.Fatalf("rows = %+v", rows)
		}
	})
	t.Run("at most MaxRows rows are kept", func(t *testing.T) {
		t.Parallel()
		many := make([]Item, 5*MaxRows)
		for i := range many {
			many[i] = Item{ID: i, Text: fmt.Sprintf("item%d", i)}
		}
		if got := len(rankItems("item", many)); got != MaxRows {
			t.Fatalf("kept %d rows", got)
		}
	})
}

func TestNewClipsItemsAndRows(t *testing.T) {
	t.Parallel()
	many := make([]Item, MaxItems+10)
	for i := range many {
		many[i] = Item{ID: i, Text: "x"}
	}
	p := New(style.New(theme.Theme{}), Options{Items: many})
	if len(p.items) != MaxItems || p.Rows() != MaxRows {
		t.Fatalf("items %d rows %d, want %d and %d", len(p.items), p.Rows(), MaxItems, MaxRows)
	}
	// The picker keeps its own copy: the owner may change its slice.
	many[0].Text = "changed"
	if p.items[0].Text != "x" {
		t.Fatal("the picker shares the owner's slice")
	}
}

func TestFuzzyTypingRanksInACommand(t *testing.T) {
	t.Parallel()
	p := New(style.New(theme.Theme{}), Options{Items: items("alpha", "beta", "gamma", "alphabet"), Fuzzy: true})
	p.Init()
	if !slices.Equal(ids(p), []int{100, 101, 102, 103}) {
		t.Fatalf("opening rows = %v, the items as given", ids(p))
	}
	r := p.Update(key("b"))
	if r.Rank == nil || len(r.Events) != 0 {
		t.Fatalf("typing = %+v, want a ranking command and no event", r)
	}
	if got := ids(p); len(got) != 4 {
		t.Fatalf("Update filtered synchronously: %v", got)
	}
	apply(p, r.Rank)
	if got := ids(p); !slices.Equal(got, []int{101, 103}) && !slices.Equal(got, []int{103, 101}) {
		t.Fatalf("rows for b = %v, want beta and alphabet", got)
	}
	// Backspace to the empty query shows the first rows at once, with no command.
	r = p.Update(key("backspace"))
	if r.Rank != nil || len(ids(p)) != 4 {
		t.Fatalf("empty query: rank %v rows %v", r.Rank != nil, ids(p))
	}
}

func TestStaleRankingIsDropped(t *testing.T) {
	t.Parallel()
	p := New(style.New(theme.Theme{}), Options{Items: items("apple", "banana"), Fuzzy: true})
	_, rankA := typeText(p, "a")
	_, rankB := typeText(p, "n") // the query is "an" now
	apply(p, rankB)
	want := ids(p)
	// The slow ranking of the older query arrives last and must not replace the rows.
	apply(p, rankA)
	if !slices.Equal(ids(p), want) || !slices.Equal(want, []int{101}) {
		t.Fatalf("rows = %v, want %v (only banana matches an)", ids(p), want)
	}
	// The same after the query was cleared: a ranking in flight is dropped.
	_, rankC := typeText(p, "b")
	p.Update(key("backspace"))
	p.Update(key("backspace"))
	p.Update(key("backspace"))
	apply(p, rankC)
	if len(ids(p)) != 2 {
		t.Fatalf("a ranking of a cleared query replaced the rows: %v", ids(p))
	}
}

func TestEventsOfALivePreviewPicker(t *testing.T) {
	t.Parallel()
	p := New(style.New(theme.Theme{}), Options{Items: items("one", "two", "three"), Live: true, Preview: true})
	first := p.Init()
	if len(first.Events) != 1 || first.Events[0] != (Moved{ID: 100}) {
		t.Fatalf("Init events = %+v, want the first highlight", first.Events)
	}
	ev, rank := typeText(p, "ab")
	if rank != nil || !slices.Equal(ev, []Event{Changed{Query: "a"}, Changed{Query: "ab"}}) {
		t.Fatalf("typing = %+v rank %v", ev, rank != nil)
	}
	if got := p.Update(key("down")).Events; !slices.Equal(got, []Event{Moved{ID: 101}}) {
		t.Fatalf("down = %+v", got)
	}
	if got := p.Update(key("up")).Events; !slices.Equal(got, []Event{Moved{ID: 100}}) {
		t.Fatalf("up = %+v", got)
	}
	// A move that stays on the same item reports nothing.
	if got := p.Update(key("up")).Events; len(got) != 0 {
		t.Fatalf("up at the top = %+v", got)
	}
	// Replacing the items keeps the highlight on the item with the same ID, so nothing moved.
	if r := p.SetItems(items("zero", "one", "two")); len(r.Events) != 0 {
		t.Fatalf("SetItems events = %+v", r.Events)
	}
	// When the highlighted item is gone the first row is highlighted and that is reported.
	if r := p.SetItems([]Item{{ID: 5, Text: "five"}}); len(r.Events) != 1 || r.Events[0] != (Moved{ID: 5}) {
		t.Fatalf("SetItems events = %+v", r.Events)
	}
}

func TestNoEventsWithoutLiveOrPreview(t *testing.T) {
	t.Parallel()
	p := New(style.New(theme.Theme{}), Options{Items: items("one", "two"), Fuzzy: true})
	if ev := p.Init().Events; len(ev) != 0 {
		t.Fatalf("Init events = %+v", ev)
	}
	ev, _ := typeText(p, "t")
	if len(ev) != 0 {
		t.Fatalf("events = %+v, a plain fuzzy picker reports none", ev)
	}
	if ev := p.Update(key("down")).Events; len(ev) != 0 {
		t.Fatalf("move events = %+v", ev)
	}
}

func TestChoose(t *testing.T) {
	t.Parallel()
	list := []Item{{ID: 7, Text: "a"}, {ID: 8, Text: "b", Disabled: true}, {ID: 9, Text: "c"}}
	tests := []struct {
		name   string
		keys   []string
		want   []Event
		active bool
	}{
		{"enter on the first", []string{"enter"}, []Event{Chosen{ID: 7}}, false},
		{"enter on the third", []string{"down", "down", "enter"}, []Event{Chosen{ID: 9}}, false},
		{"a disabled item is not chosen", []string{"down", "enter"}, nil, true},
		{"navigation clamps", []string{"up", "up", "enter"}, []Event{Chosen{ID: 7}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := New(style.New(theme.Theme{}), Options{Items: list})
			var got []Event
			for _, k := range tt.keys {
				got = append(got, p.Update(key(k)).Events...)
			}
			if !slices.Equal(got, tt.want) || p.Active() != tt.active {
				t.Fatalf("events %+v active %v, want %+v %v", got, p.Active(), tt.want, tt.active)
			}
		})
	}
	t.Run("an empty picker has nothing to choose", func(t *testing.T) {
		t.Parallel()
		p := New(style.New(theme.Theme{}), Options{})
		if ev := p.Update(key("enter")).Events; len(ev) != 0 || !p.Active() {
			t.Fatalf("events %+v active %v", ev, p.Active())
		}
	})
	t.Run("a closed picker ignores input", func(t *testing.T) {
		t.Parallel()
		p := New(style.New(theme.Theme{}), Options{Items: list})
		if _, ok := p.Close().(Closed); !ok {
			t.Fatal("Close did not return Closed")
		}
		if r := p.Update(key("enter")); len(r.Events) != 0 || r.Rank != nil {
			t.Fatalf("result = %+v", r)
		}
	})
}

func TestNoQueryLineWithoutFilterOrLive(t *testing.T) {
	t.Parallel()
	p := New(style.New(theme.Theme{}), Options{Items: items("one", "two")})
	r := p.Update(key("x"))
	if r.Rank != nil || len(r.Events) != 0 || p.Query() != "" {
		t.Fatalf("a plain picker took text: %+v query %q", r, p.Query())
	}
	g := grid.New(40, 12)
	p.Draw(g)
	if last := strings.TrimSpace(g.Plain()[11]); last != "" {
		t.Fatalf("a plain picker painted a query line: %q", last)
	}
}

func TestSetItemsKeepsTheHighlightedItem(t *testing.T) {
	t.Parallel()
	p := New(style.New(theme.Theme{}), Options{Items: items("a", "b", "c")})
	p.Update(key("down"))
	if id, _ := p.SelectedID(); id != 101 {
		t.Fatalf("selected %d", id)
	}
	p.SetItems([]Item{{ID: 105, Text: "x"}, {ID: 101, Text: "b"}, {ID: 106, Text: "y"}})
	if id, _ := p.SelectedID(); id != 101 {
		t.Fatalf("after SetItems selected %d, want 101", id)
	}
	p.SetItems([]Item{{ID: 1, Text: "p"}, {ID: 2, Text: "q"}})
	if id, _ := p.SelectedID(); id != 1 {
		t.Fatalf("the highlighted item is gone: selected %d, want the first", id)
	}
	p.SetItems(nil)
	if _, ok := p.SelectedID(); ok || p.Rows() != 0 {
		t.Fatal("an empty list still has a highlight")
	}
}

func TestSetItemsOfAFuzzyPickerRanksTheNewItems(t *testing.T) {
	t.Parallel()
	p := New(style.New(theme.Theme{}), Options{Items: items("alpha", "beta"), Fuzzy: true, InitialQuery: "al"})
	init := p.Init()
	if init.Rank == nil {
		t.Fatal("the opening query was not ranked")
	}
	apply(p, init.Rank)
	if !slices.Equal(ids(p), []int{100}) {
		t.Fatalf("rows = %v", ids(p))
	}
	r := p.SetItems([]Item{{ID: 1, Text: "alpine"}, {ID: 2, Text: "zeta"}, {ID: 3, Text: "alder"}})
	if r.Rank == nil {
		t.Fatal("new items of a filtered picker were not ranked")
	}
	// The old rows stay until the result arrives.
	if !slices.Equal(ids(p), []int{100}) {
		t.Fatalf("rows changed before the ranking: %v", ids(p))
	}
	apply(p, r.Rank)
	if got := ids(p); len(got) != 2 || slices.Contains(got, 2) {
		t.Fatalf("rows = %v", got)
	}
}

func TestSelect(t *testing.T) {
	t.Parallel()
	p := New(style.New(theme.Theme{}), Options{Items: items("a", "b", "c"), Preview: true})
	p.Init()
	if ev := p.Select(102).Events; !slices.Equal(ev, []Event{Moved{ID: 102}}) {
		t.Fatalf("events = %+v", ev)
	}
	if id, _ := p.SelectedID(); id != 102 {
		t.Fatalf("selected %d", id)
	}
}

func TestMouse(t *testing.T) {
	t.Parallel()
	p := New(style.New(theme.Theme{}), Options{Items: items("a", "b", "c", "d"), Preview: true})
	g := grid.New(40, 20)
	p.Draw(g) // records the geometry
	first := p.box.top + 1
	if r := p.Update(tea.MouseMotionMsg(tea.Mouse{X: 5, Y: first + 2})); !slices.Equal(r.Events, []Event{Moved{ID: 102}}) {
		t.Fatalf("hover = %+v", r.Events)
	}
	if r := p.Update(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelDown})); !slices.Equal(r.Events, []Event{Moved{ID: 103}}) {
		t.Fatalf("wheel = %+v", r.Events)
	}
	// A click outside the rows is swallowed; one on a row chooses it.
	if r := p.Update(tea.MouseClickMsg(tea.Mouse{X: 5, Y: 0, Button: tea.MouseLeft})); len(r.Events) != 0 || !p.Active() {
		t.Fatalf("click outside = %+v", r.Events)
	}
	if r := p.Update(tea.MouseClickMsg(tea.Mouse{X: 5, Y: first + 1, Button: tea.MouseLeft})); !slices.Equal(r.Events, []Event{Chosen{ID: 101}}) {
		t.Fatalf("click = %+v", r.Events)
	}
	if p.Active() {
		t.Fatal("a chosen picker stays open")
	}
}

func TestScrollKeepsTheHighlightInView(t *testing.T) {
	t.Parallel()
	many := make([]Item, 40)
	for i := range many {
		many[i] = Item{ID: i, Text: fmt.Sprintf("row%02d", i)}
	}
	p := New(style.New(theme.Theme{}), Options{Items: many})
	g := grid.New(40, 24)
	p.Draw(g)
	for range 15 {
		p.Update(key("down"))
	}
	g = grid.New(40, 24)
	p.Draw(g)
	text := strings.Join(g.Plain(), "\n")
	if !strings.Contains(text, "row15") || strings.Contains(text, "row00") {
		t.Fatalf("the window did not follow the highlight:\n%s", text)
	}
	if p.top == 0 || p.sel < p.top || p.sel >= p.top+p.window() {
		t.Fatalf("top %d sel %d window %d", p.top, p.sel, p.window())
	}
}

func TestDrawCells(t *testing.T) {
	t.Parallel()
	th, err := theme.Load("dark")
	if err != nil {
		t.Fatal(err)
	}
	st := style.New(th)
	p := New(st, Options{
		Title: "Pick", Footer: "foot", Placeholder: "type", Fuzzy: true,
		Items: []Item{{ID: 1, Text: "alpha", Detail: "dd", Keybinding: "ctrl+a"}, {ID: 2, Text: "beta", Disabled: true}},
	})
	g := grid.New(50, 20)
	p.Draw(g)
	plain := g.Plain()
	// Layout: query row 19, footer 18, box rows 6..17 (10 item rows + borders), items from 7.
	if !strings.Contains(plain[19], "type") {
		t.Fatalf("query row = %q", plain[19])
	}
	if !strings.Contains(plain[18], "foot") || !strings.HasSuffix(strings.TrimRight(plain[18], " "), "2/2") {
		t.Fatalf("footer row = %q", plain[18])
	}
	if !strings.Contains(plain[6], "Pick") {
		t.Fatalf("title row = %q", plain[6])
	}
	row := plain[7]
	if !strings.Contains(row, "alpha") || !strings.Contains(row, "dd") || !strings.HasSuffix(strings.TrimRight(row, " │"), "ctrl+a") {
		t.Fatalf("first item row = %q", row)
	}
	ov := st.Overlay
	if got := g.At(2, 7).Style; got.BG != ov.Selected.BG {
		t.Fatalf("the highlighted row has background %v, want the selected style", got.BG)
	}
	if got := g.At(2, 8).Style; !got.Faint {
		t.Fatalf("a disabled item is not faint: %+v", got)
	}
	// The matched runes are bold.
	p.Update(key("a"))
	apply(p, p.rank())
	g = grid.New(50, 20)
	p.Draw(g)
	if !g.At(2, 7).Style.Bold {
		t.Fatalf("the first matched rune is not bold: %+v", g.At(2, 7))
	}
}

func TestDrawInTinyFrames(t *testing.T) {
	t.Parallel()
	for _, hw := range [][2]int{{0, 0}, {1, 1}, {3, 3}, {10, 3}, {40, 4}, {40, 5}} {
		p := New(style.New(theme.Theme{}), Options{Items: items("one", "two"), Fuzzy: true, Title: "T"})
		g := grid.New(hw[0], hw[1])
		p.Draw(g) // must not panic or write outside the grid
		p.Update(tea.MouseClickMsg(tea.Mouse{X: 1, Y: 1, Button: tea.MouseLeft}))
	}
}

// TestMovedReportsNoSelectionWhenNoRowIsShown checks S13-4: ranking down to zero rows, and
// replacing the items with none, tell the owner that nothing is highlighted any more, and a
// row that appears again is reported again.
func TestMovedReportsNoSelectionWhenNoRowIsShown(t *testing.T) {
	t.Parallel()
	p := New(style.New(theme.Theme{}), Options{Items: items("apple", "banana"), Fuzzy: true, Preview: true})
	if ev := p.Init().Events; !slices.Equal(ev, []Event{Moved{ID: 100}}) {
		t.Fatalf("Init events = %+v", ev)
	}
	ev, rank := typeText(p, "zzz")
	if len(ev) != 0 {
		t.Fatalf("typing events = %+v, the rows stay until the ranking answers", ev)
	}
	if got := apply(p, rank).Events; !slices.Equal(got, []Event{Moved{ID: NoItem}}) {
		t.Fatalf("zero rows reported %+v, want Moved{NoItem}", got)
	}
	// Nothing more to report while nothing is shown.
	if got := p.SetItems(nil).Events; len(got) != 0 {
		t.Fatalf("SetItems(nil) events = %+v", got)
	}
	p.SetItems(items("apple", "banana"))
	// Clearing the query shows the rows again and the highlight is reported again.
	var got []Event
	for range 3 {
		r := p.Update(key("backspace"))
		got = append(got, r.Events...)
		got = append(got, apply(p, r.Rank).Events...)
	}
	if !slices.Contains(got, Event(Moved{ID: 100})) {
		t.Fatalf("events after clearing the query = %+v, want the highlight back", got)
	}

	q := New(style.New(theme.Theme{}), Options{Items: items("one"), Preview: true})
	q.Init()
	if got := q.SetItems(nil).Events; !slices.Equal(got, []Event{Moved{ID: NoItem}}) {
		t.Fatalf("SetItems(nil) of a plain picker reported %+v", got)
	}
}
