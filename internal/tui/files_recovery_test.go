package tui

import (
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/theme"
)

// fakeRecovery counts the plans and the writes of the crash-recovery copy.
type fakeRecovery struct {
	plans, writes int
	err           error
}

func (f *fakeRecovery) Plan() func() error {
	f.plans++
	return func() error {
		f.writes++
		return f.err
	}
}

func newRecoveryModel(t *testing.T, rec RecoveryPort, every time.Duration) *Model {
	t.Helper()
	m := New(Deps{
		Editor: newFakeEditor("x"), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock,
		Root: RootPorts{Recovery: rec, RecoveryEvery: every},
	})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return m
}

func TestRecoveryTickIsArmedOnlyWithAPortAndAnInterval(t *testing.T) {
	t.Parallel()
	if newRecoveryModel(t, &fakeRecovery{}, time.Second).recoveryCmd() == nil {
		t.Error("no tick with a port and an interval")
	}
	if newRecoveryModel(t, &fakeRecovery{}, 0).recoveryCmd() != nil {
		t.Error("a tick without an interval")
	}
	if newRecoveryModel(t, nil, time.Second).recoveryCmd() != nil {
		t.Error("a tick without a port")
	}
}

func TestRecoveryTickPlansOnTheLoopAndWritesInACommand(t *testing.T) {
	t.Parallel()
	f := &fakeRecovery{}
	m := newRecoveryModel(t, f, 0)
	_, cmd := m.Update(recoveryTickMsg{})
	if f.plans != 1 || f.writes != 0 {
		t.Fatalf("after the tick: plans %d, writes %d, want 1 and 0 (the write is the command)", f.plans, f.writes)
	}
	msgs := runCmd(cmd)
	if f.writes != 1 || len(msgs) != 1 {
		t.Fatalf("after the command: writes %d, messages %d, want 1 and 1", f.writes, len(msgs))
	}
	if _, ok := msgs[0].(recoveryDoneMsg); !ok {
		t.Fatalf("command result = %T, want recoveryDoneMsg", msgs[0])
	}
}

func TestRecoveryFailureIsShownOnce(t *testing.T) {
	t.Parallel()
	f := &fakeRecovery{err: errors.New("disk full")}
	m := newRecoveryModel(t, f, 0)
	m.Update(recoveryDoneMsg{err: f.err})
	want := "crash recovery copy failed: disk full"
	if m.notice != want {
		t.Fatalf("notice = %q, want %q", m.notice, want)
	}
	m.notice = ""
	m.Update(recoveryDoneMsg{err: f.err})
	if m.notice != "" {
		t.Fatalf("the same failure was shown again: %q", m.notice)
	}
	m.Update(recoveryDoneMsg{})
	m.Update(recoveryDoneMsg{err: f.err})
	if m.notice != want {
		t.Fatalf("a failure after a success was not shown: %q", m.notice)
	}
}
