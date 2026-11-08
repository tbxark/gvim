package keymap_test

import (
	"testing"

	"github.com/tbxark/gvim/internal/keymap"
)

// TestNormalizeFreshKeyAliases pins the key-name aliases of Fresh's
// docs/configuration/keyboard.md (Key Names). A Fresh-form config must keep
// working, so every documented alias has to normalize.
func TestNormalizeFreshKeyAliases(t *testing.T) {
	tests := []struct{ in, want string }{
		{"esc", "escape"}, {"Esc", "escape"}, {"ret", "enter"}, {"return", "enter"},
		{"bs", "backspace"}, {"del", "delete"}, {"ins", "insert"}, {"spc", "space"},
		{"pgup", "pageup"}, {"pgdn", "pagedown"}, {"backtab", "shift+tab"},
		{"capslock", "capslock"}, {"scrolllock", "scrolllock"}, {"numlock", "numlock"},
		{"printscreen", "printscreen"}, {"pause", "pause"}, {"menu", "menu"},
		{"asterisk", "*"}, {"star", "*"}, {"plus", "+"}, {"minus", "-"}, {"hyphen", "-"},
		{"slash", "/"}, {"period", "."}, {"dot", "."}, {"equal", "="}, {"equals", "="},
		{"backslash", `\`}, {"comma", ","}, {"semicolon", ";"}, {"colon", ":"},
		{"apostrophe", "'"}, {"quote", "'"}, {"quotedbl", `"`}, {"doublequote", `"`},
		{"grave", "`"}, {"backtick", "`"}, {"tilde", "~"}, {"exclam", "!"},
		{"exclamation", "!"}, {"at", "@"}, {"numbersign", "#"}, {"hash", "#"},
		{"dollar", "$"}, {"percent", "%"}, {"asciicircum", "^"}, {"caret", "^"},
		{"ampersand", "&"}, {"underscore", "_"}, {"bar", "|"}, {"pipe", "|"},
		{"question", "?"}, {"less", "<"}, {"lessthan", "<"}, {"greater", ">"},
		{"greaterthan", ">"}, {"parenleft", "("}, {"parenright", ")"},
		{"bracketleft", "["}, {"bracketright", "]"}, {"braceleft", "{"}, {"braceright", "}"},
		{"kp_0", "0"}, {"kp_9", "9"}, {"kp_decimal", "."}, {"kp_divide", "/"},
		{"kp_multiply", "*"}, {"kp_subtract", "-"}, {"kp_add", "+"}, {"kp_enter", "enter"},
		{"kp_equal", "="}, {"kp_separator", ","}, {"kp_left", "left"}, {"kp_right", "right"},
		{"kp_up", "up"}, {"kp_down", "down"}, {"kp_page_up", "pageup"},
		{"kp_page_down", "pagedown"}, {"kp_home", "home"}, {"kp_end", "end"},
		{"kp_insert", "insert"}, {"kp_delete", "delete"}, {"kp_begin", "keypadbegin"},
		{"f13", "F13"}, {"F24", "F24"}, {"f35", "F35"},
		{"ctrl+esc", "ctrl+escape"}, {"alt+plus", "alt++"},
	}
	for _, tc := range tests {
		got, err := keymap.Normalize(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("Normalize(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
	for _, bad := range []string{"f0", "f36", "weird", "kp_nope"} {
		if got, err := keymap.Normalize(bad); err == nil {
			t.Errorf("Normalize(%q) = %q; want an error", bad, got)
		}
	}
}

// TestUserBindingFreshModifierAliases pins Fresh's parse_modifiers: control is ctrl, and
// cmd, command and meta are super.
func TestUserBindingFreshModifierAliases(t *testing.T) {
	tests := []struct {
		data, want string
	}{
		{`[{"key":"esc","modifiers":["control"],"action":"save"}]`, "ctrl+escape"},
		{`[{"key":"d","modifiers":["cmd"],"action":"save"}]`, "super+d"},
		{`[{"key":"d","modifiers":["Command"],"action":"save"}]`, "super+d"},
		{`[{"key":"d","modifiers":["meta","shift"],"action":"save"}]`, "shift+super+d"},
		{`[{"key":"pgup","modifiers":["ctrl"],"action":"save"}]`, "ctrl+pageup"},
		{`[{"key":"f13","modifiers":[],"action":"save"}]`, "F13"},
		{`[{"key":"bracketleft","modifiers":["ctrl"],"action":"save"}]`, "ctrl+["},
	}
	for _, tc := range tests {
		got, warns := keymap.ParseUserBindings([]byte(tc.data), nil)
		if len(warns) != 0 || len(got) != 1 || got[0].Chord != tc.want {
			t.Errorf("%s: got %+v, warnings %v; want chord %q", tc.data, got, warns, tc.want)
		}
	}
}
