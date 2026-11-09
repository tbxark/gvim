package tui

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/prompt"
	"github.com/tbxark/gvim/pluginapi"
	"github.com/tbxark/gvim/theme"
)

// urlTestModel builds a root with the plugin host and an injected opener port.
func urlTestModel(t *testing.T, open func(string) error) *Model {
	t.Helper()
	rt := newFakeRuntime(t)
	ed := newFakeEditor("hello", "world")
	pe := &fakePluginEditor{ed: ed, active: 1, bufs: []pluginapi.BufferInfo{{ID: 1, Path: "/w/a.go", Active: true}}}
	m := New(Deps{Editor: ed, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock,
		Root: RootPorts{Plugins: rt, PluginEditor: pe, OpenURL: open}})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	useHost(rt, m)
	return m
}

// TestValidateOpenURLAcceptsOnlyWebURLs is the scheme/format policy table: only absolute
// http(s) URLs with a host and no credentials are accepted.
func TestValidateOpenURLAcceptsOnlyWebURLs(t *testing.T) {
	good := []string{
		"https://example.com",
		"https://github.com/owner/repo",
		"http://127.0.0.1:8080/a?b=1#c",
		"https://example.com/path%20with%20escape",
	}
	for _, raw := range good {
		if err := validateOpenURL(raw); err != nil {
			t.Errorf("validateOpenURL(%q) = %v, want accepted", raw, err)
		}
	}
	bad := []string{
		"",
		"javascript:alert(1)",
		"file:///etc/passwd",
		"data:text/html,x",
		"ssh://git@example.com/repo",
		"mailto:a@b",
		"ftp://example.com/x",
		"https://user:pass@example.com/x",
		"https:///no-host",
		"not a url",
		"https://example.com/a b",
		"https://example.com/a\nopen x",
		" https://example.com",
		"https://example.com ",
	}
	for _, raw := range bad {
		if err := validateOpenURL(raw); pluginapi.CodeOf(err) != pluginapi.CodeInvalid {
			t.Errorf("validateOpenURL(%q) = %v, want CodeInvalid", raw, err)
		}
	}
}

// TestOpenURLRequiresKeyboardConfirmation: a valid URL only queues the confirm dialog;
// nothing reaches the opener until Yes is answered. No and an opener failure are both
// reported, never silent.
func TestOpenURLRequiresKeyboardConfirmation(t *testing.T) {
	const raw = "https://example.com/repo"
	var opened []string
	m := urlTestModel(t, func(u string) error { opened = append(opened, u); return nil })
	overlays := len(m.overlays)
	if err := m.plug.host.OpenURL(raw); err != nil {
		t.Fatal(err)
	}
	if len(m.overlays) != overlays+1 {
		t.Fatalf("confirmation overlay was not pushed: %d -> %d", overlays, len(m.overlays))
	}
	if len(opened) != 0 {
		t.Fatalf("opener ran before confirmation: %v", opened)
	}
	cmd := m.answer(prompt.Done{Tag: urlOpenTag{url: raw}, Outcome: prompt.Yes})
	for _, msg := range deliver(cmd) {
		if msg != nil {
			m.Update(msg)
		}
	}
	if len(opened) != 1 || opened[0] != raw {
		t.Fatalf("opened = %v, want exactly %q", opened, raw)
	}
	// A cancelled confirmation opens nothing.
	_ = m.plug.host.OpenURL(raw)
	if cmd := m.answer(prompt.Done{Tag: urlOpenTag{url: raw}, Outcome: prompt.No}); cmd != nil {
		t.Fatalf("No returned a command: %T", cmd)
	}
	if len(opened) != 1 {
		t.Fatalf("cancelled confirmation opened %v", opened)
	}
}

// TestOpenURLUnsafeSchemeIsRefusedBeforePrompt: an unsafe URL never reaches the confirm
// dialog or the opener.
func TestOpenURLUnsafeSchemeIsRefusedBeforePrompt(t *testing.T) {
	called := 0
	m := urlTestModel(t, func(string) error { called++; return nil })
	before := len(m.overlays)
	for _, raw := range []string{"javascript:alert(1)", "file:///tmp/x", "ssh://git@h/x", "https://u:p@h/x"} {
		if err := m.plug.host.OpenURL(raw); pluginapi.CodeOf(err) != pluginapi.CodeInvalid {
			t.Errorf("OpenURL(%q) = %v, want CodeInvalid", raw, err)
		}
	}
	if len(m.overlays) != before || called != 0 {
		t.Fatalf("unsafe URL prompted/opened: overlays %d->%d opener %d", before, len(m.overlays), called)
	}
}

// TestOpenURLReportsOpenerFailure: a platform opener error is delivered back as a notice.
func TestOpenURLReportsOpenerFailure(t *testing.T) {
	boom := errors.New("no browser")
	m := urlTestModel(t, func(string) error { return boom })
	const raw = "https://example.com"
	if err := m.plug.host.OpenURL(raw); err != nil {
		t.Fatal(err)
	}
	cmd := m.answer(prompt.Done{Tag: urlOpenTag{url: raw}, Outcome: prompt.Yes})
	for _, msg := range deliver(cmd) {
		if msg != nil {
			m.Update(msg)
		}
	}
	if m.notice == "" {
		t.Fatal("opener failure was not reported")
	}
	if !bytesContains(m.notice, "no browser") {
		t.Fatalf("notice = %q, want the opener error", m.notice)
	}
}

// TestOpenURLWithoutPortIsUnsupported: a host with no opener port refuses rather than
// silently doing nothing.
func TestOpenURLWithoutPortIsUnsupported(t *testing.T) {
	rt := newFakeRuntime(t)
	ed := newFakeEditor("hello")
	pe := &fakePluginEditor{ed: ed, active: 1, bufs: []pluginapi.BufferInfo{{ID: 1, Path: "/w/a.go", Active: true}}}
	m := New(Deps{Editor: ed, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock,
		Root: RootPorts{Plugins: rt, PluginEditor: pe}})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	useHost(rt, m)
	if err := m.plug.host.OpenURL("https://example.com"); pluginapi.CodeOf(err) != pluginapi.CodeUnsupported {
		t.Fatalf("OpenURL without a port = %v, want CodeUnsupported", err)
	}
}

// bytesContains is strings.Contains without an extra import in the table test file.
func bytesContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestOpenURLConfirmIsKeyboardReachable drives the confirmation with real key messages:
// y opens (after the answer flows through Update), n cancels. The dialog is the normal
// prompt overlay, so it is focusable and dismissible from the keyboard.
func TestOpenURLConfirmIsKeyboardReachable(t *testing.T) {
	const raw = "https://example.com/repo"
	var opened []string
	m := urlTestModel(t, func(u string) error { opened = append(opened, u); return nil })
	if err := m.plug.host.OpenURL(raw); err != nil {
		t.Fatal(err)
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	var answers []prompt.Done
	for _, msg := range deliver(cmd) {
		if d, ok := msg.(prompt.Done); ok {
			answers = append(answers, d)
		}
	}
	if len(answers) != 1 || answers[0].Outcome != prompt.Yes {
		t.Fatalf("keyboard answer = %+v, want one Yes", answers)
	}
	for _, out := range deliver(m.answer(answers[0])) {
		if out != nil {
			m.Update(out)
		}
	}
	if len(opened) != 1 || opened[0] != raw {
		t.Fatalf("opened after keyboard yes = %v, want %q", opened, raw)
	}
	// n answers No and opens nothing.
	opened = nil
	if err := m.plug.host.OpenURL(raw); err != nil {
		t.Fatal(err)
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	answers = answers[:0]
	for _, msg := range deliver(cmd) {
		if d, ok := msg.(prompt.Done); ok {
			answers = append(answers, d)
		}
	}
	if len(answers) != 1 || answers[0].Outcome != prompt.No {
		t.Fatalf("keyboard answer = %+v, want one No", answers)
	}
	if cmd := m.answer(answers[0]); cmd != nil {
		for _, msg := range deliver(cmd) {
			m.Update(msg)
		}
	}
	if len(opened) != 0 {
		t.Fatalf("keyboard no opened %v", opened)
	}
}
