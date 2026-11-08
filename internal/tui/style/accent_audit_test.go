package style

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/theme"
)

// popupSelectionRefs are Fresh's popup selection colors, which gvim's accent
// extension keys fall back to.
var (
	refPopupSelectionFG = ref{"ui", "popup_selection_fg"}
	refPopupSelectionBG = ref{"ui", "popup_selection_bg"}
)

// TestBundledThemesPaintThePopupSelectionAccent pins the accent audit at the style
// layer: every bundled theme's Accent role and every accent surface of the overlay
// styles paint the theme's popup selection colors, because the bundled themes leave
// ui.accent_* to their fallbacks.
func TestBundledThemesPaintThePopupSelectionAccent(t *testing.T) {
	t.Parallel()
	for _, name := range theme.Names() {
		th, err := theme.Load(name)
		if err != nil {
			t.Fatalf("Load(%q): %v", name, err)
		}
		want := Cell(build(th, refPopupSelectionFG, refPopupSelectionBG))
		st := New(th)
		if !samePaint(Cell(st.Accent), want) {
			t.Errorf("%s: Accent = %+v, want the popup selection %+v", name, Cell(st.Accent), want)
		}
		ov := NewOverlay(th)
		if !samePaint(ov.Selected, want) {
			t.Errorf("%s: overlay Selected = %+v, want the accent %+v", name, ov.Selected, want)
		}
		focused := want
		focused.Bold = true
		if !samePaint(ov.Focus, focused) {
			t.Errorf("%s: overlay Focus = %+v, want the bold accent %+v", name, ov.Focus, focused)
		}
		// The overlay border takes the accent as its foreground.
		wantBorder := Cell(build(th, refAccentBG, refPopupBG))
		if !samePaint(ov.Border, wantBorder) {
			t.Errorf("%s: overlay Border = %+v, want %+v", name, ov.Border, wantBorder)
		}
	}
}

// samePaint compares the colours and attributes of two grid styles, ignoring the
// per-cell provenance ids the overlay roles now carry.
func samePaint(a, b grid.Style) bool {
	a.FGKey, a.BGKey, b.FGKey, b.BGKey = 0, 0, 0, 0
	return reflect.DeepEqual(a, b)
}

// TestStandaloneThemePaintsThePopupSelectionAccent checks the fallback path a user
// theme can rely on: naming the popup selection but not the accent keys paints the
// accent roles from the popup selection.
func TestStandaloneThemePaintsThePopupSelectionAccent(t *testing.T) {
	t.Parallel()
	data := standaloneAccentThemeJSON(t, map[string][3]int{
		"ui.popup_selection_bg": {11, 22, 33},
		"ui.popup_selection_fg": {44, 55, 66},
	})
	th, err := theme.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	st := New(th)
	accent := Cell(st.Accent)
	if accent.BG == nil || !samePaint(Cell(build(th, refPopupSelectionFG, refPopupSelectionBG)), accent) {
		t.Fatalf("Accent = %+v, want the popup selection colors", accent)
	}
}

// standaloneAccentThemeJSON names every required color and the overrides, so the
// theme needs no base and its omitted accent keys are filled by the chain.
func standaloneAccentThemeJSON(t *testing.T, overrides map[string][3]int) []byte {
	t.Helper()
	doc := map[string]any{"name": "accent-style-audit"}
	sections := map[string]map[string][]int{}
	set := func(name string, rgb [3]int) {
		section, key, ok := cutThemeKey(name)
		if !ok {
			t.Fatalf("bad key %q", name)
		}
		if sections[section] == nil {
			sections[section] = map[string][]int{}
		}
		sections[section][key] = rgb[:]
	}
	for _, name := range theme.RequiredKeys() {
		set(name, [3]int{10, 20, 30})
	}
	for name, rgb := range overrides {
		set(name, rgb)
	}
	for section, values := range sections {
		doc[section] = values
	}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// cutThemeKey splits a canonical "section.key" theme key.
func cutThemeKey(name string) (section, key string, ok bool) {
	for i := range name {
		if name[i] == '.' {
			return name[:i], name[i+1:], true
		}
	}
	return "", "", false
}
