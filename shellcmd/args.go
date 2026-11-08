package shellcmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"
)

// RunArgs runs argv in dir with no shell, with input on its standard input, and returns
// its standard output with CRLF breaks turned into LF and its exit code. timeout bounds
// the run. Unlike Run, a non-zero exit code is not an error: the caller decides what it
// means (git grep, for instance, exits 1 for no matches). err is non-nil for a start
// failure, a timeout, or output that is not UTF-8.
func RunArgs(dir string, argv []string, input string, timeout time.Duration) (stdout string, exitCode int, err error) {
	if len(argv) == 0 {
		return "", 0, errors.New("shell command: empty argument list")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(input)
	cmd.WaitDelay = time.Second
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	if errors.Is(runErr, exec.ErrWaitDelay) && cmd.ProcessState != nil && cmd.ProcessState.Success() {
		runErr = nil
	}
	if runErr != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", 0, fmt.Errorf("shell command timed out after %s", timeout)
	}
	if !utf8.Valid(out.Bytes()) {
		return "", 0, errors.New("shell command output is not valid UTF-8")
	}
	text := strings.ReplaceAll(out.String(), "\r\n", "\n")
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return text, exitErr.ExitCode(), nil
	}
	if runErr != nil {
		return "", 0, fmt.Errorf("shell command: %w", runErr)
	}
	return text, 0, nil
}
