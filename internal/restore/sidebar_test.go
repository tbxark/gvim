package restore

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/tbxark/gvim/explorer"
	"github.com/tbxark/gvim/internal/apptest"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/session"
)

// TestSidebarStateConversion checks the tui<->session conversion: every field
// survives, the width form and side are typed both ways, an unknown side/mode
// falls back to the defaults, and no state means nil (config defaults).
func TestSidebarStateConversion(t *testing.T) {
	t.Parallel()
	ui := tui.SidebarState{
		ExplorerCollapsed:    true,
		Width:                explorer.PercentWidth(30),
		Side:                 explorer.SideRight,
		ExplorerScroll:       2,
		ContentsOpen:         true,
		ContentsCollapsed:    true,
		ContentsScroll:       1,
		DiagnosticsOpen:      true,
		DiagnosticsCollapsed: true,
		SectionRows:          map[string]int{"explorer": 6, "contents": 11, "diagnostics": 7, "plugin:1": 5},
		PluginOrder:          []string{"3", "1"},
		PluginCollapsed:      map[string]bool{"3": true},
	}
	saved := SaveSidebarState(ui)
	if saved.WidthMode != int(explorer.WidthPercent) || saved.WidthValue != 30 || saved.Side != "right" {
		t.Fatalf("saved width/side = %+v", saved)
	}
	got := SidebarState(session.Session{Sidebar: saved})
	if got == nil {
		t.Fatal("conversion to nil")
	}
	if !reflect.DeepEqual(*got, ui) {
		t.Fatalf("converted = %+v, want %+v", *got, ui)
	}
	// Missing state keeps the defaults; a bad side/mode falls back rather than
	// refusing the session.
	if SidebarState(session.Session{}) != nil {
		t.Error("a session without sidebar state converted to non-nil")
	}
	got = SidebarState(session.Session{Sidebar: &session.SidebarState{Side: "bogus", WidthMode: 9}})
	if got == nil || got.Side != explorer.SideLeft || got.Width != (explorer.Width{}) {
		t.Errorf("bad sidebar values = %+v, want the defaults", got)
	}
	// Automatic width also round-trips.
	automatic := tui.SidebarState{Width: explorer.Width{}, Side: explorer.SideLeft}
	if v := SidebarState(session.Session{Sidebar: SaveSidebarState(automatic)}); v == nil || !reflect.DeepEqual(*v, automatic) {
		t.Errorf("automatic width round trip = %+v", v)
	}
}

// TestWriteSessionKeepsSidebarState checks the save path: the sidebar state the
// model reports goes into the session file and is read back.
func TestWriteSessionKeepsSidebarState(t *testing.T) {
	apptest.IsolateConfig(t)
	dir := t.TempDir()
	sessDir := filepath.Join(dir, "session")
	ui := tui.SidebarState{
		ExplorerCollapsed: true,
		Width:             explorer.ColumnsWidth(40),
		Side:              explorer.SideRight,
		ContentsOpen:      true,
		DiagnosticsOpen:   true,
		SectionRows:       map[string]int{"contents": 9, "diagnostics": 5, "plugin:7": 4},
		PluginOrder:       []string{"7"},
		PluginCollapsed:   map[string]bool{"7": true},
	}
	ed := apptest.NewSessionEditor()
	apptest.Must(t, SaveSession(ed, sessDir, tui.ExplorerState{Open: true, Root: dir, Sidebar: ui}))
	got, ok, err := session.Load(sessDir)
	apptest.Must(t, err)
	if !ok {
		t.Fatal("no session was saved")
	}
	if got.Sidebar == nil {
		t.Fatal("the session file has no sidebar state")
	}
	if back := SidebarState(got); back == nil || !reflect.DeepEqual(*back, ui) {
		t.Fatalf("restored sidebar = %+v, want %+v", back, ui)
	}
}
