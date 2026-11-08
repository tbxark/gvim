package editor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/diff"
	"github.com/tbxark/gvim/shellcmd"
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
	// Env is the active project environment's set-only overlay (the env package's
	// Provider.Overlay), stamped by BeginShell when the app wired one.
	Env []string

	// doc, from, to and selected say where Input came from, so that EndShell
	// replaces the same text, and only when the text is still the same (see snapshot).
	Doc      *Document
	From, To buffer.Pos
	Selected bool
	// snapshot marks the whole text of doc when BeginShell ran (see contentMark).
	// EndShell applies a replace only while the text is still the same, so an edit
	// anywhere in the document while the command runs stops the result, not only an
	// edit inside the range.
	snapshot contentMark
	// format is true for format_buffer, so that the status says it formatted.
	format bool
}

// SetShellEnv wires the active project environment: the returned overlay is
// appended to the environment of shell commands and formatters. The provider is
// read at BeginShell time, so an activation applies to the next run.
func (e *Editor) SetShellEnv(overlay func() []string) { e.shellEnv = overlay }

// envOverlay reads the wired environment overlay, if any.
func (e *Editor) envOverlay() []string {
	if e.shellEnv == nil {
		return nil
	}
	return e.shellEnv()
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
	if replace && d.ReadOnly() {
		return ShellJob{}, ErrReadOnly
	}
	if replace {
		if err := d.RefuseEditWhileRestoring(string(action)); err != nil {
			return ShellJob{}, err
		}
	}
	from, to := buffer.Pos{}, d.Buffer.End()
	selected := d.HasSelection()
	if selected {
		from, to = d.Sel.Ordered()
	} else if d.LargeState() != nil {
		// The input would be the whole text of the file.
		return ShellJob{}, errLargeAction(action)
	}
	_ = d.TakeRefusal()
	input, err := d.ReadRange(from, to)
	if err != nil {
		return ShellJob{}, fmt.Errorf("%s: %w", action, err)
	}
	// Without a selection the input is the whole text, so it is the snapshot too.
	snapshot := contentMark{Text: input}
	if selected {
		snapshot = d.ContentMark()
	}
	return ShellJob{
		Command:  command,
		Input:    input,
		Dir:      shellDir(d),
		Replace:  replace,
		Timeout:  shellTimeout,
		Env:      e.envOverlay(),
		Doc:      d,
		From:     from,
		To:       to,
		Selected: selected,
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
	return shellcmd.RunEnv(job.Dir, job.Command, job.Input, timeout, job.Env)
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
	d := job.Doc
	if d.ReadOnly() {
		return ErrReadOnly
	}
	if err := d.RefuseEditWhileRestoring("shell_command_replace"); err != nil {
		return err
	}
	if e.indexOfDoc(d) < 0 || d.ContentMark() != job.snapshot {
		return errShellStale
	}
	caret := d.Cursor
	var after buffer.Pos
	var hunks []diff.Hunk
	if job.format {
		// The snapshot is the text the formatter was given, which is the text now.
		hunks = diff.Lines(strings.Split(job.snapshot.Text, "\n"), strings.Split(out, "\n"))
	}
	d.Buffer.Group(func() {
		d.Buffer.Delete(job.From, job.To)
		after = d.Buffer.Insert(job.From, out)
	})
	d.LastMulti = nil
	switch {
	case job.Selected:
		d.Cursor = after
	case job.format:
		d.Cursor = d.Buffer.Clamp(buffer.Pos{Line: formatLine(hunks, caret.Line), Col: caret.Col})
	default:
		d.Cursor = d.Buffer.Clamp(caret)
	}
	d.Sel = nil
	d.Want = -1
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
