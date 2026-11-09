package uiadapter

import (
	"testing"

	"github.com/tbxark/gvim/i18n"
)

// TestTerminalDirNoticesUseTheCatalogue checks the terminal-directory notices: German
// falls back to the English text and the quoted dir/reason placeholders compose exactly
// as the old fmt.Sprintf did. Not parallel: locale.
func TestTerminalDirNoticesUseTheCatalogue(t *testing.T) {
	i18n.SetLocale("de")
	t.Cleanup(func() { i18n.SetLocale("en") })
	got := uiadapterTextVars("gvim.notice.remote_terminal_dir_unavailable", "Remote terminal directory %{dir} is unavailable (%{reason}); starting in the remote default directory", map[string]any{"dir": "\"/w/old\"", "reason": "missing"})
	if got != "Remote terminal directory \"/w/old\" is unavailable (missing); starting in the remote default directory" {
		t.Errorf("remote terminal notice = %q", got)
	}
	if got := uiadapterText("gvim.notice.terminal_dir_invalid", "Terminal directory %{dir} is not an absolute path on this platform; starting in the default directory"); got == "gvim.notice.terminal_dir_invalid" {
		t.Errorf("invalid terminal dir notice leaked the key")
	}
}
