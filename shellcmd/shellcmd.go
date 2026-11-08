// Package shellcmd runs one shell command with a timeout and returns its output.
// It is the execution half of the editor's shell actions: it knows nothing about
// documents or editors, so a command can run on any goroutine.
package shellcmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"
)

// Run runs command with sh -c in dir, with input on its standard input, and
// returns its standard output with CRLF breaks turned into LF. timeout bounds the
// run. A non-zero exit, a timeout, or output that is not UTF-8 is an error. A
// background child that keeps the output open after the shell exited 0 does not
// make a successful command fail.
func Run(dir, command, input string, timeout time.Duration) (string, error) {
	return RunEnv(dir, command, input, timeout, nil)
}

// RunEnv is Run with extra environment entries appended to the inherited
// environment (the active project environment's set-only overlay). A nil or
// empty overlay leaves the child's environment inherited.
func RunEnv(dir, command, input string, timeout time.Duration, extraEnv []string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(input)
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	cmd.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	// A background child can hold the output open after the shell has exited 0.
	// Wait then returns exec.ErrWaitDelay, and the command still succeeded.
	if errors.Is(err, exec.ErrWaitDelay) && cmd.ProcessState != nil && cmd.ProcessState.Success() {
		err = nil
	}
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", fmt.Errorf("shell command timed out after %s", timeout)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		msg := fmt.Sprintf("shell command failed: exit status %d", exitErr.ExitCode())
		if s := strings.TrimSpace(stderr.String()); s != "" {
			first, _, _ := strings.Cut(s, "\n")
			msg += ": " + first
		}
		return "", errors.New(msg)
	}
	if err != nil {
		return "", fmt.Errorf("shell command: %w", err)
	}
	if !utf8.Valid(stdout.Bytes()) {
		return "", errors.New("shell command output is not valid UTF-8")
	}
	return strings.ReplaceAll(stdout.String(), "\r\n", "\n"), nil
}
