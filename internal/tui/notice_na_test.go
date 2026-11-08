package tui

import (
	"strings"
	"testing"
)

// TestNotApplicableCommandsSayWhy checks that each command that cannot work in gvim
// is handled by a notice that starts with the same prefix and says why, so it is not
// reported as unsupported. None of them starts a command.
func TestNotApplicableCommandsSayWhy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		action string
		why    string
	}{
		{"detach", "no server"},
		{"workspace_trust_prompt", "workspace trust model"},
		{"update_fresh", "does not update itself"},
		{"open_update_log", "does not update itself"},
		{"trigger_wave_animation", "easter egg"},
		{"inspect_theme_at_cursor", "reverse video"},
	}
	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			t.Parallel()
			m := newTestModel(t, newFakeEditor("x"), nil, 80, 24)
			if cmd := rootPaletteRun(m, tt.action); cmd != nil {
				t.Fatalf("%s started a command", tt.action)
			}
			if !strings.HasPrefix(m.notice, "Not available in gvim: ") {
				t.Fatalf("notice = %q, want the not-available prefix", m.notice)
			}
			if !strings.Contains(m.notice, tt.why) {
				t.Fatalf("notice = %q, want it to say %q", m.notice, tt.why)
			}
		})
	}
}

// TestUnfinishedCommandsSayWhatIsMissing checks the commands that gvim can run in part
// only: they show a notice that says what is missing, and they are not reported as
// unsupported. The notices name the missing piece.
func TestUnfinishedCommandsSayWhatIsMissing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		action string
		want   string
	}{
		{"list_bookmarks", "List Bookmarks is not available yet: the editor does not list its bookmark slots"},
	}
	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			t.Parallel()
			m := newTestModel(t, newFakeEditor("x"), nil, 80, 24)
			if cmd := rootPaletteRun(m, tt.action); cmd != nil {
				t.Fatalf("%s started a command", tt.action)
			}
			if m.notice != tt.want {
				t.Fatalf("notice = %q, want %q", m.notice, tt.want)
			}
		})
	}
}
