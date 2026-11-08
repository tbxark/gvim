package dock

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// keyCase is one key press and the bytes a shell reads for it. An empty want with
// ok false means the key sends nothing. appCursor is the shell's DECCKM mode.
type keyCase struct {
	name      string
	key       tea.Key
	appCursor bool
	want      string
	ok        bool
}

// checkDockKeys runs each case through dockBytes.
func checkDockKeys(t *testing.T, tests []keyCase) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := dockBytes(tt.key, tt.appCursor)
			if ok != tt.ok || string(got) != tt.want {
				t.Fatalf("dockBytes(%+v, %v) = %q, %v; want %q, %v", tt.key, tt.appCursor, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestDockTextKeysSendUTF8(t *testing.T) {
	checkDockKeys(t, []keyCase{
		{name: "lower letter", key: tea.Key{Code: 'a', Text: "a"}, want: "a", ok: true},
		{name: "shifted letter", key: tea.Key{Code: 'A', Text: "A", Mod: tea.ModShift}, want: "A", ok: true},
		{name: "digit", key: tea.Key{Code: '7', Text: "7"}, want: "7", ok: true},
		{name: "symbol", key: tea.Key{Code: '|', Text: "|"}, want: "|", ok: true},
		{name: "space", key: tea.Key{Code: tea.KeySpace, Text: " "}, want: " ", ok: true},
		{name: "accented letter", key: tea.Key{Code: 'é', Text: "é"}, want: "é", ok: true},
		{name: "CJK character", key: tea.Key{Code: '漢', Text: "漢"}, want: "漢", ok: true},
	})
}

func TestDockEditingKeys(t *testing.T) {
	checkDockKeys(t, []keyCase{
		{name: "Enter sends carriage return", key: tea.Key{Code: tea.KeyEnter}, want: "\r", ok: true},
		{name: "Backspace sends DEL", key: tea.Key{Code: tea.KeyBackspace}, want: "\x7f", ok: true},
		{name: "Ctrl+Backspace sends BS", key: tea.Key{Code: tea.KeyBackspace, Mod: tea.ModCtrl}, want: "\b", ok: true},
		{name: "Tab", key: tea.Key{Code: tea.KeyTab}, want: "\t", ok: true},
		{name: "Shift+Tab is backtab", key: tea.Key{Code: tea.KeyTab, Mod: tea.ModShift}, want: "\x1b[Z", ok: true},
		{name: "Escape", key: tea.Key{Code: tea.KeyEscape}, want: "\x1b", ok: true},
		{name: "Delete", key: tea.Key{Code: tea.KeyDelete}, want: "\x1b[3~", ok: true},
		{name: "Insert", key: tea.Key{Code: tea.KeyInsert}, want: "\x1b[2~", ok: true},
	})
}

func TestDockNavigationKeys(t *testing.T) {
	checkDockKeys(t, []keyCase{
		{name: "Up", key: tea.Key{Code: tea.KeyUp}, want: "\x1b[A", ok: true},
		{name: "Down", key: tea.Key{Code: tea.KeyDown}, want: "\x1b[B", ok: true},
		{name: "Right", key: tea.Key{Code: tea.KeyRight}, want: "\x1b[C", ok: true},
		{name: "Left", key: tea.Key{Code: tea.KeyLeft}, want: "\x1b[D", ok: true},
		{name: "Home", key: tea.Key{Code: tea.KeyHome}, want: "\x1b[H", ok: true},
		{name: "End", key: tea.Key{Code: tea.KeyEnd}, want: "\x1b[F", ok: true},
		{name: "PageUp", key: tea.Key{Code: tea.KeyPgUp}, want: "\x1b[5~", ok: true},
		{name: "PageDown", key: tea.Key{Code: tea.KeyPgDown}, want: "\x1b[6~", ok: true},
		{name: "Ctrl+Left carries the modifier", key: tea.Key{Code: tea.KeyLeft, Mod: tea.ModCtrl}, want: "\x1b[1;5D", ok: true},
		{name: "Shift+Up carries the modifier", key: tea.Key{Code: tea.KeyUp, Mod: tea.ModShift}, want: "\x1b[1;2A", ok: true},
		{name: "Ctrl+PageDown carries the modifier", key: tea.Key{Code: tea.KeyPgDown, Mod: tea.ModCtrl}, want: "\x1b[6;5~", ok: true},
	})
}

func TestDockCursorKeysFollowDECCKM(t *testing.T) {
	// With application cursor keys on, the unmodified cursor keys, Home and End take
	// the SS3 form. A modifier keeps the CSI form with its parameter, as xterm does.
	checkDockKeys(t, []keyCase{
		{name: "Up in normal mode", key: tea.Key{Code: tea.KeyUp}, want: "\x1b[A", ok: true},
		{name: "Up in application mode", key: tea.Key{Code: tea.KeyUp}, appCursor: true, want: "\x1bOA", ok: true},
		{name: "Down in application mode", key: tea.Key{Code: tea.KeyDown}, appCursor: true, want: "\x1bOB", ok: true},
		{name: "Right in application mode", key: tea.Key{Code: tea.KeyRight}, appCursor: true, want: "\x1bOC", ok: true},
		{name: "Left in application mode", key: tea.Key{Code: tea.KeyLeft}, appCursor: true, want: "\x1bOD", ok: true},
		{name: "Home in normal mode", key: tea.Key{Code: tea.KeyHome}, want: "\x1b[H", ok: true},
		{name: "Home in application mode", key: tea.Key{Code: tea.KeyHome}, appCursor: true, want: "\x1bOH", ok: true},
		{name: "End in normal mode", key: tea.Key{Code: tea.KeyEnd}, want: "\x1b[F", ok: true},
		{name: "End in application mode", key: tea.Key{Code: tea.KeyEnd}, appCursor: true, want: "\x1bOF", ok: true},
		{name: "Ctrl+Up keeps the CSI form in application mode", key: tea.Key{Code: tea.KeyUp, Mod: tea.ModCtrl}, appCursor: true, want: "\x1b[1;5A", ok: true},
		{name: "Shift+Home keeps the CSI form in application mode", key: tea.Key{Code: tea.KeyHome, Mod: tea.ModShift}, appCursor: true, want: "\x1b[1;2H", ok: true},
		{name: "PageUp is not a cursor key", key: tea.Key{Code: tea.KeyPgUp}, appCursor: true, want: "\x1b[5~", ok: true},
	})
}

func TestDockFunctionKeys(t *testing.T) {
	checkDockKeys(t, []keyCase{
		{name: "F1 is SS3 P", key: tea.Key{Code: tea.KeyF1}, want: "\x1bOP", ok: true},
		{name: "F4 is SS3 S", key: tea.Key{Code: tea.KeyF4}, want: "\x1bOS", ok: true},
		{name: "F5 is CSI 15 ~", key: tea.Key{Code: tea.KeyF5}, want: "\x1b[15~", ok: true},
		{name: "F9 is CSI 20 ~", key: tea.Key{Code: tea.KeyF9}, want: "\x1b[20~", ok: true},
		{name: "F12 is CSI 24 ~", key: tea.Key{Code: tea.KeyF12}, want: "\x1b[24~", ok: true},
		{name: "Ctrl+F1 carries the modifier", key: tea.Key{Code: tea.KeyF1, Mod: tea.ModCtrl}, want: "\x1b[1;5P", ok: true},
		{name: "Shift+F5 carries the modifier", key: tea.Key{Code: tea.KeyF5, Mod: tea.ModShift}, want: "\x1b[15;2~", ok: true},
		{name: "F20 has no sequence here", key: tea.Key{Code: tea.KeyF20}, want: "", ok: false},
	})
}

func TestDockCtrlKeys(t *testing.T) {
	checkDockKeys(t, []keyCase{
		{name: "Ctrl+A", key: tea.Key{Code: 'a', Mod: tea.ModCtrl}, want: "\x01", ok: true},
		{name: "Ctrl+C", key: tea.Key{Code: 'c', Mod: tea.ModCtrl}, want: "\x03", ok: true},
		{name: "Ctrl+D", key: tea.Key{Code: 'd', Mod: tea.ModCtrl}, want: "\x04", ok: true},
		{name: "Ctrl+Z", key: tea.Key{Code: 'z', Mod: tea.ModCtrl}, want: "\x1a", ok: true},
		{name: "Ctrl+S is XOFF and goes to the shell", key: tea.Key{Code: 's', Mod: tea.ModCtrl}, want: "\x13", ok: true},
		{name: "Ctrl+[ is ESC", key: tea.Key{Code: '[', Mod: tea.ModCtrl}, want: "\x1b", ok: true},
		{name: "Ctrl+\\ is FS", key: tea.Key{Code: '\\', Mod: tea.ModCtrl}, want: "\x1c", ok: true},
		{name: "Ctrl+] is GS", key: tea.Key{Code: ']', Mod: tea.ModCtrl}, want: "\x1d", ok: true},
		{name: "Ctrl+_ is US", key: tea.Key{Code: '_', Mod: tea.ModCtrl}, want: "\x1f", ok: true},
		{name: "Ctrl+Space is NUL", key: tea.Key{Code: tea.KeySpace, Mod: tea.ModCtrl}, want: "\x00", ok: true},
		{name: "Ctrl+digit has no control byte", key: tea.Key{Code: '1', Mod: tea.ModCtrl}, want: "", ok: false},
	})
}

func TestDockAltKeysPrefixEscape(t *testing.T) {
	checkDockKeys(t, []keyCase{
		{name: "Alt+letter is ESC then the letter", key: tea.Key{Code: 'x', Mod: tea.ModAlt}, want: "\x1bx", ok: true},
		{name: "Alt+letter with its text", key: tea.Key{Code: 'x', Text: "x", Mod: tea.ModAlt}, want: "\x1bx", ok: true},
		{name: "Alt+Enter", key: tea.Key{Code: tea.KeyEnter, Mod: tea.ModAlt}, want: "\x1b\r", ok: true},
		{name: "Alt+Backspace deletes a word", key: tea.Key{Code: tea.KeyBackspace, Mod: tea.ModAlt}, want: "\x1b\x7f", ok: true},
		{name: "Alt+Ctrl+A is ESC then SOH", key: tea.Key{Code: 'a', Mod: tea.ModAlt | tea.ModCtrl}, want: "\x1b\x01", ok: true},
		{name: "Alt+Left keeps the CSI form with the modifier", key: tea.Key{Code: tea.KeyLeft, Mod: tea.ModAlt}, want: "\x1b[1;3D", ok: true},
	})
}

func TestDockKeysSendNothingForUnknownKeys(t *testing.T) {
	checkDockKeys(t, []keyCase{
		{name: "no key code", key: tea.Key{}, want: "", ok: false},
		{name: "begin key", key: tea.Key{Code: tea.KeyBegin}, want: "", ok: false},
	})
}
