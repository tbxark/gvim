package dock

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/tbxark/gvim/internal/terminal"
)

// exitedRestartTabs restores one exited tab with the given launch record/status.
func exitedRestartTabs(t *testing.T, effects Effects, launch terminal.Launch, code int, known bool) *Tabs {
	t.Helper()
	tabs := liveTestTabs(t, effects, &fakeStarter{term: newFakeTerm()})
	tabs.Restore(terminal.SavedState{Version: terminal.SavedStateVersion, Tabs: []terminal.SavedTab{{
		Name: "shell", Open: true, Rows: []string{"out"}, Live: false,
		LaunchArgv: launch.Argv, ResumeArgv: launch.ResumeArgv,
		ExitCode: code, ExitKnown: known,
	}}})
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
	if info, ok := launch.ExitedRestartInfo(); !ok || info.Program != "npm" || info.ResumesAgent || info.ProgramFromForeground {
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

// TestExitedRestartInfoUsesLastForegroundName checks the restart indicator names the
// real foreground process the bounded async poll last saw, not the recorded argv,
// and that the displayed name is cleaned/runes-bounded. No poll result (or a failed
// one, which clears the cache) keeps the documented recorded-argv fallback.
func TestExitedRestartInfoUsesLastForegroundName(t *testing.T) {
	t.Parallel()
	tabs := exitedRestartTabs(t, Effects{ResumeAgents: true}, terminal.Launch{
		Argv: []string{"/usr/bin/npm", "run", "dev"}, ResumeArgv: []string{"/opt/bin/claude", "--resume", "x"},
	}, 0, true)
	p := tabs.Active()
	p.fgName = "node"
	info, ok := tabs.ExitedRestartInfo()
	if !ok || info.Program != "node" || !info.ProgramFromForeground || !info.ResumesAgent {
		t.Fatalf("foreground restart info = %+v, %v", info, ok)
	}
	// The program is cleaned and bounded even if a poll cached something longer
	// (the terminal sanitizer already bounds it; the display bound is a second gate).
	p.fgName = "bad\x01name" + strings.Repeat("x", 200)
	info, ok = tabs.ExitedRestartInfo()
	if !ok || utf8.RuneCountInString(info.Program) != maxRestartProgramRunes || strings.ContainsRune(info.Program, '\x01') {
		t.Fatalf("bounded foreground program = %q, %v", info.Program, ok)
	}
	// A missing/failed poll result falls back to the recorded program honestly.
	p.fgName = ""
	if info, ok := tabs.ExitedRestartInfo(); !ok || info.Program != "claude" || info.ProgramFromForeground {
		t.Fatalf("recorded fallback = %+v, %v", info, ok)
	}
}

// TestRestartInfoKeepsForegroundAcrossExitAndClearsOnNewShell checks the cached
// foreground name survives an exit for the indicator, a late poll for the dead
// shell is ignored, and a new shell generation clears it so a later exit cannot
// name the previous process again.
func TestRestartInfoKeepsForegroundAcrossExitAndClearsOnNewShell(t *testing.T) {
	t.Parallel()
	tabs := exitedRestartTabs(t, Effects{}, terminal.Launch{Argv: []string{"/usr/bin/npm"}}, 1, true)
	p := tabs.Active()
	p.fgName = "claude"
	if info, ok := tabs.ExitedRestartInfo(); !ok || info.Program != "claude" || !info.ProgramFromForeground {
		t.Fatalf("exited info = %+v, %v", info, ok)
	}
	// A late poll answer for the dead shell is ignored and does not change it.
	p.Update(foregroundPollMsg{gen: p.gen, name: "late", ok: true})
	if info, _ := tabs.ExitedRestartInfo(); info.Program != "claude" {
		t.Fatalf("a late poll changed the dead indicator: %+v", info)
	}
	// The new shell generation starts without a foreground name; when it exits too,
	// the indicator falls back to the recorded launch program, never the old one.
	p.Update(startedMsg{term: newFakeTerm(), cols: 80, rows: 4})
	if p.lastForegroundName() != "" {
		t.Fatalf("a new shell kept the old foreground name %q", p.lastForegroundName())
	}
	p.Update(exitMsg{gen: p.gen, code: 0, codeOK: true})
	if info, ok := tabs.ExitedRestartInfo(); !ok || info.Program != "npm" || info.ProgramFromForeground {
		t.Fatalf("new-shell exit reused the old program: %+v, %v", info, ok)
	}
}
