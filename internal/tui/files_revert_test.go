package tui

import (
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/theme"
)

// pollTabsEditor is an autoTabsEditor that can check the disk. changes is what the check
// finds, and checks counts the plans.
type pollTabsEditor struct {
	*autoTabsEditor
	changes []editor.DiskChange
	checks  int
}

func (e *pollTabsEditor) PlanDiskCheck() func() []editor.DiskChange {
	e.checks++
	return func() []editor.DiskChange { return e.changes }
}

func newPollModel(t *testing.T, poll time.Duration, tabs ...revertDoc) (*Model, *pollTabsEditor) {
	t.Helper()
	ed := &pollTabsEditor{autoTabsEditor: newAutoTabsEditor(0, tabs...)}
	m := New(Deps{Editor: ed, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock, Root: RootPorts{AutoRevertPoll: poll}})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return m, ed
}

func TestRevertPollIsArmedOnlyWithAnIntervalAndADiskCheck(t *testing.T) {
	t.Parallel()
	m, _ := newPollModel(t, 2*time.Second, openDoc("/ws/a.txt", false))
	if m.revertPollCmd() == nil {
		t.Error("no tick with an interval and an editor that checks the disk")
	}
	m, _ = newPollModel(t, 0, openDoc("/ws/a.txt", false))
	if m.revertPollCmd() != nil {
		t.Error("a tick without an interval")
	}
	plain := newRevertModel(t, newDocEditor("/ws/a.txt", false))
	plain.root.ports.AutoRevertPoll = time.Second
	if plain.revertPollCmd() != nil {
		t.Error("a tick for an editor that cannot check the disk")
	}
}

func TestRevertPollTickChecksTheDiskInACommand(t *testing.T) {
	t.Parallel()
	m, ed := newPollModel(t, 0, openDoc("/ws/a.txt", false))
	ed.changes = []editor.DiskChange{{Path: "/ws/a.txt", Changed: true, Stamp: "1"}}
	_, cmd := m.Update(revertPollMsg{})
	if ed.checks != 1 {
		t.Fatalf("checks planned on the update loop = %d, want 1", ed.checks)
	}
	msgs := runCmd(cmd)
	if len(msgs) != 1 {
		t.Fatalf("tick produced %d messages, want the check result", len(msgs))
	}
	if got, ok := msgs[0].(revertStatMsg); !ok || !slices.Equal(got.changes, ed.changes) {
		t.Fatalf("tick result = %+v, want the changes of the check", msgs[0])
	}
}

func TestRevertPollReloadsChangedFilesOncePerChange(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		tabs      []revertDoc
		off       []string
		stats     [][]editor.DiskChange // one result per tick
		wantReads []string
		notices   int // times the unsaved-changes notice is shown
	}{
		{
			name: "a clean file reloads once for one change",
			tabs: []revertDoc{openDoc("/ws/a.txt", false)},
			stats: [][]editor.DiskChange{
				{{Path: "/ws/a.txt", Changed: true, Stamp: "1"}},
				{{Path: "/ws/a.txt", Changed: true, Stamp: "1"}},
			},
			wantReads: []string{"/ws/a.txt"},
		},
		{
			name: "a second change reloads again",
			tabs: []revertDoc{openDoc("/ws/a.txt", false)},
			stats: [][]editor.DiskChange{
				{{Path: "/ws/a.txt", Changed: true, Stamp: "1"}},
				{{Path: "/ws/a.txt", Changed: false, Stamp: "1"}},
				{{Path: "/ws/a.txt", Changed: true, Stamp: "2"}},
			},
			wantReads: []string{"/ws/a.txt", "/ws/a.txt"},
		},
		{
			name: "a background file in another folder reloads",
			tabs: []revertDoc{openDoc("/ws/a.txt", false), openDoc("/other/b.txt", false)},
			stats: [][]editor.DiskChange{
				{{Path: "/ws/a.txt"}, {Path: "/other/b.txt", Changed: true, Stamp: "7"}},
			},
			wantReads: []string{"/other/b.txt"},
		},
		{
			name: "a file with unsaved edits is kept and reported once",
			tabs: []revertDoc{openDoc("/ws/a.txt", true)},
			stats: [][]editor.DiskChange{
				{{Path: "/ws/a.txt", Changed: true, Stamp: "1"}},
				{{Path: "/ws/a.txt", Changed: true, Stamp: "1"}},
			},
			notices: 1,
		},
		{
			name: "a file switched off is left alone",
			tabs: []revertDoc{openDoc("/ws/a.txt", false)},
			off:  []string{"/ws/a.txt"},
			stats: [][]editor.DiskChange{
				{{Path: "/ws/a.txt", Changed: true, Stamp: "1"}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, ed := newPollModel(t, 0, tt.tabs...)
			for _, p := range tt.off {
				m.fix.autoRevert = map[string]bool{p: false}
			}
			notices := 0
			for _, changes := range tt.stats {
				m.notice = ""
				_, cmd := m.Update(revertStatMsg{changes: changes})
				feedMessages(m, cmd)
				if m.notice == "a.txt changed on disk; unsaved changes kept" {
					notices++
				}
			}
			if !slices.Equal(ed.reads, tt.wantReads) {
				t.Errorf("reads = %q, want %q", ed.reads, tt.wantReads)
			}
			if notices != tt.notices {
				t.Errorf("unsaved-changes notices = %d, want %d", notices, tt.notices)
			}
		})
	}
}
