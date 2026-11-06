//go:build unix

package term

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// pollTimeout bounds every wait on the child. Waits poll the screen until a
// condition holds; they never sleep for a fixed time.
const pollTimeout = 3 * time.Second

// startSh starts /bin/sh -c script in a 200x24 session. The width keeps long
// lines from wrapping. The session is closed when the test ends.
func startSh(t *testing.T, script string) *Session {
	t.Helper()
	s, err := Start(context.Background(), Options{
		Shell: "/bin/sh",
		Args:  []string{"-c", script},
		Cols:  200,
		Rows:  24,
	})
	if err != nil {
		t.Fatalf("Start(%q): %v", script, err)
	}
	t.Cleanup(func() { closeSession(t, s) })
	return s
}

// closeSession closes s and fails the test if Close does not return in time,
// so a wedged session fails the test instead of hanging the run.
func closeSession(t *testing.T, s *Session) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		_ = s.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(pollTimeout):
		t.Errorf("Close did not return within %v", pollTimeout)
	}
}

// waitFor polls the screen until ok reports true. Each snapshot is taken in its
// own goroutine, so a session that wedges on its own lock still fails the test
// at the deadline. When pollTimeout passes it fails the test with the last
// screen it saw.
func waitFor(t *testing.T, s *Session, what string, ok func(Screen) bool) {
	t.Helper()
	deadline := time.NewTimer(pollTimeout)
	defer deadline.Stop()
	var last Screen
	for {
		snap := make(chan Screen, 1)
		go func() { snap <- s.Screen() }()
		select {
		case sc := <-snap:
			last = sc
			if ok(sc) {
				return
			}
		case <-deadline.C:
			t.Fatalf("%s not seen within %v; last screen:\n%s", what, pollTimeout, screenText(last))
		}
		select {
		case <-time.After(5 * time.Millisecond):
		case <-deadline.C:
			t.Fatalf("%s not seen within %v; last screen:\n%s", what, pollTimeout, screenText(last))
		}
	}
}

// hasText reports a predicate that is true when sub appears on the screen.
func hasText(sub string) func(Screen) bool {
	return func(sc Screen) bool { return strings.Contains(screenText(sc), sub) }
}

// screenText renders the screen as newline-separated rows of runes.
func screenText(sc Screen) string {
	var b strings.Builder
	for r := 0; r < sc.Rows; r++ {
		for c := 0; c < sc.Cols; c++ {
			ch := sc.Cell(r, c).Rune
			if ch == 0 {
				ch = ' '
			}
			b.WriteRune(ch)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func TestStartShowsChildOutput(t *testing.T) {
	s := startSh(t, `printf 'term-%s\n' marker-7f3a; exec cat`)
	waitFor(t, s, "child output", hasText("term-marker-7f3a"))
}

func TestWriteReachesChild(t *testing.T) {
	s := startSh(t, `exec cat`)
	const line = "echo-9q\n"
	n, err := s.Write([]byte(line))
	if err != nil || n != len(line) {
		t.Fatalf("Write(%q) = %d, %v; want %d, nil", line, n, err, len(line))
	}
	// The tty echoes the typed line once. cat prints it a second time only if
	// it received the line, so two copies show that the child got the input.
	waitFor(t, s, "echoed and repeated input", func(sc Screen) bool {
		return strings.Count(screenText(sc), "echo-9q") >= 2
	})
}

// writeWithin writes p and fails the test if Write does not return within d.
func writeWithin(t *testing.T, s *Session, p []byte, d time.Duration) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		_, err := s.Write(p)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Write: %v", err)
		}
	case <-time.After(d):
		t.Fatalf("Write did not return within %v", d)
	}
}

// backlog is far more than a PTY input queue holds.
var backlog = bytes.Repeat([]byte("x"), 256<<10)

func TestWriteDoesNotBlockOnChildThatIgnoresInput(t *testing.T) {
	// Non-canonical mode makes the line discipline apply backpressure, so a
	// blocking write would stall once the input queue is full.
	s := startSh(t, `stty -icanon -echo; printf 'ready\n'; exec sleep 30`)
	waitFor(t, s, "child in raw mode", hasText("ready"))
	writeWithin(t, s, backlog, 2*time.Second)
}

func TestOutputKeepsFlowingWhileInputIsBacklogged(t *testing.T) {
	// The child never reads its input. It asks for the cursor position 20
	// times, so the emulator has replies to deliver while the input queue is
	// full. The replies must be queued, or the emulator blocks and the final
	// marker never reaches the screen.
	s := startSh(t, `stty -icanon -echo; printf 'ready\n'; i=0; `+
		`while [ $i -lt 20 ]; do i=$((i+1)); printf '\033[6n'; sleep 0.01; done; `+
		`printf 'loop-done\n'; exec sleep 30`)
	waitFor(t, s, "child in raw mode", hasText("ready"))
	writeWithin(t, s, backlog, 2*time.Second)
	waitFor(t, s, "output after the input backlog", hasText("loop-done"))
}

func TestTerminalQueryGetsReply(t *testing.T) {
	// The child asks for the cursor position (CSI 6 n). The emulator answers
	// with CSI 1;1 R, which must reach the child as input. Without the reply
	// path the child never sees the answer and never prints cpr-ok.
	script := `stty -icanon -echo; printf '\033[6n'; ` +
		`reply=$(dd bs=1 count=6 2>/dev/null); ` +
		`if [ "$reply" = "$(printf '\033[1;1R')" ]; then printf 'cpr-ok\n'; fi; exec cat`
	s := startSh(t, script)
	waitFor(t, s, "cursor position reply", hasText("cpr-ok"))
}

func TestStartHonoursDir(t *testing.T) {
	dir := t.TempDir()
	// pwd -P prints the physical path, so compare against the resolved dir.
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", dir, err)
	}
	s, err := Start(context.Background(), Options{
		Shell: "/bin/sh",
		Args:  []string{"-c", "pwd -P; exec cat"},
		Dir:   dir,
		Cols:  200,
		Rows:  24,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { closeSession(t, s) })
	waitFor(t, s, "working directory", hasText(want))
}

func TestStartPassesEnv(t *testing.T) {
	tests := []struct {
		name   string
		env    []string
		script string
		want   string
	}{
		{
			name:   "custom variable",
			env:    []string{"GVIM_TERM_TEST=ok-5c1"},
			script: `printf 'val-%s\n' "$GVIM_TERM_TEST"; exec cat`,
			want:   "val-ok-5c1",
		},
		{
			name:   "TERM defaults to xterm-256color",
			script: `printf 'term-%s\n' "$TERM"; exec cat`,
			want:   "term-xterm-256color",
		},
		{
			name:   "Env overrides the default TERM",
			env:    []string{"TERM=vt100"},
			script: `printf 'term-%s\n' "$TERM"; exec cat`,
			want:   "term-vt100",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := Start(context.Background(), Options{
				Shell: "/bin/sh",
				Args:  []string{"-c", tt.script},
				Env:   tt.env,
				Cols:  200,
				Rows:  24,
			})
			if err != nil {
				t.Fatalf("Start: %v", err)
			}
			t.Cleanup(func() { closeSession(t, s) })
			waitFor(t, s, "environment value", hasText(tt.want))
		})
	}
}

func TestStartErrors(t *testing.T) {
	tests := []struct {
		name      string
		opts      Options
		cancelled bool
	}{
		{name: "absolute path that does not exist", opts: Options{Shell: "/nonexistent/gvim-shell"}},
		{name: "bare name not on PATH", opts: Options{Shell: "gvim-no-such-shell-7f3a"}},
		{name: "working directory that does not exist", opts: Options{Shell: "/bin/sh", Dir: "/nonexistent/gvim-dir"}},
		{name: "context already cancelled", opts: Options{Shell: "/bin/sh"}, cancelled: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.cancelled {
				cancel()
			}
			s, err := Start(ctx, tt.opts)
			if err == nil {
				_ = s.Close()
				t.Fatal("Start() error = nil, want an error")
			}
			if s != nil {
				t.Fatalf("Start() session = %v with error %v, want nil session", s, err)
			}
		})
	}
}

func TestDefaultShellComesFromEnv(t *testing.T) {
	tests := []struct {
		name     string
		shellEnv string
		wantErr  bool
	}{
		{name: "SHELL names a missing program", shellEnv: "/nonexistent/gvim-shell", wantErr: true},
		{name: "SHELL unset falls back to /bin/sh", shellEnv: "", wantErr: false},
		{name: "SHELL is /bin/sh", shellEnv: "/bin/sh", wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SHELL", tt.shellEnv)
			s, err := Start(context.Background(), Options{
				Args: []string{"-c", "printf 'fallback-%s\\n' ok; exec cat"},
				Cols: 200,
				Rows: 24,
			})
			if tt.wantErr {
				if err == nil {
					_ = s.Close()
					t.Fatal("Start() error = nil, want an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Start: %v", err)
			}
			t.Cleanup(func() { closeSession(t, s) })
			waitFor(t, s, "fallback shell output", hasText("fallback-ok"))
		})
	}
}

func TestResizeChangesScreenAndChildSize(t *testing.T) {
	s := startSh(t, `read line; stty size; exec cat`)
	if err := s.Resize(100, 30); err != nil {
		t.Fatalf("Resize(100, 30): %v", err)
	}
	sc := s.Screen()
	if sc.Cols != 100 || sc.Rows != 30 {
		t.Fatalf("Screen() size = %dx%d, want 100x30", sc.Cols, sc.Rows)
	}
	// stty runs after the resize, so the child reports the new size.
	if _, err := s.Write([]byte("go\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	waitFor(t, s, "child window size", hasText("30 100"))
}

func TestResizeRejectsInvalidSize(t *testing.T) {
	tests := []struct {
		name       string
		cols, rows int
	}{
		{name: "zero columns", cols: 0, rows: 24},
		{name: "zero rows", cols: 80, rows: 0},
		{name: "negative columns", cols: -1, rows: 24},
		{name: "columns beyond uint16", cols: 70000, rows: 24},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := startSh(t, `exec cat`)
			if err := s.Resize(tt.cols, tt.rows); err == nil {
				t.Fatalf("Resize(%d, %d) error = nil, want an error", tt.cols, tt.rows)
			}
			if sc := s.Screen(); sc.Cols != 200 || sc.Rows != 24 {
				t.Fatalf("Screen() size = %dx%d after rejected resize, want 200x24", sc.Cols, sc.Rows)
			}
		})
	}
}

func TestCloseEndsChild(t *testing.T) {
	s, err := Start(context.Background(), Options{Shell: "/bin/sh", Args: []string{"-c", "exec cat"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}
	select {
	case <-s.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Done() not closed within 2s of Close")
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close() = %v, want nil", err)
	}
}

func TestUseAfterCloseFails(t *testing.T) {
	tests := []struct {
		name string
		op   func(s *Session) error
	}{
		{name: "Write", op: func(s *Session) error {
			_, err := s.Write([]byte("x"))
			return err
		}},
		{name: "Resize", op: func(s *Session) error { return s.Resize(80, 24) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := Start(context.Background(), Options{Shell: "/bin/sh", Args: []string{"-c", "exec cat"}})
			if err != nil {
				t.Fatalf("Start: %v", err)
			}
			if err := s.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
			if err := tt.op(s); err == nil {
				t.Fatalf("%s after Close error = nil, want an error", tt.name)
			}
		})
	}
}

func TestContextCancelClosesSession(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := Start(ctx, Options{Shell: "/bin/sh", Args: []string{"-c", "exec cat"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	cancel()
	select {
	case <-s.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Done() not closed within 2s of context cancellation")
	}
	_ = s.Close()
}

func TestWaitReportsExitStatus(t *testing.T) {
	s := startSh(t, `exit 3`)
	select {
	case <-s.Done():
	case <-time.After(pollTimeout):
		t.Fatalf("Done() not closed within %v after the child exited", pollTimeout)
	}
	err := s.Wait()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 3 {
		t.Fatalf("Wait() = %v, want an exit error with code 3", err)
	}
}

// styleFlags is the subset of Style that tests compare: the boolean
// attributes, and whether a colour is set at all.
type styleFlags struct {
	bold, underline, reverse, fg, bg bool
}

func flagsOf(st Style) styleFlags {
	return styleFlags{
		bold:      st.Bold,
		underline: st.Underline,
		reverse:   st.Reverse,
		fg:        st.Fg != nil,
		bg:        st.Bg != nil,
	}
}

func TestScreenCellStyle(t *testing.T) {
	tests := []struct {
		name string
		sgr  string
		want styleFlags
	}{
		{name: "plain", sgr: "", want: styleFlags{}},
		{name: "bold", sgr: "\x1b[1m", want: styleFlags{bold: true}},
		{name: "underline", sgr: "\x1b[4m", want: styleFlags{underline: true}},
		{name: "reverse", sgr: "\x1b[7m", want: styleFlags{reverse: true}},
		{name: "foreground", sgr: "\x1b[31m", want: styleFlags{fg: true}},
		{name: "background", sgr: "\x1b[42m", want: styleFlags{bg: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := startSh(t, "printf '%s' '"+tt.sgr+"X'; exec cat")
			waitFor(t, s, "styled X", func(sc Screen) bool { return sc.Cell(0, 0).Rune == 'X' })
			if got := flagsOf(s.Screen().Cell(0, 0).Style); got != tt.want {
				t.Fatalf("style flags = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestAppCursorKeysFollowDECCKM checks that AppCursorKeys reports the application
// cursor key mode: on after the child sets DECSET 1, and off again after DECRST 1.
func TestAppCursorKeysFollowDECCKM(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   bool
	}{
		{name: "off until the child sets it", script: "printf 'ready\\n'", want: false},
		{name: "on after DECSET 1", script: "printf '\\033[?1h'; printf 'ready\\n'", want: true},
		{name: "off again after DECRST 1", script: "printf '\\033[?1h\\033[?1l'; printf 'ready\\n'", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := startSh(t, tt.script+"; exec cat")
			// The mode is set by output before "ready", so once "ready" shows the mode is current.
			waitFor(t, s, "ready", hasText("ready"))
			if got := s.AppCursorKeys(); got != tt.want {
				t.Fatalf("AppCursorKeys() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestDoneClosesWhileABackgroundJobHoldsThePTY checks that Done announces the child's
// exit while a background job is still running on the PTY. Only Done is checked: on
// some platforms Updates closes with the shell, so output does not show whether the
// job still holds the PTY.
func TestDoneClosesWhileABackgroundJobHoldsThePTY(t *testing.T) {
	s := startSh(t, "sleep 3 & exit 0")
	select {
	case <-s.Done():
	case <-time.After(pollTimeout):
		t.Fatalf("Done() not closed within %v after the child exited", pollTimeout)
	}
}
