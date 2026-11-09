package tui

import (
	"context"
	"errors"
	"testing"
)

// invalidateOverlay records InvalidatePreviews calls without owning the display.
type invalidateOverlay struct {
	fakeOverlay
	calls int
}

func (o *invalidateOverlay) InvalidatePreviews() { o.calls++ }

// TestAuthorityCarrierChangesInvalidatePreviews: a disconnect transition and a
// reconnect both drop cached remote preview reads; the overlay is not closed.
func TestAuthorityCarrierChangesInvalidatePreviews(t *testing.T) {
	m := newTestModel(t, newFakeEditor("x"), nil, 80, 24)
	o := &invalidateOverlay{fakeOverlay: fakeOverlay{open: true}}
	m.pushOverlay(o)
	connected := true
	m.SetAuthorityHooks(AuthorityHooks{
		Status: func() AuthorityStatus { return AuthorityStatus{Text: "SSH:h", Connected: connected} },
		Reconnect: func(context.Context, string) error {
			return nil
		},
	})
	m.Update(authorityPollTickMsg{})
	if o.calls != 0 {
		t.Fatalf("connected poll invalidated previews %d times", o.calls)
	}
	connected = false
	m.Update(authorityPollTickMsg{})
	if o.calls != 1 {
		t.Fatalf("disconnect transition invalidated previews %d times, want 1", o.calls)
	}
	connected = true
	m.Update(authorityReconnectedMsg{})
	if o.calls != 2 {
		t.Fatalf("reconnect invalidated previews %d times, want 2", o.calls)
	}
	// A reconnect marks the carrier connected even before the next poll; the immediately
	// following disconnect must invalidate again rather than being missed.
	connected = false
	m.Update(authorityPollTickMsg{})
	if o.calls != 3 {
		t.Fatalf("disconnect after reconnect invalidated previews %d times, want 3", o.calls)
	}
	if !o.Active() {
		t.Fatal("preview overlay was closed by cache invalidation")
	}
}

// TestAuthorityHooksSeedConnectionState: SetAuthorityHooks carries the current carrier
// connectivity into the preview-cache tracker, so a disconnect before the first poll is
// still detected.
func TestAuthorityHooksSeedConnectionState(t *testing.T) {
	m := newTestModel(t, newFakeEditor("x"), nil, 80, 24)
	o := &invalidateOverlay{fakeOverlay: fakeOverlay{open: true}}
	m.pushOverlay(o)
	connected := true
	m.SetAuthorityHooks(AuthorityHooks{
		Status:    func() AuthorityStatus { return AuthorityStatus{Text: "SSH:h", Connected: connected} },
		Reconnect: func(context.Context, string) error { return nil },
	})
	connected = false
	m.Update(authorityPollTickMsg{})
	if o.calls != 1 {
		t.Fatalf("first disconnected poll invalidated previews %d times, want 1", o.calls)
	}
}

// TestReconnectFailureClearsPreviewsImmediately: a focus-triggered reconnect clears old
// previews when the attempt starts, and a failed attempt cannot leave a pre-disconnect
// preview painted while waiting for the next poll.
func TestReconnectFailureClearsPreviewsImmediately(t *testing.T) {
	m := newTestModel(t, newFakeEditor("x"), nil, 80, 24)
	o := &invalidateOverlay{fakeOverlay: fakeOverlay{open: true}}
	m.pushOverlay(o)
	connected := true
	m.SetAuthorityHooks(AuthorityHooks{
		Status: func() AuthorityStatus { return AuthorityStatus{Text: "SSH:h", Connected: connected} },
		Reconnect: func(context.Context, string) error {
			return errors.New("dial failed")
		},
	})
	connected = false
	cmd := m.beginAuthorityReconnect()
	if cmd == nil {
		t.Fatal("beginAuthorityReconnect returned no command")
	}
	if o.calls != 1 {
		t.Fatalf("reconnect attempt start invalidated previews %d times, want 1", o.calls)
	}
	m.Update(cmd())
	if o.calls < 1 || m.authorityPreviewConnected {
		t.Fatalf("failed reconnect left calls=%d connected=%v, want previews cleared", o.calls, m.authorityPreviewConnected)
	}
}
