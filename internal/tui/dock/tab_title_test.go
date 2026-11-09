package dock

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/terminal"
)

// titleTabs restores one or more tabs with a launch record and an OSC title.
func titleTabs(t *testing.T, tabs []terminal.SavedTab) *Tabs {
	t.Helper()
	var dock Tabs
	dock.SetHost(Host{RootDir: func() string { return "/ws" }, Notice: func(string) {}, ContentSize: func() (int, int) { return 80, 24 }, Focus: func() {}})
	dock.SetPorts(Ports{Starter: &fakeStarter{term: newFakeTerm()}})
	// These tests check the base label: keep the tabs dormant-live so the exited
	// marker (tested elsewhere) does not decorate them.
	for i := range tabs {
		tabs[i].Live = true
	}
	dock.Restore(terminal.SavedState{Version: terminal.SavedStateVersion, Open: true, Tabs: tabs})
	return &dock
}

// TestTabTitleCombination checks the tab label rule taken from Fresh's
// combine_terminal_title: the command leads and the OSC title follows as context,
// unless the OSC title already names the command; explicit rename still wins; a
// record without a title keeps the old command/default label.
func TestTabTitleCombination(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		tab    terminal.SavedTab
		want   string
		notHas string
	}{
		{
			name: "command and OSC context",
			tab:  terminal.SavedTab{Open: true, Rows: []string{"x"}, LaunchArgv: []string{"/usr/bin/python3"}, Title: "root@host: ~/proj"},
			want: "python3 — root@host: ~/proj",
		},
		{
			name: "OSC title already names the command",
			tab:  terminal.SavedTab{Open: true, Rows: []string{"x"}, LaunchArgv: []string{"vim"}, Title: "file - VIM"},
			want: "file - VIM",
		},
		{
			name: "only command",
			tab:  terminal.SavedTab{Open: true, Rows: []string{"x"}, LaunchArgv: []string{"/bin/bash"}},
			want: "bash",
		},
		{
			name: "only OSC title",
			tab:  terminal.SavedTab{Open: true, Rows: []string{"x"}, Title: "user@host: ~/proj"},
			want: "user@host: ~/proj",
		},
		{
			name: "explicit rename wins over both",
			tab:  terminal.SavedTab{Name: "mine", Open: true, Rows: []string{"x"}, LaunchArgv: []string{"bash"}, Title: "some OSC title"},
			want: "mine",
		},
		{
			name: "old record without title keeps the command",
			tab:  terminal.SavedTab{Open: true, Rows: []string{"x"}, LaunchArgv: []string{"/bin/bash"}},
			want: "bash",
		},
		{
			name: "case-insensitive containment",
			tab:  terminal.SavedTab{Open: true, Rows: []string{"x"}, LaunchArgv: []string{"Vim"}, Title: "file - vim"},
			want: "file - vim",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tabs := titleTabs(t, []terminal.SavedTab{tt.tab})
			if got := tabs.TabNames()[0]; got != tt.want {
				t.Fatalf("TabNames = %q, want %q", got, tt.want)
			}
		})
	}
	// An untrusted title is cleaned before it reaches the label: no control
	// characters survive and the command context still leads.
	evil := titleTabs(t, []terminal.SavedTab{{
		Open: true, Rows: []string{"x"}, LaunchArgv: []string{"bash"},
		Title: "\x1b]0;evil\x07\nname",
	}})
	got := evil.TabNames()[0]
	if strings.ContainsAny(got, "\x1b\x07\n\x00") {
		t.Fatalf("control characters in tab label: %q", got)
	}
	if !strings.HasPrefix(got, "bash — ") || !strings.Contains(got, "name") {
		t.Fatalf("cleaned combined label = %q", got)
	}
}

// TestTabTitleDuplicateAndStripWidth checks the combined labels still go through
// Fresh's duplicate disambiguation and the strip's cell-budget truncation.
func TestTabTitleDuplicateAndStripWidth(t *testing.T) {
	t.Parallel()
	mk := func() terminal.SavedTab {
		return terminal.SavedTab{Open: true, Rows: []string{"x"}, LaunchArgv: []string{"python3"}, Title: "root@host: ~/proj"}
	}
	tabs := titleTabs(t, []terminal.SavedTab{mk(), mk()})
	labels := tabs.TabNames()
	if labels[0] != "python3 — root@host: ~/proj" || labels[1] != "python3 — root@host: ~/proj (2)" {
		t.Fatalf("duplicate labels = %q", labels)
	}
	long := titleTabs(t, []terminal.SavedTab{{
		Open: true, Rows: []string{"x"}, LaunchArgv: []string{"python3"},
		Title: strings.Repeat("very long context ", 40),
	}})
	strip := long.stripLabel(0)
	if w := ansi.StringWidth(strip); w > MaxTerminalTabTitleCells {
		t.Fatalf("strip label width = %d > %d: %q", w, MaxTerminalTabTitleCells, strip)
	}
	if !strings.Contains(strip, "python3 — ") {
		t.Fatalf("long label lost the command lead: %q", strip)
	}
}

// TestCombineTabTitleUnit checks the helper directly, including the empty cases.
func TestCombineTabTitleUnit(t *testing.T) {
	t.Parallel()
	tests := map[[2]string]string{
		{"python3", "root@host: ~/proj"}: "python3 — root@host: ~/proj",
		{"vim", "file - VIM"}:            "file - VIM",
		{"Vim", "file - vim"}:            "file - vim",
		{"bash", ""}:                     "bash",
		{"", "root@host: ~/proj"}:        "root@host: ~/proj",
		{"", ""}:                         "",
		{"bash", "  "}:                   "bash",
	}
	for in, want := range tests {
		if got := combineTabTitle(in[0], in[1]); got != want {
			t.Errorf("combineTabTitle(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}
