package keymap_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/tbxark/gvim/internal/keymap"
)

func TestDefaultParses(t *testing.T) {
	base, err := keymap.ByName("default")
	if err != nil {
		t.Fatal(err)
	}
	if got := len(base.Bindings()); got != 276 {
		t.Fatalf("default bindings = %d, want 276 (count in default.json)", got)
	}
	m := keymap.Default()
	if keymap.Default() != m {
		t.Fatal("Default must return the same parsed map on every call")
	}
	if want, err := keymap.ByName(keymap.DefaultName()); err != nil || want != m {
		t.Fatalf("Default must be ByName(DefaultName()): %v", err)
	}
}

func TestResolveRequiredChords(t *testing.T) {
	m := keymap.Default()
	tests := []struct {
		chord string
		ctx   keymap.Context
		want  string
	}{
		{"ctrl+p", keymap.ContextNormal, "quick_open"},
		{"ctrl+b", keymap.ContextNormal, "toggle_file_explorer"},
		{"ctrl+s", keymap.ContextNormal, "save"},
		{"F10", keymap.ContextGlobal, "menu_activate"},
		{"alt+f", keymap.ContextGlobal, "menu_open"},
		{"ctrl+alt+up", keymap.ContextNormal, "add_cursor_above"},
		{"alt+shift+i", keymap.ContextNormal, "add_cursors_to_line_ends"},
		{"ctrl+e", keymap.ContextPrompt, "prompt_move_end"},
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

func TestLastBindingWins(t *testing.T) {
	src := `{"bindings":[
		{"key":"a","modifiers":["ctrl"],"action":"first","when":"normal"},
		{"key":"a","modifiers":["ctrl"],"action":"second","when":"normal"}]}`
	m, err := keymap.Load([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := m.Resolve("ctrl+a", keymap.ContextNormal); b.Action != "second" {
		t.Fatalf("action = %q, want second", b.Action)
	}
	if got := len(m.BindingsFor("first")); got != 1 {
		t.Fatalf("BindingsFor keeps every entry, got %d", got)
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
	b, ok := keymap.Default().Resolve("alt+f", keymap.ContextGlobal)
	if !ok {
		t.Fatal("alt+f unbound")
	}
	if b.Args["name"] != "File" {
		t.Fatalf("args = %v, want name=File", b.Args)
	}
	var check map[string]any
	if err := json.Unmarshal([]byte(`{"name":"File"}`), &check); err != nil {
		t.Fatal(err)
	}
}

func ExampleDefault() {
	m := keymap.Default()
	for _, chord := range []string{"ctrl+p", "ctrl+s", "F10"} {
		b, _ := m.Resolve(chord, keymap.ContextNormal)
		fmt.Println(chord, "->", b.Action)
	}
	// Output:
	// ctrl+p -> quick_open
	// ctrl+s -> save
	// F10 -> menu_activate
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
