package app

import (
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/trust"
)

// TestRemoteTrustRecordKeyedByTarget checks a remote workspace decision is stored by the
// remote target (host/user/port plus workspace root): the same target reconnects/restarts
// with the recorded level and asks nothing, while another target has its own record.
func TestRemoteTrustRecordKeyedByTarget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trust.json")
	store := trust.NewStore(path)
	spec := &authority.RemoteSpec{User: "u", Host: "h", Port: 2222, Path: "/srv/proj/a.go"}
	key := remoteTrustKey(spec)
	if key != trust.RecordKey("u@h:2222:/srv/proj") {
		t.Fatalf("target key = %q", key)
	}
	state, err := store.OpenRecord(key, nil)
	if err != nil || !state.Decided || state.Level != trust.Trusted {
		t.Fatalf("first open = %+v, %v", state, err)
	}
	ctl := newTrustController(store, state)
	ctl.recordKey = key
	if _, err := ctl.Decide(trust.Restricted); err != nil {
		t.Fatalf("Decide: %v", err)
	}
	// A reconnect/restart of the same target reads the decision and needs no prompt.
	reopened := trust.NewStore(path)
	state2, err := reopened.OpenRecord(remoteTrustKey(&authority.RemoteSpec{User: "u", Host: "h", Port: 2222, Path: "/srv/proj/b.go"}), nil)
	if err != nil || !state2.Decided || state2.Level != trust.Restricted {
		t.Fatalf("same target after restart = %+v, %v; want the recorded restriction", state2, err)
	}
	// A different workspace on the same host is its own record.
	state3, err := reopened.OpenRecord(remoteTrustKey(&authority.RemoteSpec{User: "u", Host: "h", Port: 2222, Path: "/srv/other/a.go"}), nil)
	if err != nil || state3.Level != trust.Trusted {
		t.Fatalf("other target = %+v, %v; want its own markerless record", state3, err)
	}
	// The readable root shown by the TUI names the remote target, not a local path.
	display := "SSH:" + remoteTrustTarget(spec)
	if display != "SSH:u@h:2222:/srv/proj" {
		t.Fatalf("display target = %q", display)
	}
}
