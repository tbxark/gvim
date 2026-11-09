package tui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/ctxmenu"
	"github.com/tbxark/gvim/internal/tui/list"
	"github.com/tbxark/gvim/internal/tui/prompt"
	"github.com/tbxark/gvim/theme"
)

// overlayModel is a model over one document with the bindings of an overlay context.
func overlayModel(t *testing.T, bindings fakeKeymap) *Model {
	t.Helper()
	m := New(Deps{
		Editor: newFakeEditor("hello"), Explorer: newFakeTree(1), Keys: bindings,
		Theme: theme.Theme{}, Clock: testClock,
	})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return m
}

// pushPrompt pushes a one-line prompt and returns it.
func pushPrompt(m *Model, input string) *prompt.Line {
	p := prompt.NewLine(m.styles, prompt.LineOptions{Label: "Value:", Input: input, Tag: "test"})
	m.pushOverlay(p)
	return p
}

func TestOverlayContextsMapToTheirFreshNames(t *testing.T) {
	t.Parallel()
	m := overlayModel(t, fakeKeymap{})
	p := pushPrompt(m, "")
	if ctx, ok := overlayContextOf(p); !ok || ctx != KeyContextPrompt {
		t.Fatalf("prompt context = %q, %v", ctx, ok)
	}
	c := prompt.NewConfirm(m.styles, "Sure?", "tag")
	if ctx, ok := overlayContextOf(c); !ok || ctx != KeyContextPrompt {
		t.Fatalf("confirm context = %q, %v", ctx, ok)
	}
	v := list.New("Pick", []list.Row{{Left: "a", Value: "a"}}, m.styles.Overlay, nil)
	if ctx, ok := overlayContextOf(v); !ok || ctx != KeyContextPopup {
		t.Fatalf("list context = %q, %v", ctx, ok)
	}
	menu := ctxmenu.New(m.styles.Overlay)
	if ctx, ok := overlayContextOf(menu); !ok || ctx != KeyContextMenu {
		t.Fatalf("context menu context = %q, %v", ctx, ok)
	}
	m.openFind(false)
	if ctx, ok := overlayContextOf(m.overlays[len(m.overlays)-1]); !ok || ctx != KeyContextSearchPrompt {
		t.Fatalf("find bar context = %q, %v", ctx, ok)
	}
}

func TestPromptContextBindings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		bindings fakeKeymap
		open     func(m *Model) *prompt.Line
		key      tea.KeyPressMsg
		check    func(t *testing.T, m *Model, p *prompt.Line)
	}{
		{
			name:     "a bound cancel chord closes the prompt",
			bindings: fakeKeymap{"prompt|t|ctrl": {Action: "prompt_cancel"}},
			open:     func(m *Model) *prompt.Line { return pushPrompt(m, "x") },
			key:      ctrl('t'),
			check: func(t *testing.T, m *Model, _ *prompt.Line) {
				t.Helper()
				if len(m.overlays) != 0 {
					t.Fatal("the bound cancel chord left the prompt open")
				}
			},
		},
		{
			name:     "the default cancel chord still works after a rebind",
			bindings: fakeKeymap{"prompt|t|ctrl": {Action: "prompt_cancel"}},
			open:     func(m *Model) *prompt.Line { return pushPrompt(m, "x") },
			key:      special(tea.KeyEscape),
			check: func(t *testing.T, m *Model, _ *prompt.Line) {
				t.Helper()
				if len(m.overlays) != 0 {
					t.Fatal("Esc left the prompt open")
				}
			},
		},
		{
			name:     "a bound edit action edits the field",
			bindings: fakeKeymap{"prompt|u|ctrl": {Action: "prompt_delete_word_backward"}},
			open:     func(m *Model) *prompt.Line { return pushPrompt(m, "one two") },
			key:      ctrl('u'),
			check: func(t *testing.T, _ *Model, p *prompt.Line) {
				t.Helper()
				if got := p.Value(); got != "one " {
					t.Fatalf("value after the bound delete-word = %q, want %q", got, "one ")
				}
			},
		},
		{
			name:     "an unbound key falls through to the field",
			bindings: fakeKeymap{"prompt|t|ctrl": {Action: "prompt_cancel"}},
			open:     func(m *Model) *prompt.Line { return pushPrompt(m, "one two") },
			key:      ctrl('u'),
			check: func(t *testing.T, _ *Model, p *prompt.Line) {
				t.Helper()
				if got := p.Value(); got != "" {
					t.Fatalf("value after the unbound Ctrl+U = %q, want the field's own delete", got)
				}
			},
		},
		{
			name:     "the default confirm chord still submits",
			bindings: fakeKeymap{"prompt|t|ctrl": {Action: "prompt_paste"}},
			open:     func(m *Model) *prompt.Line { return pushPrompt(m, "42") },
			key:      special(tea.KeyEnter),
			check: func(t *testing.T, m *Model, _ *prompt.Line) {
				t.Helper()
				if len(m.overlays) != 0 {
					t.Fatal("Enter left the prompt open")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := overlayModel(t, tt.bindings)
			p := tt.open(m)
			_, cmd := m.Update(tt.key)
			runCmds(m, cmd)
			tt.check(t, m, p)
		})
	}
}

func TestPopupContextBindings(t *testing.T) {
	t.Parallel()
	t.Run("a bound confirm chord chooses the highlighted row", func(t *testing.T) {
		m := overlayModel(t, fakeKeymap{"popup|s|ctrl": {Action: "popup_confirm"}})
		chosen := ""
		v := list.New("Pick", []list.Row{{Left: "a", Value: "a"}, {Left: "b", Value: "b"}}, m.styles.Overlay, func(_ *list.View, r list.Row) tea.Cmd {
			chosen = r.Value
			return nil
		})
		m.pushOverlay(v)
		_, cmd := m.Update(ctrl('s'))
		runCmds(m, cmd)
		if chosen != "a" {
			t.Fatalf("chosen = %q, want the highlighted row", chosen)
		}
	})
	t.Run("a bound cancel chord closes the list", func(t *testing.T) {
		m := overlayModel(t, fakeKeymap{"popup|q|ctrl": {Action: "popup_cancel"}})
		m.pushOverlay(list.New("Pick", []list.Row{{Left: "a"}}, m.styles.Overlay, nil))
		m.Update(ctrl('q'))
		if len(m.overlays) != 0 {
			t.Fatal("the bound cancel chord left the list open")
		}
	})
	t.Run("an unbound key falls through to the list", func(t *testing.T) {
		m := overlayModel(t, fakeKeymap{"popup|s|ctrl": {Action: "popup_confirm"}})
		m.pushOverlay(list.New("Pick", []list.Row{{Left: "a"}}, m.styles.Overlay, nil))
		if _, cmd := m.Update(ctrl('x')); cmd != nil || len(m.overlays) != 1 {
			t.Fatalf("the unbound key = %v, overlays %d", cmd, len(m.overlays))
		}
	})
}

func TestMenuContextBindings(t *testing.T) {
	t.Parallel()
	m := menuModel(t, false)
	keys := menuKeys()
	keys["menu|j|"] = Command{Action: "menu_down"}
	keys["menu|q|ctrl"] = Command{Action: "menu_close"}
	m.keys = keys
	runAction(t, m, "menu_activate")
	if !m.menu.IsOpen() {
		t.Fatal("the menu did not open")
	}
	before := m.menu.Selected()
	m.Update(typed("j"))
	if got := m.menu.Selected(); got == before {
		t.Fatalf("the bound menu_down chord left the highlight at %d", got)
	}
	m.Update(ctrl('q'))
	if m.menu.IsOpen() {
		t.Fatal("the bound menu_close chord left the menu open")
	}
}

func TestSettingsContextBindingClosesThePage(t *testing.T) {
	t.Parallel()
	m := overlayModel(t, fakeKeymap{"settings|t|ctrl": {Action: "close_settings"}})
	page := &settingsPage{m: m, active: true}
	m.pushOverlay(page)
	m.Update(ctrl('t'))
	if len(m.overlays) != 0 {
		t.Fatal("close_settings left the settings page open")
	}
}

func TestDockContextBindingRunsTheActionNotTheShell(t *testing.T) {
	t.Parallel()
	starter := newFakeStarter()
	m := newDockModel(t, newFakeEditor("hello"), DockPorts{Starter: starter})
	openDock(t, m)
	keys := dockKeys()
	keys["dock|n|alt"] = Command{Action: "focus_editor"}
	m.keys = keys
	// With capture off the dock's own context runs first: the key must not reach the shell.
	m.Update(altKey('n'))
	if got := starter.term.writes; len(got) != 0 {
		t.Fatalf("the dock context binding wrote to the shell: %q", got)
	}
}

func TestSearchPromptContextBindingStepsTheMatch(t *testing.T) {
	t.Parallel()
	m, sf := newDefaultSearchModel(t, "foo bar foo")
	keys := testKeys()
	keys["normal|f|ctrl"] = Command{Action: "search"}
	keys["searchPrompt|z|ctrl"] = Command{Action: "find_next"}
	m.keys = keys
	// Open the bar, search, and submit: the caret lands on the first match.
	press(m, ctrl('f'))
	for _, r := range "foo" {
		press(m, typed(string(r)))
	}
	press(m, special(tea.KeyEnter))
	// Reopen the bar and press the bound step chord: it runs find_next, not the bar's own key.
	press(m, ctrl('f'))
	press(m, ctrl('z'))
	if sf.anchor.Col != 8 {
		t.Fatalf("the bound find_next chord left the anchor at %+v, want the second match", sf.anchor)
	}
}

// fakeUnbindKeymap is a fakeKeymap that also answers the optional UnbindKeymap port: keys
// in unbound are explicitly unbound in their context.
type fakeUnbindKeymap struct {
	fakeKeymap
	unbound map[string]bool
}

func (k fakeUnbindKeymap) Unbound(key KeyPress, ctx KeyContext) bool {
	return k.unbound[string(ctx)+"|"+key.Name+"|"+strings.Join(key.Mods, "+")]
}

// TestOverlayExplicitUnbindSwallowsTheBuiltinFallback checks that unbinding a chord in an
// overlay context makes the key do nothing there instead of falling back to the surface's
// built-in handling, while other contexts keep their fallback.
func TestOverlayExplicitUnbindSwallowsTheBuiltinFallback(t *testing.T) {
	t.Parallel()
	popupUnbind := func() fakeUnbindKeymap {
		return fakeUnbindKeymap{
			fakeKeymap: fakeKeymap{},
			unbound: map[string]bool{
				string(KeyContextPopup) + "|Escape|": true,
			},
		}
	}
	unbindModel := func(k fakeUnbindKeymap) *Model {
		m := New(Deps{
			Editor: newFakeEditor("hello"), Explorer: newFakeTree(1), Keys: k,
			Theme: theme.Theme{}, Clock: testClock,
		})
		m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
		return m
	}
	pushList := func(m *Model) {
		v := list.New("Pick", []list.Row{{Left: "a", Value: "a"}}, m.styles.Overlay, nil)
		m.pushOverlay(v)
	}
	// With the popup Esc unbound, the popup stays open: the key was swallowed.
	m := unbindModel(popupUnbind())
	pushList(m)
	press(m, special(tea.KeyEscape))
	if len(m.overlays) != 1 {
		t.Fatalf("the explicitly unbound Esc ran the popup's built-in close: overlays = %d", len(m.overlays))
	}
	// Without the unbind the same Esc closes the popup: the fallback is unchanged.
	m2 := overlayModel(t, fakeKeymap{})
	pushList(m2)
	press(m2, special(tea.KeyEscape))
	if len(m2.overlays) != 0 {
		t.Fatalf("an unbound Esc did not close the popup: overlays = %d", len(m2.overlays))
	}
	// The unbind is per context: a prompt still closes with Esc.
	m3 := unbindModel(popupUnbind())
	pushPrompt(m3, "x")
	press(m3, special(tea.KeyEscape))
	if len(m3.overlays) != 0 {
		t.Fatalf("the popup unbind leaked to the prompt: overlays = %d", len(m3.overlays))
	}
}

// promptCutClipboard is a native clipboard writer that records writes and can fail.
type promptCutClipboard struct {
	writes []string
	err    error
}

func (c *promptCutClipboard) Read() (string, error) { return "", nil }
func (c *promptCutClipboard) Write(s string) error {
	c.writes = append(c.writes, s)
	return c.err
}

// cutOnPromptCtrlX opens a prompt with OnChange recording changes and presses Ctrl+X.
func cutOnPromptCtrlX(t *testing.T, m *Model, input string) (*prompt.Line, *[]string, tea.Cmd) {
	t.Helper()
	changed := &[]string{}
	p := prompt.NewLine(m.styles, prompt.LineOptions{
		Label: "Value:", Input: input, Tag: "test",
		OnChange: func(v string) { *changed = append(*changed, v) },
	})
	m.pushOverlay(p)
	_, cmd := m.Update(ctrl('x'))
	return p, changed, cmd
}

func firstPromptCutMsg(t *testing.T, cmd tea.Cmd) promptCutMsg {
	t.Helper()
	for _, msg := range collect(cmd) {
		if m, ok := msg.(promptCutMsg); ok {
			return m
		}
	}
	t.Fatal("the cut command did not produce a promptCutMsg")
	return promptCutMsg{}
}

func firstPromptCopyMsg(t *testing.T, cmd tea.Cmd) promptCopyMsg {
	t.Helper()
	for _, msg := range collect(cmd) {
		if m, ok := msg.(promptCopyMsg); ok {
			return m
		}
	}
	t.Fatal("the copy command did not produce a promptCopyMsg")
	return promptCopyMsg{}
}

// TestPromptCopyReportsNativeWriterResult: Ctrl+C copies the whole prompt text (or a
// confirm's question) without mutating it. With a native writer the Copied notice waits
// for that write's result; a failure says so and a reader-only clipboard is reported as
// an unconfirmed OSC 52 request, never as Copied.
func TestPromptCopyReportsNativeWriterResult(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		cb := &promptCutClipboard{}
		m := overlayModel(t, fakeKeymap{"prompt|c|ctrl": {Action: "prompt_copy"}})
		m.SetDock(DockPorts{Clipboard: cb})
		p := pushPrompt(m, "one two")
		_, cmd := m.Update(ctrl('c'))
		if m.notice == "Copied" {
			t.Fatal("Ctrl+C claimed Copied before the native writer ran")
		}
		msg := firstPromptCopyMsg(t, cmd)
		if msg.err != nil || msg.text != "one two" || len(cb.writes) != 1 || cb.writes[0] != "one two" {
			t.Fatalf("copy msg = %+v writes = %q", msg, cb.writes)
		}
		m.Update(msg)
		if got := p.Value(); got != "one two" {
			t.Fatalf("Ctrl+C changed the prompt to %q", got)
		}
		if m.notice != "Copied" {
			t.Fatalf("copy notice = %q, want Copied after the writer succeeded", m.notice)
		}
	})

	t.Run("failed writer", func(t *testing.T) {
		cb := &promptCutClipboard{err: errors.New("native clipboard unavailable")}
		m := overlayModel(t, fakeKeymap{"prompt|c|ctrl": {Action: "prompt_copy"}})
		m.SetDock(DockPorts{Clipboard: cb})
		p := pushPrompt(m, "keep me")
		_, cmd := m.Update(ctrl('c'))
		msg := firstPromptCopyMsg(t, cmd)
		m.Update(msg)
		if got := p.Value(); got != "keep me" {
			t.Fatalf("failed copy changed the prompt: %q", got)
		}
		if len(cb.writes) != 1 || cb.writes[0] != "keep me" {
			t.Fatalf("native writes = %q", cb.writes)
		}
		if !strings.Contains(m.notice, "Copy failed") {
			t.Fatalf("failed copy notice = %q", m.notice)
		}
	})

	t.Run("no native writer", func(t *testing.T) {
		m := overlayModel(t, fakeKeymap{"prompt|c|ctrl": {Action: "prompt_copy"}})
		m.SetDock(DockPorts{Clipboard: fakeClipboard{}})
		p := pushPrompt(m, "best effort")
		_, cmd := m.Update(ctrl('c'))
		if got := clipboardWrites(cmd); len(got) != 1 || got[0] != "best effort" {
			t.Fatalf("OSC 52 writes = %q", got)
		}
		for _, msg := range collect(cmd) {
			if _, ok := msg.(promptCopyMsg); ok {
				t.Fatal("a read-only clipboard produced a copy confirmation")
			}
		}
		if got := p.Value(); got != "best effort" {
			t.Fatalf("unconfirmed copy changed the prompt: %q", got)
		}
		if !strings.Contains(m.notice, "requested") || strings.Contains(m.notice, "Copied") {
			t.Fatalf("unconfirmed copy notice = %q", m.notice)
		}
	})

	t.Run("confirm copies its question", func(t *testing.T) {
		cb := &promptCutClipboard{}
		m := overlayModel(t, fakeKeymap{"prompt|c|ctrl": {Action: "prompt_copy"}})
		m.SetDock(DockPorts{Clipboard: cb})
		m.pushOverlay(prompt.NewConfirm(m.styles, "Delete the file?", "confirm"))
		_, cmd := m.Update(ctrl('c'))
		msg := firstPromptCopyMsg(t, cmd)
		m.Update(msg)
		if len(cb.writes) != 1 || cb.writes[0] != "Delete the file?" {
			t.Fatalf("confirm native writes = %q", cb.writes)
		}
		if len(m.overlays) == 0 {
			t.Fatal("confirm Ctrl+C closed the prompt")
		}
	})

	t.Run("empty prompt writes nothing", func(t *testing.T) {
		m := overlayModel(t, fakeKeymap{"prompt|c|ctrl": {Action: "prompt_copy"}})
		pushPrompt(m, "")
		_, cmd := m.Update(ctrl('c'))
		if got := clipboardWrites(cmd); len(got) != 0 {
			t.Fatalf("empty prompt Ctrl+C wrote %q", got)
		}
	})
}

// TestPromptCutClearsOnlyAfterNativeWrite: Ctrl+X does not clear the field in Update. The
// native writer runs in the returned command; only its success message clears the same
// prompt and fires onChange. A failed writer keeps the text and reports the failure; a
// clipboard with no native writer only requests an OSC 52 copy and keeps the text.
func TestPromptCutClearsOnlyAfterNativeWrite(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		cb := &promptCutClipboard{}
		m := overlayModel(t, fakeKeymap{"prompt|x|ctrl": {Action: "prompt_cut"}})
		m.SetDock(DockPorts{Clipboard: cb})
		p, changed, cmd := cutOnPromptCtrlX(t, m, "one two")
		if got := p.Value(); got != "one two" {
			t.Fatalf("Update cleared the prompt before confirmation: %q", got)
		}
		if len(*changed) != 0 {
			t.Fatalf("onChange ran before confirmation: %v", *changed)
		}
		msg := firstPromptCutMsg(t, cmd)
		if msg.err != nil || len(cb.writes) != 1 || cb.writes[0] != "one two" {
			t.Fatalf("cut msg = %+v writes = %q", msg, cb.writes)
		}
		m.Update(msg)
		if got := p.Value(); got != "" {
			t.Fatalf("successful cut left %q in the prompt", got)
		}
		if len(*changed) != 1 || (*changed)[0] != "" {
			t.Fatalf("onChange after success = %v, want one empty value", *changed)
		}
		if m.notice == "" || strings.Contains(m.notice, "failed") {
			t.Fatalf("cut notice = %q, want a Cut notice", m.notice)
		}
	})

	t.Run("failed writer keeps text", func(t *testing.T) {
		cb := &promptCutClipboard{err: errors.New("native clipboard unavailable")}
		m := overlayModel(t, fakeKeymap{"prompt|x|ctrl": {Action: "prompt_cut"}})
		m.SetDock(DockPorts{Clipboard: cb})
		p, changed, cmd := cutOnPromptCtrlX(t, m, "keep me")
		msg := firstPromptCutMsg(t, cmd)
		m.Update(msg)
		if got := p.Value(); got != "keep me" {
			t.Fatalf("failed cut lost the prompt text: %q", got)
		}
		if len(*changed) != 0 {
			t.Fatalf("failed cut fired onChange: %v", *changed)
		}
		if len(cb.writes) != 1 || cb.writes[0] != "keep me" {
			t.Fatalf("native writes = %q, want the attempted text", cb.writes)
		}
		if !strings.Contains(m.notice, "Cut failed") || !strings.Contains(m.notice, "kept") {
			t.Fatalf("failed cut notice = %q", m.notice)
		}
	})

	t.Run("no native writer keeps text", func(t *testing.T) {
		m := overlayModel(t, fakeKeymap{"prompt|x|ctrl": {Action: "prompt_cut"}})
		m.SetDock(DockPorts{Clipboard: fakeClipboard{}})
		p, changed, cmd := cutOnPromptCtrlX(t, m, "best effort")
		for _, msg := range collect(cmd) {
			if _, ok := msg.(promptCutMsg); ok {
				t.Fatal("a read-only clipboard produced a cut confirmation")
			}
		}
		if got := p.Value(); got != "best effort" {
			t.Fatalf("unconfirmed cut lost the prompt text: %q", got)
		}
		if len(*changed) != 0 {
			t.Fatalf("unconfirmed cut fired onChange: %v", *changed)
		}
		if !strings.Contains(m.notice, "no native clipboard") {
			t.Fatalf("unconfirmed cut notice = %q", m.notice)
		}
	})

	t.Run("late result does not clear another prompt", func(t *testing.T) {
		cb := &promptCutClipboard{}
		m := overlayModel(t, fakeKeymap{"prompt|x|ctrl": {Action: "prompt_cut"}})
		m.SetDock(DockPorts{Clipboard: cb})
		p, _, cmd := cutOnPromptCtrlX(t, m, "first")
		msg := firstPromptCutMsg(t, cmd)
		next := pushPrompt(m, "second")
		m.Update(msg)
		if got := next.Value(); got != "second" {
			t.Fatalf("late cut result changed another prompt: %q", got)
		}
		if got := p.Value(); got != "first" {
			t.Fatalf("late cut cleared a prompt it no longer owned: %q", got)
		}
	})

	t.Run("edited prompt is not cleared", func(t *testing.T) {
		cb := &promptCutClipboard{}
		m := overlayModel(t, fakeKeymap{"prompt|x|ctrl": {Action: "prompt_cut"}})
		m.SetDock(DockPorts{Clipboard: cb})
		p, changed, cmd := cutOnPromptCtrlX(t, m, "one")
		msg := firstPromptCutMsg(t, cmd)
		m.Update(typed("!"))
		m.Update(msg)
		if got := p.Value(); got != "one!" {
			t.Fatalf("late cut cleared newer typing: %q", got)
		}
		if len(*changed) != 1 || (*changed)[0] != "one!" {
			t.Fatalf("onChange = %v, want only the typed edit", *changed)
		}
	})
}

// TestPromptClipboardStaleAndSupersededResults: every copy/cut result is checked against
// the same active top prompt, the workspace owner and the latest operation token before it
// may change the notice or the field. Closed/switched prompts, an older workspace
// generation, and an older cut are ignored.
func TestPromptClipboardStaleAndSupersededResults(t *testing.T) {
	t.Run("stale copy after close", func(t *testing.T) {
		cb := &promptCutClipboard{}
		m := overlayModel(t, fakeKeymap{"prompt|c|ctrl": {Action: "prompt_copy"}})
		m.SetDock(DockPorts{Clipboard: cb})
		pushPrompt(m, "copied late")
		_, cmd := m.Update(ctrl('c'))
		msg := firstPromptCopyMsg(t, cmd)
		m.Update(special(tea.KeyEscape))
		if len(m.overlays) != 0 {
			t.Fatal("Esc did not close the prompt")
		}
		m.notice = "marker"
		m.Update(msg)
		if m.notice != "marker" {
			t.Fatalf("stale copy notice = %q, want the untouched marker", m.notice)
		}
	})

	t.Run("stale cut failure after switch", func(t *testing.T) {
		cb := &promptCutClipboard{err: errors.New("native clipboard unavailable")}
		m := overlayModel(t, fakeKeymap{"prompt|x|ctrl": {Action: "prompt_cut"}})
		m.SetDock(DockPorts{Clipboard: cb})
		p, changed, cmd := cutOnPromptCtrlX(t, m, "first")
		msg := firstPromptCutMsg(t, cmd)
		next := pushPrompt(m, "second")
		m.notice = "marker"
		m.Update(msg)
		if m.notice != "marker" {
			t.Fatalf("stale cut failure notice = %q, want the untouched marker", m.notice)
		}
		if got := next.Value(); got != "second" {
			t.Fatalf("stale cut failure changed another prompt: %q", got)
		}
		if got := p.Value(); got != "first" || len(*changed) != 0 {
			t.Fatalf("stale cut failure changed its old prompt: %q changes %v", got, *changed)
		}
	})

	t.Run("second cut while pending is refused", func(t *testing.T) {
		cb := &promptCutClipboard{}
		m := overlayModel(t, fakeKeymap{"prompt|x|ctrl": {Action: "prompt_cut"}})
		m.SetDock(DockPorts{Clipboard: cb})
		p, changed, first := cutOnPromptCtrlX(t, m, "same")
		_, second := m.Update(ctrl('x'))
		if second != nil {
			t.Fatal("a second cut started a command while the first write was pending")
		}
		if !strings.Contains(m.notice, "still in progress") {
			t.Fatalf("duplicate cut notice = %q", m.notice)
		}
		if got := p.Value(); got != "same" {
			t.Fatalf("duplicate cut changed the prompt: %q", got)
		}
		msg := firstPromptCutMsg(t, first)
		m.Update(msg)
		if got := p.Value(); got != "" {
			t.Fatalf("first cut did not clear after completing: %q", got)
		}
		if len(*changed) != 1 || (*changed)[0] != "" {
			t.Fatalf("onChange = %v, want one empty value", *changed)
		}
		if m.notice != "Cut" {
			t.Fatalf("notice = %q, want Cut", m.notice)
		}
	})

	t.Run("same workspace generation accepted", func(t *testing.T) {
		cb := &promptCutClipboard{}
		m := overlayModel(t, fakeKeymap{"prompt|x|ctrl": {Action: "prompt_cut"}})
		m.SetDock(DockPorts{Clipboard: cb})
		m.SetWorkspaceOwner("ws", 7)
		p, changed, cmd := cutOnPromptCtrlX(t, m, "same generation")
		msg := firstPromptCutMsg(t, cmd)
		m.Update(msg)
		if got := p.Value(); got != "" {
			t.Fatalf("same-generation cut did not clear: %q", got)
		}
		if len(*changed) != 1 || (*changed)[0] != "" {
			t.Fatalf("same-generation onChange = %v", *changed)
		}
	})

	t.Run("stale workspace generation", func(t *testing.T) {
		cb := &promptCutClipboard{}
		m := overlayModel(t, fakeKeymap{"prompt|x|ctrl": {Action: "prompt_cut"}})
		m.SetDock(DockPorts{Clipboard: cb})
		m.SetWorkspaceOwner("ws", 7)
		p, changed, cmd := cutOnPromptCtrlX(t, m, "generation")
		msg := firstPromptCutMsg(t, cmd)
		m.SetWorkspaceOwner("ws", 8)
		m.notice = "marker"
		m.Update(msg)
		if got := p.Value(); got != "generation" {
			t.Fatalf("stale-generation cut cleared the prompt: %q", got)
		}
		if len(*changed) != 0 {
			t.Fatalf("stale-generation cut fired onChange: %v", *changed)
		}
		if m.notice != "marker" {
			t.Fatalf("stale-generation notice = %q, want the untouched marker", m.notice)
		}
	})
}
