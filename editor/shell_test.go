package editor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/buffer"
)

// shellArgs is the argument map of shell_command and shell_command_replace.
func shellArgs(command string) map[string]any {
	return map[string]any{"command": command}
}

func TestShellCommandOpensOutputInANewDocument(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name    string
		Text    string
		command string
		Want    string // text of the new untitled document
		wantErr string // substring of the error; "" means success
	}{
		{Name: "output of a filter", Text: "hello", command: "tr a-z A-Z", Want: "HELLO"},
		{Name: "the whole buffer is the input", Text: "a\nb", command: "cat", Want: "a\nb"},
		{Name: "a failing command reports its exit status", Text: "x", command: "exit 3", wantErr: "exit status 3"},
		{Name: "stderr is part of the failure", Text: "x", command: "echo boom >&2; exit 2", wantErr: "boom"},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := withDoc(tt.Text)
			err := e.Do("shell_command", shellArgs(tt.command)).Err
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
				}
				if len(e.docs) != 1 {
					t.Fatalf("tabs = %d after a failed command, want 1", len(e.docs))
				}
				return
			}
			if err != nil {
				t.Fatalf("shell_command: %v", err)
			}
			if len(e.docs) != 2 {
				t.Fatalf("tabs = %d, want 2", len(e.docs))
			}
			if got := e.Active().Name(); got != "Untitled-1" {
				t.Fatalf("active tab = %q, want Untitled-1", got)
			}
			if got := e.Active().Buffer.Text(); got != tt.Want {
				t.Fatalf("output = %q, want %q", got, tt.Want)
			}
			if got := e.docs[0].Buffer.Text(); got != tt.Text {
				t.Fatalf("original text changed to %q", got)
			}
		})
	}
}

func TestShellCommandReplaceChangesTextOnlyOnSuccess(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name    string
		Text    string
		Anchor  int // selection anchor column, or -1 for no selection
		Head    int // selection head column
		command string
		Want    string // text after the command
		wantErr string // substring of the error; "" means success
	}{
		{Name: "whole buffer without a selection", Text: "abc\ndef", Anchor: -1, command: "tr a-z A-Z", Want: "ABC\nDEF"},
		{Name: "selection is replaced by the output", Text: "one two three", Anchor: 4, Head: 7, command: "tr a-z A-Z", Want: "one TWO three"},
		{Name: "failing command leaves the text alone", Text: "keep", Anchor: -1, command: "echo nope >&2; exit 1", Want: "keep", wantErr: "exit status 1"},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := withDoc(tt.Text)
			if tt.Anchor >= 0 {
				d := e.Active()
				d.Sel = &buffer.Selection{Anchor: buffer.Pos{Col: tt.Anchor}, Head: buffer.Pos{Col: tt.Head}}
				d.Cursor = buffer.Pos{Col: tt.Head}
			}
			err := e.Do("shell_command_replace", shellArgs(tt.command)).Err
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("shell_command_replace: %v", err)
			}
			if got := text(e); got != tt.Want {
				t.Fatalf("text = %q, want %q", got, tt.Want)
			}
		})
	}
}

func TestShellCommandNeedsTheCommandArgument(t *testing.T) {
	t.Parallel()
	for _, a := range []Action{"shell_command", "shell_command_replace"} {
		e := withDoc("x")
		if r := e.Do(a, nil); r.Err == nil {
			t.Errorf("Do(%s) without a command returned nil, want an error", a)
		}
		if r := e.Do(a, map[string]any{"command": ""}); r.Err == nil {
			t.Errorf("Do(%s) with an empty command returned nil, want an error", a)
		}
		if got := text(e); got != "x" {
			t.Errorf("Do(%s) changed the text to %q", a, got)
		}
	}
}

func TestShellCommandRunsInTheDocumentDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.txt")
	if err := os.WriteFile(path, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	e := New(Options{})
	if err := e.OpenFile(path); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if r := e.Do("shell_command", shellArgs("pwd -P")); r.Err != nil {
		t.Fatalf("shell_command: %v", r.Err)
	}
	if got := strings.TrimSpace(text(e)); got != want {
		t.Fatalf("command ran in %q, want %q", got, want)
	}
}

func TestRunShellJobNeedsNoEditor(t *testing.T) {
	t.Parallel()
	// RunShellJob takes only the job, so the UI can run it on a command goroutine.
	job := ShellJob{Command: "tr a-z A-Z", Input: "abc", Dir: t.TempDir(), Timeout: 10 * time.Second}
	out, err := RunShellJob(job)
	if err != nil {
		t.Fatalf("RunShellJob: %v", err)
	}
	if out != "ABC" {
		t.Fatalf("output = %q, want ABC", out)
	}
}

func TestBeginShellRefusesOnlyReplaceOnReadOnly(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name    string
		action  Action
		wantErr error
	}{
		{"replace refuses a read-only document", "shell_command_replace", ErrReadOnly},
		{"a new document is allowed on a read-only document", "shell_command", nil},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := withDoc("abc")
			run(t, e, "toggle_read_only")
			job, err := e.BeginShell(tt.action, shellArgs("cat"))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("BeginShell err = %v, want %v", err, tt.wantErr)
			}
			if err == nil && job.Command != "cat" {
				t.Fatalf("job command = %q, want cat", job.Command)
			}
		})
	}
}

func TestEndShellRefusesTextThatChangedWhileTheCommandRan(t *testing.T) {
	t.Parallel()
	e := withDoc("abc")
	e.SelectRange(caretPos(0, 1), caretPos(0, 2))
	job, err := e.BeginShell("shell_command_replace", shellArgs("tr a-z A-Z"))
	if err != nil {
		t.Fatalf("BeginShell: %v", err)
	}
	out, runErr := RunShellJob(job)
	// The user types at the start while the command runs, so the selected text moves.
	e.SetCursor(caretPos(0, 0), false)
	typeKey(t, e, "Z")
	r := e.EndShell(job, out, runErr)
	if r.Err == nil {
		t.Fatal("EndShell applied a result to text that had changed")
	}
	if got := text(e); got != "Zabc" {
		t.Fatalf("text = %q, want the user's text unchanged", got)
	}
}

func TestEndShellRefusesEditsAfterTheJobRange(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name     string
		Text     string
		Selected bool       // whether the job replaces the selection anchor..head
		Anchor   buffer.Pos // selection start, when selected
		Head     buffer.Pos // selection end, when selected
		typeAt   buffer.Pos // where the user types while the command runs
		typed    string
		Want     string // text after the typing; the refused replace leaves it as is
	}{
		{Name: "whole buffer, typed after its end", Text: "abc", typeAt: buffer.Pos{Col: 3}, typed: "Z", Want: "abcZ"},
		{Name: "selection, typed after it on the same line", Text: "one two", Selected: true,
			Anchor: buffer.Pos{Col: 0}, Head: buffer.Pos{Col: 3}, typeAt: buffer.Pos{Col: 7}, typed: "!", Want: "one two!"},
		{Name: "selection, typed on a later line", Text: "one\ntwo", Selected: true,
			Anchor: buffer.Pos{Col: 0}, Head: buffer.Pos{Col: 3}, typeAt: buffer.Pos{Line: 1, Col: 3}, typed: "!", Want: "one\ntwo!"},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := withDoc(tt.Text)
			if tt.Selected {
				e.SelectRange(tt.Anchor, tt.Head)
			}
			job, err := e.BeginShell("shell_command_replace", shellArgs("tr a-z A-Z"))
			if err != nil {
				t.Fatalf("BeginShell: %v", err)
			}
			out, runErr := RunShellJob(job)
			// The user types outside the job's range while the command runs.
			e.SetCursor(tt.typeAt, false)
			typeKey(t, e, tt.typed)
			r := e.EndShell(job, out, runErr)
			if !errors.Is(r.Err, errShellStale) {
				t.Fatalf("EndShell err = %v, want errShellStale", r.Err)
			}
			if got := text(e); got != tt.Want {
				t.Fatalf("text = %q, want the user's text %q unchanged", got, tt.Want)
			}
		})
	}
}

func TestEndShellAppliesReplaceWhenTextIsUnchanged(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name     string
		Text     string
		Selected bool
		Anchor   buffer.Pos
		Head     buffer.Pos
		Edit     func(t *testing.T, e *Editor) // changes the text while the command runs; nil for none
		Want     string
	}{
		{Name: "whole buffer, nothing changed", Text: "abc\ndef", Want: "ABC\nDEF"},
		{Name: "selection, nothing changed", Text: "one two three", Selected: true,
			Anchor: buffer.Pos{Col: 4}, Head: buffer.Pos{Col: 7}, Want: "one TWO three"},
		{Name: "edit undone while the command runs", Text: "one two three", Selected: true,
			Anchor: buffer.Pos{Col: 4}, Head: buffer.Pos{Col: 7},
			Edit: func(t *testing.T, e *Editor) {
				e.SetCursor(caretPos(0, 0), false)
				typeKey(t, e, "Z")
				run(t, e, "undo")
			},
			Want: "one TWO three"},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := withDoc(tt.Text)
			if tt.Selected {
				e.SelectRange(tt.Anchor, tt.Head)
			}
			job, err := e.BeginShell("shell_command_replace", shellArgs("tr a-z A-Z"))
			if err != nil {
				t.Fatalf("BeginShell: %v", err)
			}
			out, runErr := RunShellJob(job)
			if tt.Edit != nil {
				tt.Edit(t, e)
			}
			if r := e.EndShell(job, out, runErr); r.Err != nil {
				t.Fatalf("EndShell: %v", r.Err)
			}
			if got := text(e); got != tt.Want {
				t.Fatalf("text = %q, want %q", got, tt.Want)
			}
		})
	}
}

func TestEndShellOpensANewDocumentWithItsOutput(t *testing.T) {
	t.Parallel()
	e := withDoc("hello")
	job, err := e.BeginShell("shell_command", shellArgs("tr a-z A-Z"))
	if err != nil {
		t.Fatalf("BeginShell: %v", err)
	}
	out, runErr := RunShellJob(job)
	if r := e.EndShell(job, out, runErr); r.Err != nil {
		t.Fatalf("EndShell: %v", r.Err)
	}
	if got := text(e); got != "HELLO" {
		t.Fatalf("new document text = %q, want HELLO", got)
	}
	if n := len(e.Tabs()); n != 2 {
		t.Fatalf("tabs = %d, want 2", n)
	}
}
