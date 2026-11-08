package agentdiscover

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/tbxark/gvim/trust"
)

// newline is the line separator of command output.
var newline = string(rune(10))

// lines joins rows with newlines.
func lines(rows ...string) string { return strings.Join(rows, newline) }

// escaped builds one list-sessions/list-panes line from fields, escaping each
// the way tmux does: a backslash is doubled and the separator becomes the four
// characters backslash 0 3 7.
func escaped(fields ...string) string {
	sep := string(rune(sepByte))
	out := make([]string, len(fields))
	for i, field := range fields {
		field = strings.ReplaceAll(field, string(rune(backslashByte)), string(rune(backslashByte))+string(rune(backslashByte)))
		field = strings.ReplaceAll(field, sep, string(rune(backslashByte))+"037")
		out[i] = field
	}
	return strings.Join(out, sep)
}

// fakeRunner answers list-sessions and list-panes from scripted results.
type fakeRunner struct {
	sessions Result
	panes    Result
	calls    [][]string
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) Result {
	f.calls = append(f.calls, append([]string{name}, args...))
	for _, a := range args {
		if a == "list-panes" {
			return f.panes
		}
		if a == "list-sessions" {
			return f.sessions
		}
	}
	return Result{Code: 1, Stderr: "unexpected command"}
}

// socketDir creates <tmp>/tmux-501 and the named sockets in it.
func socketDir(t *testing.T, sockets ...string) string {
	t.Helper()
	dir := t.TempDir()
	tmux := filepath.Join(dir, "tmux-501")
	if err := os.MkdirAll(tmux, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range sockets {
		if err := os.WriteFile(filepath.Join(tmux, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestSplitEscapedFields(t *testing.T) {
	t.Parallel()
	sep := string(rune(sepByte))
	backslash := string(rune(backslashByte))
	tests := []struct {
		name string
		line string
		want []string
	}{
		{"a raw separator splits", "a" + sep + "b" + sep + "c", []string{"a", "b", "c"}},
		{"an escaped separator splits", "a" + backslash + "037b", []string{"a", "b"}},
		{"a doubled backslash is one", "a" + backslash + backslash + "b", []string{"a" + backslash + "b"}},
		{"other octal is the byte", "x" + backslash + "101y", []string{"xAy"}},
		{"an incomplete escape passes through", "a" + backslash + "12", []string{"a" + backslash + "12"}},
		{"a lone backslash stays", "a" + backslash, []string{"a" + backslash}},
		{"no separator is one field", "plain", []string{"plain"}},
		{"an empty line has one empty field", "", []string{""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := splitEscapedFields(tt.line)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("splitEscapedFields(%q) = %q, want %q", tt.line, got, tt.want)
			}
		})
	}
}

func TestScanWithoutTmux(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{}
	report := Scan(context.Background(), Config{TmpDir: t.TempDir(), Runner: runner})
	if report.Installed || len(report.Sessions) != 0 || len(report.Problems) != 0 {
		t.Fatalf("report = %+v, want nothing installed", report)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("commands ran without a socket: %v", runner.calls)
	}
}

func TestScanIgnoresOtherDirectories(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, name := range []string{"tmux-abc", "tmux-", "other", "tmux-501x"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "tmux-501"), []byte("not a dir"), 0o600); err != nil {
		t.Fatal(err)
	}
	report := Scan(context.Background(), Config{TmpDir: dir, Runner: &fakeRunner{}})
	if report.Installed || len(report.Sessions) != 0 {
		t.Fatalf("report = %+v, want no sockets", report)
	}
}

func TestScanSocketOnlyWhenCommandsAreOff(t *testing.T) {
	t.Parallel()
	dir := socketDir(t, "default")
	runner := &fakeRunner{}
	report := Scan(context.Background(), Config{TmpDir: dir, Runner: runner})
	if !report.Installed || report.AllowCommands {
		t.Fatalf("report = %+v, want installed and commands off", report)
	}
	if len(report.Sessions) != 1 {
		t.Fatalf("sessions = %+v", report.Sessions)
	}
	s := report.Sessions[0]
	if !s.SocketOnly || s.Name != "default" || s.Server != "default" {
		t.Fatalf("socket-only session = %+v", s)
	}
	if !slices.Equal(s.AttachArgv, []string{"tmux", "-S", s.Socket, "attach"}) {
		t.Fatalf("attach argv = %v", s.AttachArgv)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("commands ran with commands off: %v", runner.calls)
	}
}

func TestScanSessionsAndAgentPanes(t *testing.T) {
	t.Parallel()
	dir := socketDir(t, "sock1", "sock2")
	sock1 := filepath.Join(dir, "tmux-501", "sock1")
	sock2 := filepath.Join(dir, "tmux-501", "sock2")
	// The runner answers per socket: sock1 has two sessions and three panes,
	// sock2 one session with no agent.
	runner := &runnerBySocket{responses: map[string]fakeSessionPanes{
		sock1: {
			sessions: lines(
				escaped("$0", "work", "1700000000", "1", "/home/me/project"),
				escaped("$1", "agent two", "0", "0", "/tmp/a b"),
			),
			panes: lines(
				escaped("$0", "0", "win", "%1", "123", "/home/me/project", "claude", "title"),
				escaped("$0", "0", "win", "%2", "124", "", "bash", "title"),
				escaped("$1", "1", "win", "%3", "125", "/tmp/other", "codex", "title"),
			),
		},
		sock2: {
			sessions: lines(escaped("$0", "plain", "1", "0", "/w")),
			panes:    lines(escaped("$0", "0", "w", "%1", "1", "/w", "vim", "t")),
		},
	}}
	report := Scan(context.Background(), Config{TmpDir: dir, Runner: runner, AllowCommands: true})
	if !report.Installed || !report.AllowCommands || len(report.Problems) != 0 {
		t.Fatalf("report = %+v", report)
	}
	if len(report.Sessions) != 3 {
		t.Fatalf("sessions = %+v", report.Sessions)
	}
	// sock1 sorts before sock2; its sessions keep their tmux order.
	first := report.Sessions[0]
	if first.Name != "work" || first.Path != "/home/me/project" || first.Created != 1700000000 || !first.Attached {
		t.Fatalf("first session = %+v", first)
	}
	if first.ID != "sock1/work" || first.Server != "sock1" || first.Socket != sock1 {
		t.Fatalf("first identity = %+v", first)
	}
	if first.Panes != 2 || !slices.Equal(first.Agents, []string{"claude"}) {
		t.Fatalf("first panes/agents = %d %v", first.Panes, first.Agents)
	}
	if !slices.Equal(first.AttachArgv, []string{"tmux", "-S", sock1, "attach", "-t", "work"}) {
		t.Fatalf("attach argv = %v", first.AttachArgv)
	}
	second := report.Sessions[1]
	if second.Name != "agent two" || second.Path != "/tmp/a b" || second.Attached {
		t.Fatalf("second session = %+v", second)
	}
	if second.Panes != 1 || !slices.Equal(second.Agents, []string{"codex"}) {
		t.Fatalf("second panes/agents = %d %v", second.Panes, second.Agents)
	}
	third := report.Sessions[2]
	if third.Name != "plain" {
		t.Fatalf("third session = %+v", third)
	}
	if third.Panes != 1 || len(third.Agents) != 0 {
		t.Fatalf("third panes/agents = %d %v", third.Panes, third.Agents)
	}
}

// runnerBySocket answers the tmux calls of one socket from a script.
type runnerBySocket struct {
	responses map[string]fakeSessionPanes
	calls     [][]string
}

type fakeSessionPanes struct {
	sessions string
	panes    string
}

func (r *runnerBySocket) Run(_ context.Context, name string, args ...string) Result {
	r.calls = append(r.calls, append([]string{name}, args...))
	if len(args) < 2 || args[0] != "-S" {
		return Result{Code: 1, Stderr: "unexpected command"}
	}
	script := r.responses[args[1]]
	for _, a := range args {
		if a == "list-panes" {
			return Result{Stdout: script.panes}
		}
		if a == "list-sessions" {
			return Result{Stdout: script.sessions}
		}
	}
	return Result{Code: 1, Stderr: "unexpected command"}
}

func TestScanStaleSocketIsNotAProblem(t *testing.T) {
	t.Parallel()
	dir := socketDir(t, "stale")
	report := Scan(context.Background(), Config{TmpDir: dir, Runner: &fakeRunner{
		sessions: Result{Code: 1, Stderr: "no server running on /tmp/x"},
	}, AllowCommands: true})
	if len(report.Problems) != 0 {
		t.Fatalf("problems = %v", report.Problems)
	}
	if len(report.Sessions) != 1 || !report.Sessions[0].SocketOnly {
		t.Fatalf("sessions = %+v, want the socket-only fallback", report.Sessions)
	}
}

func TestScanCommandFailureIsAProblem(t *testing.T) {
	t.Parallel()
	dir := socketDir(t, "denied")
	report := Scan(context.Background(), Config{TmpDir: dir, Runner: &fakeRunner{
		sessions: Result{Code: 2, Stderr: "permission denied"},
	}, AllowCommands: true})
	if len(report.Problems) != 1 || !strings.Contains(report.Problems[0], "permission denied") {
		t.Fatalf("problems = %v", report.Problems)
	}
	if len(report.Sessions) != 1 || !report.Sessions[0].SocketOnly {
		t.Fatalf("sessions = %+v", report.Sessions)
	}
}

func TestScanPanesFailureKeepsTheSessionRows(t *testing.T) {
	t.Parallel()
	dir := socketDir(t, "s")
	report := Scan(context.Background(), Config{TmpDir: dir, Runner: &fakeRunner{
		sessions: Result{Stdout: escaped("$0", "work", "1", "0", "/w")},
		panes:    Result{Code: 1, Stderr: "boom"},
	}, AllowCommands: true})
	if len(report.Sessions) != 1 || report.Sessions[0].Name != "work" || report.Sessions[0].Panes != 0 {
		t.Fatalf("sessions = %+v", report.Sessions)
	}
	if len(report.Problems) != 1 || !strings.Contains(report.Problems[0], "list-panes") {
		t.Fatalf("problems = %v", report.Problems)
	}
}

func TestScanExtendsTheAgentSetFromConfig(t *testing.T) {
	t.Parallel()
	dir := socketDir(t, "s")
	report := Scan(context.Background(), Config{
		TmpDir: dir,
		Runner: &fakeRunner{
			sessions: Result{Stdout: escaped("$0", "work", "1", "0", "/w")},
			panes:    Result{Stdout: escaped("$0", "0", "w", "%1", "1", "/w", "myagent", "t")},
		},
		AllowCommands: true,
		KnownAgents:   []string{"/opt/bin/myagent", ""},
	})
	if len(report.Sessions) != 1 || !slices.Equal(report.Sessions[0].Agents, []string{"myagent"}) {
		t.Fatalf("sessions = %+v, want the configured agent", report.Sessions)
	}
}

func TestScanSessionAndPaneBounds(t *testing.T) {
	t.Parallel()
	dir := socketDir(t, "s")
	sessionRows := []string{
		escaped("$0", "one", "1", "0", "/w"),
		escaped("$1", "two", "1", "0", "/w"),
		escaped("$2", "three", "1", "0", "/w"),
	}
	report := Scan(context.Background(), Config{
		TmpDir: dir,
		Runner: &fakeRunner{
			sessions: Result{Stdout: lines(sessionRows...)},
			panes: Result{Stdout: lines(
				escaped("$0", "0", "w", "%1", "1", "/w", "bash", "t"),
				escaped("$1", "0", "w", "%2", "1", "/w", "bash", "t"),
			)},
		},
		AllowCommands: true,
		MaxSessions:   2,
		MaxPanes:      1,
	})
	if len(report.Sessions) != 2 || !report.Truncated {
		t.Fatalf("sessions = %d truncated %v, want 2 and truncated", len(report.Sessions), report.Truncated)
	}
	if report.Sessions[0].Panes != 1 {
		t.Fatalf("pane bound not applied: %+v", report.Sessions[0])
	}
}

func TestScanMalformedLinesAreSkipped(t *testing.T) {
	t.Parallel()
	dir := socketDir(t, "s")
	report := Scan(context.Background(), Config{
		TmpDir: dir,
		Runner: &fakeRunner{
			sessions: Result{Stdout: lines("", string(rune(sepByte))+"not a session", escaped("$0", "ok", "1", "0", "/w"))},
			panes:    Result{Stdout: lines("", "short", escaped("$0", "0", "w", "%1", "1", "/w", "bash", "t"))},
		},
		AllowCommands: true,
	})
	if len(report.Sessions) != 1 || report.Sessions[0].Name != "ok" {
		t.Fatalf("sessions = %+v", report.Sessions)
	}
}

// TestExecRunnerRunsInItsDir checks that the runner executes in the directory the
// trust gate judged (a bare/relative command must resolve the same in both).
func TestExecRunnerRunsInItsDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pwd is Unix only")
	}
	dir := t.TempDir()
	res := ExecRunner{Dir: dir}.Run(context.Background(), "pwd")
	if res.Code != 0 || strings.TrimSpace(res.Stdout) != dir {
		t.Fatalf("pwd = %q code %d, want the runner's dir %q", res.Stdout, res.Code, dir)
	}
}

// TestExecRunnerResolvesRelativePathAgainstDir is the regression for os/exec's
// LookPath running in the parent process: with PATH="bin" and a process cwd that
// differs from the runner's Dir, LookPath would look for <parent cwd>/bin/tool
// while the trust gate resolved <Dir>/bin/tool. The explicit Resolve must make the
// runner execute the same file the gate judged.
func TestExecRunnerResolvesRelativePathAgainstDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the shell script needs Unix")
	}
	workspace := t.TempDir()
	bin := filepath.Join(workspace, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	tool := filepath.Join(bin, "reltool")
	script := "#!/bin/sh" + newline + "printf rel-ok" + newline
	if err := os.WriteFile(tool, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	// The editor's cwd (here: the test's cwd) is not the workspace root.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(wd) == filepath.Clean(workspace) {
		t.Skip("the test cwd happens to be the workspace")
	}
	t.Setenv("PATH", "bin")
	resolver := func(name string) (string, bool) {
		return trust.ResolveSpawnPath(workspace, name, "bin")
	}
	resolved := ExecRunner{Dir: workspace, Resolve: resolver}.Run(context.Background(), "reltool")
	if resolved.Code != 0 || strings.TrimSpace(resolved.Stdout) != "rel-ok" {
		t.Fatalf("resolved run = %+v, want rel-ok from %s", resolved, tool)
	}
	// Without the resolver, LookPath resolves against the parent cwd (PATH="bin"
	// is relative), so the same command is not found: that is the mismatch the
	// explicit Resolve exists to prevent.
	naive := ExecRunner{Dir: workspace}.Run(context.Background(), "reltool")
	if naive.Code != -1 {
		t.Fatalf("without Resolve the runner ran %+v; the regression scenario is not exercised", naive)
	}
}

func TestExecRunnerReportsAMissingCommand(t *testing.T) {
	t.Parallel()
	res := ExecRunner{}.Run(context.Background(), "gvim-no-such-command-for-agentdiscover-test")
	if res.Code != -1 || res.Err == nil {
		t.Fatalf("result = %+v, want code -1 and the start error", res)
	}
}

func TestLimitedBufferDropsOverflow(t *testing.T) {
	t.Parallel()
	b := &limitedBuffer{limit: 3}
	if n, err := b.Write([]byte("abcdef")); n != 6 || err != nil {
		t.Fatalf("Write = %d, %v", n, err)
	}
	if got := b.String(); got != "abc" {
		t.Fatalf("buffer = %q, want abc", got)
	}
}

func TestClipTextIsBoundedOnRunes(t *testing.T) {
	t.Parallel()
	// A three-byte rune split by the byte bound must not leave half a rune.
	got := clipText(strings.Repeat("é", 10), 5)
	if len(got) != 4 || !strings.HasSuffix(got, "é") {
		t.Fatalf("clipText = %q (%d bytes)", got, len(got))
	}
	if got := clipText("abc", 10); got != "abc" {
		t.Fatalf("short text clipped: %q", got)
	}
}

var _ = errors.New
