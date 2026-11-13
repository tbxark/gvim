package tui

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/dialog"
)

type quitWorkspaceFake struct {
	fakeWorkspacePort
	models []*Model
	host   *WorkspaceHost
}

func (p *quitWorkspaceFake) QuitDirtyCount() int {
	n := 0
	for _, m := range p.models {
		for _, tab := range m.ed.Tabs() {
			if tab.Modified {
				n++
			}
		}
	}
	return n
}
func (p *quitWorkspaceFake) QuitNextDirty() (*Model, int) {
	for _, m := range p.models {
		for i, tab := range m.ed.Tabs() {
			if tab.Modified {
				return m, i
			}
		}
	}
	return nil, -1
}
func (p *quitWorkspaceFake) QuitActivate(m *Model) error { p.host.Show(m); return nil }
func (p *quitWorkspaceFake) QuitDiscardAll() error {
	for _, m := range p.models {
		if err := m.QuitDiscardModified(); err != nil {
			return err
		}
	}
	return nil
}

func TestWorkspaceQuitCancelAndDiscardCoverBackground(t *testing.T) {
	for _, choice := range []dialog.Choice{dialog.ChoiceCancel, dialog.ChoiceDiscard} {
		t.Run(fmt.Sprint(choice), func(t *testing.T) {
			aed, bed := newTabFake("a"), newTabFake("b")
			aed.dirty[0], bed.dirty[0] = true, true
			aed.sync()
			bed.sync()
			d := &fakeDialogs{}
			a, b := shellModel(t, aed, d), shellModel(t, bed, &fakeDialogs{})
			p := &quitWorkspaceFake{models: []*Model{a, b}, host: NewWorkspaceHost(a)}
			a.SetWorkspacePort(p)
			b.SetWorkspacePort(p)
			a.dispatch(Command{Action: "quit"})
			msgs := runCmd(answer(a, d, choice, ""))
			if choice == dialog.ChoiceCancel {
				if quitted(msgs) || len(aed.closed) != 0 || len(bed.closed) != 0 {
					t.Fatal("cancel changed workspaces")
				}
			} else if !quitted(msgs) || len(aed.closed) != 1 || len(bed.closed) != 1 {
				t.Fatal("discard did not close both workspaces and quit")
			}
		})
	}
}

func TestWorkspaceQuitConfirmsAllAndSavesBackground(t *testing.T) {
	first, second := newTabFake("a"), newTabFake("b")
	first.dirty[0], second.dirty[0] = true, true
	first.sync()
	second.sync()
	d := &fakeDialogs{}
	a, b := shellModel(t, first, d), shellModel(t, second, &fakeDialogs{})
	host := NewWorkspaceHost(a)
	p := &quitWorkspaceFake{models: []*Model{a, b}, host: host}
	a.SetWorkspacePort(p)
	b.SetWorkspacePort(p)
	a.dispatch(Command{Action: "quit"})
	if len(d.opened) != 1 || d.last(t).text != "2 modified files" {
		t.Fatalf("dialogs = %+v", d.opened)
	}
	cmd := answer(a, d, dialog.ChoiceSave, "")
	// The host routes each continuation to the relevant workspace model.
	quit := false
	for steps := 0; cmd != nil && steps < 12; steps++ {
		var next tea.Cmd
		for _, msg := range runCmd(cmd) {
			if _, ok := msg.(tea.QuitMsg); ok {
				quit = true
				continue
			}
			if _, ok := msg.(modelCmdMsg); !ok {
				msg = modelCmdMsg{owner: host.Active().WorkspaceOwner(), source: host.Active(), msg: msg}
			}
			_, c := host.Update(msg)
			next = tea.Batch(next, c)
		}
		cmd = next
	}
	if !quit {
		t.Fatal("quit was not delivered after saving")
	}
	if first.dirty[0] || second.dirty[0] {
		t.Fatalf("unsaved documents: %v %v", first.dirty, second.dirty)
	}
	if len(d.opened) != 1 {
		t.Fatalf("opened %d confirmations", len(d.opened))
	}
}
