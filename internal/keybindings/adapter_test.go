package keybindings

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/internal/apptest"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/keymap"
	"github.com/tbxark/gvim/pluginapi"
)

func TestKeymapAdapterResolvesInTheContext(t *testing.T) {
	k := adapter{km: keymap.Default()}
	save, ok := k.Resolve(tui.KeyPress{Name: "s", Mods: []string{"ctrl"}}, tui.KeyContextNormal)
	if !ok || save.Action != "save" {
		t.Fatalf("ctrl+s in normal = %+v, %v", save, ok)
	}
	// Save is bound in the normal context only, so the explorer context does not see it.
	if cmd, ok := k.Resolve(tui.KeyPress{Name: "s", Mods: []string{"ctrl"}}, tui.KeyContextFileExplorer); ok {
		t.Errorf("ctrl+s in the explorer = %+v, want no binding", cmd)
	}
	down, ok := k.Resolve(tui.KeyPress{Name: "Down"}, tui.KeyContextFileExplorer)
	if !ok || down.Action != "file_explorer_down" {
		t.Errorf("Down in the explorer = %+v, %v", down, ok)
	}
	if editorDown, ok := k.Resolve(tui.KeyPress{Name: "Down"}, tui.KeyContextNormal); !ok || editorDown.Action == "file_explorer_down" {
		t.Errorf("Down in the normal context = %+v, %v; want the editor binding", editorDown, ok)
	}
	if _, ok := k.Resolve(tui.KeyPress{Name: "NoSuchKey"}, tui.KeyContextNormal); ok {
		t.Error("an unknown key name resolved")
	}
}

func TestKeymapAdapterResolvesSequences(t *testing.T) {
	em, err := keymap.ByName("emacs")
	if err != nil {
		t.Fatal(err)
	}
	k := adapter{km: em}
	cx := tui.KeyPress{Name: "x", Mods: []string{"ctrl"}}
	cs := tui.KeyPress{Name: "s", Mods: []string{"ctrl"}}
	tests := []struct {
		name       string
		steps      []tui.KeyPress
		action     string
		ok, prefix bool
	}{
		{"first key is a prefix, not a binding", []tui.KeyPress{cx}, "", false, true},
		{"C-x C-s saves", []tui.KeyPress{cx, cs}, "save", true, false},
		{"C-x k closes", []tui.KeyPress{cx, {Name: "k"}}, "close", true, false},
		{"M-g g goes to a line", []tui.KeyPress{{Name: "g", Mods: []string{"alt"}}, {Name: "g"}}, "goto_line", true, false},
		{"a miss", []tui.KeyPress{cx, {Name: "q"}}, "", false, false},
		{"a single binding is no prefix", []tui.KeyPress{cs}, "", false, false},
		{"a bad key name", []tui.KeyPress{{Name: "NoSuchKey"}}, "", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, ok, prefix := k.ResolveSequence(tt.steps, tui.KeyContextNormal)
			if cmd.Action != tt.action || ok != tt.ok || prefix != tt.prefix {
				t.Errorf("ResolveSequence = %q, %v, %v; want %q, %v, %v", cmd.Action, ok, prefix, tt.action, tt.ok, tt.prefix)
			}
		})
	}
}

func TestKeymapAdapterResolvesSuperBindings(t *testing.T) {
	km, err := keymap.ByName("macos-gui")
	if err != nil {
		t.Fatal(err)
	}
	k := adapter{km: km}
	tests := []struct {
		key    tui.KeyPress
		action string
	}{
		{tui.KeyPress{Name: "s", Mods: []string{"super"}}, "save"},
		{tui.KeyPress{Name: "p", Mods: []string{"super"}}, "quick_open"},
		{tui.KeyPress{Name: "q", Mods: []string{"super"}}, "quit"},
	}
	for _, tt := range tests {
		cmd, ok := k.Resolve(tt.key, tui.KeyContextNormal)
		if !ok || cmd.Action != tt.action {
			t.Errorf("%v = %+v, %v; want %s", tt.key, cmd, ok, tt.action)
		}
	}
}

func TestKeymapContextMapping(t *testing.T) {
	if got := keymapContext(tui.KeyContextFileExplorer); got != keymap.ContextFileExplorer {
		t.Errorf("explorer context = %q", got)
	}
	if got := keymapContext(tui.KeyContextNormal); got != keymap.ContextNormal {
		t.Errorf("normal context = %q", got)
	}
}

func TestKeymapAdapterListsItsBindings(t *testing.T) {
	t.Parallel()
	km := keymap.Default()
	k := adapter{km: km}
	got, want := k.Bindings(), km.Bindings()
	if len(got) != len(want) {
		t.Fatalf("Bindings() has %d entries, want %d", len(got), len(want))
	}
	for i, b := range want {
		if w := (tui.KeyBinding{Action: b.Action, Chord: b.Chord, Context: string(b.When)}); got[i] != w {
			t.Fatalf("Bindings()[%d] = %+v, want %+v", i, got[i], w)
		}
	}
	if p, ok := k.Primary("save"); !ok || p.Action != "save" || p.Chord == "" {
		t.Fatalf("Primary(save) = %+v, %v; want a chord for save", p, ok)
	}
	if _, ok := k.Primary("no_such_action"); ok {
		t.Fatal("Primary(no_such_action) found a binding")
	}
}

func TestTerminalKeyContextMapping(t *testing.T) {
	tests := []struct {
		name string
		ctx  tui.KeyContext
		want keymap.Context
	}{
		{name: "terminal dock", ctx: tui.KeyContextTerminal, want: keymap.ContextTerminal},
		{name: "file explorer", ctx: tui.KeyContextFileExplorer, want: keymap.ContextFileExplorer},
		{name: "normal editor", ctx: tui.KeyContextNormal, want: keymap.ContextNormal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := keymapContext(tt.ctx); got != tt.want {
				t.Fatalf("keymapContext(%q) = %q, want %q", tt.ctx, got, tt.want)
			}
		})
	}
}

func TestPluginKeymapLayers(t *testing.T) {
	cfg := config.Config{}
	userSaves := config.Config{Keybindings: json.RawMessage(`{"bindings":[{"key":"s","modifiers":["ctrl"],"action":"save_all","when":"normal"}]}`)}
	resolve := func(km *keymap.Map, chord string) string {
		t.Helper()
		b, ok := km.Resolve(chord, keymap.ContextNormal)
		if !ok {
			return ""
		}
		return b.Action
	}
	preset := keymap.Default()
	if got := resolve(preset, "ctrl+s"); got != "save" {
		t.Fatalf("preset ctrl+s = %q", got)
	}

	t.Run("a plugin binding beats the preset", func(t *testing.T) {
		km, err := PluginKeymap(preset, cfg, []pluginapi.Binding{{Chord: "ctrl+s", Action: "hello.hello", Plugin: "hello"}})
		apptest.Must(t, err)
		if got := resolve(km, "ctrl+s"); got != "hello.hello" {
			t.Fatalf("ctrl+s = %q, want the plugin command", got)
		}
		if got := resolve(preset, "ctrl+s"); got != "save" {
			t.Fatalf("the preset was changed: ctrl+s = %q", got)
		}
	})
	t.Run("the user's keybindings beat the plugin", func(t *testing.T) {
		km, err := PluginKeymap(preset, userSaves, []pluginapi.Binding{{Chord: "ctrl+s", Action: "hello.hello", Plugin: "hello"}})
		apptest.Must(t, err)
		if got := resolve(km, "ctrl+s"); got != "save_all" {
			t.Fatalf("ctrl+s = %q, want the user's binding", got)
		}
	})
	t.Run("a new chord, a modifier combination, the plus key and a sequence", func(t *testing.T) {
		km, err := PluginKeymap(preset, cfg, []pluginapi.Binding{
			{Chord: "ctrl+alt+shift+j", Action: "p.one", Plugin: "p"},
			{Chord: "Alt+F9", Action: "p.two", Plugin: "p"},
			{Chord: "ctrl++", Action: "p.plus", Plugin: "p"},
			{Chord: "ctrl+k ctrl+j", Action: "p.seq", Plugin: "p", Args: map[string]any{"n": float64(2)}},
		})
		apptest.Must(t, err)
		for chord, want := range map[string]string{"ctrl+alt+shift+j": "p.one", "alt+F9": "p.two", "ctrl++": "p.plus"} {
			if got := resolve(km, chord); got != want {
				t.Errorf("%s = %q, want %q", chord, got, want)
			}
		}
		b, ok := km.ResolveSequence([]string{"ctrl+k", "ctrl+j"}, keymap.ContextNormal)
		if !ok || b.Action != "p.seq" || b.Args["n"] != float64(2) {
			t.Errorf("sequence = %+v, %v", b, ok)
		}
	})
	t.Run("a bad chord is dropped and named, the others stay", func(t *testing.T) {
		km, err := PluginKeymap(preset, cfg, []pluginapi.Binding{
			{Chord: "ctrl+nonsense", Action: "p.bad", Plugin: "p"}, {Chord: "", Action: "p.empty", Plugin: "q"}, {Chord: "ctrl+alt+j", Action: "p.good", Plugin: "p"},
		})
		if err == nil || !strings.Contains(err.Error(), "ctrl+nonsense") || !strings.Contains(err.Error(), "q:") {
			t.Fatalf("error = %v, want it to name the bad chords", err)
		}
		if km == nil || resolve(km, "ctrl+alt+j") != "p.good" {
			t.Fatalf("the good binding is lost: %v", km)
		}
	})
	t.Run("no plugin bindings: the preset with the user's layer", func(t *testing.T) {
		km, err := PluginKeymap(preset, userSaves, nil)
		apptest.Must(t, err)
		if got := resolve(km, "ctrl+s"); got != "save_all" {
			t.Fatalf("ctrl+s = %q", got)
		}
	})
	t.Run("a broken user entry is left out and the rest applies", func(t *testing.T) {
		bad := config.Config{Keybindings: json.RawMessage(`[{"key":"s","modifiers":["bogus"],"action":"x"},{"key":"j","modifiers":["alt"],"action":"save_all"}]`)}
		km, err := PluginKeymap(preset, bad, nil)
		apptest.Must(t, err)
		if got := resolve(km, "alt+j"); got != "save_all" {
			t.Fatalf("alt+j = %q, want the good entry", got)
		}
		if got := resolve(km, "ctrl+s"); got != "save" {
			t.Fatalf("ctrl+s = %q, want the preset's save", got)
		}
	})
}

func TestSplitChord(t *testing.T) {
	tests := []struct {
		chord, key string
		mods       []string
	}{
		{"a", "a", []string{}},
		{"ctrl+s", "s", []string{"ctrl"}},
		{"ctrl+alt+shift+F5", "F5", []string{"ctrl", "alt", "shift"}},
		{"+", "+", []string{}},
		{"ctrl++", "+", []string{"ctrl"}},
		{"ctrl+alt++", "+", []string{"ctrl", "alt"}},
	}
	for _, tt := range tests {
		key, mods := splitChord(tt.chord)
		if key != tt.key || !slices.Equal(mods, tt.mods) {
			t.Errorf("splitChord(%q) = %q, %v, want %q, %v", tt.chord, key, mods, tt.key, tt.mods)
		}
	}
}

func TestKeymapAdapterLabel(t *testing.T) {
	k := adapter{km: keymap.Default()}
	if got := k.Label("save"); got == "" || !strings.HasSuffix(got, "S") {
		t.Errorf("Label(save) = %q, want a formatted ctrl+s", got)
	}
	if got := k.Label("no_such_action"); got != "" {
		t.Errorf("Label of an unbound action = %q", got)
	}
	// A plugin command bound through the layered keymap gets a label.
	km, err := PluginKeymap(keymap.Default(), config.Config{}, []pluginapi.Binding{{Chord: "ctrl+alt+j", Action: "hello.hello", Plugin: "hello"}})
	apptest.Must(t, err)
	if got := (adapter{km: km}).Label("hello.hello"); got == "" {
		t.Error("Label of a plugin command bound by a plugin is empty")
	}
}

// The adapter forwards the keymap's menu mnemonic lookup, so the menu bar can
// underline the Alt+letter of a menu the active keymap binds.
func TestKeymapAdapterMenuMnemonic(t *testing.T) {
	k := adapter{km: keymap.Default()}
	for name, want := range map[string]rune{
		"File": 'f', "Edit": 'e', "View": 'v', "Selection": 's', "Go": 'g',
		"Help": 'h', "Explorer": 'x', "LSP": 'l',
	} {
		if got, ok := k.MenuMnemonic(name); !ok || got != want {
			t.Errorf("MenuMnemonic(%q) = %q, %v; want %q", name, got, ok, want)
		}
	}
	if _, ok := k.MenuMnemonic("NoSuchMenu"); ok {
		t.Error("MenuMnemonic of a menu no binding names reported a letter")
	}
}

func TestOneBadBindingKeepsOthers(t *testing.T) {
	preset := keymap.Default()
	tests := []struct {
		name string
		bad  pluginapi.Binding
	}{
		{"empty action", pluginapi.Binding{Chord: "ctrl+alt+m", Action: "", Plugin: "c"}},
		{"unmarshalable args", pluginapi.Binding{Chord: "ctrl+alt+m", Action: "c.x", Plugin: "c", Args: map[string]any{"f": func() {}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			km, err := PluginKeymap(preset, config.Config{}, []pluginapi.Binding{
				{Chord: "ctrl+alt+j", Action: "a.one", Plugin: "a"},
				tt.bad,
				{Chord: "ctrl+alt+n", Action: "d.x", Plugin: "d"},
				{Chord: "ctrl+alt+o", Action: "c.ok", Plugin: "c"}, // a second binding of the dropped plugin
			})
			if err == nil || !strings.Contains(err.Error(), "plugin 'c'") {
				t.Fatalf("error = %v, want plugin 'c' named", err)
			}
			if km == nil {
				t.Fatal("no keymap: one plugin's bad binding unbound the others")
			}
			for chord, want := range map[string]string{"ctrl+alt+j": "a.one", "ctrl+alt+n": "d.x", "ctrl+alt+m": "", "ctrl+alt+o": ""} {
				got := ""
				if b, ok := km.Resolve(chord, keymap.ContextNormal); ok {
					got = b.Action
				}
				if got != want {
					t.Errorf("%s = %q, want %q", chord, got, want)
				}
			}
		})
	}
}

func TestPluginBindingConflictDropsOnlyLoser(t *testing.T) {
	km, err := PluginKeymap(keymap.Default(), config.Config{}, []pluginapi.Binding{
		{Chord: "ctrl+alt+j", Action: "a.one", Plugin: "a"}, {Chord: "Alt+Ctrl+J", Action: "b.one", Plugin: "b"},
		{Chord: "ctrl+alt+k", Action: "b.two", Plugin: "b"},
	})
	if err == nil || !strings.Contains(err.Error(), "plugin 'b': key ctrl+alt+j already bound by plugin 'a'") {
		t.Fatalf("error = %v, want the loser reported", err)
	}
	if km == nil {
		t.Fatal("no keymap")
	}
	for chord, want := range map[string]string{"ctrl+alt+j": "a.one", "ctrl+alt+k": "b.two"} {
		if b, ok := km.Resolve(chord, keymap.ContextNormal); !ok || b.Action != want {
			t.Errorf("%s = %+v, %v, want %q", chord, b, ok, want)
		}
	}
}

func TestKeymapDefaultsToPlatformKeymap(t *testing.T) {
	def, err := Keymap("")
	if err != nil {
		t.Fatal(err)
	}
	if def == nil {
		t.Fatal("Keymap(\"\") = nil")
	}
	named, err := Keymap("default")
	if err != nil {
		t.Fatalf("Keymap(default) error = %v", err)
	}
	if named == nil {
		t.Fatal("Keymap(default) = nil")
	}
}

// TestAdapterExactContextPrecedence covers the exact-context API the root uses for the
// remote-menu action and the dock: a user Terminal F6 rebind or unbind must not be
// shadowed by the preset's Global F6, while normal Resolve keeps its global fallback.
func TestAdapterExactContextPrecedence(t *testing.T) {
	t.Parallel()
	base, err := keymap.ByName("default")
	if err != nil {
		t.Fatal(err)
	}
	f6 := tui.KeyPress{Name: "F6"}
	// The preset binds F6 globally to show_remote_indicator_menu.
	ad := NewKeymap(base)
	if cmd, ok := ad.(interface {
		ResolveExact(tui.KeyPress, tui.KeyContext) (tui.Command, bool)
	}).ResolveExact(f6, tui.KeyContextTerminal); ok {
		t.Fatalf("ResolveExact(Terminal) = %+v, want no terminal binding", cmd)
	}
	if cmd, ok := ad.Resolve(f6, tui.KeyContextDock); !ok || cmd.Action != "show_remote_indicator_menu" {
		t.Fatalf("Resolve(Dock) = %+v/%v, want the global fallback", cmd, ok)
	}

	// A user Terminal rebind wins in the exact context; Dock stays on the global fallback.
	rebound, _ := base.WithUserBindings([]byte(`[{"key":"F6","modifiers":[],"action":"toggle_keyboard_capture","when":"terminal"}]`), nil)
	if err != nil {
		t.Fatal(err)
	}
	ad = NewKeymap(rebound)
	exact := ad.(interface {
		ResolveExact(tui.KeyPress, tui.KeyContext) (tui.Command, bool)
	})
	if cmd, ok := exact.ResolveExact(f6, tui.KeyContextTerminal); !ok || cmd.Action != "toggle_keyboard_capture" {
		t.Fatalf("ResolveExact(Terminal) after rebind = %+v/%v, want toggle_keyboard_capture", cmd, ok)
	}
	if cmd, ok := exact.ResolveExact(f6, tui.KeyContextDock); ok {
		t.Fatalf("ResolveExact(Dock) after a Terminal rebind = %+v, want none", cmd)
	}
	if cmd, ok := ad.Resolve(f6, tui.KeyContextDock); !ok || cmd.Action != "show_remote_indicator_menu" {
		t.Fatalf("Resolve(Dock) still falls back global after the terminal rebind: %+v/%v", cmd, ok)
	}

	// A Terminal unbind is exact: it suppresses the remote action there, not in Dock/Global.
	unbound, _ := base.WithUserBindings([]byte(`[{"key":"F6","modifiers":[],"action":"unbind","when":"terminal"}]`), nil)
	if err != nil {
		t.Fatal(err)
	}
	ad = NewKeymap(unbound)
	exact = ad.(interface {
		ResolveExact(tui.KeyPress, tui.KeyContext) (tui.Command, bool)
	})
	exactUnbind := ad.(interface {
		UnboundExact(tui.KeyPress, tui.KeyContext) bool
	})
	if !exactUnbind.UnboundExact(f6, tui.KeyContextTerminal) {
		t.Fatal("UnboundExact(Terminal) = false after a terminal noop")
	}
	if exactUnbind.UnboundExact(f6, tui.KeyContextDock) {
		t.Fatal("UnboundExact(Dock) = true after only a terminal noop")
	}
	if cmd, ok := exact.ResolveExact(f6, tui.KeyContextTerminal); ok {
		t.Fatalf("ResolveExact(Terminal) after unbind = %+v, want none", cmd)
	}

	// Unbinding Global F6 removes the fallback everywhere; it does not erase an explicit
	// Terminal binding in the exact context.
	globalOff, _ := rebound.WithUserBindings([]byte(`[{"key":"F6","modifiers":[],"action":"unbind","when":"global"}]`), nil)
	if err != nil {
		t.Fatal(err)
	}
	ad = NewKeymap(globalOff)
	exact = ad.(interface {
		ResolveExact(tui.KeyPress, tui.KeyContext) (tui.Command, bool)
	})
	if cmd, ok := exact.ResolveExact(f6, tui.KeyContextTerminal); !ok || cmd.Action != "toggle_keyboard_capture" {
		t.Fatalf("global unbind erased the terminal rebind: %+v/%v", cmd, ok)
	}
	if cmd, ok := ad.Resolve(f6, tui.KeyContextNormal); ok {
		t.Fatalf("global unbind left a normal/global binding: %+v", cmd)
	}
}

// TestAdapterExactUnbindNormalAndExplorer covers the two focus contexts that resolve
// before the global fallback: a user unbind in normal or file_explorer is visible as an
// ExactUnbind tombstone with no exact binding, while the other context stays untouched.
func TestAdapterExactUnbindNormalAndExplorer(t *testing.T) {
	t.Parallel()
	base, err := keymap.ByName("default")
	if err != nil {
		t.Fatal(err)
	}
	f6 := tui.KeyPress{Name: "F6"}
	for _, tc := range []struct {
		name string
		when string
		ctx  tui.KeyContext
	}{
		{name: "normal", when: "normal", ctx: tui.KeyContextNormal},
		{name: "file explorer", when: "file_explorer", ctx: tui.KeyContextFileExplorer},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			raw := "[{\"key\":\"F6\",\"modifiers\":[],\"action\":\"unbind\",\"when\":\"" + tc.when + "\"}]"
			layer, _ := base.WithUserBindings([]byte(raw), nil)
			ad := NewKeymap(layer)
			exact := ad.(interface {
				ResolveExact(tui.KeyPress, tui.KeyContext) (tui.Command, bool)
			})
			unbind := ad.(interface {
				UnboundExact(tui.KeyPress, tui.KeyContext) bool
			})
			if !unbind.UnboundExact(f6, tc.ctx) {
				t.Fatalf("UnboundExact(%s) = false", tc.name)
			}
			if cmd, ok := exact.ResolveExact(f6, tc.ctx); ok {
				t.Fatalf("ResolveExact(%s) after unbind = %+v, want none", tc.name, cmd)
			}
			other := tui.KeyContextNormal
			if tc.ctx == tui.KeyContextNormal {
				other = tui.KeyContextFileExplorer
			}
			if unbind.UnboundExact(f6, other) {
				t.Fatalf("the %s unbind leaked into %s", tc.name, other)
			}
		})
	}
}
