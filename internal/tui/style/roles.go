package style

import (
	"charm.land/lipgloss/v2"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/theme"
)

// Theme-key ids are the theme inspector's per-cell provenance: a style records
// which theme.Keys() entry its foreground and background came from. The ids are
// dense (1..len(theme.Keys())) in key-table order and zero means unknown (a color
// no theme key names, or a plain grid.Style built without a role). The table is
// the single place section+key becomes an id; aliases resolve through
// theme.KeyByName, so "ui.popup_fg" is the same id as "ui.popup_text_fg".
var (
	themeKeyIDs   map[string]uint16
	themeKeyTable []theme.Key
)

func init() {
	keys := theme.Keys()
	themeKeyTable = keys
	themeKeyIDs = make(map[string]uint16, len(keys))
	for i, key := range keys {
		themeKeyIDs[key.Name()] = uint16(i + 1)
	}
}

// KeyIDFor returns the dense id of a theme key, 0 for a key the table does not
// know. section and key are named separately because call sites take them from
// style refs.
func KeyIDFor(section, key string) uint16 {
	if section == "" || key == "" {
		return 0
	}
	return KeyID(section + "." + key)
}

// KeyID returns the dense id of a canonical or aliased theme key name, 0 when the
// name is not a theme key.
func KeyID(name string) uint16 {
	key, ok := theme.KeyByName(name)
	if !ok {
		return 0
	}
	return themeKeyIDs[key.Name()]
}

// KeyName resolves an id back to the theme key it names.
func KeyName(id uint16) (section, key string, ok bool) {
	if id == 0 || int(id) > len(themeKeyTable) {
		return "", "", false
	}
	entry := themeKeyTable[id-1]
	return entry.Section, entry.Key, true
}

// TaggedCell converts a lipgloss style and records the theme keys its foreground
// and background come from, for the inspector. Empty section/key pairs leave that
// side unknown; the colors come from st itself.
func TaggedCell(st lipgloss.Style, fgSection, fgKey, bgSection, bgKey string) grid.Style {
	c := Cell(st)
	c.FGKey = KeyIDFor(fgSection, fgKey)
	c.BGKey = KeyIDFor(bgSection, bgKey)
	return c
}

// gridStyleWithKeys is TaggedStyle for ids that are already resolved (the overlay
// roles mix colors, so their grid.Style is built directly).
func gridStyleWithKeys(c grid.Style, fg, bg uint16) grid.Style {
	c.FGKey, c.BGKey = fg, bg
	return c
}

// focusAccent is the accent cell made bold for the focused button, keeping its
// captured theme-key ids.
func focusAccent(accent grid.Style) grid.Style {
	accent.Bold = true
	return accent
}

// TaggedStyle records the ids of the given theme keys on an existing grid style.
// It is the direct-grid.Style form of TaggedCell (the overlay roles build their
// grid styles themselves). Empty section/key pairs leave that side unchanged.
func TaggedStyle(c grid.Style, fgSection, fgKey, bgSection, bgKey string) grid.Style {
	if fgSection != "" && fgKey != "" {
		c.FGKey = KeyIDFor(fgSection, fgKey)
	}
	if bgSection != "" && bgKey != "" {
		c.BGKey = KeyIDFor(bgSection, bgKey)
	}
	return c
}
