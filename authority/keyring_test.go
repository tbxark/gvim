package authority

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type keyringCall struct {
	name  string
	args  []string
	stdin []byte
}

type fakeKeyringRun struct {
	calls  []keyringCall
	out    []byte
	stderr []byte
	code   int
	err    error
}

func (f *fakeKeyringRun) run(ctx context.Context, name string, args []string, stdin []byte) ([]byte, []byte, int, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, 0, err
	}
	f.calls = append(f.calls, keyringCall{name: name, args: append([]string(nil), args...), stdin: append([]byte(nil), stdin...)})
	return f.out, f.stderr, f.code, f.err
}

func TestKeyringSecurityLookup(t *testing.T) {
	run := &fakeKeyringRun{out: []byte("pw\n")}
	store := &execPasswordStore{kind: keyringSecurity, tool: "/usr/bin/security", run: run.run}
	pw, ok, err := store.Lookup(context.Background(), "host", "user")
	if err != nil || !ok || pw != "pw" {
		t.Fatalf("lookup = %q, %v, %v", pw, ok, err)
	}
	if len(run.calls) != 1 {
		t.Fatalf("calls = %+v", run.calls)
	}
	call := run.calls[0]
	if call.name != "/usr/bin/security" || strings.HasPrefix(call.name, "sh") {
		t.Fatalf("lookup ran %q (want the tool directly, no shell)", call.name)
	}
	wantArgs := []string{"find-generic-password", "-s", keyringService, "-a", "user@host", "-w"}
	if strings.Join(call.args, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Fatalf("lookup argv = %v, want %v", call.args, wantArgs)
	}
	if call.stdin != nil {
		t.Fatalf("lookup sent stdin %q, want none", call.stdin)
	}
}

func TestKeyringSecurityMissAndErrors(t *testing.T) {
	store := &execPasswordStore{kind: keyringSecurity, tool: "security", run: (&fakeKeyringRun{code: 44, stderr: []byte("The specified item could not be found in the keychain.")}).run}
	if pw, ok, err := store.Lookup(context.Background(), "h", "u"); err != nil || ok || pw != "" {
		t.Fatalf("security miss = %q, %v, %v", pw, ok, err)
	}
	store = &execPasswordStore{kind: keyringSecurity, tool: "security", run: (&fakeKeyringRun{code: 1, stderr: []byte("keychain is locked")}).run}
	if _, _, err := store.Lookup(context.Background(), "h", "u"); err == nil || !strings.Contains(err.Error(), "keychain is locked") {
		t.Fatalf("security error = %v", err)
	}
	runErr := errors.New("cannot execute")
	store = &execPasswordStore{kind: keyringSecurity, tool: "security", run: (&fakeKeyringRun{err: runErr}).run}
	if _, _, err := store.Lookup(context.Background(), "h", "u"); !errors.Is(err, runErr) {
		t.Fatalf("security run error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := store.Lookup(ctx, "h", "u"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled lookup = %v", err)
	}
}

func TestKeyringSecurityStoreUsesArgv(t *testing.T) {
	run := &fakeKeyringRun{}
	store := &execPasswordStore{kind: keyringSecurity, tool: "/usr/bin/security", run: run.run}
	if err := store.Store(context.Background(), "host", "user", "secret pw"); err != nil {
		t.Fatal(err)
	}
	call := run.calls[0]
	wantPrefix := []string{"add-generic-password", "-U", "-s", keyringService, "-a", "user@host", "-w"}
	if strings.Join(call.args[:len(wantPrefix)], "\x00") != strings.Join(wantPrefix, "\x00") || call.args[len(call.args)-1] != "secret pw" {
		t.Fatalf("store argv = %v (the macOS password is passed as -w by design)", call.args)
	}
	if call.stdin != nil {
		t.Fatalf("security store stdin = %q, want none", call.stdin)
	}
}

func TestKeyringSecretToolLookupAndStore(t *testing.T) {
	lookup := &fakeKeyringRun{out: []byte("pw\n")}
	store := &execPasswordStore{kind: keyringSecretTool, tool: "/usr/bin/secret-tool", run: lookup.run}
	pw, ok, err := store.Lookup(context.Background(), "host", "user")
	if err != nil || !ok || pw != "pw" {
		t.Fatalf("secret-tool lookup = %q, %v, %v", pw, ok, err)
	}
	wantArgs := []string{"lookup", "service", keyringService, "account", "user@host"}
	if strings.Join(lookup.calls[0].args, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Fatalf("secret-tool lookup argv = %v", lookup.calls[0].args)
	}

	miss := &fakeKeyringRun{code: 1}
	store = &execPasswordStore{kind: keyringSecretTool, tool: "secret-tool", run: miss.run}
	if _, ok, err := store.Lookup(context.Background(), "h", "u"); ok || err != nil {
		t.Fatalf("secret-tool miss = %v, %v", ok, err)
	}

	save := &fakeKeyringRun{}
	store = &execPasswordStore{kind: keyringSecretTool, tool: "/usr/bin/secret-tool", run: save.run}
	if err := store.Store(context.Background(), "host", "user", "secret pw"); err != nil {
		t.Fatal(err)
	}
	call := save.calls[0]
	if string(call.stdin) != "secret pw" {
		t.Fatalf("secret-tool store stdin = %q, want the password only", call.stdin)
	}
	for _, arg := range call.args {
		if arg == "secret pw" {
			t.Fatalf("secret-tool store put the password in argv: %v", call.args)
		}
	}
	wantStoreArgs := []string{"store", "--label=gvim SSH password", "service", keyringService, "account", "user@host"}
	if strings.Join(call.args, "\x00") != strings.Join(wantStoreArgs, "\x00") {
		t.Fatalf("secret-tool store argv = %v", call.args)
	}
}

func TestKeyringAnswerPreservesPasswordBytes(t *testing.T) {
	for _, tc := range []struct {
		out  string
		want string
	}{
		{"pw\n", "pw"},
		{"a\n\n", "a\n"},
		{"b\r\n", "b\r"},
		{" \n", " "},
		{"trailing\x00nul", "trailing\x00nul"},
	} {
		got, ok := decodeKeyringAnswer([]byte(tc.out))
		if !ok || got != tc.want {
			t.Fatalf("decode %q = %q, %v; want %q", tc.out, got, ok, tc.want)
		}
	}
	if _, ok := decodeKeyringAnswer(nil); ok {
		t.Fatal("empty answer decoded as a password")
	}
}

func TestKeyringSelectAndNoop(t *testing.T) {
	lookPath := func(name string) (string, error) {
		if name == "security" || name == "secret-tool" {
			return "/fake/" + name, nil
		}
		return "", exec.ErrNotFound
	}
	for _, goos := range []string{"darwin", "linux"} {
		store := systemPasswordStoreFor(goos, lookPath, (&fakeKeyringRun{}).run)
		if _, ok := store.(*execPasswordStore); !ok {
			t.Fatalf("%s store = %T, want the tool-backed store", goos, store)
		}
	}
	missing := func(string) (string, error) { return "", exec.ErrNotFound }
	if store := systemPasswordStoreFor("darwin", missing, (&fakeKeyringRun{}).run); store != (NoopPasswordStore{}) {
		t.Fatalf("missing tool store = %T, want NoopPasswordStore", store)
	}
	noop := NoopPasswordStore{}
	if pw, ok, err := noop.Lookup(context.Background(), "h", "u"); pw != "" || ok || err != nil {
		t.Fatalf("noop lookup = %q, %v, %v", pw, ok, err)
	}
	if err := noop.Store(context.Background(), "h", "u", "pw"); !errors.Is(err, ErrPasswordStoreUnavailable) {
		t.Fatalf("noop store error = %v", err)
	}
}

func TestKeyringCommandBoundsAndNoShell(t *testing.T) {
	// The production runner uses the tool as argv directly; sh is only the test child here
	// to produce deterministic output and is never on the keyring path.
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	out, _, code, err := runKeyringCommand(context.Background(), sh, []string{"-c", "printf 'pw\\n'"}, nil)
	if err != nil || code != 0 || string(out) != "pw\n" {
		t.Fatalf("bounded command = %q, %d, %v", out, code, err)
	}
	if _, _, code, err := runKeyringCommand(context.Background(), sh, []string{"-c", "printf x; exit 3"}, nil); err != nil || code != 3 {
		t.Fatalf("exit-code command = %d, %v", code, err)
	}
	long := strings.Repeat("x", keyringMaxAnswer+1)
	if _, _, _, err := runKeyringCommand(context.Background(), sh, []string{"-c", "printf %s " + long}, nil); err == nil || !strings.Contains(err.Error(), "exceeded") {
		t.Fatalf("oversize command = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, _, _, err := runKeyringCommand(ctx, sh, []string{"-c", "sleep 2"}, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancelled command = %v", err)
	}
}
