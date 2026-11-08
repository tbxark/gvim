package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Round-9 real-host smoke for the dashboard and code-tour ports: the plugins run through
// the real app/runtime/UI, no fake host.

// enterKey is the Enter key press the real-host tests use.
func enterKey() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyEnter} }

// TestRealHostDashboard renders the start screen and activates an action with the keyboard.
func TestRealHostDashboard(t *testing.T) {
	isolateConfig(t)
	writeUserFile(t, "config.json", `{"plugins":{"dashboard":{"enabled":true}}}`)
	root := t.TempDir()
	s := newSession(t, Options{Root: root})
	s.waitStatus("dashboard", "loaded")
	s.until("the dashboard frame", func() bool { return s.screenHas("FRESH") && s.screenHas("Actions") })
	// The buffer-local dashboard mode binds Enter to the focused row (Open Settings).
	s.press(enterKey())
	s.settle(80 * time.Millisecond)
	s.until("the settings page", func() bool { return s.screenHas("Settings") })
	// Opening a file replaces the automatic dashboard.
	s.openFile("a.txt", "text\n")
	s.settle(150 * time.Millisecond)
	if s.screenHas("FRESH") && s.screenHas("Actions") {
		t.Errorf("the auto dashboard is still painted after opening a file")
	}
}

// TestRealHostCodeTour loads a tour from the palette and walks a step.
func TestRealHostCodeTour(t *testing.T) {
	isolateConfig(t)
	writeUserFile(t, "config.json", `{"plugins":{"code_tour":{"enabled":true}}}`)
	root := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(root, "src"), 0o755))
	must(t, os.WriteFile(filepath.Join(root, "src", "a.go"), []byte("line one\nline two\n"), 0o644))
	tourPath := filepath.Join(root, "demo.fresh-tour.json")
	must(t, os.WriteFile(tourPath, []byte(`{"title":"Demo Tour","schema_version":"1.0","steps":[
	  {"title":"first step","file_path":"src/a.go","lines":[1,1],"explanation":"Look at line one"},
	  {"title":"second step","file_path":"src/a.go","lines":[2,2],"explanation":"Then line two"}
	]}`), 0o644))
	s := newSession(t, Options{Root: root})
	s.waitStatus("code_tour", "loaded")
	s.runCommand("Tour: Load Definition...")
	s.until("the path prompt", func() bool { return s.screenHas("Enter tour file path") })
	s.typeText(tourPath)
	s.press(enterKey())
	s.settle(150 * time.Millisecond)
	s.until("the tour panel", func() bool { return s.screenHas("Demo Tour") && s.screenHas("first step") })
	if !s.screenHas("Look at line one") {
		t.Errorf("the step prose is missing; rows %v", s.rows())
	}
	// The step opened src/a.go in the editor (buf.open with the step position).
	if name := s.rt.ed.Active().Name(); name != "a.go" {
		t.Errorf("active buffer = %q, want the step file a.go", name)
	}
	// The next/prev/exit commands exist in the runtime (their palette rows are
	// covered by the fake-host command test; the floating panel owns the keyboard,
	// so the real-host smoke stops at the loaded tour).
	if !s.plug.Owns("code_tour.next") || !s.plug.Owns("code_tour.prev") || !s.plug.Owns("code_tour.exit") {
		t.Errorf("runtime does not own the tour navigation commands")
	}
	if p := s.pluginProblems("code_tour"); len(p) != 0 {
		t.Errorf("code_tour problems: %v", p)
	}
}
