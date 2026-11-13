package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/internal/tui/style"
	"github.com/tbxark/gvim/trust"
)

// statusTrustRun returns the workspace-trust pill on the status row of m.
func statusTrustRun(t *testing.T, m *Model) frame.Run {
	t.Helper()
	l := m.layout()
	if !l.StatusOn {
		t.Fatal("the status row is hidden")
	}
	for _, r := range l.Status {
		if r.Kind == frame.HitStatusSegment && r.Index == frame.StatusSegmentTrust {
			return r
		}
	}
	t.Fatalf("no trust pill on the status row: %+v", l.Status)
	return frame.Run{}
}

// TestStatusRowTrustPillShowsTheLevel checks the status-bar wiring: the trust level is
// the first left element, and it is "Restricted" (warn) while the workspace is
// undecided, Fresh's safe default. Only an unavailable state paints no pill.
func TestStatusRowTrustPillShowsTheLevel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		level trust.Level
		label string
		warn  bool
	}{
		{trust.Trusted, "Trusted", false},
		{trust.Restricted, "Restricted", true},
		{trust.Blocked, "Blocked", true},
		{trust.Undecided, "Restricted", true},
	}
	for _, tt := range tests {
		p := &fakeTrustPort{state: trust.State{Root: "/ws", Level: tt.level, Decided: tt.level.Valid()}}
		m := newTrustModel(t, p)
		v := m.statusView()
		if v.Trust != tt.label || v.TrustWarn != tt.warn {
			t.Errorf("%q: trust pill = %q warn=%v, want %q warn=%v", tt.level, v.Trust, v.TrustWarn, tt.label, tt.warn)
		}
		r := statusTrustRun(t, m)
		if r.X != 0 || r.Text != " "+tt.label+" " {
			t.Errorf("%q: the trust run = %+v, want it first at column 0", tt.level, r)
		}
		wantStyle := frame.StyleDefault
		if tt.warn {
			wantStyle = frame.StyleWarning
		}
		if r.Style != wantStyle {
			t.Errorf("%q: trust run style = %v, want %v", tt.level, r.Style, wantStyle)
		}
	}
}

// TestStatusRowTrustPillIsClickable checks the click routing: a press on the pill opens
// the Workspace Trust dialog, the same surface the palette command opens.
func TestStatusRowTrustPillIsClickable(t *testing.T) {
	t.Parallel()
	p := &fakeTrustPort{state: trust.State{Root: "/ws", Level: trust.Restricted, Decided: true}}
	m := newTrustModel(t, p)
	r := statusTrustRun(t, m)
	l := m.layout()
	if got := l.Hit(r.X+1, l.StatusY); got.Kind != frame.HitStatusSegment || got.Index != frame.StatusSegmentTrust {
		t.Fatalf("hit at column %d = %+v, want the trust segment", r.X+1, got)
	}
	m.Update(click(r.X+1, l.StatusY, tea.MouseLeft))
	if len(m.overlays) == 0 {
		t.Fatal("a click on the trust pill opened no surface")
	}
	d := topTrustDialog(t, m)
	if !d.cancellable {
		t.Error("the dialog opened from the status pill is not cancellable")
	}
}

// TestStatusRowTrustPillPaintsTheWarningRole checks the paint: Restricted and Blocked
// take the theme's warning foreground on the bar's fill, Trusted keeps the bar's own
// style, as Fresh paints the trust pill with its WARNING role.
func TestStatusRowTrustPillPaintsTheWarningRole(t *testing.T) {
	t.Parallel()
	p := &fakeTrustPort{state: trust.State{Root: "/ws", Level: trust.Restricted, Decided: true}}
	m := newTrustModel(t, p)
	r := statusTrustRun(t, m)
	l := m.layout()
	got := m.paint().At(r.X+1, l.StatusY).Style
	want := style.Cell(m.styles.DiagWarning).FG
	if want != nil && got.FG != want {
		t.Errorf("restricted pill foreground = %v, want the theme's warning %v", got.FG, want)
	}
	if !got.Bold {
		t.Error("the restricted pill is not bold")
	}
	if barBG := style.Cell(m.styles.Status).BG; got.BG != barBG {
		t.Errorf("restricted pill background = %v, want the bar fill %v", got.BG, barBG)
	}

	p2 := &fakeTrustPort{state: trust.State{Root: "/ws", Level: trust.Trusted, Decided: true}}
	m2 := newTrustModel(t, p2)
	r2 := statusTrustRun(t, m2)
	l2 := m2.layout()
	got2 := m2.paint().At(r2.X+1, l2.StatusY).Style
	if want2 := style.Cell(m2.styles.Status).FG; got2.FG != want2 {
		t.Errorf("trusted pill foreground = %v, want the bar's %v", got2.FG, want2)
	}
}

// TestStatusRowHasNoTrustPillWithoutTrust checks the hidden states: no trust port (most
// tests, and a user without a config directory) or no workspace loaded paints no pill.
func TestStatusRowHasNoTrustPillWithoutTrust(t *testing.T) {
	t.Parallel()
	for _, m := range []*Model{
		newTrustModel(t, nil),
		newTrustModel(t, &fakeTrustPort{state: trust.State{}}),
	} {
		if got := m.statusView().Trust; got != "" {
			t.Errorf("trust pill = %q without a trust state, want none", got)
		}
		l := m.layout()
		for _, r := range l.Status {
			if r.Kind == frame.HitStatusSegment && r.Index == frame.StatusSegmentTrust {
				t.Fatalf("a trust run %+v is on the row without a trust state", r)
			}
		}
	}
}
