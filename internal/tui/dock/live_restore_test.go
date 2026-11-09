package dock

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/terminal"
)

// liveTestTabs returns a Tabs with the fake host/ports and the given effects.
func liveTestTabs(t *testing.T, effects Effects, starter *fakeStarter) *Tabs {
	t.Helper()
	var tabs Tabs
	tabs.SetHost(Host{
		RootDir:     func() string { return "/ws" },
		Notice:      func(string) {},
		ContentSize: func() (int, int) { return 80, 24 },
		Focus:       func() {},
	})
	tabs.SetPorts(Ports{Starter: starter})
	tabs.SetEffects(effects)
	return &tabs
}

// liveSavedState is a two-tab restore: tab 0 was live (an agent with a resume
// record), tab 1 was already exited.
func liveSavedState() terminal.SavedState {
	return terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true, ActiveTab: 0,
		Tabs: []terminal.SavedTab{
			{
				Name: "agent", Open: true, Cwd: "/work", LaunchDir: "/chosen",
				Rows:       []string{"agent transcript"},
				LaunchArgv: []string{"claude", "--session-id", "s1"},
				ResumeArgv: []string{"claude", "--resume", "s1"},
				Agent:      "claude", Live: true,
			},
			{Name: "build", Open: true, Rows: []string{"build transcript"}, Live: false},
		},
	}
}

// finishStart runs a tea command (a tabStartedMsg) through Update.
func finishStart(t *testing.T, tabs *Tabs, msg any) {
	t.Helper()
	started, ok := msg.(tabStartedMsg)
	if !ok {
		t.Fatalf("dive command returned %T, want tabStartedMsg", msg)
	}
	if next, ok := tabs.Update(started); !ok {
		t.Fatalf("the dive start was not taken (next %v)", next)
	}
}

func TestTabsLiveRestoreWaitsForDiveAndUsesResume(t *testing.T) {
	t.Parallel()
	starter := &fakeStarter{term: newFakeTerm()}
	tabs := liveTestTabs(t, Effects{RestoreLive: true, ResumeAgents: true}, starter)
	tabs.Restore(liveSavedState())
	if len(starter.dirs) != 0 || len(starter.launches) != 0 {
		t.Fatalf("restore started something: dirs %v launches %v", starter.dirs, starter.launches)
	}
	// A live-at-capture tab is dormant, not exited: no exited marker on it; the
	// exited tab keeps its marker.
	if got := tabs.TabNames(); got[0] != "agent" || got[1] != "build (exited)" {
		t.Fatalf("restored labels = %q", got)
	}
	// No dive: a Sync starts nothing.
	if cmd := tabs.Sync(); cmd != nil {
		t.Fatalf("Sync without a dive returned a command")
	}
	// Diving into the live tab (it is the active one: a press/switch) arms the
	// revive; Sync starts it with Fresh's resume precedence in the launch dir.
	if !tabs.SetActive(0) {
		t.Fatal("SetActive on the active tab failed")
	}
	cmd := tabs.Sync()
	if cmd == nil {
		t.Fatal("the dive did not arm a start")
	}
	finishStart(t, tabs, cmd())
	if len(starter.launches) != 1 || !slices.Equal(starter.launches[0].Argv, []string{"claude", "--resume", "s1"}) {
		t.Fatalf("launches = %+v, want the resume argv", starter.launches)
	}
	// Fresh respawns in the persisted last reported directory, not the recorded
	// launch_dir: the record's cwd wins.
	if len(starter.launchDirs) != 1 || starter.launchDirs[0] != "/work" {
		t.Fatalf("launch dirs = %v, want the saved cwd", starter.launchDirs)
	}
	if !tabs.Running() || tabs.Restored() || tabs.Exited() {
		t.Fatalf("after the dive: running %v restored %v exited %v", tabs.Running(), tabs.Restored(), tabs.Exited())
	}
	// The transcript is kept above the new shell and the tab keeps its identity.
	panel := tabs.tabs[0]
	if len(panel.prior) != 1 || panel.prior[0].Text() != "agent transcript" {
		t.Fatalf("prior transcript = %+v", panel.prior)
	}
	if got := tabs.TabNames(); got[0] != "agent" || got[1] != "build (exited)" {
		t.Fatalf("labels after the dive = %q", got)
	}
	if tabs.Count() != 2 || tabs.ActiveIndex() != 0 {
		t.Fatalf("tabs/active after the dive = %d/%d", tabs.Count(), tabs.ActiveIndex())
	}
	// A capture now records the revived tab as live, the untouched exited one dead.
	st := tabs.SavedState()
	if !st.Tabs[0].Live || st.Tabs[1].Live {
		t.Fatalf("captured liveness = %v/%v", st.Tabs[0].Live, st.Tabs[1].Live)
	}
}

func TestTabsLiveRestoreOnlyOnDiveAndRespectsOptions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		effects Effects
		state   terminal.SavedState
		tab     int
		starts  int
	}{
		{name: "restore_live off", effects: Effects{RestoreLive: false, ResumeAgents: true}, state: liveSavedState(), tab: 0, starts: 0},
		{name: "exited tab never revives", effects: Effects{RestoreLive: true, ResumeAgents: true}, state: liveSavedState(), tab: 1, starts: 0},
		{name: "old record is dead", effects: Effects{RestoreLive: true, ResumeAgents: true}, state: func() terminal.SavedState {
			st := liveSavedState()
			for i := range st.Tabs {
				st.Tabs[i].Live = false
			}
			return st
		}(), tab: 0, starts: 0},
		{name: "plain live shell revives", effects: Effects{RestoreLive: true, ResumeAgents: true}, state: func() terminal.SavedState {
			st := liveSavedState()
			st.Tabs[0] = terminal.SavedTab{Name: "shell", Open: true, Cwd: "/cwd", Rows: []string{"old"}, Live: true}
			return st
		}(), tab: 0, starts: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			starter := &fakeStarter{term: newFakeTerm()}
			tabs := liveTestTabs(t, tt.effects, starter)
			tabs.Restore(tt.state)
			tabs.SetActive(tt.tab)
			if cmd := tabs.Sync(); cmd != nil {
				finishStart(t, tabs, cmd())
			}
			if got := len(starter.dirs) + len(starter.launches); got != tt.starts {
				t.Fatalf("starts = %d (dirs %v launches %v), want %d", got, starter.dirs, starter.launches, tt.starts)
			}
		})
	}
}

func TestTabsLiveRestoreResumeAgentsOffUsesLaunchArgv(t *testing.T) {
	t.Parallel()
	starter := &fakeStarter{term: newFakeTerm()}
	tabs := liveTestTabs(t, Effects{RestoreLive: true, ResumeAgents: false}, starter)
	tabs.Restore(liveSavedState())
	tabs.SetActive(0)
	cmd := tabs.Sync()
	if cmd == nil {
		t.Fatal("the dive did not arm a start")
	}
	finishStart(t, tabs, cmd())
	if len(starter.launches) != 1 || !slices.Equal(starter.launches[0].Argv, []string{"claude", "--session-id", "s1"}) {
		t.Fatalf("launches = %+v, want the launch argv without resume", starter.launches)
	}
}

func TestTabsLiveRestoreFocusDivesAndFailureDoesNotRetry(t *testing.T) {
	t.Parallel()
	starter := &fakeStarter{term: newFakeTerm(), err: errors.New("start failed")}
	tabs := liveTestTabs(t, Effects{RestoreLive: true, ResumeAgents: true}, starter)
	tabs.Restore(liveSavedState())
	// Giving the dock the keyboard dives into its active live tab.
	tabs.SetFocus(true)
	cmd := tabs.Sync()
	if cmd == nil {
		t.Fatal("focusing the dock did not arm the dive")
	}
	finishStart(t, tabs, cmd())
	if got := tabs.Notice(); !strings.Contains(got, "start") {
		t.Fatalf("notice after the failed dive = %q", got)
	}
	// The failed dive is not retried by another dive; an explicit restart can still
	// be asked for.
	tabs.SetActive(0)
	if cmd := tabs.Sync(); cmd != nil {
		t.Fatalf("a failed dive retried: %T", cmd())
	}
	if got := len(starter.dirs) + len(starter.launches); got != 1 {
		t.Fatalf("start attempts = %d, want one", got)
	}
}

func TestTabsRestoreCapturesLiveFlag(t *testing.T) {
	t.Parallel()
	starter := &fakeStarter{term: newFakeTerm()}
	tabs := liveTestTabs(t, Effects{RestoreLive: true, ResumeAgents: true}, starter)
	startTab(t, tabs)
	if st := tabs.SavedState(); !st.Tabs[0].Live || !st.Live {
		t.Fatalf("a running tab captured dead: %+v", st)
	}
	// Exiting the tab captures it dead.
	if _, ok := tabs.Update(exitMsg{gen: tabs.tabs[0].gen}); !ok {
		t.Fatal("the exit message was not taken")
	}
	if st := tabs.SavedState(); st.Tabs[0].Live || st.Live {
		t.Fatalf("an exited tab captured live: %+v", st)
	}
}

func TestTabsRunningTabsListsBaseLabels(t *testing.T) {
	t.Parallel()
	starter := &fakeStarter{term: newFakeTerm()}
	tabs := liveTestTabs(t, Effects{RestoreLive: true, ResumeAgents: true}, starter)
	startTab(t, tabs)
	tabs.RenameActive("build")
	startTab(t, tabs)
	tabs.tabs[1].launch = terminal.Launch{Argv: []string{"claude"}, Agent: "claude", ResumeArgv: []string{"claude", "--resume"}}
	running := tabs.RunningTabs()
	if len(running) != 2 {
		t.Fatalf("running tabs = %+v", running)
	}
	if running[0].Name != "build" || running[0].Agent != "" {
		t.Fatalf("first running tab = %+v", running[0])
	}
	if running[1].Name != "claude" || running[1].Agent != "claude" {
		t.Fatalf("second running tab = %+v", running[1])
	}
}

// TestDockShowKeepsARestoredTranscriptReadOnly checks opening the dock after a
// restore does not spawn anything behind the read-only transcript (a dive or an
// explicit restart is still required).
func TestDockShowKeepsARestoredTranscriptReadOnly(t *testing.T) {
	t.Parallel()
	starter := &fakeStarter{term: newFakeTerm()}
	tabs := liveTestTabs(t, Effects{RestoreLive: true, ResumeAgents: true}, starter)
	var st terminal.SavedState
	st.Version, st.Open, st.Cwd, st.Live = terminal.SavedStateVersion, true, "/work", true
	st.Rows = []string{"restored output"}
	st.LaunchArgv, st.ResumeArgv, st.Agent = []string{"claude"}, []string{"claude", "--resume"}, "claude"
	st.Tabs = []terminal.SavedTab{{
		Name: "agent", Open: true, Cwd: "/work", Rows: []string{"restored output"},
		LaunchArgv: st.LaunchArgv, ResumeArgv: st.ResumeArgv, Agent: "claude", Live: true,
	}}
	tabs.Restore(st)
	tabs.Hide()
	cmd := tabs.Show()
	if cmd != nil {
		// Show may return the transcript request command; it must not be a start.
		for _, msg := range messages(cmd) {
			if _, ok := msg.(tabStartedMsg); ok {
				t.Fatalf("Show spawned a shell: %T", msg)
			}
			tabs.Update(msg)
		}
	}
	if len(starter.dirs) != 0 || len(starter.launches) != 0 {
		t.Fatalf("Show started something: dirs %v launches %v", starter.dirs, starter.launches)
	}
	if !tabs.Restored() {
		t.Fatal("the restored transcript is no longer read-only after Show")
	}
}
