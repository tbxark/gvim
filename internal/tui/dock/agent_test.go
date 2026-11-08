package dock

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/terminal"
)

// agentPanel returns a dock panel with the host and ports but no shell started.
func agentPanel(effects Effects, starter *fakeStarter) *Panel {
	var p Panel
	p.SetHost(Host{
		RootDir:     func() string { return "/ws" },
		Notice:      func(string) {},
		ContentSize: func() (int, int) { return 80, 4 },
		Focus:       func() {},
	})
	p.SetPorts(Ports{Starter: starter})
	p.SetEffects(effects)
	return &p
}

func TestRestartPrecedence(t *testing.T) {
	t.Parallel()
	claude := terminal.Launch{
		Argv:       []string{"claude", "--session-id", "s1"},
		Agent:      "claude",
		ResumeArgv: []string{"claude", "--resume", "s1"},
	}
	tests := []struct {
		name    string
		resume  bool
		record  terminal.Launch
		want    []string // the StartLaunch argv, nil for the plain shell
		started bool
	}{
		{"resume on uses the resume argv", true, claude, []string{"claude", "--resume", "s1"}, true},
		{"resume off uses the launch argv", false, claude, []string{"claude", "--session-id", "s1"}, true},
		{"launch only", true, terminal.Launch{Argv: []string{"aider"}}, []string{"aider"}, true},
		{"agent without a resume argv uses the launch argv", true, terminal.Launch{Argv: []string{"codex"}, Agent: "codex"}, []string{"codex"}, true},
		{"no record uses the shell", true, terminal.Launch{}, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			starter := &fakeStarter{term: newFakeTerm()}
			p := agentPanel(Effects{ResumeAgents: tt.resume}, starter)
			p.launch = tt.record
			cmd := p.Restart()
			if cmd == nil {
				t.Fatal("Restart started no command")
			}
			p.Update(cmd())
			if tt.started {
				if len(starter.launches) != 1 || !slices.Equal(starter.launches[0].Argv, tt.want) {
					t.Fatalf("launches = %+v, want argv %v", starter.launches, tt.want)
				}
				if len(starter.dirs) != 0 {
					t.Fatalf("the shell started too: %v", starter.dirs)
				}
				// The record is kept, so a second restart can resume again.
				if p.launch.Agent != tt.record.Agent {
					t.Fatalf("record agent = %q after the restart", p.launch.Agent)
				}
				return
			}
			if len(starter.launches) != 0 || len(starter.dirs) != 1 {
				t.Fatalf("launches = %+v shell starts = %v, want the shell", starter.launches, starter.dirs)
			}
		})
	}
}

func TestRestoredTerminalNeverStartsAndRestartsWithItsRecord(t *testing.T) {
	t.Parallel()
	starter := &fakeStarter{term: newFakeTerm()}
	p := agentPanel(Effects{ResumeAgents: true}, starter)
	p.Restore(terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true, Cwd: "/ws",
		Rows:       []string{"old output"},
		LaunchArgv: []string{"claude", "--session-id", "s1"},
		ResumeArgv: []string{"claude", "--resume", "s1"},
		Agent:      "claude",
		Ephemeral:  true,
	})
	if len(starter.dirs) != 0 || len(starter.launches) != 0 {
		t.Fatalf("restore started something: shell %v launches %+v", starter.dirs, starter.launches)
	}
	if !p.Exited() || !p.Restored() {
		t.Fatalf("restored: exited %v restored %v, want both", p.Exited(), p.Restored())
	}
	if title := p.title(); !strings.Contains(title, "restored") || !strings.Contains(title, "claude") {
		t.Fatalf("title = %q, want the restored agent transcript", title)
	}
	// A failed start keeps the record, so a retry can still resume.
	starter.err = errors.New("no pty")
	p.Update(p.Restart()())
	if len(starter.launches) != 1 || starter.launches[0].Argv[1] != "--resume" {
		t.Fatalf("launches = %+v, want the resume argv", starter.launches)
	}
	if p.launch.Agent != "claude" {
		t.Fatalf("the failed start lost the record: %+v", p.launch)
	}
	starter.err = nil
	p.Update(p.Restart()())
	if len(starter.launches) != 2 || !slices.Equal(starter.launches[1].Argv, []string{"claude", "--resume", "s1"}) {
		t.Fatalf("retry launches = %+v", starter.launches)
	}
}

func TestSavedStateCarriesTheLaunchRecord(t *testing.T) {
	t.Parallel()
	starter := &fakeStarter{term: newFakeTerm()}
	p := agentPanel(Effects{}, starter)
	p.launch = terminal.Launch{
		Argv: []string{"aider"}, Agent: "aider",
		ResumeArgv: []string{"aider", "--restore"}, Ephemeral: true,
	}
	st := p.SavedState()
	if !slices.Equal(st.LaunchArgv, []string{"aider"}) ||
		!slices.Equal(st.ResumeArgv, []string{"aider", "--restore"}) ||
		st.Agent != "aider" || !st.Ephemeral {
		t.Fatalf("saved launch record = %+v", st)
	}
	var q Panel
	q.SetHost(p.host)
	q.SetPorts(Ports{Starter: starter})
	q.SetEffects(Effects{})
	q.Restore(st)
	if !slices.Equal(q.launch.ResumeArgv, []string{"aider", "--restore"}) || q.launch.Agent != "aider" || !q.launch.Ephemeral {
		t.Fatalf("restored record = %+v", q.launch)
	}
}

func TestTabsStartAgentStartsATabAndRestartsWithResume(t *testing.T) {
	t.Parallel()
	starter := &fakeStarter{term: newFakeTerm()}
	var tabs Tabs
	tabs.SetHost(Host{
		RootDir:     func() string { return "/ws" },
		Notice:      func(string) {},
		ContentSize: func() (int, int) { return 80, 4 },
		Focus:       func() {},
	})
	tabs.SetPorts(Ports{Starter: starter})
	tabs.SetEffects(Effects{
		ResumeAgents: true,
		Agents: []Agent{{
			Name: "claude", Argv: []string{"claude", "--session-id", "s1"},
			ResumeArgv: []string{"claude", "--resume", "s1"},
		}},
	})
	if _, ok := tabs.StartAgent("nope"); ok {
		t.Fatal("StartAgent accepted an unknown agent")
	}
	cmd, ok := tabs.StartAgent("claude")
	if !ok || cmd == nil {
		t.Fatalf("StartAgent = %v, %v", cmd != nil, ok)
	}
	msg := cmd() // tabStartedMsg
	if _, ok := tabs.Update(msg); !ok {
		t.Fatal("the start message was not taken")
	}
	if tabs.Count() != 1 || tabs.Name(0) != "claude" || !tabs.Active().Running() {
		t.Fatalf("tabs after StartAgent: count %d name %q running %v", tabs.Count(), tabs.Name(0), tabs.Active().Running())
	}
	if len(starter.launches) != 1 || !slices.Equal(starter.launches[0].Argv, []string{"claude", "--session-id", "s1"}) {
		t.Fatalf("launches = %+v", starter.launches)
	}
	if !tabs.Active().launch.Ephemeral || tabs.Active().launch.Agent != "claude" {
		t.Fatalf("record = %+v", tabs.Active().launch)
	}
	// A user restart resumes.
	if cmd := tabs.Restart(); cmd != nil {
		tabs.Update(cmd())
	}
	if len(starter.launches) != 2 || !slices.Equal(starter.launches[1].Argv, []string{"claude", "--resume", "s1"}) {
		t.Fatalf("restart launches = %+v", starter.launches)
	}
}
