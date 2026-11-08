package tui

import (
	"testing"
	"time"

	"github.com/tbxark/gvim/pluginapi"
)

// Three quick Esc presses are the panic chord only while the plugin input is stuck: a mode that
// binds Esc itself, as vi_mode does, answers every press, so Esc mashing must not switch it off.
func TestTripleEscSparesAModeThatBindsEsc(t *testing.T) {
	t.Parallel()
	vi := pluginapi.ModeInfo{Name: "vi_normal", Text: pluginapi.TextSwallow, Fallthrough: true,
		Bindings: map[string]string{"escape": "p.fn", "ctrl+[": "p.fn"}}
	tests := []struct {
		name  string
		mode  pluginapi.ModeInfo
		setup func(e *keyEnv)
		want  bool // the mode is cleared
	}{
		{"a mode that binds Esc", vi, nil, false},
		{"a mode that does not", pluginapi.ModeInfo{Name: "m", Text: pluginapi.TextSwallow}, nil, true},
		{"a mode that binds Esc, with a key wait pending the whole time", vi, func(e *keyEnv) {
			must(t, e.m.plug.host.WaitKey(7, 0))
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newKeyEnv(t)
			e.mode(t, tt.mode)
			if tt.setup != nil {
				tt.setup(e)
			}
			for i := range 6 {
				if tt.setup != nil && i > 0 {
					// a plugin that loops on next_key waits again after each key
					_ = e.m.plug.host.WaitKey(pluginapi.Token(10+i), 0)
				}
				pressEsc(e, 100*time.Millisecond)
			}
			if cleared := e.m.plug.keys.editor == (modeRef{}); cleared != tt.want {
				t.Fatalf("mode cleared = %v, want %v (notice %q)", cleared, tt.want, e.m.notice)
			}
		})
	}
}

// ctrl+c cancels a plugin prompt like Esc (vi's `:` and `/` prompts), and does nothing to the
// overlays of the editor itself.
func TestCtrlCCancelsAPluginOverlay(t *testing.T) {
	t.Parallel()
	e := newKeyEnv(t)
	o := &plugOverlayStub{}
	e.m.pushOverlay(o)
	e.press(ctrl('c'))
	if !o.dismissed || len(e.m.overlays) != 0 {
		t.Fatalf("ctrl+c left the plugin overlay: dismissed %v, %d overlays", o.dismissed, len(e.m.overlays))
	}
	if e.m.pluginCancelKey(ctrl('c').Key()) {
		t.Fatal("ctrl+c counts as a cancel with no overlay open")
	}
}

// A prompt that the first Esc closes does not make the next two presses a panic: they find vi
// back in charge.
func TestTripleEscAfterAClosedPromptIsNotAPanic(t *testing.T) {
	t.Parallel()
	e := newKeyEnv(t)
	e.mode(t, pluginapi.ModeInfo{Name: "vi_normal", Text: pluginapi.TextSwallow, Fallthrough: true,
		Bindings: map[string]string{"escape": "p.fn"}})
	e.m.pushOverlay(&plugOverlayStub{})
	for range 3 {
		pressEsc(e, 50*time.Millisecond)
	}
	if e.m.plug.keys.editor == (modeRef{}) {
		t.Fatalf("Esc, Esc, Esc after a prompt cleared the mode: %q", e.m.notice)
	}
}
