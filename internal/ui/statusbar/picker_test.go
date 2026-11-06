package statusbar

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/encoding"
	"github.com/tbxark/gvim/internal/theme"
	"github.com/tbxark/gvim/internal/ui/grid"
	"github.com/tbxark/gvim/internal/ui/style"
)

// call records one dispatch: the action and its arguments.
type call struct {
	action string
	args   map[string]any
}

// recorder returns a Dispatch that records its calls and returns no command.
func recorder(calls *[]call) Dispatch {
	return func(action string, args map[string]any) tea.Cmd {
		*calls = append(*calls, call{action, args})
		return nil
	}
}

func keyPress(code rune) tea.KeyPressMsg { return tea.KeyPressMsg(tea.Key{Code: code}) }

func clickAt(x, y int) tea.MouseClickMsg {
	return tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft})
}

// paintPicker draws p on a w by h grid and returns the grid.
// encodingPicker is the open encoding picker with the given dispatch.
func encodingPicker(t *testing.T, d Dispatch) *Picker {
	t.Helper()
	p, ok := NewPicker(KindEncoding, nil, d, style.New(theme.Theme{}))
	if !ok {
		t.Fatal("no picker for the encoding segment")
	}
	return p
}

func paintPicker(p *Picker, w, h int) *grid.Grid {
	g := grid.New(w, h)
	p.Draw(g)
	return g
}

func TestEncodingPickerListsTheEncodingNames(t *testing.T) {
	p := encodingPicker(t, nil)
	if !reflect.DeepEqual(p.items, encoding.Names()) {
		t.Errorf("items = %v, want %v", p.items, encoding.Names())
	}
}

func TestEnterRunsSetEncodingWithTheChosenName(t *testing.T) {
	var calls []call
	p := encodingPicker(t, recorder(&calls))
	paintPicker(p, 80, 20)
	p, _ = p.Update(keyPress(tea.KeyDown))
	_, _ = p.Update(keyPress(tea.KeyEnter))
	want := []call{{"set_encoding", map[string]any{"name": encoding.Names()[1]}}}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("calls = %+v, want %+v", calls, want)
	}
	if p.Active() {
		t.Error("picker is still open after a choice")
	}
}

func TestLineEndingPickerRunsSetLineEnding(t *testing.T) {
	var calls []call
	p, ok := NewPicker(KindLineEnding, nil, recorder(&calls), style.New(theme.Theme{}))
	if !ok {
		t.Fatal("no picker for the line ending segment")
	}
	paintPicker(p, 80, 20)
	p, _ = p.Update(keyPress(tea.KeyDown))
	_, _ = p.Update(keyPress(tea.KeyEnter))
	want := []call{{"set_line_ending", map[string]any{"name": "CRLF"}}}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("calls = %+v, want %+v", calls, want)
	}
}

func TestLanguagePickerRunsSetLanguage(t *testing.T) {
	var calls []call
	p, ok := NewPicker(KindLanguage, []string{"Go", "Text"}, recorder(&calls), style.New(theme.Theme{}))
	if !ok {
		t.Fatal("no picker for the language segment")
	}
	paintPicker(p, 80, 20)
	_, _ = p.Update(keyPress(tea.KeyEnter))
	want := []call{{"set_language", map[string]any{"name": "Go"}}}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("calls = %+v, want %+v", calls, want)
	}
}

func TestNewPickerOnlyForThePickerSegments(t *testing.T) {
	tests := []struct {
		kind Kind
		want bool
	}{
		{KindNone, false},
		{KindPosition, false},
		{KindEncoding, true},
		{KindLineEnding, true},
		{KindLanguage, true},
	}
	for _, tc := range tests {
		if _, ok := NewPicker(tc.kind, []string{"Go"}, nil, style.New(theme.Theme{})); ok != tc.want {
			t.Errorf("NewPicker(%v) ok = %v, want %v", tc.kind, ok, tc.want)
		}
	}
}

func TestEscapeClosesWithoutRunning(t *testing.T) {
	var calls []call
	p := encodingPicker(t, recorder(&calls))
	paintPicker(p, 80, 20)
	p, _ = p.Update(keyPress(tea.KeyEscape))
	if p.Active() || len(calls) != 0 {
		t.Errorf("active = %v, calls = %v; want closed and no call", p.Active(), calls)
	}
}

func TestArrowsStopAtTheEnds(t *testing.T) {
	var calls []call
	p := encodingPicker(t, recorder(&calls))
	paintPicker(p, 80, 20)
	p, _ = p.Update(keyPress(tea.KeyUp))
	_, _ = p.Update(keyPress(tea.KeyEnter))
	if got := calls[0].args["name"]; got != encoding.Names()[0] {
		t.Errorf("up at the top chose %v, want the first item", got)
	}
}

func TestClickOnAnItemRunsIt(t *testing.T) {
	var calls []call
	p := encodingPicker(t, recorder(&calls))
	paintPicker(p, 80, 20)
	x, y := p.box.x+1, p.box.y+2
	p, _ = p.Update(clickAt(x, y))
	if len(calls) != 1 || calls[0].args["name"] != encoding.Names()[2] {
		t.Errorf("click on the third row ran %+v, want the third encoding", calls)
	}
	if p.Active() {
		t.Error("picker is still open after a click on an item")
	}
}

func TestClickOutsideClosesWithoutRunning(t *testing.T) {
	var calls []call
	p := encodingPicker(t, recorder(&calls))
	paintPicker(p, 80, 20)
	p, _ = p.Update(clickAt(0, 0))
	if p.Active() || len(calls) != 0 {
		t.Errorf("active = %v, calls = %v; want closed and no call", p.Active(), calls)
	}
}

func TestDrawPlacesTheListFlushRightAboveTheStatusRow(t *testing.T) {
	p := encodingPicker(t, nil)
	const w, h = 60, 12
	g := paintPicker(p, w, h)
	if p.box.x+p.box.w != w {
		t.Errorf("list ends at column %d, want %d", p.box.x+p.box.w, w)
	}
	if last := p.box.y + p.box.h - 1; last != h-2 {
		t.Errorf("list ends at row %d, want the row above the status row %d", last, h-2)
	}
	if g.At(w-1, h-1).Text != " " {
		t.Error("status row was painted over")
	}
}

func TestClosedPickerDrawsNothing(t *testing.T) {
	p := encodingPicker(t, nil)
	p, _ = p.Update(keyPress(tea.KeyEscape))
	g := paintPicker(p, 60, 12)
	for y := range g.Height() {
		for x := range g.Width() {
			if g.At(x, y).Text != " " {
				t.Fatalf("cell (%d,%d) = %q after close, want blank", x, y, g.At(x, y).Text)
			}
		}
	}
}
