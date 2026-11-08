package plugintest

import (
	"fmt"

	"github.com/tbxark/gvim/pluginapi"
)

// The prompt, picker and popup part of the fake host (stage S13). The fake keeps every
// request, and the overlays still open; a test answers one by resolving its token
// (Harness.Resolve) the way the ui does.
//
// Like the real ui it does not know which plugin owns an overlay: an overlay goes away
// when the runtime closes it (ClosePicker, ClosePopup, CancelWait), so a leak after an
// unload shows up in Resources.

// FakeOverlay is one prompt, picker or popup a plugin asked the host for.
type FakeOverlay struct {
	Kind  string // "input", "confirm", "picker" or "popup"
	Token pluginapi.Token
	ID    uint64 // picker and popup id; 0 for a prompt

	Input   pluginapi.InputSpec
	Confirm pluginapi.ConfirmSpec
	Picker  pluginapi.PickerSpec
	Popup   pluginapi.PopupSpec

	// Updates are the UpdatePicker calls.
	Updates []pluginapi.PickerUpdate
	// Open is false once the overlay was closed (by the runtime, or by CloseOverlay).
	Open bool
}

// FakeUI is the state of the prompt, picker and popup fake.
type FakeUI struct {
	// Overlays are all requests, in order.
	Overlays []*FakeOverlay
	// Cancelled are the tokens of CancelWait calls, in order.
	Cancelled []pluginapi.Token
	// Reject, when set, is returned by Input, Confirm, OpenPicker and ShowPopup (the
	// request is not recorded).
	Reject error
}

// UI returns the prompt/picker/popup state of the fake host.
func (f *FakeHost) UI() *FakeUI {
	u, ok := f.Ext["ui"].(*FakeUI)
	if !ok {
		u = &FakeUI{}
		f.Ext["ui"] = u
	}
	return u
}

// Opened returns the overlays that are open, oldest first.
func (u *FakeUI) Opened() []*FakeOverlay {
	var out []*FakeOverlay
	for _, o := range u.Overlays {
		if o.Open {
			out = append(out, o)
		}
	}
	return out
}

// Last returns the most recent overlay of the kind ("" for any), open or not; nil when
// there is none.
func (u *FakeUI) Last(kind string) *FakeOverlay {
	for i := len(u.Overlays) - 1; i >= 0; i-- {
		if kind == "" || u.Overlays[i].Kind == kind {
			return u.Overlays[i]
		}
	}
	return nil
}

// ByID returns the picker or popup with the id (the latest one when an id was reused).
func (u *FakeUI) ByID(id uint64) *FakeOverlay {
	for i := len(u.Overlays) - 1; i >= 0; i-- {
		if u.Overlays[i].ID == id {
			return u.Overlays[i]
		}
	}
	return nil
}

func (f *FakeHost) addOverlay(o *FakeOverlay) error {
	u := f.UI()
	if u.Reject != nil {
		return u.Reject
	}
	o.Open = true
	u.Overlays = append(u.Overlays, o)
	return nil
}

// Input records the prompt.
func (f *FakeHost) Input(t pluginapi.Token, o pluginapi.InputSpec) error {
	f.rec("Input", t, o.Prompt)
	return f.addOverlay(&FakeOverlay{Kind: "input", Token: t, Input: o})
}

// Confirm records the question.
func (f *FakeHost) Confirm(t pluginapi.Token, o pluginapi.ConfirmSpec) error {
	f.rec("Confirm", t, o.Message)
	return f.addOverlay(&FakeOverlay{Kind: "confirm", Token: t, Confirm: o})
}

// OpenPicker records the picker.
func (f *FakeHost) OpenPicker(t pluginapi.Token, id uint64, o pluginapi.PickerSpec) error {
	f.rec("OpenPicker", t, id, o.Title)
	return f.addOverlay(&FakeOverlay{Kind: "picker", Token: t, ID: id, Picker: o})
}

// UpdatePicker records the update and applies it to the open picker's spec.
func (f *FakeHost) UpdatePicker(id uint64, u pluginapi.PickerUpdate) error {
	f.rec("UpdatePicker", id)
	o := f.UI().ByID(id)
	if o == nil || !o.Open || o.Kind != "picker" {
		return pluginapi.Errf(pluginapi.CodeClosed, "picker %d is not open", id)
	}
	o.Updates = append(o.Updates, u)
	if u.Title != nil {
		o.Picker.Title = *u.Title
	}
	if u.Footer != nil {
		o.Picker.Footer = *u.Footer
	}
	if u.Items != nil {
		o.Picker.Items = *u.Items
	}
	return nil
}

// ClosePicker closes the picker without resolving anything.
func (f *FakeHost) ClosePicker(id uint64) {
	f.rec("ClosePicker", id)
	if o := f.UI().ByID(id); o != nil && o.Kind == "picker" {
		o.Open = false
	}
}

// ShowPopup records the popup.
func (f *FakeHost) ShowPopup(t pluginapi.Token, id uint64, o pluginapi.PopupSpec) error {
	f.rec("ShowPopup", t, id, o.Title)
	return f.addOverlay(&FakeOverlay{Kind: "popup", Token: t, ID: id, Popup: o})
}

// ClosePopup closes the popup without resolving anything.
func (f *FakeHost) ClosePopup(id uint64) {
	f.rec("ClosePopup", id)
	if o := f.UI().ByID(id); o != nil && o.Kind == "popup" {
		o.Open = false
	}
}

// CancelWait closes the overlay that waits on t, as the ui does, and tells the functions
// registered with OnCancelWait (areas that keep waits of their own, such as key waits).
func (f *FakeHost) CancelWait(t pluginapi.Token) {
	f.rec("CancelWait", t)
	u := f.UI()
	u.Cancelled = append(u.Cancelled, t)
	for _, o := range u.Overlays {
		if o.Token == t {
			o.Open = false
		}
	}
	for _, g := range cancelWaitHooks {
		g(f, t)
	}
}

var cancelWaitHooks []func(f *FakeHost, t pluginapi.Token)

// OnCancelWait registers a function CancelWait calls, so an area's fake can drop the wait
// it holds for t. Call it from init.
func OnCancelWait(g func(f *FakeHost, t pluginapi.Token)) {
	cancelWaitHooks = append(cancelWaitHooks, g)
}

func init() {
	RegisterResourceKind(func(f *FakeHost) []string {
		var out []string
		for _, o := range f.UI().Opened() {
			out = append(out, fmt.Sprintf("overlay:%s/%d", o.Kind, o.Token))
		}
		return out
	})
}
