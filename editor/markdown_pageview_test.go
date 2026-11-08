package editor

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/document"
)

func pageViewDoc(t *testing.T, content string) *Editor {
	t.Helper()
	e := docWith(Options{}, content)
	e.SetLanguageSettings(func(language, path string) LanguageSettings {
		return LanguageSettings{TabSize: 4, LineWrap: true}
	})
	return e
}

func TestTogglePageViewIsPerBufferAndLive(t *testing.T) {
	t.Parallel()
	e := pageViewDoc(t, "a **b** c\n")
	e.Do("toggle_page_view", nil)
	if got := e.SettingsFor(e.Active()).PageView; !got {
		t.Fatal("the page view did not reach the document settings")
	}
	if got := e.Status(); !strings.Contains(got, "Page view: on for this buffer") {
		t.Fatalf("status = %q", got)
	}
	// Another buffer does not inherit the toggle.
	run(t, e, "new")
	if got := e.SettingsFor(e.Active()).PageView; got {
		t.Fatal("the page view leaked into a new buffer")
	}
	run(t, e, "close_tab", "close_tab")
}

func TestSetPageWidthIsPerBuffer(t *testing.T) {
	t.Parallel()
	e := pageViewDoc(t, "abc\n")
	e.Do("set_page_width", map[string]any{"width": 42})
	if got := e.SettingsFor(e.Active()).PageWidth; got != 42 {
		t.Fatalf("page width = %d, want 42", got)
	}
}

func TestPageViewCopyUsesTheSourceMarkers(t *testing.T) {
	t.Parallel()
	e := pageViewDoc(t, "a **b** c\n")
	e.Do("toggle_page_view", nil)
	if got := text(e); !strings.Contains(got, "**b**") {
		t.Fatalf("text changed by the page view: %q", got)
	}
	// Select the whole line and copy: the clipboard text is the source text, not
	// the composed one.
	e.SetCursor(buffer.Pos{Line: 0, Col: 0}, false)
	e.Do("select_all", nil)
	r := e.Do("copy", nil)
	if r.Err != nil {
		t.Fatalf("copy: %v", r.Err)
	}
	if !strings.Contains(r.Text, "**b**") {
		t.Fatalf("copied %q, want the source markers", r.Text)
	}
}

func TestPageViewTurnsVirtualSpaceOff(t *testing.T) {
	t.Parallel()
	e := docWith(Options{VirtualSpace: document.VirtualSpaceOn}, "abc\n")
	e.SetLanguageSettings(func(language, path string) LanguageSettings {
		return LanguageSettings{TabSize: 4, LineWrap: false}
	})
	e.SetCursor(buffer.Pos{Line: 0, Col: 3}, false)
	run(t, e, "move_right")
	if got := e.Active().VirtualColumns(); got != 1 {
		t.Fatalf("virtual columns = %d before the page view, want 1", got)
	}
	e.Do("toggle_page_view", nil)
	run(t, e, "move_left")
	e.SetCursor(buffer.Pos{Line: 0, Col: 3}, false)
	run(t, e, "move_right")
	if got := e.Active().VirtualColumns(); got != 0 {
		t.Fatalf("virtual columns = %d with the page view on, want the caret clamped", got)
	}
}
