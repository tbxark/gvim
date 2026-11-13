package tui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/browser"
	"github.com/tbxark/gvim/internal/tui/palette"
	"github.com/tbxark/gvim/theme"
	"github.com/tbxark/gvim/trust"
)

// fakeTrustPort is an in-memory TrustPort: decisions are recorded, and the state
// moves the way the app's controller moves it. loadFor answers Load per root.
type fakeTrustPort struct {
	state   trust.State
	decided []trust.Level
	loads   []string
	loadFor map[string]trust.State
	err     error
}

func (p *fakeTrustPort) State() trust.State { return p.state }

func (p *fakeTrustPort) Decide(level trust.Level) (trust.State, error) {
	p.decided = append(p.decided, level)
	// The decision applies even when the write fails, as the app's controller
	// does: the error only says it will not be remembered.
	p.state.Level = level
	p.state.Decided = true
	return p.state, p.err
}

func (p *fakeTrustPort) Load(root string) (trust.State, error) {
	p.loads = append(p.loads, root)
	if p.err != nil {
		return p.state, p.err
	}
	if state, ok := p.loadFor[root]; ok {
		p.state = state
	}
	return p.state, nil
}

// errTrustSave is the failure a test store reports.
var errTrustSave = testTrustError("the store could not be written")

// testTrustError is a string error, for the tests.
type testTrustError string

func (e testTrustError) Error() string { return string(e) }

// newTrustModel returns a sized model over the trust port p.
func newTrustModel(t *testing.T, p TrustPort) *Model {
	t.Helper()
	m := New(Deps{Editor: newFakeEditor("hello"), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock, Trust: p})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return m
}

// topTrustDialog returns the trust dialog on top of the overlay stack.
func topTrustDialog(t *testing.T, m *Model) *trustDialog {
	t.Helper()
	if len(m.overlays) == 0 {
		t.Fatal("no overlay is open")
	}
	top := m.overlays[len(m.overlays)-1]
	d, ok := top.(*trustDialog)
	if !ok {
		t.Fatalf("top overlay is %T, want *trustDialog", top)
	}
	return d
}

// trustFrame returns the painted frame of m as rows without styles.
func trustFrame(m *Model) []string {
	return m.paint().Plain()
}

func TestTrustRetriesUnavailableAndInflightLSPOpens(t *testing.T) {
	for _, inflight := range []bool{false, true} {
		t.Run(map[bool]string{false: "unavailable", true: "in flight"}[inflight], func(t *testing.T) {
			p := &fakeTrustPort{state: trust.State{Root: "/ws", Level: trust.Restricted}}
			m := newTrustModel(t, p)
			tr := &lspTrack{path: "/ws/main.go", lang: "go", state: lspClosed}
			m.lsp.tracks = map[string]*lspTrack{tr.path: tr}
			if inflight {
				tr.state, tr.busy = lspOpening, true
			} else {
				m.lsp.unavailable = map[string]*LSPUnavailableError{"go": {Err: errors.New("blocked")}}
			}
			m.trustDecided(trustDecidedMsg{state: trust.State{Root: "/ws", Level: trust.Trusted}})
			if m.lsp.unavailable["go"] != nil || !m.lsp.stale {
				t.Fatal("trust did not invalidate unavailable languages and request reconciliation")
			}
			if inflight {
				cmd := m.lspOpened(lspOpenedMsg{path: tr.path, err: &LSPUnavailableError{Err: errors.New("blocked")}})
				if cmd == nil || m.lsp.unavailable["go"] != nil {
					t.Fatal("in-flight failure was not retried after trust")
				}
			}
		})
	}
}

func TestWorkspaceTrustCommandOpensTheDialog(t *testing.T) {
	t.Parallel()
	p := &fakeTrustPort{state: trust.State{Root: "/ws", Level: trust.Restricted, Decided: true, Markers: []string{"go.mod"}}}
	m := newTrustModel(t, p)
	m.notice = ""
	m.Update(palette.RunCommandMsg{Action: "workspace_trust_prompt"})

	d := topTrustDialog(t, m)
	if !d.cancellable {
		t.Error("the dialog opened from the command is not cancellable")
	}
	if d.sel != 1 {
		t.Errorf("selection = %d, want 1 (the Restricted row)", d.sel)
	}
	if m.notice != "" {
		t.Errorf("notice = %q, want the dialog with no notice", m.notice)
	}
}

func TestWorkspaceTrustCommandWithoutAPortSaysWhy(t *testing.T) {
	t.Parallel()
	m := New(Deps{Editor: newFakeEditor("hello"), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.Update(palette.RunCommandMsg{Action: "workspace_trust_prompt"})
	if len(m.overlays) != 0 {
		t.Fatalf("%d overlays are open, want none", len(m.overlays))
	}
	if m.notice != trustUnavailableNotice {
		t.Fatalf("notice = %q, want %q", m.notice, trustUnavailableNotice)
	}
}

func TestTrustDialogStringsAndMarkersAreDrawn(t *testing.T) {
	t.Parallel()
	state := trust.State{Root: "/home/u/projects/fresh", Level: trust.Restricted, Decided: true, Markers: []string{"go.mod", ".envrc"}}
	p := &fakeTrustPort{state: state}
	m := newTrustModel(t, p)
	m.Update(palette.RunCommandMsg{Action: "workspace_trust_prompt"})
	d := topTrustDialog(t, m)

	joined := strings.Join(trustFrame(m), "\n")
	for _, want := range []string{
		"SECURITY WARNING",
		trustCanExecute,
		trustPathLabel,
		"/home/u/projects/fresh",
		"Detected: go.mod, .envrc",
		trustHowProceed,
		"Trust folder & Allow Tooling (T)",
		"Keep Restricted (Default) (K)",
		"Block All Execution (B)",
		"[ OK ]",
		"[ Cancel (Esc) ]",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("the frame does not show %q:\n%s", want, joined)
		}
	}
	if d.box.X <= 0 || d.box.Y <= 0 {
		t.Errorf("card box = %+v, want it centred with a margin", d.box)
	}
	if d.box.W != 68 {
		t.Errorf("card width = %d, want Fresh's 68", d.box.W)
	}
	if d.sel != 1 {
		t.Errorf("selection = %d, want 1", d.sel)
	}
}

func TestTrustDialogKeyboardSelectionAndConfirm(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		keys []tea.KeyPressMsg
		want trust.Level
	}{
		{
			name: "T selects Trust and Enter confirms",
			keys: []tea.KeyPressMsg{typed("t"), special(tea.KeyEnter)},
			want: trust.Trusted,
		},
		{
			name: "K selects Restricted and O confirms",
			keys: []tea.KeyPressMsg{typed("k"), typed("o")},
			want: trust.Restricted,
		},
		{
			name: "B selects Block and Enter confirms",
			keys: []tea.KeyPressMsg{typed("b"), special(tea.KeyEnter)},
			want: trust.Blocked,
		},
		{
			name: "the arrows wrap around the three rows",
			keys: []tea.KeyPressMsg{special(tea.KeyUp), special(tea.KeyUp), special(tea.KeyUp), special(tea.KeyEnter)},
			want: trust.Restricted,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := &fakeTrustPort{state: trust.State{Root: "/ws", Level: trust.Restricted, Decided: true}}
			m := newTrustModel(t, p)
			m.Update(palette.RunCommandMsg{Action: "workspace_trust_prompt"})
			d := topTrustDialog(t, m)
			var cmd tea.Cmd
			for i, key := range tt.keys {
				cmd = press(m, key)
				if cmd != nil && i < len(tt.keys)-1 {
					t.Fatalf("key %d started a command before the decision", i)
				}
			}
			if d.sel != trustSelectionFromLevel(tt.want) {
				t.Errorf("selection = %d, want %d", d.sel, trustSelectionFromLevel(tt.want))
			}
			if d.Active() {
				t.Error("the dialog is still open after the decision")
			}
			msg := firstOf[trustDecidedMsg](t, collect(cmd))
			if len(p.decided) != 1 || p.decided[0] != tt.want {
				t.Fatalf("decisions = %v, want [%q]", p.decided, string(tt.want))
			}
			m.Update(msg)
			if m.trust.state.Level != tt.want || !m.trust.state.Decided {
				t.Fatalf("trust state = %+v, want level %q decided", m.trust.state, string(tt.want))
			}
			if m.notice != trustNotice(tt.want) {
				t.Errorf("notice = %q, want %q", m.notice, trustNotice(tt.want))
			}
		})
	}
}

func TestTrustDialogCancelLeavesTheDecisionAlone(t *testing.T) {
	t.Parallel()
	p := &fakeTrustPort{state: trust.State{Root: "/ws", Level: trust.Restricted, Decided: true}}
	m := newTrustModel(t, p)
	m.Update(palette.RunCommandMsg{Action: "workspace_trust_prompt"})
	topTrustDialog(t, m)
	cmd := press(m, special(tea.KeyEscape))
	if cmd != nil {
		t.Errorf("Esc started a command: %v", collect(cmd))
	}
	if len(m.overlays) != 0 {
		t.Fatalf("%d overlays are open after Esc, want none", len(m.overlays))
	}
	if len(p.decided) != 0 {
		t.Fatalf("decisions = %v, want none", p.decided)
	}
	if m.trust.state.Level != trust.Restricted {
		t.Errorf("trust state = %+v, want it unchanged", m.trust.state)
	}
}

func TestTrustDialogMouseSelectsWithoutConsenting(t *testing.T) {
	t.Parallel()
	p := &fakeTrustPort{state: trust.State{Root: "/ws", Level: trust.Restricted, Decided: true}}
	m := newTrustModel(t, p)
	m.Update(palette.RunCommandMsg{Action: "workspace_trust_prompt"})
	d := topTrustDialog(t, m)
	trustFrame(m) // records the hit rectangles

	// A click on the Block radio selects it and does not record anything.
	m.Update(click(d.radios[2].X+1, d.radios[2].Y, tea.MouseLeft))
	if d.sel != 2 {
		t.Fatalf("selection = %d, want 2 after the click", d.sel)
	}
	if len(p.decided) != 0 {
		t.Fatalf("a radio click recorded %v; selecting is not consenting", p.decided)
	}
	// A click on OK answers the selection.
	_, cmd := m.Update(click(d.ok.X+1, d.ok.Y, tea.MouseLeft))
	msg := firstOf[trustDecidedMsg](t, collect(cmd))
	if msg.state.Level != trust.Blocked {
		t.Fatalf("decided level = %q, want blocked", string(msg.state.Level))
	}
}

func TestTrustDialogSecondaryButtonCancels(t *testing.T) {
	t.Parallel()
	p := &fakeTrustPort{state: trust.State{Root: "/ws", Level: trust.Restricted, Decided: true}}
	m := newTrustModel(t, p)
	m.Update(palette.RunCommandMsg{Action: "workspace_trust_prompt"})
	d := topTrustDialog(t, m)
	trustFrame(m)
	_, cmd := m.Update(click(d.secondary.X+1, d.secondary.Y, tea.MouseLeft))
	if cmd != nil {
		t.Errorf("the Cancel click started a command")
	}
	if len(m.overlays) != 0 {
		t.Fatalf("%d overlays are open after the Cancel click, want none", len(m.overlays))
	}
	if len(p.decided) != 0 {
		t.Fatalf("decisions = %v, want none", p.decided)
	}
}

func TestTrustStartupPromptIsMandatoryAndEscIsInert(t *testing.T) {
	t.Parallel()
	p := &fakeTrustPort{state: trust.State{Root: "/ws", Level: trust.Restricted, Markers: []string{"go.mod"}}}
	m := newTrustModel(t, p)
	d := topTrustDialog(t, m)
	if d.cancellable {
		t.Error("the open-time prompt is cancellable")
	}
	if cmd := press(m, special(tea.KeyEscape)); cmd != nil {
		t.Errorf("Esc started a command")
	}
	if len(m.overlays) != 1 {
		t.Fatalf("Esc dismissed the mandatory prompt: %d overlays", len(m.overlays))
	}
	// The secondary button names the quit key the keymap binds.
	if got := d.secondaryLabel(); got != "Quit (Ctrl+Q)" {
		t.Errorf("secondary label = %q, want %q", got, "Quit (Ctrl+Q)")
	}
	// The quit key quits the editor and leaves the store undecided.
	cmd := press(m, ctrl('q'))
	if cmd == nil {
		t.Fatal("the quit key started no command")
	}
	if d.Active() {
		t.Error("the dialog is still open after the quit key")
	}
	if len(p.decided) != 0 {
		t.Fatalf("decisions = %v, want none: quitting must not decide", p.decided)
	}
}

func TestTrustStartupPromptNotShownWhenDecidedOrPlain(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		state trust.State
	}{
		{name: "a recorded decision", state: trust.State{Root: "/ws", Level: trust.Restricted, Decided: true, Markers: []string{"go.mod"}}},
		{name: "a plain folder", state: trust.State{Root: "/ws", Level: trust.Trusted, Decided: true}},
		{name: "an undecided folder with no markers", state: trust.State{Root: "/ws", Level: trust.Trusted, Decided: false}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := &fakeTrustPort{state: tt.state}
			m := newTrustModel(t, p)
			if len(m.overlays) != 0 {
				t.Fatalf("%d overlays are open, want no startup prompt", len(m.overlays))
			}
		})
	}
}

func TestTrustDecisionSaveFailureStillApplies(t *testing.T) {
	t.Parallel()
	p := &fakeTrustPort{state: trust.State{Root: "/ws", Level: trust.Restricted, Decided: true}, err: errTrustSave}
	m := newTrustModel(t, p)
	m.Update(palette.RunCommandMsg{Action: "workspace_trust_prompt"})
	topTrustDialog(t, m)
	press(m, typed("t"))
	cmd := press(m, special(tea.KeyEnter))
	msg := firstOf[trustDecidedMsg](t, collect(cmd))
	m.Update(msg)
	if m.trust.state.Level != trust.Trusted || !m.trust.state.Decided {
		t.Fatalf("trust state = %+v, want trusted despite the failed write", m.trust.state)
	}
	if !strings.Contains(m.notice, "not saved") {
		t.Errorf("notice = %q, want it to say the decision was not saved", m.notice)
	}
}

func TestTrustStatusLabels(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		state     trust.State
		available bool
		wantLabel string
		wantWarn  bool
	}{
		{name: "unavailable", available: false},
		{name: "trusted", state: trust.State{Root: "/ws", Level: trust.Trusted, Decided: true}, available: true, wantLabel: "Trusted"},
		{name: "restricted", state: trust.State{Root: "/ws", Level: trust.Restricted, Decided: true}, available: true, wantLabel: "Restricted", wantWarn: true},
		{name: "blocked", state: trust.State{Root: "/ws", Level: trust.Blocked, Decided: true}, available: true, wantLabel: "Blocked", wantWarn: true},
		{name: "undecided", state: trust.State{Root: "/ws", Level: trust.Restricted, Markers: []string{"go.mod"}}, available: true, wantLabel: "Restricted", wantWarn: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var m *Model
			if tt.available {
				m = newTrustModel(t, &fakeTrustPort{state: tt.state})
			} else {
				m = New(Deps{Editor: newFakeEditor("hello"), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
				m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			}
			label, warn := m.trustStatus()
			if label != tt.wantLabel || warn != tt.wantWarn {
				t.Errorf("trustStatus() = (%q, %v), want (%q, %v)", label, warn, tt.wantLabel, tt.wantWarn)
			}
		})
	}
}

func TestProjectSwitchLoadsTheTrustState(t *testing.T) {
	t.Parallel()
	projects := &fakeProjects{project: Project{Tree: treeAt("/next", 2), Files: &fakeIndex{}}}
	p := &fakeTrustPort{
		state:   trust.State{Root: "/ws", Level: trust.Trusted, Decided: true},
		loadFor: map[string]trust.State{"/next": {Root: "/next", Level: trust.Restricted, Markers: []string{"go.mod"}}},
	}
	m := New(Deps{
		Editor:   newFakeEditor("x"),
		Explorer: newFakeTree(3),
		Files:    &fakeIndex{files: []string{"old.go"}},
		Keys:     testKeys(),
		Theme:    theme.Theme{},
		Clock:    testClock,
		Root:     RootPorts{Projects: projects},
		Trust:    p,
	})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	_, cmd := m.Update(browser.Choose{Path: "/next"})
	opened := firstOf[projectOpenedMsg](t, collect(cmd))
	_, cmd = m.Update(opened)
	for _, msg := range collect(cmd) {
		m.Update(msg)
	}
	if len(p.loads) != 1 || p.loads[0] != "/next" {
		t.Fatalf("loaded roots = %v, want [/next]", p.loads)
	}
	if m.trust.state.Root != "/next" {
		t.Fatalf("trust root = %q, want /next", m.trust.state.Root)
	}
	// The new workspace is undecided and can execute code, so it asks.
	d := topTrustDialog(t, m)
	if !d.cancellable {
		t.Error("the prompt of a switched workspace is not cancellable")
	}
}

func TestTrustDialogFitsAShortFrame(t *testing.T) {
	t.Parallel()
	p := &fakeTrustPort{state: trust.State{Root: "/ws", Level: trust.Restricted, Markers: []string{"go.mod"}}}
	m := New(Deps{Editor: newFakeEditor("hello"), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock, Trust: p})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 18})
	d := topTrustDialog(t, m)
	joined := strings.Join(trustFrame(m), "\n")
	for _, want := range []string{"SECURITY WARNING", "Keep Restricted (Default) (K)", "[ OK ]", "[ Quit (Ctrl+Q) ]"} {
		if !strings.Contains(joined, want) {
			t.Errorf("a short frame does not show %q:\n%s", want, joined)
		}
	}
	if d.box.H > 18 || d.box.Y < 0 {
		t.Errorf("card box = %+v, want it inside an 18-row frame", d.box)
	}
}

func TestTrustDialogStartsWithTheCurrentLevelSelected(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		level trust.Level
		want  int
	}{
		{"trusted", trust.Trusted, 0},
		{"restricted", trust.Restricted, 1},
		{"blocked", trust.Blocked, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := &fakeTrustPort{state: trust.State{Root: "/ws", Level: tt.level, Decided: true}}
			m := newTrustModel(t, p)
			m.Update(palette.RunCommandMsg{Action: "workspace_trust_prompt"})
			if d := topTrustDialog(t, m); d.sel != tt.want {
				t.Errorf("selection = %d, want %d for %q", d.sel, tt.want, string(tt.level))
			}
		})
	}
}
