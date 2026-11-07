package plugintest

import (
	"strconv"
	"strings"

	"github.com/tbxark/gvim/internal/pluginapi"
)

// The key part of the fake host (stage S14): the key capture of Host.WaitKey and the mode
// contexts Host.Holds answers. The registrations themselves (SetBindings, SetModes,
// SetContext, SetStatusItems, SetInputMode) live in fakehost.go.

// KeyWait is one pending Host.WaitKey call.
type KeyWait struct {
	Token     pluginapi.Token
	TimeoutMS int
}

// FakeKeys is the state behind FakeHost.WaitKey: at most one capture is pending, as in the
// ui.
type FakeKeys struct {
	Pending *KeyWait
	// Cancelled are the tokens CancelWait was called with.
	Cancelled []pluginapi.Token
	// Err makes WaitKey fail with it (the capture is not recorded).
	Err error
}

// Keys returns the key capture state of the fake.
func (f *FakeHost) Keys() *FakeKeys {
	k, ok := f.Ext["keys"].(*FakeKeys)
	if !ok {
		k = &FakeKeys{}
		f.Ext["keys"] = k
	}
	return k
}

// WaitKey records the capture; a second one while one is pending is busy.
func (f *FakeHost) WaitKey(t pluginapi.Token, timeoutMS int) error {
	f.rec("WaitKey", t, timeoutMS)
	k := f.Keys()
	switch {
	case k.Err != nil:
		return k.Err
	case k.Pending != nil:
		return pluginapi.Errf(pluginapi.CodeBusy, "another key wait is pending")
	}
	k.Pending = &KeyWait{Token: t, TimeoutMS: timeoutMS}
	return nil
}

// cancelKeyWait forgets a pending capture; it is registered with OnCancelWait, which
// FakeHost.CancelWait (fake_ui.go) calls.
func cancelKeyWait(f *FakeHost, t pluginapi.Token) {
	k := f.Keys()
	k.Cancelled = append(k.Cancelled, t)
	if k.Pending != nil && k.Pending.Token == t {
		k.Pending = nil
	}
}

// modeHolds answers the "mode:<plugin>:<name>" when-clause names from the input modes the
// plugins entered, as the ui does.
func (f *FakeHost) modeHolds(name string) bool {
	rest, ok := strings.CutPrefix(name, "mode:")
	if !ok {
		return false
	}
	plugin, mode, ok := strings.Cut(rest, ":")
	return ok && mode != "" && f.InputMode[plugin] == mode
}

// PressKey delivers k to the pending key capture, as the ui does for the next key press,
// and settles. It fails the test when no capture is pending.
func (h *Harness) PressKey(k pluginapi.Key) {
	h.T.Helper()
	w := h.Host.Keys().Pending
	if w == nil {
		h.T.Fatal("plugintest: PressKey with no pending key wait")
	}
	h.Host.Keys().Pending = nil
	h.Resolve(pluginapi.Completion{Token: w.Token, Values: []any{k}})
}

func init() {
	OnCancelWait(cancelKeyWait)
	RegisterResourceKind(func(f *FakeHost) []string {
		if w := f.Keys().Pending; w != nil {
			return []string{"keywait:" + strconv.FormatUint(uint64(w.Token), 10)}
		}
		return nil
	})
}

// FailKeyWait ends the pending key capture with err, as a timeout or the panic chord would
// ("timeout: ..." or "cancelled: ..."), and settles.
func (h *Harness) FailKeyWait(err string) {
	h.T.Helper()
	w := h.Host.Keys().Pending
	if w == nil {
		h.T.Fatal("plugintest: FailKeyWait with no pending key wait")
	}
	h.Host.Keys().Pending = nil
	h.Resolve(pluginapi.Completion{Token: w.Token, Err: err})
}
