package dock

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// kittyKeyCase is one key with the child's kitty flags and the bytes it sends.
type kittyKeyCase struct {
	name      string
	key       tea.Key
	appCursor bool
	flags     int
	want      string
	ok        bool
}

func checkKittyKeys(t *testing.T, tests []kittyKeyCase) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := dockBytesFlags(tt.key, tt.appCursor, tt.flags)
			if ok != tt.ok || string(got) != tt.want {
				t.Fatalf("dockBytesFlags(%+v, %v, %d) = %q, %v; want %q, %v",
					tt.key, tt.appCursor, tt.flags, got, ok, tt.want, tt.ok)
			}
		})
	}
}

// TestKittyDisabledFallsBackToLegacy: a child that never enabled the protocol (or
// popped all flags) gets exactly the legacy bytes.
func TestKittyDisabledFallsBackToLegacy(t *testing.T) {
	checkKittyKeys(t, []kittyKeyCase{
		{name: "Ctrl+I is the Tab byte", key: tea.Key{Code: 'i', Mod: tea.ModCtrl}, want: "\t", ok: true},
		{name: "Up with app cursor is SS3", key: tea.Key{Code: tea.KeyUp}, appCursor: true, want: "\x1bOA", ok: true},
		{name: "Shift+Tab is backtab", key: tea.Key{Code: tea.KeyTab, Mod: tea.ModShift}, want: "\x1b[Z", ok: true},
	})
}

// TestKittyDisambiguate: with the disambiguate flag, Esc and modified keys are
// CSI-u, functional keys always use their CSI form, and plain text stays text.
func TestKittyDisambiguate(t *testing.T) {
	flags := ansi.KittyDisambiguateEscapeCodes
	checkKittyKeys(t, []kittyKeyCase{
		{name: "Ctrl+I is distinct from Tab", key: tea.Key{Code: 'i', Mod: tea.ModCtrl}, flags: flags, want: "\x1b[105;5u", ok: true},
		{name: "Ctrl+Shift+A", key: tea.Key{Code: 'a', Mod: tea.ModCtrl | tea.ModShift}, flags: flags, want: "\x1b[97;6u", ok: true},
		{name: "Alt+[ is not a CSI prefix", key: tea.Key{Code: '[', Mod: tea.ModAlt}, flags: flags, want: "\x1b[91;3u", ok: true},
		{name: "Esc", key: tea.Key{Code: tea.KeyEscape}, flags: flags, want: "\x1b[27u", ok: true},
		{name: "Shift+Enter", key: tea.Key{Code: tea.KeyEnter, Mod: tea.ModShift}, flags: flags, want: "\x1b[13;2u", ok: true},
		{name: "Shift+Tab", key: tea.Key{Code: tea.KeyTab, Mod: tea.ModShift}, flags: flags, want: "\x1b[9;2u", ok: true},
		{name: "Ctrl+Right", key: tea.Key{Code: tea.KeyRight, Mod: tea.ModCtrl}, flags: flags, want: "\x1b[1;5C", ok: true},
		{name: "Up ignores app cursor under kitty", key: tea.Key{Code: tea.KeyUp}, appCursor: true, flags: flags, want: "\x1b[A", ok: true},
		{name: "F1 uses its CSI form", key: tea.Key{Code: tea.KeyF1}, flags: flags, want: "\x1b[P", ok: true},
		{name: "F3 keeps its number", key: tea.Key{Code: tea.KeyF3}, flags: flags, want: "\x1b[13~", ok: true},
		{name: "plain text stays legacy", key: tea.Key{Code: 'a', Text: "a"}, flags: flags, want: "a", ok: true},
		{name: "shifted text stays legacy", key: tea.Key{Code: 'a', Text: "A", Mod: tea.ModShift}, flags: flags, want: "A", ok: true},
		{name: "unmodified Enter stays legacy", key: tea.Key{Code: tea.KeyEnter}, flags: flags, want: "\r", ok: true},
		{name: "unmodified Tab stays legacy", key: tea.Key{Code: tea.KeyTab}, flags: flags, want: "\t", ok: true},
		{name: "unmodified Backspace stays legacy", key: tea.Key{Code: tea.KeyBackspace}, flags: flags, want: "\x7f", ok: true},
	})
}

// TestKittyAlternateKeys: flag 4 adds the shifted key to a modified letter.
func TestKittyAlternateKeys(t *testing.T) {
	flags := ansi.KittyDisambiguateEscapeCodes | ansi.KittyReportAlternateKeys
	checkKittyKeys(t, []kittyKeyCase{
		{
			name:  "Ctrl+Shift+A reports the shifted key",
			key:   tea.Key{Code: 'a', ShiftedCode: 'A', Mod: tea.ModCtrl | tea.ModShift},
			flags: flags,
			want:  "\x1b[97:65;6u",
			ok:    true,
		},
	})
}

// TestKittyReportAllKeys: flag 8 encodes text keys and unmodified editing keys.
func TestKittyReportAllKeys(t *testing.T) {
	flags := ansi.KittyDisambiguateEscapeCodes | ansi.KittyReportAllKeysAsEscapeCodes
	checkKittyKeys(t, []kittyKeyCase{
		{name: "plain letter", key: tea.Key{Code: 'a', Text: "a"}, flags: flags, want: "\x1b[97u", ok: true},
		{name: "unmodified Enter", key: tea.Key{Code: tea.KeyEnter}, flags: flags, want: "\x1b[13u", ok: true},
		{name: "unmodified Backspace", key: tea.Key{Code: tea.KeyBackspace}, flags: flags, want: "\x1b[127u", ok: true},
	})
}

// TestKittyAssociatedText: flag 16 with report-all appends the typed text.
func TestKittyAssociatedText(t *testing.T) {
	flags := ansi.KittyDisambiguateEscapeCodes | ansi.KittyReportAllKeysAsEscapeCodes | ansi.KittyReportAssociatedKeys
	checkKittyKeys(t, []kittyKeyCase{
		{name: "letter with text", key: tea.Key{Code: 'a', Text: "a"}, flags: flags, want: "\x1b[97;;97u", ok: true},
	})
}
