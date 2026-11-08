package app

import (
	"testing"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/internal/uiadapter"
	"github.com/tbxark/gvim/internal/uitest"
	"github.com/tbxark/gvim/keymap"
	"github.com/tbxark/gvim/theme"
)

// The [stdin] buffer in the real UI: it is labelled, shows the piped text unmodified,
// editing marks it modified, and saving it asks for a path (Save As) like any untitled
// buffer.
func TestStdinBufferInTheUI(t *testing.T) {
	ed := editor.New(editor.Options{Rows: 27, Cols: 100})
	ed.OpenStdinText("hello from stdin\nsecond line\n", editor.StdinOptions{Label: "[stdin]", Language: "markdown"})
	th, err := theme.Load("dark")
	if err != nil {
		t.Fatal(err)
	}
	km, err := keymap.ByName("default")
	if err != nil {
		t.Fatal(err)
	}
	m := tui.New(uiadapter.NewDeps(ed, km, th, nil, nil))
	s := uitest.Start(t, m, 100, 30)
	s.WaitFor(func(sc uitest.Screen) bool {
		return sc.Contains("[stdin]") && sc.Contains("hello from stdin") && sc.Contains("second line")
	})
	// Typing at the top marks the buffer modified (the tab/status show a +) and the text.
	s.Key("i")
	s.Type("X")
	s.Key("esc")
	s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("Xhello from stdin") })
	if !ed.Active().Modified() {
		t.Error("editing did not mark the stdin buffer modified")
	}
	// Save on an untitled buffer opens Save As; it suggests the --stdin-name path when
	// there is one, and is cancelled here.
	s.Key("ctrl+s")
	s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("Save As") })
	s.Key("esc")
	s.WaitFor(func(sc uitest.Screen) bool { return !sc.Contains("Save As") })
}
