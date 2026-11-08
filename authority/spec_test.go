package authority_test

import (
	"errors"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/authority"
)

// TestParseRemoteTargets checks both remote forms, their defaults and the line/column and
// colon-in-path rules.
func TestParseRemoteTargets(t *testing.T) {
	t.Setenv("USER", "envuser")
	t.Setenv("USERNAME", "")
	tests := []struct {
		name string
		arg  string
		want authority.RemoteSpec
	}{
		{"scp user", "deploy@server.example.com:/etc/nginx/nginx.conf",
			authority.RemoteSpec{User: "deploy", Host: "server.example.com", Path: "/etc/nginx/nginx.conf", UserExplicit: true}},
		{"scp home", "user@host:~", authority.RemoteSpec{User: "user", Host: "host", Path: "~", UserExplicit: true}},
		{"scp line", "user@host:/var/log/app.log:100",
			authority.RemoteSpec{User: "user", Host: "host", Path: "/var/log/app.log", Line: 100, UserExplicit: true}},
		{"scp line col", "user@host:/a/b.c:4:7",
			authority.RemoteSpec{User: "user", Host: "host", Path: "/a/b.c", Line: 4, Col: 7, UserExplicit: true}},
		{"scp colon in path", "user@host:/a/b:name:12",
			authority.RemoteSpec{User: "user", Host: "host", Path: "/a/b:name", Line: 12, UserExplicit: true}},
		{"scp host only", "host.example:/srv/x",
			authority.RemoteSpec{Host: "host.example", Path: "/srv/x"}},
		{"scp localhost", "localhost:~/x", authority.RemoteSpec{Host: "localhost", Path: "~/x"}},
		{"url default user", "ssh://host.example/etc/hosts",
			authority.RemoteSpec{User: "envuser", Host: "host.example", Path: "/etc/hosts"}},
		{"url user port", "ssh://deploy@server.example.com:2222/etc/nginx/nginx.conf",
			authority.RemoteSpec{User: "deploy", Host: "server.example.com", Port: 2222, Path: "/etc/nginx/nginx.conf", UserExplicit: true, PortExplicit: true}},
		{"url line col", "ssh://alice@host/home/alice/src/main.rs:42:7",
			authority.RemoteSpec{User: "alice", Host: "host", Path: "/home/alice/src/main.rs", Line: 42, Col: 7, UserExplicit: true}},
		{"url escaped space", "ssh://u@h/tmp/a%20b.txt",
			authority.RemoteSpec{User: "u", Host: "h", Path: "/tmp/a b.txt", UserExplicit: true}},
		{"url ipv6", "ssh://u@[::1]:2222/srv/x",
			authority.RemoteSpec{User: "u", Host: "::1", Port: 2222, Path: "/srv/x", UserExplicit: true, PortExplicit: true}},
		{"url root", "ssh://u@h/", authority.RemoteSpec{User: "u", Host: "h", Path: "/", UserExplicit: true}},
		{"scp numeric path with user", "u@h:42", authority.RemoteSpec{User: "u", Host: "h", Path: "42", UserExplicit: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok, err := authority.ParseTarget(tt.arg)
			if err != nil || !ok {
				t.Fatalf("ParseTarget(%q) = %+v, %v, %v", tt.arg, got, ok, err)
			}
			tt.want.Raw = tt.arg
			if got != tt.want {
				t.Fatalf("ParseTarget(%q) = %+v, want %+v", tt.arg, got, tt.want)
			}
		})
	}
}

// TestParseLocalTargets checks local FILE[:LINE[:COL]] arguments and windows-ish forms stay
// local.
func TestParseLocalTargets(t *testing.T) {
	for _, arg := range []string{
		"main.go", "main.go:42", "main.go:42:7", "C:/tmp/x.go", `C:\tmp\x.go`, "./a:b",
		"a:b", "dir/file:3", "host.example:42",
	} {
		if _, ok, err := authority.ParseTarget(arg); ok || err != nil {
			t.Errorf("ParseTarget(%q) = remote %v, err %v; want local", arg, ok, err)
		}
	}
}

// TestParseMalformedRemoteTargets checks malformed specs are startup errors, never local
// fallbacks.
func TestParseMalformedRemoteTargets(t *testing.T) {
	t.Setenv("USER", "")
	t.Setenv("USERNAME", "")
	for _, arg := range []string{
		"ssh://", "ssh://host", "ssh://:22/path", "ssh://user@/path", "ssh://@host/path",
		"ssh://host:0/path", "ssh://host:99999/path", "ssh://host:abc/path", "ssh://host/path%zz",
		"ssh://user@/path", "ssh://[::1/path",
		"user@host:", "@host:/path",
	} {
		_, ok, err := authority.ParseTarget(arg)
		if !ok || err == nil {
			t.Errorf("ParseTarget(%q) = remote %v, err %v; want a remote error", arg, ok, err)
		}
	}
}

// TestRemoteSpecHelpers checks display, workspace and the remote-command composition.
func TestRemoteSpecHelpers(t *testing.T) {
	s := authority.RemoteSpec{User: "u", Host: "h", Port: 2222, Path: "/srv/proj/a.go", Line: 3}
	if got := s.Display(); got != "u@h:2222" {
		t.Fatalf("Display = %q", got)
	}
	if got := (authority.RemoteSpec{User: "u", Host: "h"}).Display(); got != "u@h" {
		t.Fatalf("Display without port = %q", got)
	}
	if got := s.Workspace(); got != "/srv/proj" {
		t.Fatalf("Workspace = %q", got)
	}
	if got := (authority.RemoteSpec{Path: "~"}).Workspace(); got != "~" {
		t.Fatalf("Workspace home = %q", got)
	}
	if got := authority.QuoteDir("~/my project"); got != "\"$HOME\"/'my project'" {
		t.Fatalf("QuoteDir home = %q", got)
	}
	if got := authority.QuoteDir("~"); got != "\"$HOME\"" {
		t.Fatalf("QuoteDir bare home = %q", got)
	}
	if got := authority.QuoteDir("~alice/proj"); got != "~alice/'proj'" {
		t.Fatalf("QuoteDir user home = %q", got)
	}
	if got := authority.QuoteDir("/srv/a b"); got != authority.ShellQuote("/srv/a b") {
		t.Fatalf("QuoteDir = %q", got)
	}
	cmd := authority.RemoteCommand([]string{"git", "status"}, "/srv/proj", map[string]string{"GIT_DIR": "/x/.git"})
	want := "cd " + authority.ShellQuote("/srv/proj") + " && env " + authority.ShellQuote("GIT_DIR=/x/.git") + " " +
		authority.ShellQuote("git") + " " + authority.ShellQuote("status")
	if cmd != want {
		t.Fatalf("RemoteCommand = %q, want %q", cmd, want)
	}
	if got := authority.ShellQuote("a'b"); got != `'a'\''b'` {
		t.Fatalf("ShellQuote = %q", got)
	}
	if !strings.HasPrefix(authority.RemoteCommand([]string{"x"}, "", nil), authority.ShellQuote("x")) {
		t.Fatal("RemoteCommand without dir/env")
	}
}

// TestQuoteDirTildeExpandsInRealShell runs the quoting through a real sh: the leading
// tilde (and ~user) must expand while the quoted tail keeps spaces, quotes and command
// substitutions literal (no injection). The earlier ~'/proj' form did NOT expand.
func TestQuoteDirTildeExpandsInRealShell(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	home := t.TempDir()
	pwn := filepath.Join(t.TempDir(), "pwn")
	tails := []string{"proj", "a b", "quote's file", "$(touch " + pwn + ") x", "semi;colon & pipe|"}
	for _, tail := range tails {
		t.Run(tail, func(t *testing.T) {
			dir := filepath.Join(home, tail)
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			for _, dirArg := range []string{"~", "~/" + tail} {
				cmd := exec.Command(sh, "-c", "printf '%s\\n' "+authority.QuoteDir(dirArg))
				cmd.Env = append(os.Environ(), "HOME="+home)
				out, err := cmd.Output()
				if err != nil {
					t.Fatalf("sh for %q: %v (%s)", dirArg, err, out)
				}
				want := home
				if dirArg != "~" {
					want = filepath.Join(home, tail)
				}
				if got := strings.TrimSuffix(string(out), "\n"); got != want {
					t.Fatalf("QuoteDir(%q) expanded to %q, want %q", dirArg, got, want)
				}
			}
		})
	}
	if _, err := os.Stat(pwn); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a quoted tail executed command substitution: %v", err)
	}
	// ~user + a tail with a space: the user word stays expandable and the tail literal.
	u, err := user.Current()
	if err != nil || u.Username == "" || u.HomeDir == "" {
		t.Skip("no passwd entry for the current user")
	}
	cmd := exec.Command(sh, "-c", "printf '%s\\n' "+authority.QuoteDir("~"+u.Username+"/a b"))
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSuffix(string(out), "\n"); got != filepath.Join(u.HomeDir, "a b") {
		t.Fatalf("~user expansion = %q", got)
	}
}
