package agentdiscover

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Result is one command's bounded result.
type Result struct {
	Stdout string
	Stderr string
	Code   int   // the exit code; -1 when the command did not start or timed out
	Err    error // why it did not start (or the timeout), for the scan's problems
}

// Runner runs one command and returns its bounded output. Tests supply a fake
// runner; the app uses ExecRunner. It is not a shell: name and args go to exec
// directly.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) Result
}

// limitedBuffer keeps at most limit bytes and drops the rest.
type limitedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	written := len(p)
	room := b.limit - b.buf.Len()
	if room <= 0 {
		return written, nil
	}
	if len(p) > room {
		p = p[:room]
	}
	b.buf.Write(p)
	return written, nil
}

func (b *limitedBuffer) String() string { return b.buf.String() }

// ExecRunner is the production Runner. It runs one command with a timeout and
// bounded output; it never uses a shell.
type ExecRunner struct {
	// Timeout bounds the whole command; zero means defaultCommandTimeout.
	Timeout time.Duration
	// Dir is the working directory of the child, and the directory the caller's
	// Resolve maps relative or empty PATH entries against.
	Dir string
	// Resolve maps a bare command name to the absolute path to execute, with the
	// same PATH and cwd rules as the caller's trust gate (an empty PATH entry is
	// Dir, a relative entry is Dir-relative, symlinks are followed). os/exec's
	// LookPath runs in the parent process before cmd.Dir applies, so a relative
	// PATH entry would otherwise resolve against the editor's cwd while the gate
	// resolved it against the workspace root: the executable judged would not be
	// the one that ran. The app's scan runner sets this to
	// trust.ResolveSpawnPath; nil falls back to LookPath (for tests and PATHs
	// without relative entries). A name carrying a path separator is not resolved:
	// the child resolves it under Dir.
	Resolve func(name string) (string, bool)
}

// Run implements Runner.
func (r ExecRunner) Run(ctx context.Context, name string, args ...string) Result {
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = defaultCommandTimeout
	}
	execName := name
	if r.Resolve != nil && !hasPathSeparator(name) {
		resolved, ok := r.Resolve(name)
		if !ok {
			// The gate lets a name that resolves nowhere through so the exec can
			// report the failure; keep the same outcome here.
			return Result{Code: -1, Err: fmt.Errorf("agentdiscover: %s: %w", name, exec.ErrNotFound)}
		}
		execName = resolved
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, execName, args...)
	cmd.Dir = r.Dir
	if len(cmd.Args) > 0 {
		// Keep argv[0] as the caller spelled the command, as a LookPath-based exec
		// would, while running the absolute path the resolver chose.
		cmd.Args[0] = name
	}
	out := &limitedBuffer{limit: defaultMaxCommandBytes}
	errOut := &limitedBuffer{limit: defaultMaxCommandBytes}
	cmd.Stdout, cmd.Stderr = out, errOut
	err := cmd.Run()
	res := Result{Stdout: out.String(), Stderr: errOut.String()}
	if err != nil {
		var exit *exec.ExitError
		switch {
		case errors.As(err, &exit):
			res.Code = exit.ExitCode()
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			res.Code = -1
			res.Err = ctx.Err()
		default:
			// The command did not start (not installed, not executable): code -1
			// and the error, which the scan reports.
			res.Code = -1
			res.Err = err
		}
	}
	return res
}

// hasPathSeparator reports whether name names a path rather than a bare command
// resolved through PATH.
func hasPathSeparator(name string) bool {
	if strings.ContainsRune(name, filepath.Separator) {
		return true
	}
	return runtime.GOOS == "windows" && strings.IndexByte(name, 92) >= 0
}
