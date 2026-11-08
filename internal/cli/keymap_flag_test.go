package cli

import (
	"strings"
	"testing"
)

// An unknown --keymap or --theme is one line that names the valid values, not the whole usage
// text first (KM-11).
func TestBadKeymapFlagIsOneLineWithoutTheUsageText(t *testing.T) {
	isolateHome(t)
	for _, tc := range []struct {
		args []string
		want []string
	}{
		{[]string{"--keymap", "nosuch"}, []string{`unknown keymap "nosuch"`, "default, macos, macos-gui, emacs or vscode"}},
		{[]string{"--theme", "nosuch"}, []string{`unknown theme "nosuch"`}},
	} {
		rec := &recorder{}
		code, stdout, stderr := execute(t, tc.args, rec)
		if code != 1 || rec.calls != 0 {
			t.Fatalf("%v: exit %d, runner called %d times; want 1 and 0", tc.args, code, rec.calls)
		}
		for _, w := range tc.want {
			if !strings.Contains(stderr, w) {
				t.Errorf("%v: stderr %q lacks %q", tc.args, stderr, w)
			}
		}
		if strings.Contains(stdout, "Usage:") || strings.Contains(stderr, "Usage:") || strings.Count(stderr, "\n") > 2 {
			t.Errorf("%v: printed the usage text: stdout %q, stderr %q", tc.args, stdout, stderr)
		}
	}
}
