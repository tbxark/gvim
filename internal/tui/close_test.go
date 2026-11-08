package tui

import (
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/theme"
)

// orderLSP is a language-server port whose stop is recorded in log.
type orderLSP struct {
	*fakeLSP
	log *[]string
}

func (l orderLSP) Stop() error {
	*l.log = append(*l.log, "servers")
	return l.fakeLSP.Stop()
}

// orderTerm is a shell whose close is recorded in log.
type orderTerm struct {
	*fakeTerm
	log *[]string
}

func (o orderTerm) Close() error {
	*o.log = append(*o.log, "dock")
	return o.fakeTerm.Close()
}

// hungTerm is a shell whose close does not return until release is closed, as a child
// that will not die would not.
type hungTerm struct {
	*fakeTerm
	release chan struct{}
}

func (h hungTerm) Close() error {
	<-h.release
	return nil
}

// closeStarter hands out one shell.
type closeStarter struct {
	term Terminal
}

func (s closeStarter) Start(string, int, int) (Terminal, error) { return s.term, nil }

// newCloseModel returns a model with the dock open on term and the servers lsp. The dock
// is open, so Close has a shell to end.
func newCloseModel(t *testing.T, lsp LSPSession, term Terminal) *Model {
	t.Helper()
	m := New(Deps{
		Editor:   newFakeEditor("hello"),
		Explorer: newFakeTree(3),
		Keys:     dockKeys(),
		Theme:    theme.Theme{},
		Clock:    testClock,
		LSP:      lsp,
	})
	m.SetDock(DockPorts{Starter: closeStarter{term: term}})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	openDock(t, m)
	return m
}

// TestCloseStopsTheServersThenClosesTheDock checks the two steps of Close: the language
// servers stop first, then the shell closes.
func TestCloseStopsTheServersThenClosesTheDock(t *testing.T) {
	var log []string
	m := newCloseModel(t, orderLSP{fakeLSP: &fakeLSP{}, log: &log}, orderTerm{fakeTerm: newFakeTerm(), log: &log})
	m.Close()
	if want := []string{"servers", "dock"}; !slices.Equal(log, want) {
		t.Fatalf("exit steps = %v, want %v", log, want)
	}
}

// TestCloseGivesAHungShellItsOwnDeadline checks that a shell that will not close does
// not hold Close. The dock has its own deadline, so Close returns after it.
func TestCloseGivesAHungShellItsOwnDeadline(t *testing.T) {
	prev := dockCloseWait
	dockCloseWait = 10 * time.Millisecond
	t.Cleanup(func() { dockCloseWait = prev })
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	m := newCloseModel(t, nil, hungTerm{fakeTerm: newFakeTerm(), release: release})

	done := make(chan struct{})
	go func() {
		m.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close waited for the hung shell past its deadline")
	}
}
