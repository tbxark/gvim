package cli

import (
	"testing"

	"github.com/tbxark/gvim/internal/app"
)

// TestDaemonRemoteTargetParses checks a remote target is accepted with --daemon (the
// jump-box start) and with --attach (a reattach ignores the target; the daemon owns SSH).
func TestDaemonRemoteTargetParses(t *testing.T) {
	isolateHome(t)
	rec := &recorder{}
	code, _, stderr := execute(t, []string{"--daemon=webserver", "deploy@server:/etc/nginx/nginx.conf"}, rec)
	if code != 0 {
		t.Fatalf("execute = %d, stderr %q", code, stderr)
	}
	if rec.got.Remote == nil || rec.got.Remote.Host != "server" || rec.got.Remote.Path != "/etc/nginx/nginx.conf" {
		t.Fatalf("remote target = %+v", rec.got.Remote)
	}
	if rec.got.Daemon == nil || rec.got.Daemon.Mode != app.DaemonServe || rec.got.Daemon.Name != "webserver" {
		t.Fatalf("daemon options = %+v", rec.got.Daemon)
	}
	attach := &recorder{}
	code, _, stderr = execute(t, []string{"--attach=webserver", "deploy@server:/etc/nginx/nginx.conf"}, attach)
	if code != 0 {
		t.Fatalf("attach execute = %d, stderr %q", code, stderr)
	}
	if attach.got.Daemon == nil || attach.got.Daemon.Mode != app.DaemonAttach {
		t.Fatalf("attach daemon options = %+v", attach.got.Daemon)
	}
	// The attach keeps parsing the target but Run's attach path ignores it; one daemon
	// socket owns SSH.
	if attach.got.Remote == nil {
		t.Fatal("the attach hid the parsed target; the CLI contract parses it and Run ignores it")
	}
}
