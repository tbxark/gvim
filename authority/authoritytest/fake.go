// Package authoritytest provides an in-memory authority slot for tests: the four surfaces
// can be scripted or left refusing, and the connection state can be driven by hand.
package authoritytest

import (
	"context"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/plugin/hostos"
)

// Fake is a scriptable authority slot.
type Fake struct {
	FileSystem authority.FileSystem
	OneShotter authority.OneShot
	LongRunner authority.LongRunning
	Terminaler authority.Terminal

	LabelText      string
	Remote         *authority.RemoteSpec
	ConnectedState bool
	ReasonText     string
	ReconnectErr   error
	Reconnects     int
	Closed         bool
}

// New returns a fake whose surfaces refuse everything and whose state is connected.
func New() *Fake {
	return &Fake{
		FileSystem:     authority.UnsupportedFiles{},
		OneShotter:     authority.UnsupportedProcs{},
		LongRunner:     authority.UnsupportedProcs{},
		Terminaler:     authority.UnsupportedTerminal{},
		ConnectedState: true,
	}
}

// Disconnect marks the backend disconnected with a reason.
func (f *Fake) Disconnect(reason string) {
	f.ConnectedState = false
	f.ReasonText = reason
}

// Files implements authority.Slot.
func (f *Fake) Files() authority.FileSystem { return f.FileSystem }

// OneShot implements authority.Slot.
func (f *Fake) OneShot() authority.OneShot { return f.OneShotter }

// LongRunning implements authority.Slot.
func (f *Fake) LongRunning() authority.LongRunning { return f.LongRunner }

// Terminal implements authority.Slot.
func (f *Fake) Terminal() authority.Terminal { return f.Terminaler }

// Label implements authority.Slot.
func (f *Fake) Label() string { return f.LabelText }

// State implements authority.Slot.
func (f *Fake) State() authority.State {
	return authority.State{
		Kind: "fake", Label: f.LabelText, Remote: f.Remote,
		Connected: f.ConnectedState, Reason: f.ReasonText,
	}
}

// Connected implements authority.Slot.
func (f *Fake) Connected() bool { return f.ConnectedState }

// Reconnect implements authority.Slot.
func (f *Fake) Reconnect(context.Context) error {
	f.Reconnects++
	if f.ReconnectErr != nil {
		return f.ReconnectErr
	}
	f.ConnectedState, f.ReasonText = true, ""
	return nil
}

// Close implements authority.Slot.
func (f *Fake) Close() error {
	f.Closed = true
	return nil
}

var _ authority.Slot = (*Fake)(nil)
var _ = hostos.ProcOptions{}
