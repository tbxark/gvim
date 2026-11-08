package authoritytest

import (
	"context"
	"testing"

	"github.com/tbxark/gvim/authority"
)

// TestFakeAuthorityState checks the test double drives the state and reconnect hooks the
// TUI will read.
func TestFakeAuthorityState(t *testing.T) {
	f := New()
	f.LabelText = "SSH:user@host"
	f.Remote = &authority.RemoteSpec{User: "user", Host: "host", Path: "/srv"}
	if st := f.State(); !st.Connected || st.Label != "SSH:user@host" || st.Kind != "fake" {
		t.Fatalf("state = %+v", st)
	}
	f.Disconnect("connection lost")
	if st := f.State(); st.Connected || st.Reason != "connection lost" {
		t.Fatalf("disconnected state = %+v", st)
	}
	if err := f.Reconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.Reconnects != 1 || !f.Connected() || f.State().Reason != "" {
		t.Fatalf("reconnect = %d, connected %v", f.Reconnects, f.Connected())
	}
	if err := f.Close(); err != nil || !f.Closed {
		t.Fatalf("close = %+v, %v", f, err)
	}
	if _, err := f.Files().ReadFile(context.Background(), "/x", 1); err == nil {
		t.Fatal("the fake read a remote file")
	}
}
