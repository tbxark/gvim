package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/theme"
	"github.com/tbxark/gvim/internal/ui/icons"
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
			plain := layoutFor(w, 30, layoutInput{tabs: tabs, explorerOpen: true, explorerOverride: override})
			nerd := layoutFor(w, 30, layoutInput{tabs: tabs, explorerOpen: true, explorerOverride: override, iconMode: icons.Nerd})
			if plain.exReloadX != nerd.exReloadX || plain.exCloseX != nerd.exCloseX {
				t.Fatalf("w=%d ex=%d: buttons at %d,%d (nerd) and %d,%d (plain)",
					w, plain.ex, nerd.exReloadX, nerd.exCloseX, plain.exReloadX, plain.exCloseX)
			}
			if len(plain.strip.runs) != len(nerd.strip.runs) {
				t.Fatalf("w=%d: %d strip runs (nerd), %d (plain)", w, len(nerd.strip.runs), len(plain.strip.runs))
			}
			for i, r := range nerd.strip.runs {
				p := plain.strip.runs[i]
				if r.x != p.x || r.w != p.w || r.kind != p.kind || r.index != p.index {
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
		l := layoutFor(100, 30, layoutInput{explorerOpen: true, explorerOverride: tt.ex, iconMode: icons.Nerd})
		if l.ex != tt.ex || l.exReloadX != tt.reload || l.exCloseX != tt.closeX {
			t.Errorf("E=%d: buttons at %d and %d, want %d and %d", tt.ex, l.exReloadX, l.exCloseX, tt.reload, tt.closeX)
		}
		if l.exReloadX >= 0 && l.exCloseX >= 0 && l.exCloseX-l.exReloadX < 2 {
			t.Errorf("E=%d: buttons at %d and %d touch", tt.ex, l.exReloadX, l.exCloseX)
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
		if got := cell(l.exReloadX, 1); got != mode.Glyph(icons.Refresh) {
			t.Errorf("%v: refresh cell %q", mode, got)
		}
		if got := cell(l.exCloseX, 1); got != mode.Glyph(icons.Close) {
			t.Errorf("%v: explorer close cell %q", mode, got)
		}
		if got := cell(l.w-1, l.stripY); got != mode.Glyph(icons.New) {
			t.Errorf("%v: new-tab cell %q", mode, got)
		}
		closes := 0
		for _, r := range l.strip.runs {
			if r.kind == hitTabClose {
				closes++
				if got := cell(r.x, l.stripY); got != mode.Glyph(icons.Close) {
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
	// A tab is " name " with a blank before the name; the icon takes that place and
	// a blank follows it, so every tab is two cells wider.
	first := nl.strip.runs[0]
	if got := g.At(first.x+1, nl.stripY).Text; got != want {
		t.Errorf("first tab icon cell %q, want %q", got, want)
	}
	if got := nl.strip.runs[1].x - pl.strip.runs[1].x; got != 2 {
		t.Errorf("close button of the first tab moved by %d cells, want 2", got)
	}
	for _, r := range pl.strip.runs {
		if got := plain.paint().At(r.x, pl.stripY).Text; r.kind == hitTab && strings.Contains(got, want) {
			t.Errorf("plain tab shows the file icon at %d", r.x)
		}
	}
}

func TestNerdTabScrollMarkersAndFoldMarks(t *testing.T) {
	t.Parallel()
	var tabs []TabInfo
	for _, n := range []string{"alpha.go", "bravo.go", "charlie.go", "delta.go", "echo.go"} {
		tabs = append(tabs, TabInfo{Name: n})
	}
	s := placeStrip(0, 30, tabs, 1, icons.Nerd)
	var left, right bool
	for _, r := range s.runs {
		left = left || (r.kind == hitTabScrollBefore && r.text == icons.Nerd.Glyph(icons.ScrollLeft))
		right = right || (r.kind == hitTabScrollAfter && r.text == icons.Nerd.Glyph(icons.ScrollRight))
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
	for _, r := range l.strip.runs {
		if r.kind != hitTabClose {
			continue
		}
		if got := l.hit(r.x, l.stripY); got.Kind != hitTabClose || got.Index != r.index {
			t.Errorf("close of tab %d at %d hit as %+v", r.index, r.x, got)
		}
		if got := l.hit(r.x-1, l.stripY); got.Kind != hitTab {
			t.Errorf("cell left of the close of tab %d hit as %+v", r.index, got)
		}
	}
	_, cmd := m.Update(click(l.exCloseX, 1, tea.MouseLeft))
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
