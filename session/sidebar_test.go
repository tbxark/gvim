package session

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestSessionSidebarRoundTrip checks the optional sidebar UI state: it is saved
// with the session and comes back field for field.
func TestSessionSidebarRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	want := &SidebarState{
		ExplorerCollapsed:    true,
		WidthMode:            2,
		WidthValue:           42,
		Side:                 "right",
		ExplorerScroll:       3,
		ContentsOpen:         true,
		ContentsCollapsed:    true,
		ContentsRows:         12,
		ContentsScroll:       1,
		DiagnosticsOpen:      true,
		DiagnosticsCollapsed: true,
		DiagnosticsRows:      6,
	}
	if err := Save(dir, Session{Root: "/ws", Explorer: true, Sidebar: want}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := Load(dir)
	if err != nil || !ok {
		t.Fatalf("Load = %+v, %v (ok %v)", got, err, ok)
	}
	if got.Sidebar == nil {
		t.Fatal("the saved sidebar state did not come back")
	}
	if !reflect.DeepEqual(*got.Sidebar, *want) {
		t.Fatalf("sidebar = %+v, want %+v", *got.Sidebar, *want)
	}
}

// TestSessionWithoutSidebarKeepsDefaults checks migration safety: a session file
// from a release before the sidebar state existed loads with no sidebar state (the
// model then keeps the config defaults), and it does not fail the format version.
func TestSessionWithoutSidebarKeepsDefaults(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	legacy := `{
  "version": 1,
  "root": "/ws",
  "explorer": true,
  "showHidden": false,
  "showIgnored": false,
  "active": 0,
  "files": []
}`
	if err := os.WriteFile(filepath.Join(dir, "session.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	got, ok, err := Load(dir)
	if err != nil || !ok {
		t.Fatalf("Load = %+v, %v (ok %v)", got, err, ok)
	}
	if got.Sidebar != nil {
		t.Fatalf("an old session loaded a sidebar state: %+v", got.Sidebar)
	}
	// A newer file with a future sidebar field still loads: unknown fields are
	// ignored, so the format stays additive.
	future := `{
  "version": 1,
  "root": "/ws",
  "sidebar": {"contentsOpen": true, "futureSection": {"rows": 4}}
}`
	if err := os.WriteFile(filepath.Join(dir, "session.json"), []byte(future), 0o600); err != nil {
		t.Fatal(err)
	}
	got, ok, err = Load(dir)
	if err != nil || !ok || got.Sidebar == nil || !got.Sidebar.ContentsOpen {
		t.Fatalf("a future sidebar field broke the load: %+v, %v (ok %v)", got, err, ok)
	}
}
