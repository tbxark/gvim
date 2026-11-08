package tui

import (
	"strconv"
	"testing"

	"github.com/tbxark/gvim/pluginapi"
)

// TestExplorerHostStore checks the decoration/slot store: normalization, priority and
// namespace tie-breaks, slot overrides and suppression, owner cleanup and caps.
func TestExplorerHostStore(t *testing.T) {
	rt := newFakeRuntime(t)
	m, _, _ := newPluginEditorModel(t, rt)
	h := m.plug.host
	a, b := "/w/a.go", "/w/b.go"
	if err := h.SetExplorerDecorations("p", "git", []pluginapi.FileExplorerDecoration{
		{Path: a, Symbol: "M", Color: pluginapi.Color{Set: true, R: 255}, Priority: 50},
		{Path: b, Symbol: "U", Priority: 30},
	}); err != nil {
		t.Fatal(err)
	}
	rd := m.explorerRowDecor(a)
	if rd.Trailing != "M" || rd.TrailingStyle.FG == nil {
		t.Fatalf("row decor = %+v", rd)
	}
	// Higher priority in another namespace wins; a tie goes to the earlier namespace name.
	if err := h.SetExplorerDecorations("p", "zed", []pluginapi.FileExplorerDecoration{
		{Path: a, Symbol: "X", Priority: 10},
	}); err != nil {
		t.Fatal(err)
	}
	if got := m.explorerRowDecor(a).Trailing; got != "M" {
		t.Fatalf("higher priority decoration lost: %q", got)
	}
	if err := h.SetExplorerDecorations("p", "aaa", []pluginapi.FileExplorerDecoration{
		{Path: a, Symbol: "T", Priority: 50},
	}); err != nil {
		t.Fatal(err)
	}
	if got := m.explorerRowDecor(a).Trailing; got != "T" {
		t.Fatalf("namespace tie-break = %q, want aaa", got)
	}

	// Slot overrides: trailing text, leading suppression and the name colour.
	if err := h.SetExplorerSlots("p", "git", []pluginapi.FileExplorerSlot{{
		Path: a, SuppressLeading: true,
		Trailing:  &pluginapi.FileExplorerSlotText{Text: "!", Color: pluginapi.Color{Set: true, G: 255}},
		NameColor: pluginapi.Color{Set: true, B: 255},
	}}); err != nil {
		t.Fatal(err)
	}
	rd = m.explorerRowDecor(a)
	if !rd.SuppressLeading || rd.Trailing != "!" || rd.NameColor == nil {
		t.Fatalf("slot decor = %+v", rd)
	}
	if got := m.explorerRowDecor(b).Trailing; got != "U" {
		t.Fatalf("decoration for another row = %q", got)
	}
	// Suppress the decoration's trailing slot entirely.
	if err := h.SetExplorerSlots("p", "git", []pluginapi.FileExplorerSlot{{Path: b, SuppressTrailing: true, Priority: 5}}); err != nil {
		t.Fatal(err)
	}
	if got := m.explorerRowDecor(b).Trailing; got != "" {
		t.Fatalf("suppressed trailing = %q", got)
	}

	// Relative paths are ignored without a workspace root; the clean absolute path works.
	if err := h.SetExplorerDecorations("p", "rel", []pluginapi.FileExplorerDecoration{{Path: "a.go", Symbol: "R"}}); err != nil {
		t.Fatal(err)
	}
	if got := m.explorerRowDecor("a.go").Trailing; got != "" {
		t.Fatalf("a relative path was stored without a root: %q", got)
	}

	// Namespace and entry caps: fill up to the cap from what the test already used.
	used := m.plug.explorer.countPlugin("p")
	for i := used; i < pluginapi.MaxExplorerNamespaces; i++ {
		if err := h.SetExplorerDecorations("p", "cap"+strconv.Itoa(i), []pluginapi.FileExplorerDecoration{{Path: a, Symbol: "x"}}); err != nil {
			t.Fatalf("namespace %d: %v", i, err)
		}
	}
	if err := h.SetExplorerDecorations("p", "one-too-many", []pluginapi.FileExplorerDecoration{{Path: a, Symbol: "x"}}); pluginapi.CodeOf(err) != pluginapi.CodeLimit {
		t.Fatalf("namespace cap = %v", err)
	}
	big := make([]pluginapi.FileExplorerDecoration, pluginapi.MaxExplorerEntries+1)
	for i := range big {
		big[i] = pluginapi.FileExplorerDecoration{Path: a, Symbol: "x"}
	}
	if err := h.SetExplorerDecorations("q", "big", big); pluginapi.CodeOf(err) != pluginapi.CodeLimit {
		t.Fatalf("entry cap = %v", err)
	}

	// Owner cleanup drops everything the plugin set.
	h.OwnerGone("p", 1)
	if got := m.explorerRowDecor(a); got.Trailing != "" || got.NameColor != nil || got.SuppressLeading {
		t.Fatalf("unload left explorer decoration: %+v", got)
	}
}
