package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/editor"
)

// shellDone is a shell command that ran off the update loop. out and err are what
// editor.RunShellJob returned for job.
type shellDone struct {
	job editor.ShellJob
	out string
	err error
}

// shellAction takes shell_command, shell_command_replace and format_buffer when the
// editor is a ShellRunner. It prepares the job now, and the command or the formatter
// runs in the returned Cmd, so Update does not wait for it (a formatter can take up to
// the shell timeout). The result comes back as shellDone. It reports false for any
// other action, and for an editor without the port, which runs the action in Do as
// before.
func (m *Model) shellAction(action string, args map[string]any) (tea.Cmd, bool) {
	if action != "shell_command" && action != "shell_command_replace" && action != "format_buffer" {
		return nil, false
	}
	if action == "format_buffer" {
		if cmd, ok := m.lspFormat(); ok {
			return cmd, true
		}
	}
	sr, ok := m.ed.(ShellRunner)
	if !ok {
		return nil, false
	}
	job, err := sr.BeginShell(editor.Action(action), args)
	if err != nil {
		// The editor writes the error to the status row, as Do does for a failed action.
		sr.EndShell(editor.ShellJob{}, "", err)
		return nil, true
	}
	run := editor.RunShellJob
	if m.shellRun != nil {
		run = m.shellRun
	}
	return func() tea.Msg {
		out, err := run(job)
		return shellDone{job: job, out: out, err: err}
	}, true
}

// finishShell applies a finished shell command. The editor ends the job, which writes
// any error to the status row, and the viewport follows the text the job changed.
func (m *Model) finishShell(msg shellDone) tea.Cmd {
	if sr, ok := m.ed.(ShellRunner); ok {
		sr.EndShell(msg.job, msg.out, msg.err)
	}
	m.syncViewport()
	return nil
}
