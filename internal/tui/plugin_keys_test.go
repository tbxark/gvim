package tui

import (
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/pluginapi"
	"github.com/tbxark/gvim/theme"
)

// keyEnv is a model with an explorer, a dock, a plugin runtime and a plugin editor port.
type keyEnv struct {
	m       *Model
	ed      *fakeEditor
	rt      *fakeRuntime
	pe      *fakePluginEditor
	starter *fakeStarter
	now     time.Time
}

func newKeyEnv(t *testing.T, ports ...func(*RootPorts)) *keyEnv {
	t.Helper()
	rt := newFakeRuntime(t, "p.fn", "p.text")
	ed := newFakeEditor("hello", "world")
	ed.tabs = []TabInfo{{Name: "a.txt", Path: "/w/a.txt", Active: true}}
	pe := &fakePluginEditor{ed: ed, active: 1, bufs: []pluginapi.BufferInfo{{ID: 1, Path: "/w/a.txt", Name: "a.txt", Active: true}}}
	root := RootPorts{Plugins: rt, PluginEditor: pe}
	for _, f := range ports {
		f(&root)
	}
	starter := newFakeStarter()
	m := New(Deps{Editor: ed, Explorer: newFakeTree(3), Keys: dockKeys(), Theme: theme.Theme{}, Clock: testClock, Root: root})
	m.SetDock(DockPorts{Starter: starter})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	useHost(rt, m)
	m.plug.started = true // as after Init
	env := &keyEnv{m: m, ed: ed, rt: rt, pe: pe, starter: starter, now: testClock()}
	m.now = func() time.Time { return env.now }
	return env
}

// mode defines the mode of plugin p and enters it.
func (e *keyEnv) mode(t *testing.T, info pluginapi.ModeInfo) {
	t.Helper()
	info.Plugin = "p"
	e.m.plug.host.SetModes("p", []pluginapi.ModeInfo{info})
	must(t, e.m.plug.host.SetInputMode("p", info.Name))
}

func (e *keyEnv) press(k tea.KeyPressMsg) tea.Cmd { return press(e.m, k) }

func (e *keyEnv) actions() []editor.Action { return e.ed.actions }

// decodeKey decodes a terminal byte sequence the way the Bubble Tea input driver does.
func decodeKey(t *testing.T, seq string) tea.KeyPressMsg {
	t.Helper()
	var d uv.EventDecoder
	n, ev := d.Decode([]byte(seq))
	kp, ok := ev.(uv.KeyPressEvent)
	if n != len(seq) || !ok {
		t.Fatalf("%q decoded to %d bytes of %T", seq, n, ev)
	}
	return tea.KeyPressMsg(kp)
}

// noDocFake is an editor with no tab open.
type noDocFake struct{ *fakeEditor }

func (noDocFake) Active() Document { return nil }

// plugOverlayStub is an overlay a plugin opened.
type plugOverlayStub struct{ dismissed, takesEsc bool }

func (o *plugOverlayStub) TakesEscape() bool { return o.takesEsc }

func (o *plugOverlayStub) Update(tea.Msg) (Overlay, tea.Cmd) { return o, nil }
func (o *plugOverlayStub) Draw(*grid.Grid)                   {}
func (o *plugOverlayStub) Active() bool                      { return !o.dismissed }
func (o *plugOverlayStub) Dismiss()                          { o.dismissed = true }
func (o *plugOverlayStub) pluginOverlay()                    {}

func TestModeBindingsRunActions(t *testing.T) {
	e := newKeyEnv(t)
	e.mode(t, pluginapi.ModeInfo{
		Name:     "nav",
		Bindings: map[string]string{"j": "move_down", "k": "p.fn", "ctrl+d": "page_down"},
		Args:     map[string]map[string]any{"j": {"n": 2}},
		Text:     pluginapi.TextSwallow, Fallthrough: true,
	})
	e.press(typed("j"))
	e.press(typed("k"))
	e.press(ctrl('d'))
	if want := []editor.Action{"move_down", "page_down"}; !slices.Equal(e.actions(), want) {
		t.Fatalf("editor actions = %v, want %v", e.actions(), want)
	}
	if got := e.ed.args[0]["n"]; got != 2 {
		t.Fatalf("args of j = %v, want n=2", e.ed.args[0])
	}
	if !slices.Equal(e.rt.runs, []string{"p.fn"}) || e.rt.runSrc[0] != pluginapi.SrcMode {
		t.Fatalf("plugin runs = %v from %v, want p.fn from mode", e.rt.runs, e.rt.runSrc)
	}
}

func TestModeTextPolicies(t *testing.T) {
	tests := []struct {
		name    string
		info    pluginapi.ModeInfo
		actions []editor.Action
		runs    []string
		args    map[string]any
	}{
		{"pass types the text", pluginapi.ModeInfo{Name: "m"}, []editor.Action{"insert_char"}, nil, nil},
		{"swallow drops it", pluginapi.ModeInfo{Name: "m", Text: pluginapi.TextSwallow}, nil, nil, nil},
		{"handler gets it", pluginapi.ModeInfo{Name: "m", Text: pluginapi.TextHandler, TextAction: "p.text"}, nil, []string{"p.text"}, map[string]any{"text": "z"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newKeyEnv(t)
			e.mode(t, tt.info)
			e.press(typed("z"))
			if !slices.Equal(e.actions(), tt.actions) || !slices.Equal(e.rt.runs, tt.runs) {
				t.Fatalf("actions %v runs %v, want %v and %v", e.actions(), e.rt.runs, tt.actions, tt.runs)
			}
			if tt.args != nil && e.rt.runArgs[0]["text"] != tt.args["text"] {
				t.Fatalf("args = %v, want %v", e.rt.runArgs[0], tt.args)
			}
		})
	}
}

func TestModeFallthrough(t *testing.T) {
	for _, ft := range []bool{true, false} {
		e := newKeyEnv(t)
		e.mode(t, pluginapi.ModeInfo{Name: "m", Fallthrough: ft})
		e.press(special(tea.KeyEnter)) // insert_newline in the test keymap
		if got := slices.Contains(e.actions(), "insert_newline"); got != ft {
			t.Fatalf("fallthrough %v: Enter reached the keymap = %v", ft, got)
		}
	}
}

func TestModeSequences(t *testing.T) {
	info := pluginapi.ModeInfo{
		Name: "m", Text: pluginapi.TextSwallow, Fallthrough: true,
		Bindings: map[string]string{"g g": "goto_top", "g": "p.fn", "d d": "delete_line", "ctrl+k ctrl+c": "p.text"},
	}
	t.Run("the whole sequence runs the action", func(t *testing.T) {
		e := newKeyEnv(t)
		e.mode(t, info)
		if cmd := e.press(typed("d")); cmd == nil {
			t.Fatal("the first step started no timer")
		}
		if len(e.actions()) != 0 {
			t.Fatalf("actions after one step = %v", e.actions())
		}
		e.press(typed("d"))
		if !slices.Equal(e.actions(), []editor.Action{"delete_line"}) {
			t.Fatalf("actions = %v", e.actions())
		}
	})
	t.Run("an ambiguous prefix waits, then runs on a timeout", func(t *testing.T) {
		e := newKeyEnv(t)
		e.mode(t, info)
		e.press(typed("g"))
		gen := e.m.plug.keys.seq.gen
		if len(e.rt.runs) != 0 {
			t.Fatalf("p.fn ran at the first g: %v", e.rt.runs)
		}
		e.m.Update(pluginSeqMsg{gen: gen - 1}) // a stale timer does nothing
		if len(e.rt.runs) != 0 {
			t.Fatal("a stale timer ran the action")
		}
		e.m.Update(pluginSeqMsg{gen: gen})
		if !slices.Equal(e.rt.runs, []string{"p.fn"}) {
			t.Fatalf("runs = %v, want p.fn after the timeout", e.rt.runs)
		}
		if e.m.plug.keys.seq.node != nil {
			t.Fatal("the sequence is still pending")
		}
	})
	t.Run("a longer match wins over the prefix", func(t *testing.T) {
		e := newKeyEnv(t)
		e.mode(t, info)
		e.press(typed("g"))
		e.press(typed("g"))
		if !slices.Equal(e.actions(), []editor.Action{"goto_top"}) || len(e.rt.runs) != 0 {
			t.Fatalf("actions %v runs %v", e.actions(), e.rt.runs)
		}
	})
	t.Run("a broken sequence runs the shorter binding and handles the key afresh", func(t *testing.T) {
		e := newKeyEnv(t)
		e.mode(t, info)
		e.press(typed("g"))
		e.press(typed("d"))
		if !slices.Equal(e.rt.runs, []string{"p.fn"}) {
			t.Fatalf("runs = %v, want the shorter g binding", e.rt.runs)
		}
		e.press(typed("d"))
		if !slices.Equal(e.actions(), []editor.Action{"delete_line"}) {
			t.Fatalf("actions = %v: the key that broke the sequence started a new one", e.actions())
		}
	})
	t.Run("a prefix with no binding is dropped when the next key does not continue it", func(t *testing.T) {
		e := newKeyEnv(t)
		e.mode(t, info)
		e.press(ctrl('k'))
		e.press(typed("x")) // swallowed text
		if len(e.rt.runs) != 0 || len(e.actions()) != 0 {
			t.Fatalf("runs %v actions %v", e.rt.runs, e.actions())
		}
		e.press(ctrl('k'))
		e.press(ctrl('c'))
		if !slices.Equal(e.rt.runs, []string{"p.text"}) {
			t.Fatalf("runs = %v", e.rt.runs)
		}
	})
}

func TestModeKeysSkipOtherFocus(t *testing.T) {

	t.Run("the welcome screen keeps its keys", func(t *testing.T) {
		rt := newFakeRuntime(t)
		m := New(Deps{Editor: noDocFake{newFakeEditor()}, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock, Root: RootPorts{Plugins: rt}})
		m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		useHost(rt, m)
		m.plug.host.SetModes("p", []pluginapi.ModeInfo{{Name: "m", Plugin: "p", Text: pluginapi.TextSwallow}})
		must(t, m.plug.host.SetInputMode("p", "m"))
		if !m.welcomeVisible() {
			t.Fatal("no welcome screen")
		}
		if _, taken := m.pluginKey(tea.Key(special(tea.KeyRight))); taken {
			t.Fatal("the mode took a key on the welcome screen")
		}
	})
}

func TestPasteInModes(t *testing.T) {
	t.Run("a mode that does not name paste lets it insert", func(t *testing.T) {
		e := newKeyEnv(t)
		e.mode(t, pluginapi.ModeInfo{Name: "m", Text: pluginapi.TextSwallow})
		e.m.Update(tea.PasteMsg{Content: "pasted"})
		if !slices.Equal(e.actions(), []editor.Action{"paste"}) {
			t.Fatalf("actions = %v", e.actions())
		}
	})
	t.Run("with the explorer focused the mode does not see the paste", func(t *testing.T) {
		e := newKeyEnv(t)
		e.mode(t, pluginapi.ModeInfo{Name: "m", Text: pluginapi.TextHandler, Paste: pluginapi.TextHandler, TextAction: "p.text"})
		e.m.explorerOpen, e.m.explorerFocus = true, true
		e.m.Update(tea.PasteMsg{Content: "pasted"})
		if len(e.rt.runs) != 0 {
			t.Fatalf("runs = %v", e.rt.runs)
		}
	})
}

func TestKeyWait(t *testing.T) {
	wait := func(e *keyEnv, tok pluginapi.Token, ms int) {
		t.Helper()
		must(t, e.m.plug.host.WaitKey(tok, ms))
		e.m.pluginPump()
	}

	t.Run("the next key is consumed and resolves the wait", func(t *testing.T) {
		e := newKeyEnv(t)
		wait(e, 5, 0)
		e.press(typed("a"))
		if len(e.actions()) != 0 {
			t.Fatalf("the key reached the editor: %v", e.actions())
		}
		if len(e.rt.resolved) != 1 || e.rt.resolved[0].Token != 5 {
			t.Fatalf("resolved = %+v", e.rt.resolved)
		}
		if k, _ := e.rt.resolved[0].Values[0].(pluginapi.Key); k != (pluginapi.Key{Key: "a", Text: "a"}) {
			t.Fatalf("key = %+v", k)
		}
		e.press(typed("b")) // the capture is over
		if !slices.Equal(e.actions(), []editor.Action{"insert_char"}) {
			t.Fatalf("actions after the wait = %v", e.actions())
		}
	})

	t.Run("keys arrive as the plugin sees them", func(t *testing.T) {
		tests := []struct {
			key  tea.KeyPressMsg
			want pluginapi.Key
		}{
			{special(tea.KeyEscape), pluginapi.Key{Key: "escape"}},
			{ctrl('x'), pluginapi.Key{Key: "x", Ctrl: true}},
			{tea.KeyPressMsg{Code: 'a', Text: "A", Mod: tea.ModShift}, pluginapi.Key{Key: "a", Text: "A", Shift: true}},
			{tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}, pluginapi.Key{Key: "tab", Shift: true}},
			{tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}, pluginapi.Key{Key: "space", Text: " "}},
			{tea.KeyPressMsg{Code: tea.KeyF5}, pluginapi.Key{Key: "F5"}},
			{tea.KeyPressMsg{Code: 'j', Mod: tea.ModAlt}, pluginapi.Key{Key: "j", Alt: true}},
			{special(tea.KeyUp), pluginapi.Key{Key: "up"}},
		}
		for _, tt := range tests {
			e := newKeyEnv(t)
			wait(e, 1, 0)
			e.press(tt.key)
			if len(e.rt.resolved) != 1 {
				t.Fatalf("%+v: resolved = %+v", tt.key, e.rt.resolved)
			}
			if k, _ := e.rt.resolved[0].Values[0].(pluginapi.Key); k != tt.want {
				t.Errorf("%+v: key = %+v, want %+v", tt.key, k, tt.want)
			}
		}
	})

	t.Run("the wait goes before the mode", func(t *testing.T) {
		e := newKeyEnv(t)
		e.mode(t, pluginapi.ModeInfo{Name: "m", Bindings: map[string]string{"j": "move_down"}})
		wait(e, 2, 0)
		e.press(typed("j"))
		if len(e.actions()) != 0 || len(e.rt.resolved) != 1 {
			t.Fatalf("actions %v resolved %v", e.actions(), e.rt.resolved)
		}
	})

	t.Run("a second capture is busy until the first ends", func(t *testing.T) {
		e := newKeyEnv(t)
		wait(e, 1, 0)
		if err := e.m.plug.host.WaitKey(2, 0); pluginapi.CodeOf(err) != pluginapi.CodeBusy {
			t.Fatalf("second WaitKey = %v, want busy", err)
		}
		e.m.plug.host.CancelWait(1)
		if err := e.m.plug.host.WaitKey(3, 0); err != nil {
			t.Fatalf("WaitKey after a cancel = %v", err)
		}
	})

	t.Run("a cancelled wait no longer takes keys", func(t *testing.T) {
		e := newKeyEnv(t)
		wait(e, 1, 0)
		e.m.plug.host.CancelWait(1)
		e.press(typed("a"))
		if !slices.Equal(e.actions(), []editor.Action{"insert_char"}) || len(e.rt.resolved) != 0 {
			t.Fatalf("actions %v resolved %v", e.actions(), e.rt.resolved)
		}
	})

	t.Run("the timeout resolves the wait with a timeout error", func(t *testing.T) {
		e := newKeyEnv(t)
		wait(e, 4, 250)
		e.m.Update(pluginKeyTimeoutMsg{t: 9}) // another token: ignored
		if len(e.rt.resolved) != 0 {
			t.Fatalf("resolved = %+v", e.rt.resolved)
		}
		e.m.Update(pluginKeyTimeoutMsg{t: 4})
		if len(e.rt.resolved) != 1 || !strings.HasPrefix(e.rt.resolved[0].Err, "timeout:") {
			t.Fatalf("resolved = %+v", e.rt.resolved)
		}
		e.press(typed("a"))
		if len(e.rt.resolved) != 1 {
			t.Fatal("the key after a timeout resolved something")
		}
	})

	t.Run("the timer is started by the pump with the requested time", func(t *testing.T) {
		e := newKeyEnv(t)
		must(t, e.m.plug.host.WaitKey(6, 90_000))
		if len(e.m.plug.later) == 0 {
			t.Fatal("no timer was queued")
		}
	})
}

// TestPluginKeyOfNamesSpecialKeys: the Key a plugin sees names the key alone in the keymap
// vocabulary. A key without such a name has an empty Key, never the raw code point (S14-5).
func TestPluginKeyOfNamesSpecialKeys(t *testing.T) {
	tests := []struct {
		name string
		key  tea.Key
		want string
	}{
		{"letter", tea.Key{Code: 'a', Text: "a"}, "a"},
		{"upper-case letter folds", tea.Key{Code: 'A', Text: "A", Mod: tea.ModShift}, "a"},
		{"enter", tea.Key{Code: tea.KeyEnter}, "enter"},
		{"f12", tea.Key{Code: tea.KeyF12}, "F12"},
		{"insert", tea.Key{Code: tea.KeyInsert}, "insert"},
		{"keypad enter", tea.Key{Code: tea.KeyKpEnter}, "enter"},
		{"f13 has no chord name", tea.Key{Code: tea.KeyF13}, ""},
		{"caps lock", tea.Key{Code: tea.KeyCapsLock}, ""},
		{"menu", tea.Key{Code: tea.KeyMenu}, ""},
		{"keypad 5", tea.Key{Code: tea.KeyKp5}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pluginKeyOf(tt.key).Key; got != tt.want {
				t.Fatalf("Key = %q, want %q", got, tt.want)
			}
		})
	}
}

// panicSetup is a model with an input mode, a pending key wait and a plugin overlay on top.
func panicSetup(t *testing.T) (*keyEnv, *plugOverlayStub) {
	t.Helper()
	e := newKeyEnv(t)
	e.mode(t, pluginapi.ModeInfo{Name: "m", Text: pluginapi.TextSwallow})
	must(t, e.m.plug.host.WaitKey(7, 0))
	ov := &plugOverlayStub{}
	e.m.pushOverlay(ov)
	return e, ov
}

// panicCheck asserts that the plugin input is gone: mode, wait, overlay.
func panicCheck(t *testing.T, e *keyEnv, ov *plugOverlayStub) {
	t.Helper()
	if e.m.plug.keys.editor != (modeRef{}) {
		t.Error("the input mode is still set")
	}
	if e.m.plug.keys.wait != 0 || len(e.m.plug.waits) != 0 {
		t.Errorf("the key wait is still pending: %v %v", e.m.plug.keys.wait, e.m.plug.waits)
	}
	if len(e.rt.resolved) != 1 || e.rt.resolved[0].Token != 7 || !strings.HasPrefix(e.rt.resolved[0].Err, "cancelled:") {
		t.Errorf("resolved = %+v, want the wait cancelled", e.rt.resolved)
	}
	if !ov.dismissed || len(e.m.overlays) != 0 {
		t.Errorf("the plugin overlay is still open (dismissed %v, %d overlays)", ov.dismissed, len(e.m.overlays))
	}
	if e.m.notice != "plugin input cleared" {
		t.Errorf("notice = %q", e.m.notice)
	}
	if len(e.actions()) != 0 {
		t.Errorf("the key reached the editor: %v", e.actions())
	}
}

// panicWith presses the key a terminal sends as seq. The overlay is on top, so the chord
// must reach the hook before the overlay does.
func panicWith(t *testing.T, seq string) {
	t.Helper()
	e, ov := panicSetup(t)
	e.m.Update(decodeKey(t, seq))
	panicCheck(t, e, ov)
}

// ctrl+^ in the legacy encoding is the byte 0x1e.
func TestPanicChordLegacyEncoding(t *testing.T) { panicWith(t, "\x1e") }

// An enhanced keyboard reports ctrl+^ as ctrl+shift+6, or as the shifted key.
func TestPanicChordEnhancedEncoding(t *testing.T) {
	for _, seq := range []string{"\x1b[54;6u", "\x1b[94;5u"} {
		panicWith(t, seq)
	}
}

// The ctrl+shift+6 spelling of chord 1 is only a panic chord while a plugin holds the
// keyboard. Every preset binds it to set_bookmark 6, which must keep working with no plugin
// input (S14-1).
func TestPanicAliasOnlyWithPluginInput(t *testing.T) {
	t.Run("without plugin input the key reaches the keymap", func(t *testing.T) {
		e := newKeyEnv(t)
		e.m.Update(decodeKey(t, "\x1b[54;6u"))
		if e.m.notice == "plugin input cleared" {
			t.Fatal("ctrl+shift+6 cleared plugin input with none active")
		}
	})
	t.Run("the legacy forms always fire", func(t *testing.T) {
		for _, seq := range []string{"\x1e", "\x1b[94;5u"} {
			e := newKeyEnv(t)
			e.m.Update(decodeKey(t, seq))
			if e.m.notice != "plugin input cleared" {
				t.Errorf("%q did not fire with no plugin input", seq)
			}
		}
	})
}

func TestPanicChordCtrlAltShiftEsc(t *testing.T) { panicWith(t, "\x1b[27;8u") }

func TestPanicChords(t *testing.T) {
	t.Run("with no overlay the chord goes through handleKey", func(t *testing.T) {
		e, ov := panicSetup(t)
		e.m.popOverlay()
		ov.dismissed = false
		e.m.Update(decodeKey(t, "\x1e"))
		if e.m.plug.keys.editor != (modeRef{}) || e.m.plug.keys.wait != 0 {
			t.Fatal("the chord did not clear the input")
		}
	})

	t.Run("plain keys that look alike are not panic chords", func(t *testing.T) {
		for _, k := range []tea.KeyPressMsg{ctrl('6'), typed("^"), {Code: tea.KeyEscape, Mod: tea.ModCtrl}, {Code: tea.KeyEscape, Mod: tea.ModAlt | tea.ModShift}, {Code: '6', Mod: tea.ModShift}} {
			if panicChord(tea.Key(k)) {
				t.Errorf("%+v is a panic chord", k)
			}
		}
	})

	t.Run("the clearing of a mode raises mode_changed", func(t *testing.T) {
		e := newKeyEnv(t)
		e.rt.wants["mode_changed"] = true
		e.mode(t, pluginapi.ModeInfo{Name: "m"})
		e.m.Update(decodeKey(t, "\x1e"))
		var got []pluginapi.Event
		got = append(got, e.rt.emitted...)
		if len(got) != 2 || got[0].Data["mode"] != "m" || got[1].Data["previous"] != "m" || got[1].Data["mode"] != nil {
			t.Fatalf("events = %+v", got)
		}
	})
}

func TestTripleEsc(t *testing.T) {
	t.Run("Esc presses too far apart do not add up", func(t *testing.T) {
		e := newKeyEnv(t)
		e.mode(t, pluginapi.ModeInfo{Name: "m"})
		pressEsc(e, 0)
		pressEsc(e, 400*time.Millisecond)
		pressEsc(e, 400*time.Millisecond)
		if e.m.plug.keys.editor == (modeRef{}) {
			t.Fatal("three Esc over 800 ms cleared the mode")
		}
	})
	t.Run("another key between the presses starts over", func(t *testing.T) {
		e := newKeyEnv(t)
		e.mode(t, pluginapi.ModeInfo{Name: "m"})
		pressEsc(e, 0)
		pressEsc(e, 10*time.Millisecond)
		e.press(typed("a"))
		pressEsc(e, 10*time.Millisecond)
		if e.m.plug.keys.editor == (modeRef{}) {
			t.Fatal("the run of Esc presses was not broken by another key")
		}
	})
	t.Run("a pending key wait counts", func(t *testing.T) {
		e := newKeyEnv(t)
		must(t, e.m.plug.host.WaitKey(3, 0))
		// The wait consumes the first Esc; the third clears it.
		pressEsc(e, 0)
		if len(e.rt.resolved) != 1 {
			t.Fatalf("the first Esc did not reach the wait: %+v", e.rt.resolved)
		}
		// A plugin that loops on next_key waits again after each key.
		must(t, e.m.plug.host.WaitKey(4, 0))
		pressEsc(e, 10*time.Millisecond)
		must(t, e.m.plug.host.WaitKey(5, 0))
		pressEsc(e, 10*time.Millisecond)
		if len(e.rt.resolved) != 3 || !strings.HasPrefix(e.rt.resolved[2].Err, "cancelled:") || e.rt.resolved[2].Token != 5 {
			t.Fatalf("resolved = %+v", e.rt.resolved)
		}
	})
	t.Run("a plugin overlay counts, although the first Esc closes it", func(t *testing.T) {
		e := newKeyEnv(t)
		e.mode(t, pluginapi.ModeInfo{Name: "m"})
		e.m.pushOverlay(&plugOverlayStub{})
		pressEsc(e, 0)
		pressEsc(e, 10*time.Millisecond)
		pressEsc(e, 10*time.Millisecond)
		if e.m.plug.keys.editor != (modeRef{}) {
			t.Fatal("three Esc did not clear the input")
		}
	})
	t.Run("plugins off", func(t *testing.T) {
		m, _ := newPluginModel(t, nil)
		for range 3 {
			m.Update(special(tea.KeyEscape))
		}
		if m.notice == "plugin input cleared" {
			t.Fatal("the panic chord acted with no plugin runtime")
		}
	})
}

func TestEditorModeAndBufferMode(t *testing.T) {
	t.Run("an unknown mode is not found", func(t *testing.T) {
		e := newKeyEnv(t)
		if err := e.m.plug.host.SetInputMode("p", "nope"); pluginapi.CodeOf(err) != pluginapi.CodeNotFound {
			t.Fatalf("SetInputMode = %v", err)
		}
	})
	t.Run("leaving is only for the owner", func(t *testing.T) {
		e := newKeyEnv(t)
		e.mode(t, pluginapi.ModeInfo{Name: "m"})
		must(t, e.m.plug.host.SetInputMode("q", ""))
		if e.m.plug.keys.editor == (modeRef{}) {
			t.Fatal("another plugin ended the mode")
		}
		must(t, e.m.plug.host.SetInputMode("p", ""))
		if e.m.plug.keys.editor != (modeRef{}) {
			t.Fatal("the owner could not end the mode")
		}
	})
	t.Run("redefining the mode set drops an active mode that is gone", func(t *testing.T) {
		e := newKeyEnv(t)
		e.mode(t, pluginapi.ModeInfo{Name: "m"})
		e.m.plug.host.SetModes("p", nil)
		if e.m.plug.keys.editor != (modeRef{}) {
			t.Fatal("the mode survived its definition")
		}
	})
	t.Run("an invalid chord is left out with a notice", func(t *testing.T) {
		e := newKeyEnv(t)
		e.mode(t, pluginapi.ModeInfo{Name: "m", Text: pluginapi.TextSwallow, Bindings: map[string]string{"ctrl+nonsense": "x", "j": "move_down"}})
		if !strings.Contains(e.m.notice, "ctrl+nonsense") {
			t.Fatalf("notice = %q", e.m.notice)
		}
		e.press(typed("j"))
		if !slices.Equal(e.actions(), []editor.Action{"move_down"}) {
			t.Fatalf("the valid binding is lost: %v", e.actions())
		}
	})
	t.Run("mode_changed carries the mode and the one before", func(t *testing.T) {
		e := newKeyEnv(t)
		e.rt.wants["mode_changed"] = true
		e.m.plug.host.SetModes("p", []pluginapi.ModeInfo{{Name: "a", Plugin: "p"}, {Name: "b", Plugin: "p"}})
		must(t, e.m.plug.host.SetInputMode("p", "a"))
		must(t, e.m.plug.host.SetInputMode("p", "b"))
		must(t, e.m.plug.host.SetInputMode("p", "b")) // no change, no event
		must(t, e.m.plug.host.SetInputMode("p", ""))
		e.m.pluginPump()
		var got []string
		for _, ev := range e.rt.emitted {
			if ev.Name != "mode_changed" {
				continue
			}
			got = append(got, fmtAny(ev.Data["previous"])+">"+fmtAny(ev.Data["mode"]))
		}
		if want := []string{"->a", "a>b", "b>-"}; !slices.Equal(got, want) {
			t.Fatalf("events = %q, want %q", got, want)
		}
	})
	t.Run("a buffer mode takes keys while its buffer is active", func(t *testing.T) {
		e := newKeyEnv(t)
		e.m.plug.host.SetModes("p", []pluginapi.ModeInfo{{Name: "bm", Plugin: "p", Text: pluginapi.TextSwallow, Bindings: map[string]string{"j": "move_down"}}})
		e.pe.bufs[0].Mode = "bm"
		e.press(typed("j"))
		e.press(typed("x"))
		if !slices.Equal(e.actions(), []editor.Action{"move_down"}) {
			t.Fatalf("actions = %v", e.actions())
		}
		e.m.Update(decodeKey(t, "\x1e")) // the panic chord ends it for that buffer
		e.press(typed("x"))
		if !slices.Equal(e.actions(), []editor.Action{"move_down", "insert_char"}) {
			t.Fatalf("actions after the panic chord = %v", e.actions())
		}
	})
	t.Run("the editor mode goes before the buffer mode", func(t *testing.T) {
		e := newKeyEnv(t)
		e.m.plug.host.SetModes("p", []pluginapi.ModeInfo{
			{Name: "bm", Plugin: "p", Bindings: map[string]string{"j": "buffer_action"}},
			{Name: "em", Plugin: "p", Bindings: map[string]string{"j": "editor_action"}},
		})
		e.pe.bufs[0].Mode = "bm"
		must(t, e.m.plug.host.SetInputMode("p", "em"))
		e.press(typed("j"))
		if !slices.Equal(e.actions(), []editor.Action{"editor_action"}) {
			t.Fatalf("actions = %v", e.actions())
		}
	})
}

func fmtAny(v any) string {
	if v == nil {
		return "-"
	}
	return v.(string)
}

func TestPluginContexts(t *testing.T) {
	e := newKeyEnv(t)
	h := e.m.plug.host
	if e.m.Holds("busy") {
		t.Fatal("an unset context holds")
	}
	h.SetContext("p", "busy", true)
	if !e.m.Holds("busy") {
		t.Fatal("a set context does not hold")
	}
	if !e.m.whenHolds([]string{"busy", "!idle"}) {
		t.Fatal("when-clause with a plugin context fails")
	}
	h.SetContext("p", "busy", false)
	if e.m.Holds("busy") {
		t.Fatal("a cleared context holds")
	}
	h.SetContext("p", "busy", true)
	h.SetContext("q", "busy", true)
	h.OwnerGone("p", 1)
	if !e.m.Holds("busy") {
		t.Fatal("the context of another plugin went with p")
	}
	h.OwnerGone("q", 1)
	if e.m.Holds("busy") {
		t.Fatal("a context survived its plugin")
	}

	// The active mode is a context.
	e.mode(t, pluginapi.ModeInfo{Name: "normal"})
	if !e.m.Holds(pluginapi.ModeContext("p", "normal")) || e.m.Holds(pluginapi.ModeContext("p", "insert")) || e.m.Holds(pluginapi.ModeContext("q", "normal")) {
		t.Fatal("the mode context does not follow the editor's mode")
	}
	e.m.Update(decodeKey(t, "\x1e"))
	if e.m.Holds(pluginapi.ModeContext("p", "normal")) {
		t.Fatal("the mode context holds after the panic chord")
	}
}

func TestOwnerGoneClearsTheKeyLayer(t *testing.T) {
	e := newKeyEnv(t)
	h := e.m.plug.host
	e.mode(t, pluginapi.ModeInfo{Name: "m", Text: pluginapi.TextSwallow})
	h.SetStatusItems("p", []pluginapi.StatusItem{{ID: "s", Plugin: "p", Text: "x"}})
	h.SetContext("p", "c", true)
	h.SetBindings("p", []pluginapi.Binding{{Chord: "ctrl+j", Action: "p.fn", Plugin: "p"}})
	h.OwnerGone("p", 1)
	ks := e.m.plug.keys
	if ks.editor != (modeRef{}) || len(ks.modes) != 0 || len(ks.bindings) != 0 || len(ks.status.items) != 0 || len(ks.ctx) != 0 || len(ks.morder) != 0 || len(ks.border) != 0 {
		t.Fatalf("state left behind: %+v", ks)
	}
	e.press(typed("a"))
	if !slices.Equal(e.actions(), []editor.Action{"insert_char"}) {
		t.Fatalf("the swallowing mode outlived its plugin: %v", e.actions())
	}
}

// ---- Bindings in the keymap.

// fakePluginKeys records the calls of RootPorts.PluginKeys and returns a keymap that binds
// the plugin chords (single keys named as the tests press them) over testKeys.
type fakePluginKeys struct {
	calls   [][]pluginapi.Binding
	presets []string
	err     error
}

func (f *fakePluginKeys) build(preset string, bs []pluginapi.Binding) (Keymap, error) {
	f.presets = append(f.presets, preset)
	f.calls = append(f.calls, slices.Clone(bs))
	km := testKeys()
	if preset != "" {
		km["normal|marker|"] = Command{Action: "preset:" + preset}
	}
	for _, b := range bs {
		km["normal|"+b.Chord+"|"] = Command{Action: b.Action, Args: b.Args}
	}
	return km, f.err
}

func TestPluginBindingsRebuildTheKeymapOnce(t *testing.T) {
	fk := &fakePluginKeys{}
	e := newKeyEnv(t, func(r *RootPorts) { r.PluginKeys = fk.build })
	h := e.m.plug.host
	for _, c := range []string{"a", "b", "c"} {
		h.SetBindings("p", []pluginapi.Binding{{Chord: c, Action: "p.fn", Plugin: "p"}})
	}
	h.SetBindings("q", []pluginapi.Binding{{Chord: "d", Action: "q.fn", Plugin: "q"}})
	if len(fk.calls) != 0 {
		t.Fatalf("the keymap was rebuilt %d times inside the slice", len(fk.calls))
	}
	e.m.pluginPump()
	if len(fk.calls) != 1 {
		t.Fatalf("PluginKeys called %d times, want 1", len(fk.calls))
	}
	var chords []string
	for _, b := range fk.calls[0] {
		chords = append(chords, b.Chord)
	}
	if !slices.Equal(chords, []string{"c", "d"}) {
		t.Fatalf("bindings = %v", chords)
	}
	if cmd, ok := e.m.keys.Resolve(KeyPress{Name: "c"}, KeyContextNormal); !ok || cmd.Action != "p.fn" {
		t.Fatalf("the new keymap is not in use: %+v %v", cmd, ok)
	}

	// Removing the last bindings restores the preset layer once.
	h.OwnerGone("p", 1)
	h.OwnerGone("q", 1)
	e.m.pluginPump()
	if len(fk.calls) != 2 || len(fk.calls[1]) != 0 {
		t.Fatalf("calls = %d, last %v; want one more call with no bindings", len(fk.calls), fk.calls[len(fk.calls)-1])
	}
	h.SetBindings("p", nil)
	e.m.pluginPump()
	if len(fk.calls) != 2 {
		t.Fatalf("an empty set with nothing applied rebuilt the keymap (%d calls)", len(fk.calls))
	}
}

func TestPluginBindingsFollowLoadOrder(t *testing.T) {
	fk := &fakePluginKeys{}
	e := newKeyEnv(t, func(r *RootPorts) { r.PluginKeys = fk.build })
	h := e.m.plug.host
	// b registers first, but a loads first.
	h.SetBindings("b", []pluginapi.Binding{{Chord: "x", Action: "b.fn", Plugin: "b"}})
	h.SetBindings("a", []pluginapi.Binding{{Chord: "x", Action: "a.fn", Plugin: "a"}})
	e.rt.plugins = []pluginapi.PluginInfo{{Name: "a"}, {Name: "b"}}
	e.m.pluginPump()
	if len(fk.calls) != 1 || fk.calls[0][0].Plugin != "a" || fk.calls[0][1].Plugin != "b" {
		t.Fatalf("calls = %+v, want a's bindings before b's", fk.calls)
	}
}

func TestPluginBindingSequencesAndErrorsGetNotices(t *testing.T) {
	fk := &fakePluginKeys{err: errTest("plugin key bindings ignored: plugin 'b': key x already bound by plugin 'a'")}
	e := newKeyEnv(t, func(r *RootPorts) { r.PluginKeys = fk.build })
	h := e.m.plug.host
	h.SetBindings("p", []pluginapi.Binding{{Chord: "g g", Action: "p.fn", Plugin: "p"}, {Chord: "a", Action: "p.fn", Plugin: "p"}})
	e.m.pluginPump()
	if len(fk.calls[0]) != 1 || fk.calls[0][0].Chord != "a" {
		t.Fatalf("the sequence reached the keymap: %+v", fk.calls[0])
	}
	all := strings.Join(e.m.notes.items, "\n")
	if !strings.Contains(all, "key sequence \"g g\" works in a mode only") || !strings.Contains(all, "already bound by plugin 'a'") {
		t.Fatalf("warnings = %q", all)
	}
	if _, ok := e.m.keys.Resolve(KeyPress{Name: "a"}, KeyContextNormal); !ok {
		t.Fatal("a keymap that came with an error was not used")
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }

func TestPluginBindingWhenGate(t *testing.T) {
	fk := &fakePluginKeys{}
	e := newKeyEnv(t, func(r *RootPorts) { r.PluginKeys = fk.build })
	h := e.m.plug.host
	h.SetCommands("p", []pluginapi.CommandInfo{{ID: "p.fn", Title: "Do It", Plugin: "p"}})
	h.SetBindings("p", []pluginapi.Binding{
		{Chord: "a", Action: "p.fn", Plugin: "p", When: []string{"read_only"}},
		{Chord: "b", Action: "p.fn", Plugin: "p", When: []string{"!read_only"}},
	})
	e.m.pluginPump()
	e.press(KeyMsgFor("a"))
	if len(e.rt.runs) != 0 || e.m.notice != "Do It is not available now" {
		t.Fatalf("runs %v notice %q: a key whose when fails must do nothing and say so", e.rt.runs, e.m.notice)
	}
	e.press(KeyMsgFor("b"))
	if !slices.Equal(e.rt.runs, []string{"p.fn"}) {
		t.Fatalf("runs = %v", e.rt.runs)
	}
}

// KeyMsgFor is a key press of the single-rune key s with no text, so the test keymap's
// binding for it is the only thing that can take it.
func KeyMsgFor(s string) tea.KeyPressMsg { return tea.KeyPressMsg{Code: rune(s[0])} }

func TestSwitchKeymapKeepsPluginBindings(t *testing.T) {
	fk := &fakePluginKeys{}
	var switched []string
	e := newKeyEnv(t, func(r *RootPorts) { r.PluginKeys = fk.build })
	e.m.util.switchKeymap = func(name string) (Keymap, error) {
		switched = append(switched, name)
		return testKeys(), nil
	}
	h := e.m.plug.host
	h.SetBindings("p", []pluginapi.Binding{{Chord: "a", Action: "p.fn", Plugin: "p"}})
	e.m.pluginPump()

	e.m.switchKeymap("emacs")
	if !slices.Equal(switched, []string{"emacs"}) {
		t.Fatalf("SwitchKeymap calls = %v", switched)
	}
	if got := fk.presets[len(fk.presets)-1]; got != "emacs" {
		t.Fatalf("PluginKeys got preset %q, want emacs", got)
	}
	if cmd, ok := e.m.keys.Resolve(KeyPress{Name: "a"}, KeyContextNormal); !ok || cmd.Action != "p.fn" {
		t.Fatalf("the plugin binding is lost by the switch: %+v %v", cmd, ok)
	}
	if cmd, ok := e.m.keys.Resolve(KeyPress{Name: "marker"}, KeyContextNormal); !ok || cmd.Action != "preset:emacs" {
		t.Fatalf("the new preset is not under the plugin bindings: %+v %v", cmd, ok)
	}

	// A later change of the bindings keeps the preset the user chose.
	h.SetBindings("p", []pluginapi.Binding{{Chord: "c", Action: "p.fn", Plugin: "p"}})
	e.m.pluginPump()
	if got := fk.presets[len(fk.presets)-1]; got != "emacs" {
		t.Fatalf("rebuild used preset %q after the switch, want emacs", got)
	}

	// With no plugin bindings a switch is the plain switch.
	e2 := newKeyEnv(t, func(r *RootPorts) { r.PluginKeys = fk.build })
	n := len(fk.calls)
	e2.m.util.switchKeymap = func(string) (Keymap, error) { return testKeys(), nil }
	e2.m.switchKeymap("vscode")
	if len(fk.calls) != n {
		t.Fatal("a switch with no plugin bindings built a plugin keymap")
	}
}

// ---- Idle.

func TestIdleLadder(t *testing.T) {
	var seq []int
	for ms := 0; ms < idleMax; ms = idleNext(ms) {
		seq = append(seq, idleNext(ms))
	}
	for i := 1; i < len(seq); i++ {
		if seq[i] <= seq[i-1] {
			t.Fatalf("ladder not growing at %d: %v", i, seq[i-1:i+1])
		}
	}
	for _, want := range []int{100, 1000, 2000, 5000, 10_000, 60_000, 300_000, 3_600_000} {
		if !slices.Contains(seq, want) {
			t.Errorf("the ladder skips %d ms, a common after_ms", want)
		}
	}
	if len(seq) > 500 {
		t.Errorf("%d idle events per hour of idling", len(seq))
	}
}

func TestIdleEvents(t *testing.T) {
	// The pump hands the events to the runtime at the end of each Update.
	idleEvents := func(e *keyEnv) []int {
		var out []int
		for _, ev := range e.rt.emitted {
			if ev.Name == "idle" {
				out = append(out, ev.Data["ms"].(int))
			}
		}
		return out
	}
	tick := func(e *keyEnv, after time.Duration) {
		e.now = e.now.Add(after)
		e.m.Update(pluginIdleMsg{gen: e.m.plug.keys.idle.gen})
	}

	t.Run("nothing is armed without a subscriber", func(t *testing.T) {
		e := newKeyEnv(t)
		e.press(typed("a"))
		if e.m.plug.keys.idle.armed {
			t.Fatal("a timer runs with no idle subscriber")
		}
	})

	t.Run("ms grows within a period and starts over after input", func(t *testing.T) {
		e := newKeyEnv(t)
		e.rt.wants["idle"] = true
		e.press(typed("a")) // arms the timer
		if !e.m.plug.keys.idle.armed {
			t.Fatal("the timer was not armed")
		}
		capture := func() []int { return idleEvents(e) }
		tick(e, 100*time.Millisecond)
		tick(e, 100*time.Millisecond)
		tick(e, 100*time.Millisecond)
		first := capture()
		if !slices.Equal(first, []int{100, 200, 300}) {
			t.Fatalf("idle ms = %v, want 100 200 300", first)
		}
		// An input ends the period: the next idle event is small again.
		e.press(typed("b"))
		tick(e, 100*time.Millisecond)
		all := capture()
		if len(all) != 4 || all[3] != 100 {
			t.Fatalf("idle ms = %v, want the period to start over at 100", all)
		}
	})

	t.Run("every period carries its own number, whatever the timer jitter", func(t *testing.T) {
		e := newKeyEnv(t)
		e.rt.wants["idle"] = true
		periods := func() []int {
			var out []int
			for _, ev := range e.rt.emitted {
				if ev.Name == "idle" {
					out = append(out, ev.Data["period"].(int))
				}
			}
			return out
		}
		e.press(typed("a"))
		tick(e, 105*time.Millisecond)
		e.press(typed("b"))
		tick(e, 107*time.Millisecond) // later than the last ms of the period before
		tick(e, 100*time.Millisecond)
		got := periods()
		if len(got) != 3 || got[0] == got[1] || got[1] != got[2] {
			t.Fatalf("periods = %v, want one number per period", got)
		}
	})

	t.Run("a timer that fires early re-arms and a late one reports the real time", func(t *testing.T) {
		e := newKeyEnv(t)
		e.rt.wants["idle"] = true
		e.press(typed("a"))
		tick(e, 30*time.Millisecond)
		if got := idleEvents(e); len(got) != 0 {
			t.Fatalf("an early timer raised %v", got)
		}
		tick(e, 4*time.Second)
		if got := idleEvents(e); !slices.Equal(got, []int{4030}) {
			t.Fatalf("idle ms = %v, want the real time 4030", got)
		}
	})

	t.Run("a stale timer is ignored and unsubscribing stops the timer", func(t *testing.T) {
		e := newKeyEnv(t)
		e.rt.wants["idle"] = true
		e.press(typed("a"))
		gen := e.m.plug.keys.idle.gen
		e.now = e.now.Add(time.Second)
		e.m.Update(pluginIdleMsg{gen: gen - 1})
		if len(idleEvents(e)) != 0 {
			t.Fatal("a stale timer raised idle")
		}
		e.rt.wants["idle"] = false
		e.m.Update(pluginIdleMsg{gen: gen})
		if len(idleEvents(e)) != 0 || e.m.plug.keys.idle.armed {
			t.Fatal("the timer went on with no subscriber")
		}
	})

	t.Run("an input moves a far-off timer up", func(t *testing.T) {
		e := newKeyEnv(t)
		e.rt.wants["idle"] = true
		e.press(typed("a"))
		e.m.plug.keys.idle.due = e.now.Add(time.Hour)
		gen := e.m.plug.keys.idle.gen
		e.press(typed("b"))
		if e.m.plug.keys.idle.gen == gen {
			t.Fatal("the timer in flight, an hour off, was kept")
		}
	})
}

// ---- Status items.

// A left item of a plugin never pushes the status message out: the message keeps room for
// its first cells at every width, so notices (the panic chord's among them) stay visible
// (S14-4).
func TestStatusMessageSurvivesWideLeftItem(t *testing.T) {
	const msg = "plugin input cleared"
	wide := statusItem{id: "a", plugin: "p", text: strings.Repeat("x", 80)}
	for _, w := range []int{80, 120, 200} {
		sv := statusView{position: "1:1", message: msg, eol: "LF", encoding: "UTF-8", language: "Go", lines: 10, extra: []statusItem{wide}}
		var text strings.Builder
		for _, r := range statusRuns(w, sv) {
			text.WriteString(r.text)
		}
		if !strings.Contains(text.String(), "plugin") {
			t.Errorf("w=%d: the row %q lost the message", w, text.String())
		}
	}
	t.Run("a short item keeps its place before the message", func(t *testing.T) {
		sv := statusView{position: "1:1", message: msg, eol: "LF", encoding: "UTF-8", language: "Go", lines: 10,
			extra: []statusItem{{id: "a", plugin: "p", text: "mode"}}}
		var text strings.Builder
		for _, r := range statusRuns(120, sv) {
			text.WriteString(r.text)
		}
		if got := text.String(); !strings.Contains(got, " mode  "+msg) {
			t.Fatalf("row %q", got)
		}
	})
}

func TestStatusItemsLayoutAndHit(t *testing.T) {
	sv := func(items ...statusItem) statusView {
		return statusView{position: "Ln 1, Col 1", message: "msg", eol: "LF", encoding: "UTF-8", language: "Go", lines: 10, extra: items}
	}
	find := func(l layout, text string) (run, bool) {
		for _, r := range l.status {
			if strings.Contains(r.text, text) {
				return r, true
			}
		}
		return run{}, false
	}
	items := []statusItem{
		{id: "mode", plugin: "p", text: "-- NORMAL --"},
		{id: "clock", plugin: "p", text: "12:00", right: true, clickable: true},
		{id: "note", plugin: "p", text: "note", clickable: true},
	}
	l := computeLayout(100, 24, layoutInput{status: sv(items...)})
	for i, text := range []string{"-- NORMAL --", "12:00", "note"} {
		r, ok := find(l, text)
		if !ok {
			t.Fatalf("run %q missing from %+v", text, l.status)
		}
		x := r.x + 1
		got := l.hit(x, l.statusY)
		if (i == 0) != (got.Kind == hitNone) || (i > 0 && (got.Kind != hitStatusPlugin || got.Index != i)) {
			t.Errorf("hit on %q = %+v", text, got)
		}
	}
	// Left items sit between the position and the message; right items before the palette hint.
	pos, _ := find(l, "Ln 1")
	mode, _ := find(l, "-- NORMAL --")
	msg, _ := find(l, "msg")
	clock, _ := find(l, "12:00")
	lang, _ := find(l, "Go")
	hint, _ := find(l, paletteHelp)
	if pos.x >= mode.x || mode.x >= msg.x || lang.x >= clock.x || clock.x >= hint.x {
		t.Errorf("order wrong: pos %d mode %d msg %d | lang %d clock %d hint %d", pos.x, mode.x, msg.x, lang.x, clock.x, hint.x)
	}
	// The built-in segments still hit as before.
	if got := l.hit(lang.x, l.statusY); got.Kind != hitStatusSegment {
		t.Errorf("language hit = %+v", got)
	}
	if got := l.hit(hint.x, l.statusY); got.Kind != hitStatusPalette {
		t.Errorf("palette hit = %+v", got)
	}

	// A narrow row drops the right group from its end, the plugin items after the palette hint
	// and before the language.
	narrow := computeLayout(60, 24, layoutInput{status: sv(items...)})
	if _, ok := find(narrow, paletteHelp); ok {
		if _, ok := find(narrow, "12:00"); !ok {
			t.Error("the plugin item was dropped before the palette hint")
		}
	}
	tiny := computeLayout(30, 24, layoutInput{status: sv(items...)})
	if _, ok := find(tiny, "12:00"); ok {
		t.Error("the right item survived a row too narrow for it")
	}
	// Runs never overlap and stay in the row.
	for _, lay := range []layout{l, narrow, tiny} {
		end := 0
		for _, r := range lay.status {
			if r.x < end || r.x+r.w > lay.w {
				t.Errorf("run %q at %d+%d overlaps or leaves the row (width %d)", r.text, r.x, r.w, lay.w)
			}
			end = r.x + r.w
		}
	}
}

func TestStatusItemsOfPlugins(t *testing.T) {
	e := newKeyEnv(t)
	h := e.m.plug.host
	h.SetStatusItems("p", []pluginapi.StatusItem{
		{ID: "low", Plugin: "p", Text: "low", Priority: 1},
		{ID: "high", Plugin: "p", Text: "hi\ngh" + strings.Repeat("x", 200), Priority: 9, Right: true, Clickable: true},
	})
	h.SetStatusItems("q", []pluginapi.StatusItem{{ID: "q1", Plugin: "q", Text: "q", Priority: 1}})
	items := e.m.plugStatusItems()
	var ids []string
	for _, it := range items {
		ids = append(ids, it.plugin+"/"+it.id)
	}
	if want := []string{"p/high", "p/low", "q/q1"}; !slices.Equal(ids, want) {
		t.Fatalf("items = %v, want %v (priority, then registration order)", ids, want)
	}
	if strings.Contains(items[0].text, "\n") || len([]rune(items[0].text)) > maxStatusItemCells {
		t.Fatalf("text = %q (%d runes)", items[0].text, len([]rune(items[0].text)))
	}

	// The active mode's label comes first.
	e.mode(t, pluginapi.ModeInfo{Name: "normal", Status: "-- NORMAL --"})
	items = e.m.plugStatusItems()
	if items[0].text != "-- NORMAL --" || items[0].id != "mode:normal" {
		t.Fatalf("first item = %+v", items[0])
	}

	// The status row of the frame shows them.
	if !frameHas(e.m, "-- NORMAL --") || !frameHas(e.m, "low") {
		t.Fatal("the status row does not show the plugin items")
	}

	// A click on a clickable item runs its hidden command.
	h.SetStatusItems("p", []pluginapi.StatusItem{{ID: "high", Plugin: "p", Text: "go", Right: true, Clickable: true}})
	l := e.m.layout()
	var x int
	for _, r := range l.status {
		if r.kind == hitStatusPlugin {
			x = r.x
		}
	}
	e.m.Update(click(x, l.statusY, tea.MouseLeft))
	if !slices.Equal(e.rt.runs, []string{"p.__status_high"}) {
		t.Fatalf("runs = %v, want the click command of the item", e.rt.runs)
	}

	// An empty set and OwnerGone remove them.
	h.SetStatusItems("q", nil)
	h.OwnerGone("p", 1)
	e.m.plug.keys.editor = modeRef{}
	if got := e.m.plugStatusItems(); len(got) != 0 {
		t.Fatalf("items left: %+v", got)
	}
}

func TestPluginKeyLayerIsInertWithoutRuntime(t *testing.T) {
	m, ed := newPluginModel(t, nil)
	m.Update(decodeKey(t, "\x1e"))
	m.Update(typed("a"))
	m.Update(tea.PasteMsg{Content: "x"})
	if got := ed.actions; len(got) != 2 || got[0] != "insert_char" || got[1] != "paste" {
		t.Fatalf("actions = %v", got)
	}
	if m.plugStatusItems() != nil {
		t.Fatal("status items without a runtime")
	}
}

func TestPluginModeIgnoredWhileExplorerFocused(t *testing.T) {
	e := newKeyEnv(t)
	e.mode(t, swallowRight)
	e.m.explorerOpen, e.m.explorerFocus = true, true
	if cmd, taken := e.m.pluginKey(tea.Key(typed("a"))); taken || cmd != nil {
		t.Fatal("the mode took a key while the explorer had the focus")
	}
	e.m.handleKey(tea.Key(special(tea.KeyRight)))
	if slices.Contains(e.actions(), "move_down") {
		t.Fatal("a mode binding ran while the explorer had the focus")
	}
	e.m.explorerFocus = false
	e.m.handleKey(tea.Key(special(tea.KeyRight)))
	if !slices.Contains(e.actions(), "move_down") {
		t.Fatalf("the mode binding did not run with the editor focused: %v", e.actions())
	}
}

func TestF6CyclesFocusInSwallowMode(t *testing.T) {
	e := newKeyEnv(t)
	e.mode(t, swallowRight)
	before := e.m.stage()
	e.press(special(tea.KeyF6))
	if e.m.stage() == before {
		t.Fatalf("F6 did not move the focus out of %v", before)
	}
}

func TestTabFocusKeysNotSwallowed(t *testing.T) {
	e := newKeyEnv(t)
	e.mode(t, swallowRight)
	e.m.tabFocus = true
	if _, taken := e.m.pluginKey(tea.Key(special(tea.KeyRight))); taken {
		t.Fatal("the mode took a key while the tab strip had the focus")
	}
	e.press(special(tea.KeyRight))
	if slices.Contains(e.actions(), "move_down") {
		t.Fatal("a mode binding ran while the tab strip had the focus")
	}
}

func TestDockKeysNotSwallowed(t *testing.T) {
	e := newKeyEnv(t)
	dockSettle(t, e.m, e.m.runCommand("open_terminal_in_dock"))
	e.mode(t, swallowRight)
	dockSettle(t, e.m, e.press(typed("a")))
	if !slices.Equal(e.starter.term.writes, []string{"a"}) {
		t.Fatalf("shell writes = %q, want the swallowed key to reach the shell", e.starter.term.writes)
	}
}

func TestPasteSwallowedInSwallowMode(t *testing.T) {
	e := newKeyEnv(t)
	e.mode(t, pluginapi.ModeInfo{Name: "m", Paste: pluginapi.TextSwallow})
	e.m.Update(tea.PasteMsg{Content: "pasted"})
	if len(e.actions()) != 0 {
		t.Fatalf("actions = %v, want the paste dropped", e.actions())
	}
}

func TestPasteHandlerGetsText(t *testing.T) {
	e := newKeyEnv(t)
	e.mode(t, pluginapi.ModeInfo{Name: "m", Text: pluginapi.TextHandler, Paste: pluginapi.TextHandler, TextAction: "p.text"})
	e.m.Update(tea.PasteMsg{Content: "pasted"})
	if !slices.Equal(e.rt.runs, []string{"p.text"}) {
		t.Fatalf("runs = %v", e.rt.runs)
	}
	if a := e.rt.runArgs[0]; a["text"] != "pasted" || a["paste"] != true {
		t.Fatalf("args = %v, want text and paste=true", a)
	}
	if len(e.actions()) != 0 {
		t.Fatalf("actions = %v", e.actions())
	}
}

func TestTripleEscClearsMode(t *testing.T) {
	e := newKeyEnv(t)
	e.mode(t, pluginapi.ModeInfo{Name: "m"})
	pressEsc(e, 0)
	pressEsc(e, 200*time.Millisecond)
	if e.m.plug.keys.editor == (modeRef{}) {
		t.Fatal("two Esc cleared the mode")
	}
	pressEsc(e, 200*time.Millisecond)
	if e.m.plug.keys.editor != (modeRef{}) || e.m.notice != "plugin input cleared" {
		t.Fatalf("mode %v, notice %q after three Esc", e.m.plug.keys.editor, e.m.notice)
	}
}

func TestTripleEscIgnoredWithoutPluginState(t *testing.T) {
	e := newKeyEnv(t)
	pressEsc(e, 0)
	pressEsc(e, 10*time.Millisecond)
	pressEsc(e, 10*time.Millisecond)
	if e.m.notice == "plugin input cleared" {
		t.Fatal("three Esc cleared plugin input that is not there")
	}
}

// swallowRight is a mode that drops text and every key it does not bind, and binds Right.
var swallowRight = pluginapi.ModeInfo{Name: "m", Text: pluginapi.TextSwallow, Fallthrough: false, Bindings: map[string]string{"right": "move_down"}}

// pressEsc advances the clock by gap and presses Esc.
func pressEsc(e *keyEnv, gap time.Duration) {
	e.now = e.now.Add(gap)
	e.m.Update(special(tea.KeyEscape))
}

// sgrRe matches the colour sequences the renderer writes itself.
var sgrRe = regexp.MustCompile("\x1b\\[[0-9;:]*m")

// TestPluginStatusTextHasNoControlCharacters checks that text a plugin put on the status
// row (a notice, a status item, a mode label) reaches the frame without an escape sequence:
// an unterminated OSC or DCS string makes the renderer drop the rest of the frame.
func TestPluginStatusTextHasNoControlCharacters(t *testing.T) {
	t.Parallel()
	const evil = "ab\x1b]0;pwn\x07cd\x1bPxy\u009d\u009bz"
	tests := []struct {
		name string
		set  func(e *keyEnv)
	}{
		{"info notice", func(e *keyEnv) { e.m.plug.host.Notify("p", pluginapi.LevelInfo, evil) }},
		{"warning", func(e *keyEnv) { e.m.plug.host.Notify("p", pluginapi.LevelWarn, evil) }},
		{"status item", func(e *keyEnv) {
			e.m.plug.host.SetStatusItems("p", []pluginapi.StatusItem{{ID: "s", Plugin: "p", Text: evil}})
		}},
		{"mode label", func(e *keyEnv) { e.mode(t, pluginapi.ModeInfo{Name: "m", Status: evil}) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newKeyEnv(t)
			tt.set(e)
			frame := sgrRe.ReplaceAllString(e.m.View().Content, "")
			if i := strings.IndexFunc(frame, func(r rune) bool { return r != '\n' && unicode.IsControl(r) }); i >= 0 {
				t.Fatalf("the frame holds a control character at byte %d: %q", i, frame[max(i-10, 0):min(i+20, len(frame))])
			}
			if !frameHas(e.m, "ab") {
				t.Fatal("the text is not on the status row at all")
			}
		})
	}
}
