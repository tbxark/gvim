package keymap_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/tbxark/gvim/keymap"
)

// knownTestActions is the action set the diagnostics tests validate against.
func knownTestActions(a string) bool {
	return slices.Contains([]string{"save", "quit", "goto_line", "to_upper_case", "quick_open", "popup_cancel"}, a)
}

func TestWithUserBindings(t *testing.T) {
	tests := []struct {
		name     string
		preset   string
		data     string
		chord    string
		ctx      keymap.Context
		want     string // the action the chord resolves to; "" for unbound
		warnings []string
	}{
		{
			name: "the documented array form", preset: "default",
			data:  `[{"key":"u","modifiers":["alt"],"action":"to_upper_case","when":"normal"}]`,
			chord: "alt+u", ctx: keymap.ContextNormal, want: "to_upper_case",
		},
		{
			name: "the object form still loads", preset: "default",
			data:  `{"bindings":[{"key":"u","modifiers":["alt"],"action":"to_upper_case","when":"normal"}]}`,
			chord: "alt+u", ctx: keymap.ContextNormal, want: "to_upper_case",
		},
		{
			name: "when defaults to normal", preset: "default",
			data:  `[{"key":"u","modifiers":["alt"],"action":"to_upper_case"}]`,
			chord: "alt+u", ctx: keymap.ContextNormal, want: "to_upper_case",
		},
		{
			name: "an unknown action warns and is skipped, the key keeps its meaning", preset: "default",
			data:  `[{"key":"s","modifiers":["ctrl"],"action":"unknown_action_xyz"}]`,
			chord: "ctrl+s", ctx: keymap.ContextNormal, want: "save",
			warnings: []string{`keybindings[0]: unknown action "unknown_action_xyz"; skipped`},
		},
		{
			name: "a plugin command id is not an unknown action", preset: "default",
			data:  `[{"key":"u","modifiers":["alt"],"action":"init.hello"}]`,
			chord: "alt+u", ctx: keymap.ContextNormal, want: "init.hello",
		},
		{
			name: "an unreadable key warns and is skipped", preset: "default",
			data:  `[{"key":"Weird","modifiers":[],"action":"save"},{"key":"u","modifiers":["alt"],"action":"to_upper_case"}]`,
			chord: "alt+u", ctx: keymap.ContextNormal, want: "to_upper_case",
			warnings: []string{`keybindings[0]: unknown key name "Weird"; skipped`},
		},
		{
			name: "an unknown modifier warns and is skipped", preset: "default",
			data:  `[{"key":"s","modifiers":["hyper"],"action":"quit"}]`,
			chord: "ctrl+s", ctx: keymap.ContextNormal, want: "save",
			warnings: []string{`keybindings[0]: unknown modifier "hyper"; skipped`},
		},
		{
			name: "an unknown when clause warns", preset: "default",
			data:  `[{"key":"u","modifiers":["alt"],"action":"quit","when":"nowhere"}]`,
			chord: "alt+u", ctx: keymap.ContextNormal, want: "to_upper_case",
			warnings: []string{`keybindings[0]: unknown when clause "nowhere"; skipped`},
		},
		{
			name: "a misspelt field must not bind the bare key", preset: "default",
			data:  `[{"key":"u","modifers":["alt"],"action":"to_upper_case"}]`,
			chord: "u", ctx: keymap.ContextNormal, want: "",
			warnings: []string{`keybindings[0]: json: unknown field "modifers"; skipped`},
		},
		{
			name: "noop disables a key", preset: "default",
			data:  `[{"key":"s","modifiers":["ctrl"],"action":"noop"}]`,
			chord: "ctrl+s", ctx: keymap.ContextNormal, want: "noop",
		},
		{
			name: "unbind frees the key for the global binding", preset: "default",
			data:  `[{"key":"p","modifiers":["ctrl"],"action":"unbind","when":"global"}]`,
			chord: "ctrl+p", ctx: keymap.ContextNormal, want: "",
		},
		{
			name: "unbind removes only the context it names", preset: "default",
			data:  `[{"key":"s","modifiers":["ctrl"],"action":"unbind","when":"terminal"},{"key":"a","modifiers":["ctrl"],"action":"unbind","when":"terminal"}]`,
			chord: "ctrl+s", ctx: keymap.ContextNormal, want: "save",
		},
		{
			// A context gvim does not consult: the entry is skipped with a warning and the key
			// keeps its meaning.
			name: "a context gvim does not consult is skipped with a warning", preset: "default",
			data:  `[{"key":"s","modifiers":["ctrl"],"action":"quit","when":"completion"}]`,
			chord: "ctrl+s", ctx: keymap.ContextNormal, want: "save",
			warnings: []string{`keybindings[0]: when "completion" is not used by gvim (keys run in global, normal, prompt, searchPrompt, popup, menu, settings, file_explorer, terminal, dock); skipped`},
		},
		{
			// The overlay contexts are consulted now: a binding there applies to the surface.
			name: "an overlay context binding applies", preset: "default",
			data:  `[{"key":"g","modifiers":["alt"],"action":"popup_cancel","when":"popup"}]`,
			chord: "alt+g", ctx: keymap.ContextPopup, want: "popup_cancel",
		},
		{
			// Overlay surfaces resolve one key at a time, so a sequence in their context cannot run.
			name: "an overlay context sequence is skipped", preset: "default",
			data:     `[{"keys":[{"key":"q"},{"key":"q"}],"action":"quit","when":"prompt"}]`,
			warnings: []string{`keybindings[0]: when "prompt" takes single-key bindings only; sequences are skipped; skipped`},
		},
		{
			name: "two entries for one chord: the later wins, with a warning", preset: "default",
			data:  `[{"key":"u","modifiers":["alt"],"action":"save"},{"key":"u","modifiers":["alt"],"action":"quit"}]`,
			chord: "alt+u", ctx: keymap.ContextNormal, want: "quit",
			warnings: []string{`keybindings[1]: alt+u in normal is already bound to "save"; the later entry wins`},
		},
		{
			name: "a global user binding replaces the chord where the preset binds it", preset: "default",
			data:  `[{"key":"s","modifiers":["ctrl"],"action":"quit","when":"global"}]`,
			chord: "ctrl+s", ctx: keymap.ContextNormal, want: "quit",
		},
		{
			name: "a user key frees the first key of an emacs chord", preset: "emacs",
			data:  `[{"key":"x","modifiers":["ctrl"],"action":"quit"}]`,
			chord: "ctrl+x", ctx: keymap.ContextNormal, want: "quit",
		},
		{
			name: "a list that is not a list", preset: "default",
			data:  `"alt+u"`,
			chord: "ctrl+s", ctx: keymap.ContextNormal, want: "save",
			warnings: []string{`keybindings: must be a list of bindings`},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := mustPreset(t, tc.preset)
			// The base binding of alt+u, whatever it is, must survive the user's list.
			baseAltU, baseAltUOK := base.Resolve("alt+u", keymap.ContextNormal)
			m, warnings := base.WithUserBindings([]byte(tc.data), knownTestActions)
			b, ok := m.Resolve(tc.chord, tc.ctx)
			if tc.want == "" && ok {
				t.Errorf("%s resolves to %q, want unbound", tc.chord, b.Action)
			} else if tc.want != "" && b.Action != tc.want {
				t.Errorf("%s resolves to %q (%v), want %q", tc.chord, b.Action, ok, tc.want)
			}
			if !slices.Equal(warnings, tc.warnings) {
				t.Errorf("warnings = %q, want %q", warnings, tc.warnings)
			}
			if got, ok := base.Resolve("alt+u", keymap.ContextNormal); ok != baseAltUOK || got.Action != baseAltU.Action {
				t.Error("the preset itself was changed")
			}
		})
	}
}

func TestWithUserBindingsKeepsTheEmacsChordWhenTheUserBindsAnotherKey(t *testing.T) {
	em := mustPreset(t, "emacs")
	m, warnings := em.WithUserBindings([]byte(`[{"key":"u","modifiers":["alt"],"action":"to_upper_case"}]`), knownTestActions)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %q", warnings)
	}
	if b, ok := m.ResolveSequence([]string{"ctrl+x", "ctrl+s"}, keymap.ContextNormal); !ok || b.Action != "save" {
		t.Fatalf("C-x C-s = %q (%v), want save", b.Action, ok)
	}
}

func TestWithUserBindingsUnbindsAChord(t *testing.T) {
	em := mustPreset(t, "emacs")
	data := `[{"keys":[{"key":"x","modifiers":["ctrl"]},{"key":"s","modifiers":["ctrl"]}],"action":"unbind"}]`
	m, warnings := em.WithUserBindings([]byte(data), knownTestActions)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %q", warnings)
	}
	if b, ok := m.ResolveSequence([]string{"ctrl+x", "ctrl+s"}, keymap.ContextNormal); ok {
		t.Fatalf("C-x C-s still resolves to %q after unbind", b.Action)
	}
	if !m.HasSequencePrefix([]string{"ctrl+x"}, keymap.ContextNormal) {
		t.Fatal("the other C-x chords must keep their prefix")
	}
}

func TestWithUserBindingsAcceptsEverythingWithoutAValidator(t *testing.T) {
	m, warnings := mustPreset(t, "default").WithUserBindings([]byte(`[{"key":"u","modifiers":["alt"],"action":"anything"}]`), nil)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %q", warnings)
	}
	if b, _ := m.Resolve("alt+u", keymap.ContextNormal); b.Action != "anything" {
		t.Fatalf("alt+u = %q", b.Action)
	}
}

func TestWithUserBindingsMalformedJSONLeavesTheKeymapAlone(t *testing.T) {
	base := mustPreset(t, "default")
	m, warnings := base.WithUserBindings([]byte(`[{"key":`), knownTestActions)
	if m != base {
		t.Error("a malformed list must leave the keymap as it is")
	}
	if len(warnings) != 1 || !strings.HasPrefix(warnings[0], "keybindings: ") {
		t.Errorf("warnings = %q, want one keybindings warning", warnings)
	}
}

// TestUserEntryRoundTrips: the entry the keybinding editor writes reads back as the same
// chord, context and action, for single keys, named keys and sequences.
func TestUserEntryRoundTrips(t *testing.T) {
	chords := []string{"ctrl+s", "alt+shift+down", "F12", "ctrl+pagedown", "escape", "ctrl+/", "ctrl++", "ctrl+x ctrl+s", "alt+g g", "ctrl+space", "shift+tab"}
	for _, chord := range chords {
		norm, err := keymap.NormalizeChord(chord)
		if err != nil {
			t.Fatal(err)
		}
		b := keymap.Binding{Chord: norm, Action: "quit", When: keymap.ContextTerminal}
		if strings.Contains(norm, " ") {
			b.Sequence = strings.Fields(norm)
		}
		data, err := json.Marshal([]any{keymap.UserEntry(b)})
		if err != nil {
			t.Fatal(err)
		}
		got, warnings := keymap.ParseUserBindings(data, nil)
		if len(warnings) != 0 || len(got) != 1 {
			t.Fatalf("%s: %s parsed as %v with %v", chord, data, got, warnings)
		}
		if got[0].Chord != norm || got[0].Action != "quit" || got[0].When != keymap.ContextTerminal {
			t.Errorf("%s: %s read back as %+v", chord, data, got[0])
		}
	}
}

// A user entry that takes over a binding of the preset is reported, as Fresh's replace rule
// applies it silently. An entry that does the same action, or unbinds or noops the chord, is not.
func TestReplacementsNamesThePresetBindingsAUserEntryTakesOver(t *testing.T) {
	tests := []struct {
		name   string
		preset string
		data   string
		want   []string
	}{
		{
			name: "a chord the preset binds to another action", preset: "default",
			data: `[{"key":"q","modifiers":["ctrl"],"action":"save"}]`,
			want: []string{`ctrl+q in normal replaces the keymap's "quit" on ctrl+q`},
		},
		{
			name: "a global entry takes the chord in each context that binds it", preset: "default",
			data: `[{"key":"q","modifiers":["ctrl"],"action":"save","when":"global"}]`,
			want: []string{`ctrl+q in global replaces the keymap's "quit" on ctrl+q`},
		},
		{
			name: "the same action changes nothing", preset: "default",
			data: `[{"key":"s","modifiers":["ctrl"],"action":"save"}]`,
		},
		{
			name: "unbind is deliberate", preset: "default",
			data: `[{"key":"q","modifiers":["ctrl"],"action":"unbind"}]`,
		},
		{
			name: "noop is deliberate", preset: "default",
			data: `[{"key":"q","modifiers":["ctrl"],"action":"noop"}]`,
		},
		{
			name: "a chord the preset leaves free", preset: "default",
			data: `[{"key":"j","modifiers":["ctrl","alt","shift"],"action":"save"}]`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			km := mustPreset(t, tc.preset)
			got := km.Replacements([]byte(tc.data), knownTestActions)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("Replacements = %q, want %q", got, tc.want)
			}
		})
	}
}

// A single key that starts an Emacs chord takes over every chord of that prefix, and each is named.
func TestReplacementsNamesTheSequencesAPrefixFrees(t *testing.T) {
	em := mustPreset(t, "emacs")
	got := em.Replacements([]byte(`[{"key":"x","modifiers":["ctrl"],"action":"save"}]`), knownTestActions)
	if !slices.Contains(got, `ctrl+x in normal replaces the keymap's "save" on ctrl+x ctrl+s`) {
		t.Fatalf("Replacements = %q, want the C-x C-s binding named", got)
	}
	for _, line := range got {
		if !strings.HasPrefix(line, "ctrl+x in normal replaces the keymap's ") {
			t.Fatalf("line %q does not name the prefix", line)
		}
	}
}
