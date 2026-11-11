package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/explorer"
	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/internal/tui/icons"
	"github.com/tbxark/gvim/theme"
)

// iconModel is a model over a fake editor with two tabs, an open explorer and the
// icon set named by name.
func iconModel(t *testing.T, name string) *Model {
	t.Helper()
	ed := newFakeEditor("hello")
	ed.quitAction = "quit"
	ed.tabs = []TabInfo{{Name: "a.txt", Active: true}, {Name: "b.txt"}}
	view := DefaultViewOptions()
	view.UIIcons = name
	m := New(Deps{Editor: ed, Explorer: newFakeTree(3), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock, View: &view})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	press(m, ctrl('b'))
	return m
}

func TestIconModeKeepsEveryButtonInPlace(t *testing.T) {
	t.Parallel()
	tabs := []TabInfo{{Name: "a.txt"}, {Name: "b.txt", Active: true}}
	for _, w := range []int{20, 30, 60, 100} {
		for override := 15; override <= 40; override++ {
			plain := layoutFor(w, 30, layoutInput{Tabs: tabs, ExplorerOpen: true, ExplorerWidth: explorer.ColumnsWidth(override)})
			nerd := layoutFor(w, 30, layoutInput{Tabs: tabs, ExplorerOpen: true, ExplorerWidth: explorer.ColumnsWidth(override), IconMode: icons.Nerd})
			if plain.ExReloadX != nerd.ExReloadX || plain.ExCloseX != nerd.ExCloseX {
				t.Fatalf("w=%d ex=%d: buttons at %d,%d (nerd) and %d,%d (plain)",
					w, plain.Ex, nerd.ExReloadX, nerd.ExCloseX, plain.ExReloadX, plain.ExCloseX)
			}
			if len(plain.Strip.Runs) != len(nerd.Strip.Runs) {
				t.Fatalf("w=%d: %d strip runs (nerd), %d (plain)", w, len(nerd.Strip.Runs), len(plain.Strip.Runs))
			}
			for i, r := range nerd.Strip.Runs {
				p := plain.Strip.Runs[i]
				if r.X != p.X || r.W != p.W || r.Kind != p.Kind || r.Index != p.Index {
					t.Fatalf("w=%d: strip run %d is %+v (nerd), %+v (plain)", w, i, r, p)
				}
			}
		}
	}
}

func TestNarrowExplorerHidesButtonsInNerdMode(t *testing.T) {
	t.Parallel()
	// The reload button needs 17 columns and the close button 19, as in plain mode.
	for _, tt := range []struct{ ex, reload, closeX int }{{16, -1, -1}, {17, 16, -1}, {18, 16, -1}, {19, 16, 18}, {30, 25, 27}} {
		l := layoutFor(100, 30, layoutInput{ExplorerOpen: true, ExplorerWidth: explorer.ColumnsWidth(tt.ex), IconMode: icons.Nerd})
		if l.Ex != tt.ex || l.ExReloadX != tt.reload || l.ExCloseX != tt.closeX {
			t.Errorf("E=%d: buttons at %d and %d, want %d and %d", tt.ex, l.ExReloadX, l.ExCloseX, tt.reload, tt.closeX)
		}
		if l.ExReloadX >= 0 && l.ExCloseX >= 0 && l.ExCloseX-l.ExReloadX < 2 {
			t.Errorf("E=%d: buttons at %d and %d touch", tt.ex, l.ExReloadX, l.ExCloseX)
		}
	}
}

func TestIconModeDrawsTheChosenSet(t *testing.T) {
	t.Parallel()
	for _, mode := range []icons.Mode{icons.Plain, icons.Nerd} {
		m := iconModel(t, mode.String())
		l := m.layout()
		g := m.paint()
		cell := func(x, y int) string { return g.At(x, y).Text }
		if got := cell(l.ExReloadX, l.ExTitleY); got != mode.Glyph(icons.Refresh) {
			t.Errorf("%v: refresh cell %q", mode, got)
		}
		if got := cell(l.ExCloseX, l.ExTitleY); got != mode.Glyph(icons.Close) {
			t.Errorf("%v: explorer close cell %q", mode, got)
		}
		// The new-tab button is three cells wide, so the glyph keeps one blank on
		// each side and sits in its middle cell.
		if got := cell(l.W-3, l.StripY); got != mode.Glyph(icons.New) {
			t.Errorf("%v: new-tab cell %q", mode, got)
		}
		closes := 0
		for _, r := range l.Strip.Runs {
			if r.Kind == hitTabClose {
				closes++
				if got := cell(r.X, l.StripY); got != mode.Glyph(icons.Close) {
					t.Errorf("%v: tab close cell %q", mode, got)
				}
			}
		}
		if closes != 2 {
			t.Errorf("%v: %d tab close buttons, want 2", mode, closes)
		}
	}
}

func TestNerdTabsShowTheFileIcon(t *testing.T) {
	t.Parallel()
	plain, nerd := iconModel(t, "plain"), iconModel(t, "nerd")
	pl, nl := plain.layout(), nerd.layout()
	want := icons.Nerd.FileIcon("a.txt", false, false)
	g := nerd.paint()
	// A tab has two blanks before the name; the icon and a following blank
	// make every tab two cells wider.
	first := nl.Strip.Runs[0]
	if got := g.At(first.X+2, nl.StripY).Text; got != want {
		t.Errorf("first tab icon cell %q, want %q", got, want)
	}
	if got := nl.Strip.Runs[1].X - pl.Strip.Runs[1].X; got != 2 {
		t.Errorf("close button of the first tab moved by %d cells, want 2", got)
	}
	for _, r := range pl.Strip.Runs {
		if got := plain.paint().At(r.X, pl.StripY).Text; r.Kind == hitTab && strings.Contains(got, want) {
			t.Errorf("plain tab shows the file icon at %d", r.X)
		}
	}
}

func TestNerdTabScrollMarkersAndFoldMarks(t *testing.T) {
	t.Parallel()
	var tabs []TabInfo
	for _, n := range []string{"alpha.go", "bravo.go", "charlie.go", "delta.go", "echo.go"} {
		tabs = append(tabs, TabInfo{Name: n})
	}
	s := frame.PlaceStrip(0, 30, tabs, 1, icons.Nerd)
	var left, right bool
	for _, r := range s.Runs {
		left = left || (r.Kind == hitTabScrollBefore && r.Text == icons.Nerd.Glyph(icons.ScrollLeft))
		right = right || (r.Kind == hitTabScrollAfter && r.Text == icons.Nerd.Glyph(icons.ScrollRight))
	}
	if !left || !right {
		t.Errorf("scroll markers: left %v right %v, want both in the nerd set", left, right)
	}
}

func TestUnknownIconNameIsPlain(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"", "emoji"} {
		if got := newViewState(ViewOptions{UIIcons: name}).icons; got != icons.Plain {
			t.Errorf("UIIcons %q gives %v, want plain", name, got)
		}
	}
}

func TestNerdModeClicksHitTheButtons(t *testing.T) {
	t.Parallel()
	m := iconModel(t, "nerd")
	l := m.layout()
	for _, r := range l.Strip.Runs {
		if r.Kind != hitTabClose {
			continue
		}
		if got := l.Hit(r.X, l.StripY); got.Kind != hitTabClose || got.Index != r.Index {
			t.Errorf("close of tab %d at %d hit as %+v", r.Index, r.X, got)
		}
		if got := l.Hit(r.X-1, l.StripY); got.Kind != hitTab {
			t.Errorf("cell left of the close of tab %d hit as %+v", r.Index, got)
		}
	}
	_, cmd := m.Update(click(l.ExCloseX, l.ExTitleY, tea.MouseLeft))
	drain(t, m, cmd)
	if m.explorerOpen {
		t.Fatal("a press on the nerd close button left the explorer open")
	}
}

func TestIconPreviewShowsBothSets(t *testing.T) {
	t.Parallel()
	m := newUtilityModel(t, newFakeEditor("hello"), testKeys(), testSettings(), nil)
	runAction(t, m, "open_settings")
	press(m, typed("u"))
	press(m, typed("i"))
	drain(t, m, press(m, special(tea.KeyEnter)))
	if !frameHas(m, "Interface icons") {
		t.Fatal("choosing ui_icons did not open the preview")
	}
	frame := strings.Join(frameText(m), "\n")
	for _, i := range icons.All() {
		for _, mode := range []icons.Mode{icons.Plain, icons.Nerd} {
			if !strings.Contains(frame, mode.Glyph(i)) {
				t.Errorf("preview lacks the %v glyph %q of %s", mode, mode.Glyph(i), iconLabels[i])
			}
		}
	}
	for _, i := range []icons.Icon{icons.Folder, icons.File, icons.ScrollLeft} {
		if !strings.Contains(frame, icons.Nerd.Glyph(i)) {
			t.Errorf("preview lacks the nerd glyph of %s", iconLabels[i])
		}
	}
	if !strings.Contains(frame, icons.Nerd.FileIcon("a.go", false, false)) || !strings.Contains(frame, icons.Nerd.GitMark('M')) {
		t.Error("preview lacks the file type icons or the git marks")
	}
	for _, want := range []string{"ui_icons_font", "restart gvim", "set by ui_icons", "ui_icons: nerd"} {
		if !strings.Contains(frame, want) {
			t.Errorf("preview lacks the instruction %q", want)
		}
	}
}

func TestIconPreviewEnterOpensTheConfigFile(t *testing.T) {
	t.Parallel()
	ed := &fileFake{fakeEditor: newFakeEditor("hello")}
	m := newUtilityModel(t, ed, testKeys(), testSettings(), nil)
	runAction(t, m, "open_settings")
	press(m, typed("u"))
	press(m, typed("i"))
	drain(t, m, press(m, special(tea.KeyEnter))) // the preview
	press(m, special(tea.KeyEnter))              // the config file
	if got := strings.Join(ed.opened, ","); got != testSettings().ConfigFile {
		t.Fatalf("opened %q, want the user config file", got)
	}
	if len(m.overlays) != 0 {
		t.Fatal("the preview stayed open")
	}
}

func TestIconPreviewEscClosesIt(t *testing.T) {
	t.Parallel()
	m := newUtilityModel(t, newFakeEditor("hello"), testKeys(), testSettings(), nil)
	runAction(t, m, "open_settings")
	press(m, typed("u"))
	press(m, typed("i"))
	drain(t, m, press(m, special(tea.KeyEnter)))
	press(m, special(tea.KeyEscape))
	if len(m.overlays) != 0 {
		t.Fatalf("%d overlays after Esc on the preview", len(m.overlays))
	}
}

func TestOtherSettingsRowsStillOpenTheConfigFile(t *testing.T) {
	t.Parallel()
	ed := &fileFake{fakeEditor: newFakeEditor("hello")}
	m := newUtilityModel(t, ed, testKeys(), testSettings(), nil)
	runAction(t, m, "open_settings")
	press(m, typed("t"))
	press(m, typed("h"))
	press(m, special(tea.KeyEnter))
	if len(ed.opened) != 1 || frameHas(m, "Interface icons") {
		t.Fatalf("theme row opened %v, preview shown %v", ed.opened, frameHas(m, "Interface icons"))
	}
}
