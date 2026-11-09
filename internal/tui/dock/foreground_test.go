package dock

import (
	"strings"
	"testing"
)

// startForegroundPanel starts a shell with the foreground poll enabled and returns
// the panel after the first poll answer (fgName set).
func startForegroundPanel(t *testing.T, term *fakeTerm) *Panel {
	t.Helper()
	term.fgSupported = true
	term.fgOK = true
	var p Panel
	p.SetHost(Host{
		RootDir:     func() string { return "/ws" },
		Notice:      func(string) {},
		ContentSize: func() (int, int) { return 80, 4 },
		Focus:       func() {},
	})
	p.SetPorts(Ports{Starter: &fakeStarter{term: term}})
	cmd := p.Show()
	if cmd == nil {
		t.Fatal("Show did not start a shell")
	}
	msgs := messages(cmd)
	if len(msgs) != 1 {
		t.Fatalf("start produced %d messages", len(msgs))
	}
	poll, ok := p.Update(msgs[0])
	if !ok || poll == nil {
		t.Fatalf("start did not arm the foreground poll: %v, %v", poll, ok)
	}
	pollMsgs := messages(poll)
	if len(pollMsgs) != 1 {
		t.Fatalf("poll produced %d messages", len(pollMsgs))
	}
	if _, ok := pollMsgs[0].(foregroundPollMsg); !ok {
		t.Fatalf("poll message = %T", pollMsgs[0])
	}
	tick, ok := p.Update(pollMsgs[0])
	if !ok || tick == nil {
		t.Fatalf("poll answer did not schedule the next tick: %v, %v", tick, ok)
	}
	return &p
}

// TestLocalShellPollsForegroundName checks the bounded poll chain: the start arms
// a poll command, the answer caches the name and schedules a tick, and a tick
// starts the next poll, so a changed foreground program updates the label.
func TestLocalShellPollsForegroundName(t *testing.T) {
	t.Parallel()
	term := newFakeTerm()
	term.fgName = "vim"
	p := startForegroundPanel(t, term)
	if p.fgName != "vim" {
		t.Fatalf("cached foreground name = %q, want vim", p.fgName)
	}
	if term.fgCalls != 1 {
		t.Fatalf("foreground polls = %d, want one read in the command", term.fgCalls)
	}
	// A tick starts the next poll; the answer replaces the cached name.
	poll, ok := p.Update(foregroundPollTickMsg{gen: p.gen})
	if !ok || poll == nil {
		t.Fatalf("tick did not start a poll: %v, %v", poll, ok)
	}
	term.fgName = "less"
	for _, msg := range messages(poll) {
		p.Update(msg)
	}
	if p.fgName != "less" {
		t.Fatalf("foreground name after the next poll = %q, want less", p.fgName)
	}
	// An unsupported transport has no chain: no start command, no tick poll.
	term2 := newFakeTerm() // fgSupported defaults false
	disabled := newTestPanel(t, &fakeStarter{term: term2})
	if _, ok := disabled.Update(foregroundPollTickMsg{gen: disabled.gen}); !ok {
		t.Fatal("the tick message was not taken")
	}
	if term2.fgCalls != 0 || disabled.fgName != "" {
		t.Fatalf("unsupported transport polled: calls %d name %q", term2.fgCalls, disabled.fgName)
	}
}

// TestForegroundPollStopsOnExitAndIgnoresOldGenerations checks the chain is
// bounded to the shell: exit clears the cached name and stops ticking, and an
// answer/restart from an earlier generation cannot repopulate it.
func TestForegroundPollStopsOnExitAndIgnoresOldGenerations(t *testing.T) {
	t.Parallel()
	term := newFakeTerm()
	term.fgName = "old"
	p := startForegroundPanel(t, term)
	// Capture a poll command from this generation, then exit.
	stale, _ := p.Update(foregroundPollTickMsg{gen: p.gen})
	if stale == nil {
		t.Fatal("tick did not start a poll")
	}
	p.Update(exitMsg{gen: p.gen})
	if p.fgName != "" {
		t.Fatalf("exit kept the foreground name %q", p.fgName)
	}
	if next, ok := p.Update(foregroundPollTickMsg{gen: p.gen}); !ok || next != nil {
		t.Fatalf("an exited shell kept polling: %v, %v", next, ok)
	}
	// Restart into a new generation: the stale answer is dropped.
	starter := &fakeStarter{term: newFakeTerm()}
	p.starter = starter
	starter.term.fgSupported = true
	starter.term.fgOK = true
	cmd := p.Restart()
	for _, msg := range messages(cmd) {
		p.Update(msg)
	}
	if !p.Running() {
		t.Fatal("the shell did not restart")
	}
	for _, msg := range messages(stale) {
		p.Update(msg)
	}
	if p.fgName != "" {
		t.Fatalf("a stale foreground answer repopulated the name: %q", p.fgName)
	}
	if next, ok := p.Update(foregroundPollTickMsg{gen: p.gen}); !ok || next == nil {
		t.Fatalf("the restarted shell did not poll: %v, %v", next, ok)
	}
}

// TestForegroundNameCombinesWithTheOSCTitle checks the tab label uses the live
// foreground command leading and the OSC title as context, with Fresh's
// contain-command exception, and that a later poll updates the label.
func TestForegroundNameCombinesWithTheOSCTitle(t *testing.T) {
	t.Parallel()
	term := newFakeTerm()
	term.fgSupported = true
	term.fgOK = true
	term.fgName = "python3"
	term.title = "root@host: ~/proj"
	starter := &fakeStarter{term: term}
	var tabs Tabs
	tabs.SetHost(Host{RootDir: func() string { return "/ws" }, Notice: func(string) {}, ContentSize: func() (int, int) { return 80, 4 }, Focus: func() {}})
	tabs.SetPorts(Ports{Starter: starter})
	if cmd := tabs.NewTab(); cmd == nil {
		t.Fatal("NewTab returned no command")
	} else {
		if next, ok := tabs.Update(cmd()); !ok {
			t.Fatalf("the tab did not start: %v", next)
		} else {
			for _, msg := range messages(next) {
				if poll, ok := tabs.Update(msg); ok {
					for _, pm := range messages(poll) {
						tabs.Update(pm)
					}
				}
			}
		}
	}
	if got := tabs.TabNames()[0]; got != "python3 — root@host: ~/proj" {
		t.Fatalf("tab label = %q, want the foreground command and the OSC title", got)
	}
	// A later poll replaces the command half of the label.
	term.fgName = "less"
	poll, _ := tabs.tabs[0].Update(foregroundPollTickMsg{gen: tabs.tabs[0].gen})
	for _, msg := range messages(poll) {
		tabs.Update(msg)
	}
	if got := tabs.TabNames()[0]; got != "less — root@host: ~/proj" {
		t.Fatalf("tab label after the poll = %q", got)
	}
	// Fresh's combine rule: when the OSC title already names the command, it is
	// used alone.
	term.fgName = "vim"
	term.title = "file - VIM"
	poll, _ = tabs.tabs[0].Update(foregroundPollTickMsg{gen: tabs.tabs[0].gen})
	for _, msg := range messages(poll) {
		tabs.Update(msg)
	}
	if got := tabs.TabNames()[0]; got != "file - VIM" {
		t.Fatalf("tab label = %q, want the OSC title alone", got)
	}
	// An explicit rename still wins.
	tabs.RenameActive("mine")
	if got := tabs.TabNames()[0]; !strings.HasPrefix(got, "mine") {
		t.Fatalf("renamed label = %q", got)
	}
}
