package keymap_test

import (
	"fmt"
	"testing"

	"github.com/tbxark/gvim/keymap"
)

func TestDefaultParses(t *testing.T) {
	base, err := keymap.ByName("default")
	if err != nil {
		t.Fatal(err)
	}
	// The count is pinned in TestPresetBindingCounts; here only that the file parsed.
	if got := len(base.Bindings()); got == 0 {
		t.Fatal("default keymap has no bindings")
	}
	m := keymap.Default()
	if keymap.Default() != m {
		t.Fatal("Default must return the same parsed map on every call")
	}
	if want, err := keymap.ByName(keymap.DefaultName()); err != nil || want != m {
		t.Fatalf("Default must be ByName(DefaultName()): %v", err)
	}
}

// TestResolveRequiredChords checks chords of the default keymap. It uses the default preset by
// name: keymap.Default is the macOS keymap on darwin, which binds the same actions to other keys.
func TestResolveRequiredChords(t *testing.T) {
	m := mustPreset(t, "default")
	tests := []struct {
		chord string
		ctx   keymap.Context
		want  string
	}{
		{"ctrl+p", keymap.ContextNormal, "quick_open"},
		{"ctrl+b", keymap.ContextNormal, "toggle_file_explorer"},
		{"ctrl+s", keymap.ContextNormal, "save"},
		{"ctrl+shift+p", keymap.ContextGlobal, "command_palette"},
		{"ctrl+alt+up", keymap.ContextNormal, "add_cursor_above"},
		{"alt+shift+down", keymap.ContextNormal, "block_select_down"},
		{"shift+alt+i", keymap.ContextNormal, "add_cursors_to_line_ends"},
		{"ctrl+w", keymap.ContextNormal, "select_word"},
		{"ctrl+e", keymap.ContextFileExplorer, "focus_editor"},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("%s/%s", tc.ctx, tc.chord), func(t *testing.T) {
			b, ok := m.Resolve(tc.chord, tc.ctx)
			if !ok {
				t.Fatalf("no binding for %s in %s", tc.chord, tc.ctx)
			}
			if b.Action != tc.want {
				t.Fatalf("action = %q, want %q", b.Action, tc.want)
			}
		})
	}
}

func TestResolveNormalizesSpelling(t *testing.T) {
	m := keymap.Default()
	for _, chord := range []string{"Ctrl+P", "p+ctrl", "CTRL+p", "ctrl+P"} {
		b, ok := m.Resolve(chord, keymap.ContextNormal)
		if !ok || b.Action != "quick_open" {
			t.Fatalf("Resolve(%q) = %+v, %v; want quick_open", chord, b, ok)
		}
	}
}

func TestResolveFallsBackToGlobal(t *testing.T) {
	m := keymap.Default()
	// ctrl+p is bound in global; a context without its own binding sees it.
	if b, ok := m.Resolve("ctrl+p", keymap.ContextMenu); !ok || b.Action != "quick_open" {
		t.Fatalf("menu context should fall back to global, got %+v %v", b, ok)
	}
	if _, ok := m.Resolve("ctrl+alt+f12", keymap.ContextNormal); ok {
		t.Fatal("unbound chord must not resolve")
	}
}

func TestResolveRejectsBadChord(t *testing.T) {
	if _, ok := keymap.Default().Resolve("hyper+x", keymap.ContextNormal); ok {
		t.Fatal("unknown modifier must not resolve")
	}
}

func TestBackTabIsShiftTab(t *testing.T) {
	got, err := keymap.Normalize("BackTab")
	if err != nil || got != "shift+tab" {
		t.Fatalf("Normalize(BackTab) = %q, %v; want shift+tab", got, err)
	}
	got, err = keymap.Canonical("BackTab", nil)
	if err != nil || got != "shift+tab" {
		t.Fatalf("Canonical(BackTab) = %q, %v; want shift+tab", got, err)
	}
}

func TestCanonicalOrderAndNames(t *testing.T) {
	tests := []struct {
		key  string
		mods []string
		want string
	}{
		{"f", []string{"shift", "ctrl"}, "ctrl+shift+f"},
		{"Left", []string{"shift"}, "shift+left"},
		{" ", []string{"ctrl"}, "ctrl+space"},
		{"f10", nil, "F10"},
		{"PageDown", nil, "pagedown"},
	}
	for _, tc := range tests {
		got, err := keymap.Canonical(tc.key, tc.mods)
		if err != nil || got != tc.want {
			t.Errorf("Canonical(%q, %v) = %q, %v; want %q", tc.key, tc.mods, got, err, tc.want)
		}
	}
}

func TestLoadErrors(t *testing.T) {
	bad := map[string]string{
		"not json":         `{`,
		"missing action":   `{"bindings":[{"key":"a","modifiers":[]}]}`,
		"unknown modifier": `{"bindings":[{"key":"a","modifiers":["hyper"],"action":"x"}]}`,
		"unknown key":      `{"bindings":[{"key":"Weird","modifiers":[],"action":"x"}]}`,
	}
	for name, src := range bad {
		if _, err := keymap.Load([]byte(src)); err == nil {
			t.Errorf("%s: Load succeeded, want error", name)
		}
	}
}

func TestLoadRejectsConflictingChord(t *testing.T) {
	src := `{"bindings":[
		{"key":"a","modifiers":["ctrl"],"action":"first","when":"normal"},
		{"key":"a","modifiers":["ctrl"],"action":"second","when":"normal"}]}`
	if _, err := keymap.Load([]byte(src)); err == nil {
		t.Fatal("Load accepted one chord bound to two actions in the same context")
	}
}

func TestLoadAllowsRepeatedSameBinding(t *testing.T) {
	src := `{"bindings":[
		{"key":"a","modifiers":["ctrl"],"action":"first","when":"normal"},
		{"key":"a","modifiers":["ctrl"],"action":"first","when":"normal"}]}`
	m, err := keymap.Load([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := m.Resolve("ctrl+a", keymap.ContextNormal); b.Action != "first" {
		t.Fatalf("action = %q, want first", b.Action)
	}
}

func TestLoadRejectsUnknownWhenClause(t *testing.T) {
	for _, when := range []string{"nomal", "Normal", "mode:search"} {
		src := `{"bindings":[{"key":"a","modifiers":["ctrl"],"action":"x","when":"` + when + `"}]}`
		if _, err := keymap.Load([]byte(src)); err == nil {
			t.Errorf("Load accepted when %q, want an error", when)
		}
	}
}

func TestChildPresetOverridesInheritedChord(t *testing.T) {
	src := `{"inherits":"default","bindings":[
		{"key":"b","modifiers":["ctrl"],"action":"custom_action","when":"normal"}]}`
	m, err := keymap.Load([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := m.Resolve("ctrl+b", keymap.ContextNormal); b.Action != "custom_action" {
		t.Fatalf("action = %q, want custom_action", b.Action)
	}
}

func TestPrimaryPrefersNormalThenGlobal(t *testing.T) {
	m := keymap.Default()
	b, ok := m.Primary("toggle_file_explorer")
	if !ok || b.When != keymap.ContextNormal || b.Chord != "ctrl+b" {
		t.Fatalf("Primary(toggle_file_explorer) = %+v, %v; want normal ctrl+b", b, ok)
	}
	if _, ok := m.Primary("no_such_action"); ok {
		t.Fatal("unknown action must not have a primary binding")
	}
}

func TestArgsDecode(t *testing.T) {
	m, err := keymap.Load([]byte(`{"bindings":[
		{"key":"f","modifiers":["alt"],"action":"menu_open","args":{"name":"File"},"when":"global"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	b, ok := m.Resolve("alt+f", keymap.ContextGlobal)
	if !ok {
		t.Fatal("alt+f unbound")
	}
	if b.Args["name"] != "File" {
		t.Fatalf("args = %v, want name=File", b.Args)
	}
}

func ExampleDefault() {
	m := keymap.Default()
	for _, chord := range []string{"ctrl+p", "ctrl+s", "ctrl+z"} {
		b, _ := m.Resolve(chord, keymap.ContextNormal)
		fmt.Println(chord, "->", b.Action)
	}
	// Output:
	// ctrl+p -> quick_open
	// ctrl+s -> save
	// ctrl+z -> undo
}

func TestPrimaryFallbacks(t *testing.T) {
	src := `{"bindings":[
		{"key":"x","modifiers":[],"action":"only_prompt","when":"prompt"},
		{"key":"y","modifiers":[],"action":"both","when":"global"},
		{"key":"z","modifiers":[],"action":"both","when":"prompt"}]}`
	m, err := keymap.Load([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if b, ok := m.Primary("only_prompt"); !ok || b.When != keymap.ContextPrompt {
		t.Fatalf("prompt-only action = %+v, %v", b, ok)
	}
	if b, ok := m.Primary("both"); !ok || b.When != keymap.ContextGlobal {
		t.Fatalf("global should win over prompt, got %+v", b)
	}
}

func TestBindingsReturnsCopy(t *testing.T) {
	m := keymap.Default()
	list := m.Bindings()
	list[0].Action = "tampered"
	if m.Bindings()[0].Action == "tampered" {
		t.Fatal("Bindings must return a copy")
	}
}

func TestMalformedChordErrors(t *testing.T) {
	if _, err := keymap.Normalize("a+b"); err == nil {
		t.Error("two keys in one chord must fail")
	}
	if _, err := keymap.Normalize("ctrl+"); err == nil {
		t.Error("empty key must fail")
	}
	if _, err := keymap.Normalize("ctrl+Wibble"); err == nil {
		t.Error("unknown key name must fail")
	}
	if _, err := keymap.Canonical("a", []string{"hyper"}); err == nil {
		t.Error("unknown modifier in Canonical must fail")
	}
	if _, err := keymap.Canonical("", nil); err == nil {
		t.Error("empty key in Canonical must fail")
	}
	if _, err := keymap.Canonical("Wibble", nil); err == nil {
		t.Error("unknown key name in Canonical must fail")
	}
}

func TestWithOverridesReplacesAndUnbinds(t *testing.T) {
	base := mustPreset(t, "default")
	m, err := base.WithOverrides([]byte(`{"bindings":[
		{"key":"s","modifiers":["ctrl"],"action":"quick_open","when":"normal"},
		{"key":"b","modifiers":["ctrl"],"action":"noop","when":"normal"},
		{"key":"q","modifiers":["ctrl","alt"],"action":"quick_open"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		chord string
		ctx   keymap.Context
		want  string
	}{
		{"ctrl+s", keymap.ContextNormal, "quick_open"},
		{"ctrl+b", keymap.ContextNormal, "noop"},
		{"ctrl+alt+q", keymap.ContextNormal, "quick_open"},
	}
	for _, tc := range tests {
		b, ok := m.Resolve(tc.chord, tc.ctx)
		if !ok || b.Action != tc.want {
			t.Errorf("%s in %s = %q (%v), want %q", tc.chord, tc.ctx, b.Action, ok, tc.want)
		}
	}
	// The preset is shared, so the receiver must keep its own bindings.
	if b, _ := base.Resolve("ctrl+s", keymap.ContextNormal); b.Action != "save" {
		t.Errorf("base ctrl+s = %q, want save: WithOverrides changed the receiver", b.Action)
	}
	if b, _ := base.Resolve("ctrl+b", keymap.ContextNormal); b.Action != "toggle_file_explorer" {
		t.Errorf("base ctrl+b = %q, want toggle_file_explorer", b.Action)
	}
	if _, ok := base.Resolve("ctrl+alt+q", keymap.ContextNormal); ok {
		t.Error("base ctrl+alt+q is bound; the test needs an unbound chord")
	}
}

func TestWithOverridesKeepsOtherBindings(t *testing.T) {
	base := mustPreset(t, "default")
	before := len(base.Bindings())
	m, err := base.WithOverrides([]byte(`{"bindings":[{"key":"s","modifiers":["ctrl"],"action":"noop","when":"normal"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	// The replaced entry stays in the list, shadowed by the new one.
	if got, want := len(m.Bindings()), before+1; got != want {
		t.Fatalf("bindings = %d, want %d", got, want)
	}
	if got := len(base.Bindings()); got != before {
		t.Fatalf("base bindings = %d after WithOverrides, want %d", got, before)
	}
}

func TestWithOverridesAcceptsValidOverrides(t *testing.T) {
	tests := map[string]string{
		"empty object":     `{}`,
		"empty bindings":   `{"bindings":[]}`,
		"repeated binding": `{"bindings":[{"key":"a","modifiers":["ctrl"],"action":"x","when":"normal"},{"key":"a","modifiers":["ctrl"],"action":"x","when":"normal"}]}`,
	}
	for name, src := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := keymap.Default().WithOverrides([]byte(src)); err != nil {
				t.Fatalf("WithOverrides() error = %v", err)
			}
		})
	}
}

func TestWithOverridesRejectsBadInput(t *testing.T) {
	bad := map[string]string{
		"not json":         `{`,
		"array":            `[]`,
		"misspelled key":   `{"binding":[]}`,
		"inherits":         `{"inherits":"emacs","bindings":[]}`,
		"unknown key name": `{"bindings":[{"key":"Weird","modifiers":[],"action":"x"}]}`,
		"missing action":   `{"bindings":[{"key":"a","modifiers":["ctrl"]}]}`,
		"unknown when":     `{"bindings":[{"key":"a","modifiers":["ctrl"],"action":"x","when":"nomal"}]}`,
		"conflict":         `{"bindings":[{"key":"a","modifiers":["ctrl"],"action":"x"},{"key":"a","modifiers":["ctrl"],"action":"y"}]}`,
		"trailing data":    `{"bindings":[]} {}`,
		"trailing bracket": `{"bindings":[]}]`,
	}
	base := mustPreset(t, "default")
	for name, src := range bad {
		t.Run(name, func(t *testing.T) {
			m, err := base.WithOverrides([]byte(src))
			if err == nil || m != nil {
				t.Fatalf("WithOverrides() = %v, %v; want an error and no map", m, err)
			}
		})
	}
}

// allContexts lists every when-clause value a binding can carry.
var allContexts = []keymap.Context{
	keymap.ContextGlobal, keymap.ContextNormal, keymap.ContextPrompt,
	keymap.ContextFileExplorer, keymap.ContextSearchPrompt, keymap.ContextCompositeBuffer,
	keymap.ContextTerminal, keymap.ContextPopup, keymap.ContextMenu, keymap.ContextSettings,
	keymap.ContextDock, keymap.ContextCompletion,
}

func TestWithOverridesGlobalNoopUnbindsEveryContext(t *testing.T) {
	tests := []struct {
		name     string
		preset   string
		override string // one binding object of the overrides file
		chord    string
		ctx      keymap.Context
		before   string // what the preset binds the chord to in ctx
	}{
		{"default ctrl+s in normal", "default", `{"key":"s","modifiers":["ctrl"],"action":"noop"}`, "ctrl+s", keymap.ContextNormal, "save"},
		{"macos ctrl+b in file_explorer", "macos", `{"key":"b","modifiers":["ctrl"],"action":"noop"}`, "ctrl+b", keymap.ContextFileExplorer, "focus_editor"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := mustPreset(t, tt.preset)
			if b, ok := base.Resolve(tt.chord, tt.ctx); !ok || b.Action != tt.before {
				t.Fatalf("preset %s: %s in %s = %q (%v), want %q", tt.preset, tt.chord, tt.ctx, b.Action, ok, tt.before)
			}
			m, err := base.WithOverrides([]byte(`{"bindings":[` + tt.override + `]}`))
			if err != nil {
				t.Fatal(err)
			}
			if b, ok := m.Resolve(tt.chord, tt.ctx); !ok || b.Action != "noop" {
				t.Errorf("%s in %s = %q (%v), want noop", tt.chord, tt.ctx, b.Action, ok)
			}
			for _, ctx := range allContexts {
				if b, ok := m.Resolve(tt.chord, ctx); ok && b.Action != "noop" {
					t.Errorf("%s in %s = %q, want the chord unbound or noop", tt.chord, ctx, b.Action)
				}
			}
		})
	}
}

func TestWithOverridesGlobalNoopUnbindsSequence(t *testing.T) {
	base := mustPreset(t, "emacs")
	m, err := base.WithOverrides([]byte(`{"bindings":[{"keys":[{"key":"x","modifiers":["ctrl"]},{"key":"s","modifiers":["ctrl"]}],"action":"noop"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if b, ok := base.ResolveSequence([]string{"ctrl+x", "ctrl+s"}, keymap.ContextNormal); !ok || b.Action != "save" {
		t.Fatalf("preset emacs: ctrl+x ctrl+s in normal = %q (%v), want save", b.Action, ok)
	}
	if b, ok := m.ResolveSequence([]string{"ctrl+x", "ctrl+s"}, keymap.ContextNormal); !ok || b.Action != "noop" {
		t.Errorf("ctrl+x ctrl+s in normal = %q (%v), want noop", b.Action, ok)
	}
}

// An explicit binding for a context in the same file wins over the expansion of
// a global noop for the same chord.
func TestWithOverridesExplicitContextBindingWinsOverGlobalNoop(t *testing.T) {
	base := mustPreset(t, "default")
	m, err := base.WithOverrides([]byte(`{"bindings":[
		{"key":"s","modifiers":["ctrl"],"action":"noop"},
		{"key":"s","modifiers":["ctrl"],"action":"quick_open","when":"normal"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if b, ok := m.Resolve("ctrl+s", keymap.ContextNormal); !ok || b.Action != "quick_open" {
		t.Errorf("ctrl+s in normal = %q (%v), want quick_open", b.Action, ok)
	}
	if b, ok := m.Resolve("ctrl+s", keymap.ContextDock); !ok || b.Action != "noop" {
		t.Errorf("ctrl+s in dock = %q (%v), want noop", b.Action, ok)
	}
}

// A global override that is not noop replaces the chord in each non-global
// context where the preset binds it, so the user's action runs there too. A
// context binding in the same file still wins over that replacement.
func TestWithOverridesGlobalNonNoopReplacesContextBindings(t *testing.T) {
	tests := []struct {
		name     string
		override string // the bindings array of the overrides file
		chord    string
		ctx      keymap.Context
		want     string
	}{
		{
			name:     "global replaces the preset's normal binding",
			override: `[{"key":"s","modifiers":["ctrl"],"action":"quick_open"}]`,
			chord:    "ctrl+s",
			ctx:      keymap.ContextNormal,
			want:     "quick_open",
		},
		{
			name:     "same-file normal binding wins over the global replacement",
			override: `[{"key":"s","modifiers":["ctrl"],"action":"quick_open"},{"key":"s","modifiers":["ctrl"],"action":"save_as","when":"normal"}]`,
			chord:    "ctrl+s",
			ctx:      keymap.ContextNormal,
			want:     "save_as",
		},
		{
			name:     "context the preset does not bind takes the global binding",
			override: `[{"key":"s","modifiers":["ctrl"],"action":"quick_open"}]`,
			chord:    "ctrl+s",
			ctx:      keymap.ContextDock,
			want:     "quick_open",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := mustPreset(t, "default").WithOverrides([]byte(`{"bindings":` + tt.override + `}`))
			if err != nil {
				t.Fatal(err)
			}
			if b, ok := m.Resolve(tt.chord, tt.ctx); !ok || b.Action != tt.want {
				t.Errorf("%s in %s = %q (%v), want %q", tt.chord, tt.ctx, b.Action, ok, tt.want)
			}
		})
	}
}

func TestWithOverridesGlobalNonNoopReplacesSequence(t *testing.T) {
	base := mustPreset(t, "emacs")
	m, err := base.WithOverrides([]byte(`{"bindings":[{"keys":[{"key":"x","modifiers":["ctrl"]},{"key":"s","modifiers":["ctrl"]}],"action":"quick_open"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if b, ok := m.ResolveSequence([]string{"ctrl+x", "ctrl+s"}, keymap.ContextNormal); !ok || b.Action != "quick_open" {
		t.Errorf("ctrl+x ctrl+s in normal = %q (%v), want quick_open", b.Action, ok)
	}
}

// A global binding is not the shortcut of its action when a normal binding of
// the same chord runs another action, because Resolve checks the normal context
// first. The palette and the menus run in the normal context.
func TestPrimaryIgnoresGlobalShadowedInNormal(t *testing.T) {
	tests := []struct {
		name   string
		src    string
		action string
		want   string // chord Primary shows; empty when it shows none
	}{
		{
			name: "global chord shadowed by another action in normal",
			src: `{"bindings":[
				{"key":"x","modifiers":["ctrl"],"action":"global_action"},
				{"key":"x","modifiers":["ctrl"],"action":"normal_action","when":"normal"}]}`,
			action: "global_action",
			want:   "",
		},
		{
			name: "global chord with no normal binding",
			src: `{"bindings":[
				{"key":"x","modifiers":["ctrl"],"action":"global_action"}]}`,
			action: "global_action",
			want:   "ctrl+x",
		},
		{
			name: "global chord shadowed by the same action in normal",
			src: `{"bindings":[
				{"key":"x","modifiers":["ctrl"],"action":"global_action"},
				{"key":"x","modifiers":["ctrl"],"action":"global_action","when":"normal"}]}`,
			action: "global_action",
			want:   "ctrl+x",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := keymap.Load([]byte(tt.src))
			if err != nil {
				t.Fatal(err)
			}
			b, ok := m.Primary(tt.action)
			if got := b.Chord; ok && got != tt.want || !ok && tt.want != "" {
				t.Fatalf("Primary(%s) = %q (%v), want %q", tt.action, got, ok, tt.want)
			}
		})
	}
}

// Primary shows a single chord when the action has one, and a multi-key sequence only
// when it has not (the TUI dispatches sequences, so a label can show one).
func TestPrimaryPrefersSingleChordsToSequences(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string // chord Primary shows; empty when it shows none
	}{
		{
			name: "only a sequence",
			src: `{"bindings":[
				{"keys":[{"key":"x","modifiers":["ctrl"]},{"key":"s","modifiers":["ctrl"]}],"action":"save_all","when":"normal"}]}`,
			want: "ctrl+x ctrl+s",
		},
		{
			name: "sequence in normal beside a global single chord",
			src: `{"bindings":[
				{"keys":[{"key":"x","modifiers":["ctrl"]},{"key":"s","modifiers":["ctrl"]}],"action":"save_all","when":"normal"},
				{"key":"w","modifiers":["ctrl"],"action":"save_all","when":"global"}]}`,
			want: "ctrl+w",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := keymap.Load([]byte(tt.src))
			if err != nil {
				t.Fatal(err)
			}
			b, ok := m.Primary("save_all")
			if got := b.Chord; ok && got != tt.want || !ok && tt.want != "" {
				t.Fatalf("Primary(save_all) = %q (%v), want %q", got, ok, tt.want)
			}
		})
	}
}

// Canonical spells the plus key as "+" alone or as "ctrl++", and Normalize must
// read both back to the same key.
func TestPlusKeyResolves(t *testing.T) {
	m, err := keymap.Load([]byte(`{"bindings":[
		{"key":"+","modifiers":[],"action":"zoom_in","when":"normal"},
		{"key":"+","modifiers":["ctrl"],"action":"zoom_in","when":"normal"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, chord := range []string{"+", "ctrl++"} {
		if b, ok := m.Resolve(chord, keymap.ContextNormal); !ok || b.Action != "zoom_in" {
			t.Errorf("Resolve(%q) = %q (%v), want zoom_in", chord, b.Action, ok)
		}
	}
}

func TestCanonicalNormalizeRoundTrip(t *testing.T) {
	tests := []struct {
		key  string
		mods []string
	}{
		{"+", nil},
		{"+", []string{"ctrl"}},
		{"+", []string{"shift", "alt"}},
		{"a", []string{"ctrl"}},
		{"F10", nil},
		{"left", []string{"shift"}},
	}
	for _, tt := range tests {
		c, err := keymap.Canonical(tt.key, tt.mods)
		if err != nil {
			t.Fatalf("Canonical(%q, %v): %v", tt.key, tt.mods, err)
		}
		if got, err := keymap.Normalize(c); err != nil || got != c {
			t.Errorf("Normalize(%q) = %q, %v; want the same chord", c, got, err)
		}
	}
}

func TestNormalizeRejectsMisplacedPlus(t *testing.T) {
	for _, chord := range []string{"++", "ctrl+++", "+a", "ctrl+"} {
		if got, err := keymap.Normalize(chord); err == nil {
			t.Errorf("Normalize(%q) = %q, want an error", chord, got)
		}
	}
}
