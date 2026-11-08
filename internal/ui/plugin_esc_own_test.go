package ui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/pluginapi"
	"github.com/tbxark/gvim/internal/ui/grid"
)

// A mode that uses Esc itself (vi's: Esc leaves insert mode, cancels a command) is not
// cleared by three quick Esc presses; ctrl+^ always clears it.
func TestTripleEscSparesAModeThatUsesEsc(t *testing.T) {
	t.Run("the mode stays", func(t *testing.T) {
		e := newKeyEnv(t)
		e.mode(t, pluginapi.ModeInfo{Name: "m", Bindings: map[string]string{"escape": "p.fn"}})
		pressEsc(e, 0)
		pressEsc(e, 100*time.Millisecond)
		pressEsc(e, 100*time.Millisecond)
		pressEsc(e, 100*time.Millisecond)
		if e.m.plug.keys.editor == (modeRef{}) || e.m.notice == "plugin input cleared" {
			t.Fatalf("Esc presses cleared a mode that uses Esc (notice %q)", e.m.notice)
		}
	})
	t.Run("ctrl+^ still clears it", func(t *testing.T) {
		e := newKeyEnv(t)
		e.mode(t, pluginapi.ModeInfo{Name: "m", Bindings: map[string]string{"escape": "p.fn"}})
		e.m.Update(decodeKey(t, "\x1e"))
		if e.m.plug.keys.editor != (modeRef{}) {
			t.Fatal("the panic chord left the mode")
		}
	})
	t.Run("a key wait on top of it is cleared by three Esc", func(t *testing.T) {
		e := newKeyEnv(t)
		e.mode(t, pluginapi.ModeInfo{Name: "m", Bindings: map[string]string{"escape": "p.fn"}})
		must(t, e.m.plug.host.WaitKey(3, 0))
		pressEsc(e, 0)
		must(t, e.m.plug.host.WaitKey(4, 0))
		pressEsc(e, 10*time.Millisecond)
		must(t, e.m.plug.host.WaitKey(5, 0))
		pressEsc(e, 10*time.Millisecond)
		if len(e.rt.resolved) != 3 || e.rt.resolved[2].Token != 5 || e.rt.resolved[2].Err == "" {
			t.Fatalf("resolved = %+v", e.rt.resolved)
		}
	})
	t.Run("an overlay that ignores Esc is cleared by three Esc", func(t *testing.T) {
		e := newKeyEnv(t)
		e.mode(t, pluginapi.ModeInfo{Name: "m", Bindings: map[string]string{"escape": "p.fn"}})
		e.m.pushOverlay(&plugOverlayStub{takesEsc: true})
		pressEsc(e, 0)
		pressEsc(e, 10*time.Millisecond)
		pressEsc(e, 10*time.Millisecond)
		if e.m.plug.keys.editor != (modeRef{}) {
			t.Fatal("three Esc did not clear the input held by an overlay")
		}
	})
	t.Run("a prompt that closes on Esc does not arm the chord", func(t *testing.T) {
		e := newKeyEnv(t)
		e.mode(t, pluginapi.ModeInfo{Name: "m", Bindings: map[string]string{"escape": "p.fn"}})
		e.m.pushOverlay(&plugOverlayClosing{})
		pressEsc(e, 0)
		pressEsc(e, 100*time.Millisecond)
		pressEsc(e, 100*time.Millisecond)
		if e.m.plug.keys.editor == (modeRef{}) || e.m.notice == "plugin input cleared" {
			t.Fatalf("Esc after closing a prompt cleared the mode (notice %q)", e.m.notice)
		}
	})
}

// plugOverlayClosing is a plugin prompt that closes itself on any key.
type plugOverlayClosing struct{}

func (o *plugOverlayClosing) TakesEscape() bool                 { return true }
func (o *plugOverlayClosing) Update(tea.Msg) (Overlay, tea.Cmd) { return nil, nil }
func (o *plugOverlayClosing) Draw(*grid.Grid)                   {}
func (o *plugOverlayClosing) Active() bool                      { return true }
func (o *plugOverlayClosing) pluginOverlay()                    {}
