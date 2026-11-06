package ui

import (
	"errors"
	"testing"

	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/theme"
)

// shellFake is a fakeEditor that also runs shell jobs. BeginShell returns job, or
// beginErr, and EndShell records the job it was given.
type shellFake struct {
	*fakeEditor
	job      editor.ShellJob
	beginErr error
	begun    []editor.Action
	ended    []shellEnd
}

// shellEnd is one call to EndShell.
type shellEnd struct {
	job editor.ShellJob
	out string
	err error
}

func (f *shellFake) BeginShell(a editor.Action, _ map[string]any) (editor.ShellJob, error) {
	f.begun = append(f.begun, a)
	return f.job, f.beginErr
}

func (f *shellFake) EndShell(job editor.ShellJob, out string, err error) editor.Result {
	f.ended = append(f.ended, shellEnd{job: job, out: out, err: err})
	return editor.Result{Err: err}
}

// newShellModel returns a model over the shell fake, with no window size.
func newShellModel(f *shellFake) *Model {
	return New(Deps{Editor: f, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
}

// TestShellCommandRunsOffTheUpdateLoop checks that a shell command is prepared in
// Update, runs in a command, and is applied when its result comes back.
func TestShellCommandRunsOffTheUpdateLoop(t *testing.T) {
	t.Parallel()
	f := &shellFake{fakeEditor: newFakeEditor("hello"), job: editor.ShellJob{Command: "true"}}
	m := newShellModel(f)
	cmd, ok := m.uiAction("shell_command", map[string]any{"command": "true"})
	if !ok || cmd == nil {
		t.Fatalf("uiAction = %v, %v; want a command", cmd, ok)
	}
	if len(f.actions) != 0 {
		t.Fatalf("Do ran %v; a shell command must not run in Do", f.actions)
	}
	if len(f.begun) != 1 || f.begun[0] != "shell_command" {
		t.Fatalf("BeginShell calls = %v, want one shell_command", f.begun)
	}
	done, ok := cmd().(shellDone)
	if !ok {
		t.Fatalf("command returned %T, want shellDone", done)
	}
	if done.err != nil {
		t.Fatalf("shell job err = %v, want nil", done.err)
	}
	m.Update(done)
	if len(f.ended) != 1 || f.ended[0].job.Command != "true" || f.ended[0].err != nil {
		t.Fatalf("EndShell calls = %+v, want one call for the job", f.ended)
	}
}

// TestShellCommandBeginErrorGoesToTheEditor checks that a command that cannot start
// reports its error through the editor, as Do would, and starts no command.
func TestShellCommandBeginErrorGoesToTheEditor(t *testing.T) {
	t.Parallel()
	want := errors.New("missing string argument \"command\"")
	f := &shellFake{fakeEditor: newFakeEditor("hello"), beginErr: want}
	m := newShellModel(f)
	cmd, ok := m.uiAction("shell_command", nil)
	if !ok || cmd != nil {
		t.Fatalf("uiAction = %v, %v; want it taken with no command", cmd, ok)
	}
	if len(f.ended) != 1 || f.ended[0].err != want {
		t.Fatalf("EndShell calls = %+v, want the begin error", f.ended)
	}
}

// TestShellCommandWithoutShellRunnerRunsInDo checks that an editor without the shell
// port still runs the action in Do, as before.
func TestShellCommandWithoutShellRunnerRunsInDo(t *testing.T) {
	t.Parallel()
	f := newFakeEditor("hello")
	m := New(Deps{Editor: f, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	if err := m.Run("shell_command"); err != nil {
		t.Fatalf("Run(shell_command) = %v", err)
	}
	if len(f.actions) != 1 || f.actions[0] != "shell_command" {
		t.Fatalf("Do calls = %v, want one shell_command", f.actions)
	}
}
