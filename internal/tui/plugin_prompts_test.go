package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/popup"
	"github.com/tbxark/gvim/pluginapi"
)

// openVia runs fn as a plugin command would: inside a slice of the fake runtime, with the
// host. The deferred openings run in the pump when the command returns.
func openVia(t *testing.T, m *Model, rt *fakeRuntime, fn func(h pluginapi.Host)) {
	t.Helper()
	rt.onRun = func(string, map[string]any) { fn(rt.host) }
	must(t, m.Run("x.cmd"))
	// The commands of the pump (the first events of an opened picker) reach the model.
	cmd := m.pending
	m.pending = nil
	feed(m, cmd)
}

func newPromptModel(t *testing.T) (*Model, *fakeRuntime) {
	t.Helper()
	rt := newFakeRuntime(t, "x.cmd")
	m, _ := newPluginModel(t, rt)
	return m, rt
}

// lastResolved is the latest completion the runtime received.
func lastResolved(t *testing.T, rt *fakeRuntime) pluginapi.Completion {
	t.Helper()
	if len(rt.resolved) == 0 {
		t.Fatal("the runtime was not resolved")
	}
	return rt.resolved[len(rt.resolved)-1]
}

// frameRow paints the overlays over an empty frame and returns row y as text.
func frameRow(m *Model, y int) string {
	g := grid.New(m.width, m.height)
	for _, o := range m.overlays {
		o.Draw(g)
	}
	return strings.TrimRight(g.Plain()[y], " ")
}

func TestPluginInputRoundTrip(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name string
		keys func(m *Model)
		want pluginapi.Completion
	}{
		{"submit", func(m *Model) { typeInto(m, "ab"); feed(m, runKey(m, special(tea.KeyEnter))) },
			pluginapi.Completion{Token: 7, Values: []any{"xab"}}},
		{"esc is a nil answer", func(m *Model) { typeInto(m, "a"); feed(m, runKey(m, special(tea.KeyEscape))) },
			pluginapi.Completion{Token: 7, Values: []any{nil}}},
	}
	for _, tt := range rows {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, rt := newPromptModel(t)
			openVia(t, m, rt, func(h pluginapi.Host) {
				if err := h.Input(7, pluginapi.InputSpec{Prompt: "Name:", Default: "x"}); err != nil {
					t.Error(err)
				}
				if len(m.overlays) != 0 {
					t.Error("the prompt opened inside the slice")
				}
			})
			if len(m.overlays) != 1 {
				t.Fatalf("%d overlays after the command, want the prompt", len(m.overlays))
			}
			if row := frameRow(m, m.height-1); !strings.HasPrefix(row, " Name: x") {
				t.Fatalf("prompt row = %q", row)
			}
			tt.keys(m)
			got := lastResolved(t, rt)
			if got.Token != tt.want.Token || got.Err != "" || len(got.Values) != 1 || got.Values[0] != tt.want.Values[0] {
				t.Fatalf("resolved %+v, want %+v", got, tt.want)
			}
			if len(m.overlays) != 0 || len(m.plug.waits) != 0 {
				t.Fatalf("overlays %d waits %v after the answer", len(m.overlays), m.plug.waits)
			}
		})
	}
}

func TestPluginInputSelectAllAndHooks(t *testing.T) {
	t.Parallel()
	m, rt := newPromptModel(t)
	var changed []string
	openVia(t, m, rt, func(h pluginapi.Host) {
		_ = h.Input(7, pluginapi.InputSpec{
			Prompt: "P:", Default: "old", SelectAll: true,
			Validate: func(s string) error {
				if s == "bad" {
					return errors.New("not allowed")
				}
				return nil
			},
			OnChange: func(s string) { changed = append(changed, s) },
		})
	})
	typeInto(m, "bad")
	if !slices.Equal(changed, []string{"b", "ba", "bad"}) {
		t.Fatalf("OnChange saw %q (the prefill is replaced by the first key)", changed)
	}
	// Enter on a rejected text keeps the prompt open and shows the reason.
	feed(m, runKey(m, special(tea.KeyEnter)))
	if len(m.overlays) != 1 || len(rt.resolved) != 0 {
		t.Fatalf("a rejected text closed the prompt: overlays %d resolved %v", len(m.overlays), rt.resolved)
	}
	if row := frameRow(m, m.height-1); !strings.Contains(row, "not allowed") {
		t.Fatalf("prompt row = %q, want the rejection", row)
	}
	feed(m, runKey(m, special(tea.KeyBackspace)))
	feed(m, runKey(m, special(tea.KeyEnter)))
	if got := lastResolved(t, rt); got.Values[0] != "ba" {
		t.Fatalf("resolved %+v", got)
	}
}

func TestPluginInputOnChangeAsksForADrain(t *testing.T) {
	t.Parallel()
	m, rt := newPromptModel(t)
	openVia(t, m, rt, func(h pluginapi.Host) {
		_ = h.Input(7, pluginapi.InputSpec{OnChange: func(string) {}})
	})
	drains := rt.drains
	_, cmd := m.Update(typed("a"))
	msgs := collect(cmd) // a tick can be run once
	if !hasDrainMsg(msgs) {
		t.Fatal("an edit with OnChange started no drain: the callback task would wait for the next event")
	}
	for _, msg := range msgs {
		_, next := m.Update(msg)
		feed(m, next)
	}
	if rt.drains == drains {
		t.Fatal("the runtime was not drained")
	}
}

func TestPluginConfirmRoundTrip(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name string
		spec pluginapi.ConfirmSpec
		key  tea.KeyPressMsg
		want bool
	}{
		{"y", pluginapi.ConfirmSpec{Message: "Sure?"}, typed("y"), true},
		{"n", pluginapi.ConfirmSpec{Message: "Sure?"}, typed("n"), false},
		{"enter takes the default (no)", pluginapi.ConfirmSpec{Message: "Sure?"}, special(tea.KeyEnter), false},
		{"enter takes the default (yes)", pluginapi.ConfirmSpec{Message: "Sure?", Default: true}, special(tea.KeyEnter), true},
		{"esc answers false", pluginapi.ConfirmSpec{Message: "Sure?", Default: true}, special(tea.KeyEscape), false},
	}
	for _, tt := range rows {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, rt := newPromptModel(t)
			openVia(t, m, rt, func(h pluginapi.Host) {
				tt.spec.Yes, tt.spec.No = "Do it", "Keep"
				if err := h.Confirm(3, tt.spec); err != nil {
					t.Error(err)
				}
			})
			if row := frameRow(m, m.height-1); !strings.Contains(row, "Sure?") || !strings.Contains(row, "[ Do it ] [ Keep ]") {
				t.Fatalf("question row = %q", row)
			}
			feed(m, runKey(m, tt.key))
			got := lastResolved(t, rt)
			if got.Token != 3 || got.Err != "" || len(got.Values) != 1 || got.Values[0] != tt.want {
				t.Fatalf("resolved %+v, want %v", got, tt.want)
			}
		})
	}
}

// pickerItems for the tests: IDs are not the positions, to show that the host reports IDs.
func testItems() []pluginapi.PickItem {
	return []pluginapi.PickItem{
		{ID: 10, Text: "alpha"}, {ID: 11, Text: "beta", Detail: "second"}, {ID: 12, Text: "gamma", Disabled: true}, {ID: 13, Text: "alphabet", Keybinding: "ctrl+a"},
	}
}

func TestPluginPickerRoundTrip(t *testing.T) {
	t.Parallel()
	open := func(t *testing.T, spec pluginapi.PickerSpec) (*Model, *fakeRuntime) {
		m, rt := newPromptModel(t)
		spec.Items = testItems()
		openVia(t, m, rt, func(h pluginapi.Host) {
			if err := h.OpenPicker(5, 42, spec); err != nil {
				t.Error(err)
			}
		})
		if m.plugPicker(42) == nil {
			t.Fatalf("picker not open; overlays %d", len(m.overlays))
		}
		return m, rt
	}
	t.Run("choose with Enter reports the item ID", func(t *testing.T) {
		t.Parallel()
		m, rt := open(t, pluginapi.PickerSpec{Title: "Pick"})
		feed(m, runKey(m, special(tea.KeyDown)))
		feed(m, runKey(m, special(tea.KeyEnter)))
		got := lastResolved(t, rt)
		if got.Token != 5 || got.Kind != "" || got.More || len(got.Values) != 1 || got.Values[0] != 11 {
			t.Fatalf("resolved %+v, want item 11", got)
		}
		if len(m.overlays) != 0 || len(m.plug.waits) != 0 {
			t.Fatalf("overlays %d waits %v", len(m.overlays), m.plug.waits)
		}
	})
	t.Run("a disabled item cannot be chosen", func(t *testing.T) {
		t.Parallel()
		m, rt := open(t, pluginapi.PickerSpec{})
		feed(m, runKey(m, special(tea.KeyDown)))
		feed(m, runKey(m, special(tea.KeyDown)))
		feed(m, runKey(m, special(tea.KeyEnter)))
		if len(rt.resolved) != 0 || len(m.overlays) != 1 {
			t.Fatalf("resolved %v overlays %d", rt.resolved, len(m.overlays))
		}
	})
	t.Run("Esc closes with a nil answer", func(t *testing.T) {
		t.Parallel()
		m, rt := open(t, pluginapi.PickerSpec{})
		feed(m, runKey(m, special(tea.KeyEscape)))
		got := lastResolved(t, rt)
		if got.Token != 5 || got.More || len(got.Values) != 1 || got.Values[0] != nil {
			t.Fatalf("resolved %+v", got)
		}
		if len(m.overlays) != 0 {
			t.Fatal("the picker stayed open")
		}
	})
	t.Run("fuzzy typing ranks in a command and keeps the best rows", func(t *testing.T) {
		t.Parallel()
		m, rt := open(t, pluginapi.PickerSpec{Fuzzy: true})
		if got := m.plugPicker(42).p.RowIDs(); !slices.Equal(got, []int{10, 11, 12, 13}) {
			t.Fatalf("opening rows = %v", got)
		}
		typeInto(m, "p")
		got := m.plugPicker(42).p.RowIDs()
		if len(got) != 2 || !slices.Contains(got, 10) || !slices.Contains(got, 13) {
			t.Fatalf("rows for p = %v, want alpha and alphabet", got)
		}
		_, cmd := m.Update(typed("b"))
		// Update does not rank: the rows are still those of the query before.
		if got := m.plugPicker(42).p.RowIDs(); len(got) != 2 {
			t.Fatalf("Update filtered synchronously: %v", got)
		}
		feed(m, cmd)
		if got := m.plugPicker(42).p.RowIDs(); !slices.Equal(got, []int{13}) {
			t.Fatalf("rows for pb = %v, want only alphabet", got)
		}
		feed(m, runKey(m, special(tea.KeyEnter)))
		if got := lastResolved(t, rt); got.Values[0] != 13 {
			t.Fatalf("resolved %+v", got)
		}
	})
	t.Run("a live picker reports queries and a preview picker reports moves", func(t *testing.T) {
		t.Parallel()
		m, rt := open(t, pluginapi.PickerSpec{Live: true, Preview: true})
		typeInto(m, "zq")
		feed(m, runKey(m, special(tea.KeyDown)))
		var got []string
		for _, c := range rt.resolved {
			if !c.More || c.Token != 5 {
				t.Fatalf("an intermediate item was not a stream item: %+v", c)
			}
			got = append(got, c.Kind)
			if c.Kind == "query" {
				got[len(got)-1] += ":" + c.Values[0].(string)
			}
		}
		// The first highlight is reported when the picker opens.
		want := []string{"move", "query:z", "query:zq", "move"}
		if !slices.Equal(got, want) {
			t.Fatalf("items = %v, want %v", got, want)
		}
		if len(m.plug.waits) != 1 {
			t.Fatal("an intermediate item ended the wait")
		}
	})
	t.Run("events that arrive out of order reach the runtime in order", func(t *testing.T) {
		t.Parallel()
		// Each key's events travel in a command of their own, and commands run on separate
		// goroutines: the messages of two keys may arrive in either order.
		m, rt := open(t, pluginapi.PickerSpec{Live: true})
		_, first := m.Update(typed("z"))
		_, second := m.Update(typed("q"))
		firstMsgs, secondMsgs := collect(first), collect(second)
		for _, msg := range slices.Concat(secondMsgs, firstMsgs) {
			_, next := m.Update(msg)
			feed(m, next)
		}
		var got []string
		for _, c := range rt.resolved {
			if c.Kind == "query" {
				got = append(got, c.Values[0].(string))
			}
		}
		if want := []string{"z", "zq"}; !slices.Equal(got, want) {
			t.Fatalf("queries delivered = %v, want %v", got, want)
		}
	})
	t.Run("a non-fuzzy picker without Live has no query line", func(t *testing.T) {
		t.Parallel()
		m, rt := open(t, pluginapi.PickerSpec{})
		typeInto(m, "zzz")
		if got := m.plugPicker(42).p.RowIDs(); len(got) != 4 || len(rt.resolved) != 0 {
			t.Fatalf("typing changed a plain picker: %v %v", got, rt.resolved)
		}
	})
	t.Run("click chooses a row", func(t *testing.T) {
		t.Parallel()
		m, rt := open(t, pluginapi.PickerSpec{Title: "Pick"})
		// Find the row of "beta" in the painted frame.
		g := grid.New(m.width, m.height)
		m.overlays[0].Draw(g)
		y := -1
		for i, row := range g.Plain() {
			if strings.Contains(row, "beta") {
				y = i
			}
		}
		if y < 0 {
			t.Fatalf("beta not painted:\n%s", strings.Join(g.Plain(), "\n"))
		}
		feed(m, func() tea.Cmd { _, c := m.Update(click(5, y, tea.MouseLeft)); return c }())
		if got := lastResolved(t, rt); got.Values[0] != 11 {
			t.Fatalf("resolved %+v, want item 11", got)
		}
	})
}

func TestPluginPickerPaintsItsRows(t *testing.T) {
	t.Parallel()
	rt := newFakeRuntime(t, "x.cmd")
	m := New(Deps{Editor: newFakeEditor("hello"), Keys: testKeys(), Theme: mustLoadTheme(t, "dark"), Clock: testClock, Root: RootPorts{Plugins: rt}})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	useHost(rt, m)
	openVia(t, m, rt, func(h pluginapi.Host) {
		_ = h.OpenPicker(5, 42, pluginapi.PickerSpec{Title: "Pick", Items: testItems(), Fuzzy: true, Footer: "enter to open", Placeholder: "type here"})
	})
	g := grid.New(m.width, m.height)
	m.overlays[0].Draw(g)
	plain := g.Plain()
	h := m.height
	if !strings.Contains(plain[h-1], " type here") {
		t.Fatalf("query row = %q, want the placeholder", plain[h-1])
	}
	foot := strings.TrimRight(plain[h-2], " ")
	if !strings.Contains(foot, "enter to open") || !strings.HasSuffix(foot, "4/4") {
		t.Fatalf("footer row = %q", foot)
	}
	// The box is 12 rows (10 of them for items) above the footer row, so the first item
	// is on row h-2-12+1.
	first := h - 2 - 12 + 1
	if !strings.Contains(plain[first], "alpha") || !strings.Contains(plain[first+1], "beta") || !strings.Contains(plain[first+1], "second") {
		t.Fatalf("rows:\n%s", strings.Join(plain[first-1:], "\n"))
	}
	if !strings.Contains(plain[first+3], "alphabet") || !strings.Contains(plain[first+3], "ctrl+a") {
		t.Fatalf("the key binding of the last row is missing: %q", plain[first+3])
	}
	if !strings.Contains(plain[first-1], "Pick") {
		t.Fatalf("the title is not on the top border: %q", plain[first-1])
	}
	// The highlighted row is painted with the selected style, the others are not.
	if g.At(3, first).Style == g.At(3, first+1).Style {
		t.Fatal("the highlighted row looks like the others")
	}
}

func TestPluginPickerUpdates(t *testing.T) {
	t.Parallel()
	m, rt := newPromptModel(t)
	openVia(t, m, rt, func(h pluginapi.Host) {
		_ = h.OpenPicker(5, 42, pluginapi.PickerSpec{Items: testItems(), Preview: true})
	})
	title, status := "New", "searching"
	sel := 13
	items := []pluginapi.PickItem{{ID: 20, Text: "one"}, {ID: 13, Text: "two"}}
	if err := m.plug.host.UpdatePicker(42, pluginapi.PickerUpdate{Title: &title, Status: &status, Items: &items, Selected: &sel}); err != nil {
		t.Fatal(err)
	}
	p := m.plugPicker(42).p
	if got := p.RowIDs(); !slices.Equal(got, []int{20, 13}) {
		t.Fatalf("rows = %v", got)
	}
	if id, _ := p.SelectedID(); id != 13 {
		t.Fatalf("selected = %d, want 13", id)
	}
	// The Moved events that the update caused reach the runtime through the collected command.
	feed(m, m.pending)
	m.pending = nil
	var moves []any
	for _, c := range rt.resolved {
		if c.Kind == "move" {
			moves = append(moves, c.Values[0])
		}
	}
	// The opening highlight (10), the first row of the new items (20), then the request (13).
	if !slices.Equal(moves, []any{10, 20, 13}) {
		t.Fatalf("moves = %v, want 10, 20, 13", moves)
	}
	if err := m.plug.host.UpdatePicker(99, pluginapi.PickerUpdate{}); pluginapi.CodeOf(err) != pluginapi.CodeClosed {
		t.Fatalf("UpdatePicker(unknown) = %v", err)
	}
}

func TestPluginHostOpenValidation(t *testing.T) {
	t.Parallel()
	m, rt := newPromptModel(t)
	h := m.plug.host
	if err := h.OpenPicker(1, 1, pluginapi.PickerSpec{Items: make([]pluginapi.PickItem, 50_001)}); pluginapi.CodeOf(err) != pluginapi.CodeLimit {
		t.Fatalf("too many items = %v, want limit", err)
	}
	if len(m.plug.waits) != 0 {
		t.Fatal("a refused call took the token")
	}
	if err := h.Input(2, pluginapi.InputSpec{}); err != nil {
		t.Fatal(err)
	}
	if err := h.Confirm(2, pluginapi.ConfirmSpec{}); pluginapi.CodeOf(err) != pluginapi.CodeInvalid {
		t.Fatalf("a token that already waits = %v, want invalid", err)
	}
	_ = rt
}

func TestPluginPopupRoundTrip(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name string
		spec pluginapi.PopupSpec
		do   func(m *Model)
		want any
	}{
		{"choose a row", pluginapi.PopupSpec{Items: []pluginapi.PickItem{{ID: 4, Text: "a"}, {ID: 5, Text: "b"}}},
			func(m *Model) { feed(m, runKey(m, special(tea.KeyDown))); feed(m, runKey(m, special(tea.KeyEnter))) }, 5},
		{"a text popup closes with Enter and chooses nothing", pluginapi.PopupSpec{Lines: []string{"one", "two"}},
			func(m *Model) { feed(m, runKey(m, special(tea.KeyEnter))) }, nil},
		{"Esc", pluginapi.PopupSpec{Lines: []string{"one"}},
			func(m *Model) { feed(m, runKey(m, special(tea.KeyEscape))) }, nil},
		{"a click outside", pluginapi.PopupSpec{Lines: []string{"one"}, Anchor: "center"},
			func(m *Model) {
				_, c := m.Update(click(0, 0, tea.MouseLeft))
				feed(m, c)
			}, nil},
	}
	for _, tt := range rows {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, rt := newPromptModel(t)
			openVia(t, m, rt, func(h pluginapi.Host) {
				tt.spec.Title = "Commit"
				if err := h.ShowPopup(8, 77, tt.spec); err != nil {
					t.Error(err)
				}
			})
			if len(m.overlays) != 1 {
				t.Fatalf("%d overlays, want the popup", len(m.overlays))
			}
			g := grid.New(m.width, m.height)
			m.overlays[0].Draw(g)
			if !strings.Contains(strings.Join(g.Plain(), "\n"), "Commit") {
				t.Fatalf("the title was not painted:\n%s", strings.Join(g.Plain(), "\n"))
			}
			tt.do(m)
			got := lastResolved(t, rt)
			if got.Token != 8 || got.Err != "" || len(got.Values) != 1 || got.Values[0] != tt.want {
				t.Fatalf("resolved %+v, want %v", got, tt.want)
			}
			if len(m.overlays) != 0 || len(m.plug.waits) != 0 {
				t.Fatalf("overlays %d waits %v", len(m.overlays), m.plug.waits)
			}
		})
	}
}

// TestPluginPopupChosenIgnoresOtherPopups checks the routing hunk in lspMessage: a
// popup.Chosen of the language server (or any other owner) is not a plugin's.
func TestPluginPopupChosenIgnoresOtherPopups(t *testing.T) {
	t.Parallel()
	m, rt := newPromptModel(t)
	if cmd, ok := m.pluginPopupChosen(popup.Chosen{Item: popup.Item{Label: "x", Data: lspPick{}}}); ok || cmd != nil {
		t.Fatal("a language-server row was taken")
	}
	must(t, m.plugWait(8, waitPopup))
	cmd, ok := m.lspMessage(popup.Chosen{Item: popup.Item{Data: pluginPopupData{tok: 8, item: 3}}})
	if !ok {
		t.Fatal("lspMessage did not route a plugin row")
	}
	feed(m, cmd)
	if got := lastResolved(t, rt); got.Token != 8 || got.Values[0] != 3 {
		t.Fatalf("resolved %+v", got)
	}
}

func TestPluginPopupAnchor(t *testing.T) {
	t.Parallel()
	m, _ := newPromptModel(t) // 80 x 24
	items := []popup.Item{{Label: "a"}, {Label: "b"}}
	c := m.popupAnchor("center", "T", items, 0)
	if c.X != (80-3)/2 || c.Y != (24-2-1)/2-1 {
		t.Fatalf("centre anchor = %+v", c)
	}
	// A width asks for a wider box, which moves the centre.
	if w := m.popupAnchor("center", "T", items, 40); w.X != (80-40)/2 {
		t.Fatalf("anchor for width 40 = %+v", w)
	}
	// At the caret: the caret of the editor is on screen, so the anchor is a cell of it.
	if p := m.popupAnchor("cursor", "T", items, 0); p == c {
		t.Fatalf("cursor anchor = %+v, the centre", p)
	}
}

// TestPluginDismissDoesNotRunThePluginInsideRouteOverlay is the stack-safety rule: the Esc
// of a prompt resolves the wait outside routeOverlay, so a plugin that opens its next
// prompt as the answer finds the stack intact (routeOverlay pops after Dismiss).
func TestPluginDismissDoesNotRunThePluginInsideRouteOverlay(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name string
		open func(h pluginapi.Host)
		key  tea.KeyPressMsg
	}{
		{"input", func(h pluginapi.Host) { _ = h.Input(1, pluginapi.InputSpec{}) }, special(tea.KeyEscape)},
		{"confirm", func(h pluginapi.Host) { _ = h.Confirm(1, pluginapi.ConfirmSpec{}) }, special(tea.KeyEscape)},
		{"picker", func(h pluginapi.Host) { _ = h.OpenPicker(1, 9, pluginapi.PickerSpec{Items: testItems()}) }, special(tea.KeyEscape)},
		{"picker choice", func(h pluginapi.Host) { _ = h.OpenPicker(1, 9, pluginapi.PickerSpec{Items: testItems()}) }, special(tea.KeyEnter)},
		{"popup", func(h pluginapi.Host) {
			_ = h.ShowPopup(1, 9, pluginapi.PopupSpec{Lines: []string{"x"}})
		}, special(tea.KeyEscape)},
	}
	for _, tt := range rows {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, rt := newPromptModel(t)
			openVia(t, m, rt, tt.open)
			// The answer opens the next prompt, as a wizard would.
			rt.onResolve = func(c pluginapi.Completion) {
				if c.Token == 1 {
					if err := rt.host.Input(2, pluginapi.InputSpec{Prompt: "Next:"}); err != nil {
						t.Error(err)
					}
				}
			}
			inRoute := m.plug.ui.busy
			_, cmd := m.Update(tt.key)
			if len(rt.resolved) != 0 {
				t.Fatal("the plugin ran inside the key's Update, before routeOverlay returned")
			}
			feed(m, cmd)
			if len(rt.resolved) != 1 || rt.resolved[0].Token != 1 {
				t.Fatalf("resolved %+v", rt.resolved)
			}
			if len(m.overlays) != 1 {
				t.Fatalf("%d overlays, want only the next prompt", len(m.overlays))
			}
			if row := frameRow(m, m.height-1); !strings.HasPrefix(row, " Next:") {
				t.Fatalf("top overlay row = %q", row)
			}
			if m.plug.ui.busy != inRoute {
				t.Fatal("busy was left set")
			}
		})
	}
}

func TestPluginCancelWaitClosesTheOverlay(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name  string
		open  func(h pluginapi.Host)
		close func(h pluginapi.Host)
	}{
		{"CancelWait of a prompt", func(h pluginapi.Host) { _ = h.Input(1, pluginapi.InputSpec{}) }, func(h pluginapi.Host) { h.CancelWait(1) }},
		{"CancelWait of a confirm", func(h pluginapi.Host) { _ = h.Confirm(1, pluginapi.ConfirmSpec{}) }, func(h pluginapi.Host) { h.CancelWait(1) }},
		{"CancelWait of a picker", func(h pluginapi.Host) { _ = h.OpenPicker(1, 9, pluginapi.PickerSpec{}) }, func(h pluginapi.Host) { h.CancelWait(1) }},
		{"ClosePicker", func(h pluginapi.Host) { _ = h.OpenPicker(1, 9, pluginapi.PickerSpec{}) }, func(h pluginapi.Host) { h.ClosePicker(9) }},
		{"CancelWait of a popup", func(h pluginapi.Host) { _ = h.ShowPopup(1, 9, pluginapi.PopupSpec{Lines: []string{"x"}}) }, func(h pluginapi.Host) { h.CancelWait(1) }},
		{"ClosePopup", func(h pluginapi.Host) { _ = h.ShowPopup(1, 9, pluginapi.PopupSpec{Lines: []string{"x"}}) }, func(h pluginapi.Host) { h.ClosePopup(9) }},
	}
	for _, tt := range rows {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, rt := newPromptModel(t)
			openVia(t, m, rt, tt.open)
			if len(m.overlays) != 1 {
				t.Fatalf("%d overlays", len(m.overlays))
			}
			tt.close(m.plug.host)
			if len(m.overlays) != 0 || len(m.plug.waits) != 0 {
				t.Fatalf("overlays %d waits %v after the close", len(m.overlays), m.plug.waits)
			}
			if len(rt.resolved) != 0 {
				t.Fatalf("a host-side close resolved the wait: %+v", rt.resolved)
			}
		})
	}
	t.Run("before the deferred opening ran", func(t *testing.T) {
		t.Parallel()
		m, rt := newPromptModel(t)
		openVia(t, m, rt, func(h pluginapi.Host) {
			_ = h.Input(1, pluginapi.InputSpec{})
			h.CancelWait(1) // the plugin's task ended in the same slice
		})
		if len(m.overlays) != 0 {
			t.Fatal("a cancelled wait still opened its prompt")
		}
	})
}

// TestPluginCloseInsideAnOverlayUpdateKeepsTheStack covers a validator that closes an
// overlay: the stack is written back by routeOverlay, so the close only marks it.
func TestPluginCloseInsideAnOverlayUpdateKeepsTheStack(t *testing.T) {
	t.Parallel()
	m, rt := newPromptModel(t)
	openVia(t, m, rt, func(h pluginapi.Host) {
		_ = h.OpenPicker(1, 9, pluginapi.PickerSpec{Items: testItems()})
		_ = h.Input(2, pluginapi.InputSpec{Validate: func(string) error {
			h.ClosePicker(9)
			return nil
		}})
	})
	if len(m.overlays) != 2 {
		t.Fatalf("%d overlays, want the picker and the prompt", len(m.overlays))
	}
	feed(m, runKey(m, special(tea.KeyEnter)))
	// The prompt submitted. The picker was closed from inside its Update: it is marked
	// closed, never drawn, and dropped when it surfaces.
	if got := lastResolved(t, rt); got.Token != 2 {
		t.Fatalf("resolved %+v", got)
	}
	for _, o := range m.overlays {
		if o.Active() {
			t.Fatalf("an active overlay is left: %T", o)
		}
	}
	if m.plugPicker(9) != nil {
		t.Fatal("the closed picker can still be found")
	}
}

func TestPluginOverlaysCancel(t *testing.T) {
	t.Parallel()
	m, rt := newPromptModel(t)
	openVia(t, m, rt, func(h pluginapi.Host) {
		_ = h.Input(1, pluginapi.InputSpec{})
		_ = h.OpenPicker(2, 9, pluginapi.PickerSpec{})
		_ = h.ShowPopup(3, 10, pluginapi.PopupSpec{Lines: []string{"x"}})
		_ = h.Confirm(4, pluginapi.ConfirmSpec{})
	})
	m.plug.waits[99] = "key" // a wait of another area stays
	if len(m.overlays) != 4 {
		t.Fatalf("%d overlays", len(m.overlays))
	}
	feed(m, m.plugOverlaysCancel("plugin input cleared"))
	if len(m.overlays) != 0 {
		t.Fatalf("%d overlays left", len(m.overlays))
	}
	var toks []pluginapi.Token
	for _, c := range rt.resolved {
		if c.Err != "cancelled: plugin input cleared" {
			t.Fatalf("resolved %+v, want cancelled", c)
		}
		toks = append(toks, c.Token)
	}
	if !slices.Equal(toks, []pluginapi.Token{1, 2, 3, 4}) {
		t.Fatalf("cancelled waits %v", toks)
	}
	if _, ok := m.plug.waits[99]; !ok {
		t.Fatal("a wait that is not an overlay's was cancelled")
	}
}

// TestPluginPromptsNeedNoPluginKeys checks that, with an overlay of a plugin open, keys do
// not reach the editor behind it.
func TestPluginPromptsKeepKeysFromTheEditor(t *testing.T) {
	t.Parallel()
	m, rt := newPromptModel(t)
	openVia(t, m, rt, func(h pluginapi.Host) { _ = h.Input(1, pluginapi.InputSpec{}) })
	before := len(m.ed.(*fakeEditor).actions)
	typeInto(m, "abc")
	if after := len(m.ed.(*fakeEditor).actions); after != before {
		t.Fatalf("the editor got %d actions while the prompt was open", after-before)
	}
}
