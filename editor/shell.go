package editor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/diff"
)

// shellTimeout bounds one shell command. It is a variable so that a test can
// shorten it.
var shellTimeout = 30 * time.Second

// errShellStale is returned by EndShell when the text a replace ran on has changed,
// or its tab has closed, while the command ran. The result is then not applied.
var errShellStale = errors.New("shell command: the text changed while the command ran")

// ShellJob is one shell command, prepared by BeginShell and run by RunShellJob.
// RunShellJob needs nothing else, so the UI can run the command off its update
// loop and pass the result to EndShell.
type ShellJob struct {
	// Command is the text given to sh -c.
	Command string
	// Input is the text on the command's standard input: the selection, or the
	// whole document without one.
	Input string
	// Dir is the directory the command runs in.
	Dir string
	// Replace is true for shell_command_replace, which puts the output in place of
	// Input. False opens the output in a new untitled document.
	Replace bool
	// Timeout bounds the run. A zero value uses the default timeout.
	Timeout time.Duration

	// doc, from, to and selected say where Input came from, so that EndShell
	// replaces the same text, and only when the text is still the same (see snapshot).
	doc      *Document
	from, to buffer.Pos
	selected bool
	// snapshot marks the whole text of doc when BeginShell ran (see contentMark).
	// EndShell applies a replace only while the text is still the same, so an edit
	// anywhere in the document while the command runs stops the result, not only an
	// edit inside the range.
	snapshot contentMark
	// format is true for format_buffer, so that the status says it formatted.
	format bool
}

// registerShellHandlers registers shell_command and shell_command_replace. Each
// one runs the three steps of the split in turn, as Do runs any action.
func registerShellHandlers(h map[Action]handler) {
	h["shell_command"] = shellHandler("shell_command")
	h["shell_command_replace"] = shellHandler("shell_command_replace")
}

// shellHandler runs a shell action in the three steps of the split.
func shellHandler(action Action) handler {
	return func(e *Editor, args map[string]any) error {
		job, err := e.BeginShell(action, args)
		if err != nil {
			return err
		}
		out, runErr := RunShellJob(job)
		return e.EndShell(job, out, runErr).Err
	}
}

// BeginShell prepares the command of action, which is shell_command or
// shell_command_replace, for RunShellJob. The "command" argument is required. The
// input is the selection, or the whole document without one. A replace refuses a
// read-only document with ErrReadOnly. BeginShell changes no text. It ends the typing
// group, as Do does before any action that is not typing. The action format_buffer
// takes no "command" argument; its job is built by beginFormat.
func (e *Editor) BeginShell(action Action, args map[string]any) (ShellJob, error) {
	// Do ends the typing group before an action that is not typing. The root runs
	// the job without Do, so BeginShell ends the group here.
	if d := e.Active(); d != nil {
		d.Buffer.Break()
	}
	e.lastKind = ""
	if action == "format_buffer" {
		return e.beginFormat()
	}
	command, ok := args["command"].(string)
	if !ok || command == "" {
		return ShellJob{}, fmt.Errorf("%s: missing string argument \"command\"", action)
	}
	d := e.Active()
	if d == nil {
		return ShellJob{}, ErrNoDocument
	}
	replace := action == "shell_command_replace"
	if replace && d.readOnly {
		return ShellJob{}, ErrReadOnly
	}
	if replace {
		if err := d.refuseEditWhileRestoring(string(action)); err != nil {
			return ShellJob{}, err
		}
	}
	from, to := buffer.Pos{}, d.Buffer.End()
	selected := d.HasSelection()
	if selected {
		from, to = d.Sel.Ordered()
	} else if d.large != nil {
		// The input would be the whole text of the file.
		return ShellJob{}, errLargeAction(action)
	}
	_ = d.takeRefusal()
	input, err := d.readRange(from, to)
	if err != nil {
		return ShellJob{}, fmt.Errorf("%s: %w", action, err)
	}
	// Without a selection the input is the whole text, so it is the snapshot too.
	snapshot := contentMark{text: input}
	if selected {
		snapshot = d.contentMark()
	}
	return ShellJob{
		Command:  command,
		Input:    input,
		Dir:      shellDir(d),
		Replace:  replace,
		Timeout:  shellTimeout,
		doc:      d,
		from:     from,
		to:       to,
		selected: selected,
		snapshot: snapshot,
	}, nil
}

// RunShellJob runs job.Command with sh -c in job.Dir, with job.Input on its
// standard input. It returns the output, or the error. It uses nothing from an
// Editor, so it can run on any goroutine.
func RunShellJob(job ShellJob) (string, error) {
	timeout := job.Timeout
	if timeout <= 0 {
		timeout = shellTimeout
	}
	return execShell(job.Dir, job.Command, job.Input, timeout)
}

// EndShell applies the result of a job from BeginShell and RunShellJob, with out
// and runErr as RunShellJob returned them. A new document opens with the output,
// and a replace puts it in place of the job's input. A failed run changes nothing,
// and neither does a replace when the text changed while the command ran. EndShell
// returns the Result that Do returns for the action.
func (e *Editor) EndShell(job ShellJob, out string, runErr error) Result {
	err := runErr
	if err == nil {
		err = e.applyShell(job, out)
	}
	return e.finish(err)
}

// applyShell puts the output of a successful job into the document.
func (e *Editor) applyShell(job ShellJob, out string) error {
	if !job.Replace {
		nd := e.newUntitled()
		nd.Buffer.Insert(buffer.Pos{}, out)
		e.addDoc(nd)
		e.switchTo(len(e.docs) - 1)
		e.setStatus("Shell output in " + nd.Name())
		return nil
	}
	d := job.doc
	if d.readOnly {
		return ErrReadOnly
	}
	if err := d.refuseEditWhileRestoring("shell_command_replace"); err != nil {
		return err
	}
	if e.indexOfDoc(d) < 0 || d.contentMark() != job.snapshot {
		return errShellStale
	}
	caret := d.Cursor
	var after buffer.Pos
	var hunks []diff.Hunk
	if job.format {
		// The snapshot is the text the formatter was given, which is the text now.
		hunks = diff.Lines(strings.Split(job.snapshot.text, "\n"), strings.Split(out, "\n"))
	}
	d.Buffer.Group(func() {
		d.Buffer.Delete(job.from, job.to)
		after = d.Buffer.Insert(job.from, out)
	})
	d.lastMulti = nil
	switch {
	case job.selected:
		d.Cursor = after
	case job.format:
		d.Cursor = d.Buffer.Clamp(buffer.Pos{Line: formatLine(hunks, caret.Line), Col: caret.Col})
	default:
		d.Cursor = d.Buffer.Clamp(caret)
	}
	d.Sel = nil
	d.want = -1
	if job.format {
		e.setStatus("Formatted buffer")
	} else {
		e.setStatus("Shell command completed")
	}
	return nil
}

// shellDir is the directory a command runs in: the directory of the document,
// or the working directory for an unnamed buffer.
func shellDir(d *Document) string {
	if d.Path != "" {
		return filepath.Dir(d.Path)
	}
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}

// execShell runs command with sh -c in dir, with input on its standard input,
// and returns its standard output with CRLF breaks turned into LF. A non-zero
// exit, a timeout, or output that is not UTF-8 is an error.
func execShell(dir, command, input string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(input)
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
	out, _ := splitLineEnding(stdout.String())
	return out, nil
}
