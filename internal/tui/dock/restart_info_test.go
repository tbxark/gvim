package dock

import (
	"testing"

	"github.com/tbxark/gvim/internal/terminal"
)

// exitedRestartTabs restores one exited tab with the given launch record/status.
func exitedRestartTabs(t *testing.T, effects Effects, launch terminal.Launch, code int, known bool) *Tabs {
	t.Helper()
	tabs := liveTestTabs(t, effects, &fakeStarter{term: newFakeTerm()})
	tabs.Restore(terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true, Live: false, Rows: []string{"out"},
		LaunchArgv: launch.Argv, ResumeArgv: launch.ResumeArgv, ExitCode: code, ExitKnown: known,
		Tabs: []terminal.SavedTab{{
			Name: "shell", Open: true, Rows: []string{"out"}, Live: false,
			LaunchArgv: launch.Argv, ResumeArgv: launch.ResumeArgv,
			ExitCode: code, ExitKnown: known,
		}},
	})
	return tabs
}

// TestExitedRestartInfoProgramName checks Fresh's program_name rule: the resume
// argv's program wins, the launch argv is the fallback, an empty resume vector is
// the plain-shell marker, and a plain shell names no program.
func TestExitedRestartInfoProgramName(t *testing.T) {
	t.Parallel()
	plain := exitedRestartTabs(t, Effects{}, terminal.Launch{}, 3, true)
	info, ok := plain.ExitedRestartInfo()
	if !ok || info.Program != "" || info.ResumesAgent || !info.ExitKnown || info.ExitCode != 3 {
		t.Fatalf("plain shell info = %+v, %v", info, ok)
	}

	launch := exitedRestartTabs(t, Effects{}, terminal.Launch{Argv: []string{"/usr/bin/npm", "run", "dev"}}, 0, true)
	if info, ok := launch.ExitedRestartInfo(); !ok || info.Program != "npm" || info.ResumesAgent {
		t.Fatalf("launch info = %+v, %v", info, ok)
	}

	resume := exitedRestartTabs(t, Effects{ResumeAgents: true}, terminal.Launch{
		Argv: []string{"/usr/bin/npm"}, ResumeArgv: []string{"/opt/bin/claude", "--resume", "x"},
	}, 0, true)
	if info, ok := resume.ExitedRestartInfo(); !ok || info.Program != "claude" || !info.ResumesAgent {
		t.Fatalf("resume info = %+v, %v", info, ok)
	}

	// terminal.resume_agents off: the restart runs the launch command but the
	// indicator still names the program the resume would launch (Fresh's
	// program_name prefers the resume argv regardless of the config).
	off := exitedRestartTabs(t, Effects{}, terminal.Launch{
		Argv: []string{"npm"}, ResumeArgv: []string{"/opt/bin/claude", "--resume", "x"},
	}, 0, false)
	if info, ok := off.ExitedRestartInfo(); !ok || info.Program != "claude" || info.ResumesAgent {
		t.Fatalf("resume-off info = %+v, %v", info, ok)
	}

	// An empty resume vector is Fresh's plain-shell marker, not an agent.
	marker := exitedRestartTabs(t, Effects{ResumeAgents: true}, terminal.Launch{Argv: []string{"bash"}, ResumeArgv: []string{}}, 0, true)
	if info, ok := marker.ExitedRestartInfo(); !ok || info.Program != "bash" || info.ResumesAgent {
		t.Fatalf("empty-resume info = %+v, %v", info, ok)
	}

	// Windows program paths lose their directory too.
	win := exitedRestartTabs(t, Effects{}, terminal.Launch{Argv: []string{"C:\\Tools\\codex.exe", "-x"}}, 0, true)
	if info, ok := win.ExitedRestartInfo(); !ok || info.Program != "codex.exe" {
		t.Fatalf("windows info = %+v, %v", info, ok)
	}
}

// TestExitedRestartInfoNeedsAnExitedTab checks a running or empty dock reports no
// restart description, so the status element stays absent.
func TestExitedRestartInfoNeedsAnExitedTab(t *testing.T) {
	t.Parallel()
	running := liveTestTabs(t, Effects{}, &fakeStarter{term: newFakeTerm()})
	if _, ok := running.ExitedRestartInfo(); ok {
		t.Fatal("a running dock reported a restart description")
	}
	var empty Tabs
	if _, ok := empty.ExitedRestartInfo(); ok {
		t.Fatal("an empty dock reported a restart description")
	}
}
