package cli

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/app"
)

// TestDaemonFlags checks the --daemon/--attach parse: the optional NAME is only
// accepted in the = form, an omitted name means the per-root default, and the
// ambiguous combinations are refused.
func TestDaemonFlags(t *testing.T) {
	isolateHome(t)
	tests := []struct {
		name     string
		args     []string
		mode     app.DaemonMode
		sockName string
		files    int
		wantErr  string
	}{
		{name: "daemon default name", args: []string{"--daemon"}, mode: app.DaemonServe},
		{name: "daemon named", args: []string{"--daemon=work"}, mode: app.DaemonServe, sockName: "work"},
		{name: "attach short", args: []string{"-a"}, mode: app.DaemonAttach},
		{name: "attach named", args: []string{"--attach=work"}, mode: app.DaemonAttach, sockName: "work"},
		{name: "attach named short equals", args: []string{"-a=work"}, mode: app.DaemonAttach, sockName: "work"},
		{name: "attach with a file", args: []string{"-a", "a.go"}, mode: app.DaemonAttach, files: 1},
		{name: "attach space name is a file", args: []string{"-a", "work"}, mode: app.DaemonAttach, files: 1},
		{name: "daemon with files", args: []string{"--daemon", "a.go"}, wantErr: "--daemon opens no files"},
		{name: "daemon and attach", args: []string{"--daemon", "-a"}, wantErr: "cannot be combined"},
		{name: "daemon with stdin", args: []string{"--daemon", "--stdin"}, wantErr: "cannot be combined with --stdin"},
		{name: "daemon with simple", args: []string{"--daemon", "--simple"}, wantErr: "cannot be combined with --simple"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &recorder{}
			code, _, stderr := execute(t, tt.args, rec)
			if tt.wantErr != "" {
				if code == 0 || !strings.Contains(stderr, tt.wantErr) {
					t.Fatalf("execute(%v) = %d %q, want error containing %q", tt.args, code, stderr, tt.wantErr)
				}
				return
			}
			if code != 0 {
				t.Fatalf("execute(%v) = %d, stderr %q", tt.args, code, stderr)
			}
			if rec.calls != 1 {
				t.Fatalf("runner called %d times, want 1", rec.calls)
			}
			if rec.got.Daemon == nil || rec.got.Daemon.Mode != tt.mode {
				t.Fatalf("daemon options = %+v, want mode %v", rec.got.Daemon, tt.mode)
			}
			if rec.got.Daemon.Name != tt.sockName {
				t.Errorf("socket name = %q, want %q", rec.got.Daemon.Name, tt.sockName)
			}
			if len(rec.got.Files) != tt.files {
				t.Errorf("files = %v, want %d", rec.got.Files, tt.files)
			}
		})
	}
}
