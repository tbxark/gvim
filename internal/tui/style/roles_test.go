package style

import (
	"testing"

	"reflect"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/theme"
)

// TestThemeKeyIDsAreDenseUniqueAndRoundTrip pins the role table the provenance
// uses: every theme key has one dense non-zero id in table order, ids resolve back
// to exactly that key, and an alias resolves to the canonical key's id.
func TestThemeKeyIDsAreDenseUniqueAndRoundTrip(t *testing.T) {
	t.Parallel()
	keys := theme.Keys()
	seen := map[uint16]bool{}
	for i, key := range keys {
		id := KeyIDFor(key.Section, key.Key)
		if want := uint16(i + 1); id != want {
			t.Fatalf("KeyIDFor(%s) = %d, want the dense id %d", key.Name(), id, want)
		}
		if seen[id] {
			t.Fatalf("duplicate id %d", id)
		}
		seen[id] = true
		section, name, ok := KeyName(id)
		if !ok || section != key.Section || name != key.Key {
			t.Fatalf("KeyName(%d) = %s.%s (%v), want %s", id, section, name, ok, key.Name())
		}
	}
	if len(seen) != len(keys) {
		t.Fatalf("%d ids for %d keys", len(seen), len(keys))
	}
	if got, want := KeyID("ui.popup_fg"), KeyID("ui.popup_text_fg"); got != want || got == 0 {
		t.Errorf("alias ui.popup_fg id = %d, want the canonical %d", got, want)
	}
	if KeyIDFor("ui", "not_a_key") != 0 || KeyID("nonsense") != 0 {
		t.Error("an unknown key got a non-zero id")
	}
	if _, _, ok := KeyName(0); ok || func() bool { _, _, ok := KeyName(uint16(len(keys) + 1)); return ok }() {
		t.Error("an out-of-range id resolved")
	}
}

// TestTaggedCellKeepsColoursAndRecordsKeys checks the conversion: the colours are
// style.Cell's, and the key ids are the theme keys named.
func TestTaggedCellKeepsColoursAndRecordsKeys(t *testing.T) {
	t.Parallel()
	dark, err := theme.Load("dark")
	if err != nil {
		t.Fatal(err)
	}
	plain := Cell(build(dark, ref{"editor", "fg"}, ref{"editor", "bg"}))
	tagged := TaggedCell(build(dark, ref{"editor", "fg"}, ref{"editor", "bg"}), "editor", "fg", "editor", "bg")
	if !sameGridStyle(plain, tagged) {
		t.Fatalf("tagging changed the colours: %+v vs %+v", plain, tagged)
	}
	if tagged.FGKey != KeyIDFor("editor", "fg") || tagged.BGKey != KeyIDFor("editor", "bg") {
		t.Errorf("tagged ids = %d/%d, want editor.fg/editor.bg", tagged.FGKey, tagged.BGKey)
	}
	// A direct grid.Style can be tagged the same way.
	direct := TaggedStyle(plain, "ui", "accent_fg", "", "")
	if direct.FGKey != KeyIDFor("ui", "accent_fg") || direct.BGKey != 0 {
		t.Errorf("TaggedStyle = %d/%d, want accent_fg/0", direct.FGKey, direct.BGKey)
	}
}

// TestOverlayRolesCarryThemeKeys checks the overlay construction tagging: the box,
// selection and border styles name the popup/accent keys they are built from, so
// palette, settings and popup rows inherit provenance for free.
func TestOverlayRolesCarryThemeKeys(t *testing.T) {
	t.Parallel()
	dark, err := theme.Load("dark")
	if err != nil {
		t.Fatal(err)
	}
	ov := NewOverlay(dark)
	if ov.Box.BGKey != KeyIDFor("ui", "popup_bg") || ov.Box.FGKey != KeyIDFor("ui", "popup_text_fg") {
		t.Errorf("Box keys = %d/%d, want popup_text_fg/popup_bg", ov.Box.FGKey, ov.Box.BGKey)
	}
	if ov.Selected.FGKey != KeyIDFor("ui", "accent_fg") || ov.Selected.BGKey != KeyIDFor("ui", "accent_bg") {
		t.Errorf("Selected keys = %d/%d, want accent_fg/accent_bg", ov.Selected.FGKey, ov.Selected.BGKey)
	}
	if ov.Focus.FGKey != KeyIDFor("ui", "accent_fg") || ov.Focus.BGKey != KeyIDFor("ui", "accent_bg") {
		t.Errorf("Focus keys = %d/%d, want accent_fg/accent_bg", ov.Focus.FGKey, ov.Focus.BGKey)
	}
	if ov.Border.FGKey != KeyIDFor("ui", "accent_bg") || ov.Border.BGKey != KeyIDFor("ui", "popup_bg") {
		t.Errorf("Border keys = %d/%d, want accent_bg/popup_bg", ov.Border.FGKey, ov.Border.BGKey)
	}
	if ov.Muted.FGKey != KeyIDFor("ui", "popup_text_fg") || ov.Muted.BGKey != KeyIDFor("ui", "popup_bg") {
		t.Errorf("Muted keys = %d/%d, want popup_text_fg/popup_bg", ov.Muted.FGKey, ov.Muted.BGKey)
	}
}

// sameGridStyle compares grid styles by colours and attributes, ignoring the
// provenance ids.
func sameGridStyle(a, b grid.Style) bool {
	a.FGKey, a.BGKey, b.FGKey, b.BGKey = 0, 0, 0, 0
	return reflect.DeepEqual(a, b)
}
