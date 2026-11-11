package tui

import (
	"context"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/agentdiscover"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/picker"
)

// fakeDiscoverer answers scans with a scripted report and counts them.
type fakeDiscoverer struct {
	report agentdiscover.Report
	calls  int
}

func (f *fakeDiscoverer) Discover(context.Context) agentdiscover.Report {
	f.calls++
	return f.report
}

// discoverOverlay returns the top overlay when it is the discovery picker.
func discoverOverlay(m *Model) (*agentDiscoverOverlay, bool) {
	if len(m.overlays) == 0 {
		return nil, false
	}
	o, ok := m.overlays[len(m.overlays)-1].(*agentDiscoverOverlay)
	return o, ok
}

// discoverModel is a dock model with a discovery source; the dock's fake starter
// implements the Launcher port, so attach argv can start.
func discoverModel(t *testing.T, source AgentDiscoverer) *Model {
	t.Helper()
	m := newDockModel(t, newFakeEditor("hello"), DockPorts{Starter: newFakeStarter()})
	if source != nil {
		m.SetAgentDiscoverer(source)
	}
	return m
}

// runDiscoverDone executes a scan command and applies its report message.
func runDiscoverDone(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("the scan started no command")
	}
	if _, cmd = m.Update(cmd()); cmd != nil {
		t.Logf("scan follow-up command: %T", cmd)
	}
}

func TestDiscoverWithoutAScannerShowsANotice(t *testing.T) {
	m := discoverModel(t, nil)
	rootPaletteRun(m, "discover_agent_sessions")
	if len(m.overlays) != 0 {
		t.Fatalf("overlays = %d, want none", len(m.overlays))
	}
	if m.notice != agentDiscoverMissing() {
		t.Fatalf("notice = %q, want %q", m.notice, agentDiscoverMissing())
	}
}

func TestDiscoverPickerListsSessionsAndOpensOne(t *testing.T) {
	source := &fakeDiscoverer{report: agentdiscover.Report{
		Installed:     true,
		AllowCommands: true,
		Sessions: []agentdiscover.Session{{
			Socket: "/tmp/tmux-501/default", Server: "default", ID: "default/work",
			Name: "work", Path: "/home/me/project", Attached: true, Panes: 2,
			Agents:     []string{"claude"},
			AttachArgv: []string{"tmux", "-S", "/tmp/tmux-501/default", "attach", "-t", "work"},
		}},
	}}
	m := discoverModel(t, source)
	cmd := rootPaletteRun(m, "discover_agent_sessions")
	if _, ok := discoverOverlay(m); !ok {
		t.Fatalf("top overlay = %T, want the discovery picker", m.overlays[len(m.overlays)-1])
	}
	runDiscoverDone(t, m, cmd)
	o, ok := discoverOverlay(m)
	if !ok {
		t.Fatal("the picker closed with the report")
	}
	if got := o.p.Rows(); got != 1 {
		t.Fatalf("picker rows = %d, want the one session", got)
	}
	// The row says attached and the probable agent.
	g := grid.New(80, 24)
	o.Draw(g)
	plain := strings.Join(g.Plain(), " ")
	if !strings.Contains(plain, "work") || !strings.Contains(plain, "attached") || !strings.Contains(plain, "claude") {
		t.Fatalf("painted picker = %q, want the session markers", plain)
	}
	// Enter opens the attach argv in a new dock tab.
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(m.overlays) != 0 {
		t.Fatalf("overlays after Enter = %d, want the picker closed", len(m.overlays))
	}
	if m.dock.Count() != 1 {
		t.Fatalf("dock tabs = %d, want the opened session", m.dock.Count())
	}
	if got := m.dock.Name(0); got != "tmux:work · claude" {
		t.Fatalf("tab name = %q", got)
	}
}

func TestDiscoverPickerShowsProblemsAndEmptyState(t *testing.T) {
	source := &fakeDiscoverer{report: agentdiscover.Report{
		Installed: true,
		Problems:  []string{"tmux -S /tmp/x list-sessions: permission denied"},
	}}
	m := discoverModel(t, source)
	cmd := rootPaletteRun(m, "discover_agent_sessions")
	runDiscoverDone(t, m, cmd)
	o, ok := discoverOverlay(m)
	if !ok {
		t.Fatal("the picker is not open")
	}
	if got := o.p.Rows(); got != 0 {
		t.Fatalf("rows = %d with no sessions", got)
	}
	g := grid.New(80, 24)
	o.Draw(g)
	plain := strings.Join(g.Plain(), " ")
	if !strings.Contains(plain, "Discovery problems") || !strings.Contains(plain, "permission denied") {
		t.Fatalf("painted picker = %q, want the problem stated", plain)
	}
}

func TestDiscoverCtrlRScansAgain(t *testing.T) {
	source := &fakeDiscoverer{report: agentdiscover.Report{Installed: true, Sessions: []agentdiscover.Session{{
		Name: "s", Server: "srv", AttachArgv: []string{"tmux", "attach"},
	}}}}
	m := discoverModel(t, source)
	cmd := rootPaletteRun(m, "discover_agent_sessions")
	runDiscoverDone(t, m, cmd)
	if source.calls != 1 {
		t.Fatalf("scans = %d, want 1", source.calls)
	}
	_, rescan := m.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if _, ok := discoverOverlay(m); !ok {
		t.Fatal("Ctrl+R closed the picker")
	}
	runDiscoverDone(t, m, rescan)
	if source.calls != 2 {
		t.Fatalf("scans after Ctrl+R = %d, want 2", source.calls)
	}
}

func TestDiscoverStaleReportIsDropped(t *testing.T) {
	source := &fakeDiscoverer{report: agentdiscover.Report{Installed: true, Sessions: []agentdiscover.Session{{Name: "fresh", Server: "s", AttachArgv: []string{"tmux", "attach"}}}}}
	m := discoverModel(t, source)
	cmd := rootPaletteRun(m, "discover_agent_sessions")
	o, _ := discoverOverlay(m)
	if o == nil {
		t.Fatal("no picker")
	}
	// A report for an older scan must not replace the rows.
	m.Update(agentDiscoverDoneMsg{ov: o, seq: o.seq - 1, report: agentdiscover.Report{Sessions: []agentdiscover.Session{{Name: "stale"}}}})
	if got := o.p.Rows(); got != 0 {
		t.Fatalf("stale report installed %d rows", got)
	}
	runDiscoverDone(t, m, cmd)
	if got := o.p.Rows(); got != 1 {
		t.Fatalf("rows = %d after the live report", got)
	}
}

// TestDiscoverClosedPickerLateReportIsDropped: picker A starts a scan, closes, picker B opens
// with its own scan (both start at seq 1), B installs its report, and only then does A's old
// report arrive. The instance identity must drop A's report instead of letting it replace B's
// rows; the per-picker sequence alone cannot tell them apart.
func TestDiscoverClosedPickerLateReportIsDropped(t *testing.T) {
	stale := &fakeDiscoverer{report: agentdiscover.Report{Installed: true, Sessions: []agentdiscover.Session{{
		Name: "stale", Server: "s", AttachArgv: []string{"tmux", "attach"},
	}}}}
	fresh := &fakeDiscoverer{report: agentdiscover.Report{Installed: true, Sessions: []agentdiscover.Session{{
		Name: "fresh", Server: "s", AttachArgv: []string{"tmux", "attach"},
	}}}}
	m := discoverModel(t, stale)
	cmdA := rootPaletteRun(m, "discover_agent_sessions")
	oA, ok := discoverOverlay(m)
	if !ok || cmdA == nil {
		t.Fatal("picker A did not open with a scan")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if len(m.overlays) != 0 {
		t.Fatal("picker A did not close")
	}

	m.SetAgentDiscoverer(fresh)
	cmdB := rootPaletteRun(m, "discover_agent_sessions")
	oB, ok := discoverOverlay(m)
	if !ok || cmdB == nil || oB == oA {
		t.Fatal("picker B did not open as a new instance")
	}
	runDiscoverDone(t, m, cmdB) // B's own report lands first
	if len(oB.report.Sessions) != 1 || oB.report.Sessions[0].Name != "fresh" {
		t.Fatalf("B report = %+v, want fresh", oB.report.Sessions)
	}

	m.Update(cmdA()) // A's old scan answers after B has its rows
	if got := oB.p.Rows(); got != 1 || oB.report.Sessions[0].Name != "fresh" {
		t.Fatalf("late A report replaced B: rows %d report %+v", got, oB.report.Sessions)
	}
}

func TestDiscoverRankMessageReachesItsPicker(t *testing.T) {
	source := &fakeDiscoverer{report: agentdiscover.Report{Installed: true}}
	m := discoverModel(t, source)
	rootPaletteRun(m, "discover_agent_sessions")
	o, _ := discoverOverlay(m)
	if o == nil {
		t.Fatal("no picker")
	}
	res := o.p.SetItems([]picker.Item{{ID: 0, Text: "alpha"}, {ID: 1, Text: "beta"}})
	if res.Rank != nil {
		// A ranking command executes off the update loop; its message comes back to
		// this picker and is applied rather than being dropped.
		m.Update(agentDiscoverRankMsg{ov: o, msg: res.Rank()})
	}
	if _, ok := discoverOverlay(m); !ok {
		t.Fatal("the rank message closed the picker")
	}
}

// fakeWorkspaces is the daemon workspace layer as the discovery picker calls it.
type fakeWorkspaces struct {
	created [][2]string
	err     error
}

func (f *fakeWorkspaces) Entries() []WorkspaceEntry { return nil }
func (f *fakeWorkspaces) Select(string) (WorkspaceCmd, error) {
	return nil, nil
}
func (f *fakeWorkspaces) Create(root, label string) (WorkspaceCmd, error) {
	f.created = append(f.created, [2]string{root, label})
	return nil, f.err
}
func (f *fakeWorkspaces) Close(string) (WorkspaceCmd, error) { return nil, nil }
func (f *fakeWorkspaces) Rename(string, string) error        { return nil }
func (f *fakeWorkspaces) Next() (WorkspaceCmd, error)        { return nil, nil }

// discoverSessionReport is a one-session report for the Import tests.
func discoverSessionReport() agentdiscover.Report {
	return agentdiscover.Report{Installed: true, Sessions: []agentdiscover.Session{{
		Socket: "/tmp/tmux-501/default", Server: "default", ID: "default/work",
		Name: "work", Path: "/home/me/project", Attached: true,
		AttachArgv: []string{"tmux", "-S", "/tmp/tmux-501/default", "attach", "-t", "work"},
	}}}
}

func TestDiscoverImportCreatesAWorkspace(t *testing.T) {
	source := &fakeDiscoverer{report: discoverSessionReport()}
	m := discoverModel(t, source)
	workspaces := &fakeWorkspaces{}
	m.SetWorkspacePort(workspaces)
	cmd := rootPaletteRun(m, "discover_agent_sessions")
	runDiscoverDone(t, m, cmd)
	_, importCmd := m.Update(tea.KeyPressMsg{Code: 'i', Mod: tea.ModAlt})
	if len(workspaces.created) != 1 {
		t.Fatalf("imports = %v, want one workspace", workspaces.created)
	}
	if got := workspaces.created[0]; got[0] != "/home/me/project" || got[1] != "tmux:work" {
		t.Fatalf("workspace = %v, want the session root and label", got)
	}
	if len(m.overlays) != 0 {
		t.Fatalf("overlays = %d, want the picker closed after Import", len(m.overlays))
	}
	if !strings.Contains(m.notice, "cannot start the session's terminal") || !strings.Contains(m.notice, "tmux -S") {
		t.Fatalf("notice = %q, want the create-only fallback statement with the attach argv", m.notice)
	}
	if importCmd != nil {
		_ = importCmd() // the fake returns no command; a real one only reports notices
	}
}

func TestDiscoverPlainIIsFilterText(t *testing.T) {
	source := &fakeDiscoverer{report: discoverSessionReport()}
	m := discoverModel(t, source)
	workspaces := &fakeWorkspaces{}
	m.SetWorkspacePort(workspaces)
	cmd := rootPaletteRun(m, "discover_agent_sessions")
	runDiscoverDone(t, m, cmd)
	m.Update(typed("i"))
	if len(workspaces.created) != 0 {
		t.Fatalf("a typed i imported %v, want it in the filter", workspaces.created)
	}
	o, ok := discoverOverlay(m)
	if !ok {
		t.Fatal("the picker closed on a typed i")
	}
	if got := o.p.Query(); got != "i" {
		t.Fatalf("query = %q, want %q", got, "i")
	}
}

func TestDiscoverImportWithoutTheWorkspaceLayer(t *testing.T) {
	source := &fakeDiscoverer{report: discoverSessionReport()}
	m := discoverModel(t, source)
	cmd := rootPaletteRun(m, "discover_agent_sessions")
	runDiscoverDone(t, m, cmd)
	m.Update(tea.KeyPressMsg{Code: 'i', Mod: tea.ModAlt})
	if !strings.Contains(m.notice, "daemon-hosted session") {
		t.Fatalf("notice = %q, want the workspace-layer explanation", m.notice)
	}
	if _, ok := discoverOverlay(m); !ok {
		t.Fatal("the picker closed without importing")
	}
}

func TestDiscoverImportWithoutADirectory(t *testing.T) {
	report := discoverSessionReport()
	report.Sessions[0].Path = ""
	m := discoverModel(t, &fakeDiscoverer{report: report})
	workspaces := &fakeWorkspaces{}
	m.SetWorkspacePort(workspaces)
	cmd := rootPaletteRun(m, "discover_agent_sessions")
	runDiscoverDone(t, m, cmd)
	m.Update(tea.KeyPressMsg{Code: 'i', Mod: tea.ModAlt})
	if len(workspaces.created) != 0 {
		t.Fatalf("imported a session with no directory: %v", workspaces.created)
	}
	if !strings.Contains(m.notice, "records no directory") {
		t.Fatalf("notice = %q, want the missing-directory explanation", m.notice)
	}
}

// launchWorkspaces is a workspace port with the queued-launch method.
type launchWorkspaces struct {
	fakeWorkspaces
	roots   []string
	labels  []string
	launch  []WorkspaceLaunch
	command WorkspaceCmd
}

func (l *launchWorkspaces) CreateWithLaunch(root, label string, launch WorkspaceLaunch) (WorkspaceCmd, error) {
	l.roots = append(l.roots, root)
	l.labels = append(l.labels, label)
	l.launch = append(l.launch, launch)
	return l.command, nil
}

func TestDiscoverImportQueuesTheSessionAttachLaunch(t *testing.T) {
	source := &fakeDiscoverer{report: discoverSessionReport()}
	m := discoverModel(t, source)
	workspaces := &launchWorkspaces{}
	m.SetWorkspacePort(workspaces)
	cmd := rootPaletteRun(m, "discover_agent_sessions")
	runDiscoverDone(t, m, cmd)
	_, importCmd := m.Update(tea.KeyPressMsg{Code: 'i', Mod: tea.ModAlt})
	if len(workspaces.roots) != 1 || workspaces.roots[0] != "/home/me/project" {
		t.Fatalf("import roots = %v", workspaces.roots)
	}
	if workspaces.labels[0] != "tmux:work" {
		t.Fatalf("import label = %q", workspaces.labels[0])
	}
	launch := workspaces.launch[0]
	wantArgv := []string{"tmux", "-S", "/tmp/tmux-501/default", "attach", "-t", "work"}
	if !slices.Equal(launch.Argv, wantArgv) || !slices.Equal(launch.ResumeArgv, wantArgv) {
		t.Fatalf("queued launch argv = %v / %v, want the attach argv", launch.Argv, launch.ResumeArgv)
	}
	if launch.Name != "tmux:work" || launch.Dir != "/home/me/project" || launch.Source != "tmux:default/work" {
		t.Fatalf("queued launch = %+v", launch)
	}
	if !launch.Ephemeral || launch.Agent != "" {
		t.Fatalf("queued launch flags = %+v", launch)
	}
	if len(m.overlays) != 0 {
		t.Fatalf("overlays = %d, want the picker closed after Import", len(m.overlays))
	}
	if importCmd != nil {
		if msg := importCmd(); msg != nil {
			t.Logf("import command produced %T (the fake queues no work)", msg)
		}
	}
}

// TestStartWorkspaceLaunchKeepsTheRecordAndDedupsBySource checks the hook the
// workspace layer calls once an imported workspace is shown: the attach record
// (argv, resume, dir, source) reaches the dock, a repeated source focuses the
// existing tab instead of duplicating, and a second tmux session in the same root
// opens its own tab rather than being dropped.
func TestStartWorkspaceLaunchKeepsTheRecordAndDedupsBySource(t *testing.T) {
	starter := newFakeStarter()
	m := newDockModel(t, newFakeEditor("hello"), DockPorts{Starter: starter})
	attachA := []string{"tmux", "-S", "/tmp/tmux-501/default", "attach", "-t", "a"}
	launchA := WorkspaceLaunch{
		Argv: attachA, Name: "tmux:a", Agent: "claude", ResumeArgv: attachA,
		Ephemeral: true, Dir: "/root/a", Source: "tmux:s/a",
	}
	cmd := m.StartWorkspaceLaunch(launchA)
	if cmd == nil {
		t.Fatal("the first launch returned no command")
	}
	m.Update(cmd())
	if len(starter.launchDirs) != 1 || starter.launchDirs[0] != "/root/a" {
		t.Fatalf("launch dirs = %v, want the session directory", starter.launchDirs)
	}
	got := starter.launches[0]
	if !slices.Equal(got.Argv, attachA) || !slices.Equal(got.ResumeArgv, attachA) {
		t.Fatalf("record = %+v, want the attach argv as launch and resume", got)
	}
	if got.Agent != "claude" || !got.Ephemeral || got.Source != "tmux:s/a" {
		t.Fatalf("record flags = %+v", got)
	}
	if m.dock.Count() != 1 || m.dock.Name(0) != "tmux:a" {
		t.Fatalf("dock = %d tabs, name %q", m.dock.Count(), m.dock.Name(0))
	}
	// A repeated import of the same source focuses the existing tab: no duplicate.
	if cmd := m.StartWorkspaceLaunch(launchA); cmd != nil {
		t.Fatalf("the repeated import started a command: %v", cmd)
	}
	if m.dock.Count() != 1 || len(starter.launches) != 1 {
		t.Fatalf("a repeated import duplicated the tab: %d tabs, %d launches", m.dock.Count(), len(starter.launches))
	}
	// A second session in the same root is not suppressed: its own attach tab.
	attachB := []string{"tmux", "-S", "/tmp/tmux-501/default", "attach", "-t", "b"}
	cmd = m.StartWorkspaceLaunch(WorkspaceLaunch{
		Argv: attachB, Name: "tmux:b", ResumeArgv: attachB, Ephemeral: true,
		Dir: "/root/a", Source: "tmux:s/b",
	})
	if cmd == nil {
		t.Fatal("the second session in the same root returned no command")
	}
	m.Update(cmd())
	if m.dock.Count() != 2 || len(starter.launches) != 2 || !slices.Equal(starter.launches[1].Argv, attachB) {
		t.Fatalf("second same-root session: %d tabs, launches %+v", m.dock.Count(), starter.launches)
	}
	// An empty launch directory falls back to the workspace root.
	cmd = m.StartWorkspaceLaunch(WorkspaceLaunch{
		Argv: attachA, Name: "tmux:c", Ephemeral: true, Source: "tmux:s/c",
	})
	if cmd == nil {
		t.Fatal("the empty-dir launch returned no command")
	}
	m.Update(cmd())
	if got := starter.launchDirs[len(starter.launchDirs)-1]; got != "/ws" {
		t.Fatalf("empty-dir launch dir = %q, want the workspace root", got)
	}
}

// externalReport is a scan report with the two local transcript stores and no
// tmux findings.
func externalReport() agentdiscover.Report {
	return agentdiscover.Report{
		Installed:     true,
		AllowCommands: true,
		External: agentdiscover.ExternalReport{
			Installed: true,
			Sessions: []agentdiscover.ExternalSession{{
				Tool: agentdiscover.ExternalClaude, ID: "22222222-aaaa-bbbb-cccc-000000000002",
				Title: "proj", CWD: "/home/u/proj", Path: "/home/u/.claude/projects/x/22222222.jsonl",
				Agent:      "claude",
				ResumeArgv: []string{"claude", "--resume", "22222222-aaaa-bbbb-cccc-000000000002"},
				Exact:      true,
			}, {
				Tool: agentdiscover.ExternalCodex, ID: "abcdef01-2345-6789-abcd-ef0123456789",
				Title: "codexproj", CWD: "/home/u/codexproj",
				Agent: "codex", ResumeArgv: []string{"codex", "resume", "--last"}, Exact: false,
			}},
			Tools: []agentdiscover.ExternalTool{
				{ID: agentdiscover.ExternalClaude, Name: "Claude Code", Installed: true, Count: 1},
				{ID: agentdiscover.ExternalCodex, Name: "Codex CLI", Installed: true, Count: 1},
			},
		},
	}
}

func newExternalDiscoverModel(t *testing.T, report agentdiscover.Report) (*Model, *fakeStarter) {
	t.Helper()
	starter := newFakeStarter()
	m := newDockModel(t, newFakeEditor("hello"), DockPorts{Starter: starter})
	m.SetAgentDiscoverer(&fakeDiscoverer{report: report})
	return m, starter
}

func TestDiscoverListsLocalAgentTranscripts(t *testing.T) {
	m, starter := newExternalDiscoverModel(t, externalReport())
	cmd := rootPaletteRun(m, "discover_agent_sessions")
	runDiscoverDone(t, m, cmd)
	o, ok := discoverOverlay(m)
	if !ok {
		t.Fatal("the picker is not open")
	}
	if got := o.p.Rows(); got != 2 {
		t.Fatalf("rows = %d, want the two transcript sessions", got)
	}
	// Listing is just a scan: nothing is started until the user acts.
	if len(starter.launches) != 0 || len(starter.dirs) != 0 {
		t.Fatalf("the scan started a terminal: launches %+v dirs %v", starter.launches, starter.dirs)
	}
	g := grid.New(100, 24)
	o.Draw(g)
	plain := strings.Join(g.Plain(), " ")
	for _, want := range []string{"Claude Code", "proj", "Codex CLI", "codexproj", "claude --resume", "codex resume --last"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("painted picker = %q, want %q", plain, want)
		}
	}
	// The inexact Codex resume says so on its detail line (the painted row may be
	// clipped by the picker width).
	if detail := discoverRowDetail(o.rows[1]); !strings.Contains(detail, "resumes the newest session in that directory") {
		t.Fatalf("codex detail = %q, want the inexact-resume note", detail)
	}
}

func TestDiscoverResumesATranscriptAndDedupsItsSource(t *testing.T) {
	m, starter := newExternalDiscoverModel(t, externalReport())
	cmd := rootPaletteRun(m, "discover_agent_sessions")
	runDiscoverDone(t, m, cmd)
	_, resume := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // first row: Claude Code proj
	dockSettle(t, m, resume)
	if len(starter.launches) != 1 {
		t.Fatalf("launches = %+v, want the claude resume", starter.launches)
	}
	got := starter.launches[0]
	if !slices.Equal(got.Argv, []string{"claude", "--resume", "22222222-aaaa-bbbb-cccc-000000000002"}) {
		t.Fatalf("resume argv = %v", got.Argv)
	}
	if !slices.Equal(got.ResumeArgv, got.Argv) || got.Agent != "claude" || !got.Ephemeral {
		t.Fatalf("launch record = %+v", got)
	}
	if got.Source != "claude-code:22222222-aaaa-bbbb-cccc-000000000002" {
		t.Fatalf("source = %q", got.Source)
	}
	if len(starter.launchDirs) != 1 || starter.launchDirs[0] != "/home/u/proj" {
		t.Fatalf("launch dirs = %v, want the session cwd", starter.launchDirs)
	}
	if m.dock.Count() != 1 || m.dock.Name(0) != "Claude Code:proj" {
		t.Fatalf("dock = %d tabs, name %q", m.dock.Count(), m.dock.Name(0))
	}
	// Choosing the same transcript again focuses the tab that already resumed it.
	m.dock.SetFocus(false)
	cmd = rootPaletteRun(m, "discover_agent_sessions")
	runDiscoverDone(t, m, cmd)
	_, resume = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	dockSettle(t, m, resume)
	if len(starter.launches) != 1 || m.dock.Count() != 1 {
		t.Fatalf("a repeated choice duplicated the resume: launches %+v tabs %d", starter.launches, m.dock.Count())
	}
	if !m.dock.Focus() {
		t.Fatal("a repeated choice left the dock unfocused")
	}
}

func TestDiscoverImportsATranscriptWorkspace(t *testing.T) {
	m, _ := newExternalDiscoverModel(t, externalReport())
	workspaces := &launchWorkspaces{}
	m.SetWorkspacePort(workspaces)
	cmd := rootPaletteRun(m, "discover_agent_sessions")
	runDiscoverDone(t, m, cmd)
	m.Update(tea.KeyPressMsg{Code: 'i', Mod: tea.ModAlt})
	if len(workspaces.roots) != 1 || workspaces.roots[0] != "/home/u/proj" {
		t.Fatalf("import roots = %v", workspaces.roots)
	}
	if workspaces.labels[0] != "claude:proj" {
		t.Fatalf("import label = %q", workspaces.labels[0])
	}
	launch := workspaces.launch[0]
	if !slices.Equal(launch.Argv, []string{"claude", "--resume", "22222222-aaaa-bbbb-cccc-000000000002"}) {
		t.Fatalf("import launch argv = %v", launch.Argv)
	}
	if launch.Agent != "claude" || !launch.Ephemeral || launch.Dir != "/home/u/proj" {
		t.Fatalf("import launch = %+v", launch)
	}
	if launch.Source != "claude-code:22222222-aaaa-bbbb-cccc-000000000002" {
		t.Fatalf("import source = %q", launch.Source)
	}
}

func TestDiscoverTranscriptProblemsAndNoDirectory(t *testing.T) {
	report := externalReport()
	report.External.Sessions[0].CWD = ""
	report.External.Problems = []string{"Claude Code: permission denied"}
	m, _ := newExternalDiscoverModel(t, report)
	cmd := rootPaletteRun(m, "discover_agent_sessions")
	runDiscoverDone(t, m, cmd)
	o, _ := discoverOverlay(m)
	g := grid.New(100, 24)
	o.Draw(g)
	if plain := strings.Join(g.Plain(), " "); !strings.Contains(plain, "Discovery problems") || !strings.Contains(plain, "permission denied") {
		t.Fatalf("painted picker = %q, want the transcript problem stated", plain)
	}
	workspaces := &launchWorkspaces{}
	m.SetWorkspacePort(workspaces)
	m.Update(tea.KeyPressMsg{Code: 'i', Mod: tea.ModAlt})
	if len(workspaces.roots) != 0 {
		t.Fatalf("imported a transcript with no directory: %v", workspaces.roots)
	}
	if !strings.Contains(m.notice, "records no directory") {
		t.Fatalf("notice = %q, want the missing-directory explanation", m.notice)
	}
}

func TestClipDiscoveryTextCutsOnARuneBoundary(t *testing.T) {
	for _, tc := range []struct {
		text  string
		limit int
		want  string
	}{
		{"short", 80, "short"},
		{"日本語", 4, "日"},
		{"ab日本", 5, "ab日"},
		{"abcdef", 3, "abc"},
	} {
		if got := clipDiscoveryText(tc.text, tc.limit); got != tc.want {
			t.Errorf("clipDiscoveryText(%q, %d) = %q, want %q", tc.text, tc.limit, got, tc.want)
		}
	}
}
