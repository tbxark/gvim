package restore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/internal/terminal"
)

// TestTerminalLiveFlagRoundTrip checks the additive live flag the live-restore
// path uses: it is saved per tab (and mirrored at top level), survives LoadTerminal,
// and an old record without it loads as exited/dead so migration keeps today's
// read-only restore.
func TestTerminalLiveFlagRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st := terminal.SavedState{Version: terminal.SavedStateVersion, Tabs: []terminal.SavedTab{
		{Name: "live", Open: true, Rows: []string{"live transcript"}, Live: true},
		{Name: "dead", Open: true, Rows: []string{"dead transcript"}},
	}, ActiveTab: 0}
	if err := SaveTerminal(dir, st); err != nil {
		t.Fatal(err)
	}
	got, ok, err := LoadTerminal(dir)
	if err != nil || !ok {
		t.Fatalf("LoadTerminal = %v, %v", ok, err)
	}
	if !got.Tabs[0].Live || got.Tabs[1].Live {
		t.Fatalf("live flags = %v/%v", got.Tabs[0].Live, got.Tabs[1].Live)
	}
	// An older record has no live field anywhere: it loads as exited/dead, exactly
	// the pre-live-restore behavior.
	legacy := terminal.SavedState{Version: terminal.SavedStateVersion, Tabs: []terminal.SavedTab{{Open: true, Rows: []string{"old"}}}}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, terminalFileName)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	old, ok, err := LoadTerminal(dir)
	if err != nil || !ok {
		t.Fatalf("legacy LoadTerminal = %v, %v", ok, err)
	}
	if len(old.Tabs) != 1 || old.Tabs[0].Live {
		t.Fatalf("record without a live flag = %+v, want one tab that is not live", old)
	}
}
