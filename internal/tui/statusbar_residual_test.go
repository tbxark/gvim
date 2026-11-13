package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/internal/tui/palette"
	"github.com/tbxark/gvim/theme"
)

// statusPathDoc is a document whose file path, modification and read-only state the
// status-row tests set. The other Document methods come from fakeDoc.
type statusPathDoc struct {
	*fakeDoc
	path     string
	modified bool
	readOnly bool
}

// FilePath is the document's file path, so {filename} can name it.
func (d statusPathDoc) FilePath() string { return d.path }

// Modified reports the unsaved state Fresh marks with " [+]".
func (d statusPathDoc) Modified() bool { return d.modified }

// ReadOnly reports the state Fresh marks with " [RO]".
func (d statusPathDoc) ReadOnly() bool { return d.readOnly }

// statusRootTree is a fake tree with a chosen root, so a test can give the model a
// root spelling that differs from the document's.
type statusRootTree struct {
	*fakeTree
	root string
}

// Root is the workspace root the tree reports.
func (t *statusRootTree) Root() string { return t.root }

// statusDocEditor is a fake editor whose active document is the given document, so
// a model's statusView sees its path, modified and read-only state.
type statusDocEditor struct {
	*fakeEditor
	doc Document
}

func (e statusDocEditor) Active() Document { return e.doc }

// TestStatusFilename checks Fresh's display_name_for_path rule: the workspace-relative
// path when the file is under the root, the absolute path otherwise, and the
// document's own name when it has no path.
func TestStatusFilename(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, newFakeEditor("hello"), newFakeTree(3), 80, 24) // tree root /ws
	base := &fakeDoc{lines: []string{"hello"}}
	tests := []struct {
		name string
		doc  statusPathDoc
		want string
	}{
		{"under the root", statusPathDoc{base, "/ws/src/main.go", false, false}, "src/main.go"},
		{"the root itself", statusPathDoc{base, "/ws", false, false}, "."},
		{"outside the root stays absolute", statusPathDoc{base, "/elsewhere/notes.txt", false, false}, "/elsewhere/notes.txt"},
		{"no path keeps the document name", statusPathDoc{base, "", false, false}, "a.txt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := m.statusFilename(tt.doc); got != tt.want {
				t.Errorf("statusFilename = %q, want %q", got, tt.want)
			}
		})
	}
	// Without a tree (no workspace root) the path is shown as it is.
	bare := New(Deps{Editor: newFakeEditor("hello"), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	bare.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if got := bare.statusFilename(statusPathDoc{base, "/ws/src/main.go", false, false}); got != "/ws/src/main.go" {
		t.Errorf("without a root: %q, want the absolute path", got)
	}
}

// TestStatusViewFilenameAndMarkers checks the model wiring of {filename}: the display
// name plus Fresh's " [+]" modified and " [RO]" read-only markers.
func TestStatusViewFilenameAndMarkers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		doc      statusPathDoc
		wantName string
		modified bool
		readOnly bool
	}{
		{"clean file", statusPathDoc{&fakeDoc{lines: []string{"hello"}}, "/ws/main.go", false, false}, "main.go", false, false},
		{"modified file", statusPathDoc{&fakeDoc{lines: []string{"hello"}}, "/ws/main.go", true, false}, "main.go", true, false},
		{"read-only file", statusPathDoc{&fakeDoc{lines: []string{"hello"}}, "/ws/main.go", false, true}, "main.go", false, true},
		{"untitled buffer", statusPathDoc{&fakeDoc{lines: []string{"hello"}}, "", true, false}, "a.txt", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ed := newFakeEditor("hello")
			m := New(Deps{Editor: statusDocEditor{fakeEditor: ed, doc: tt.doc}, Explorer: newFakeTree(3), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
			m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
			v := m.statusView()
			if v.Filename != tt.wantName || v.FilenameModified != tt.modified || v.ReadOnly != tt.readOnly {
				t.Errorf("filename %q modified %v read-only %v, want %q %v %v",
					v.Filename, v.FilenameModified, v.ReadOnly, tt.wantName, tt.modified, tt.readOnly)
			}
		})
	}
}

// TestStatusChordFollowsThePendingSequence checks {chord}: it shows the held keys in
// the bracket form while a multi-key sequence waits, and vanishes when the sequence
// resolves or misses.
func TestStatusChordFollowsThePendingSequence(t *testing.T) {
	t.Parallel()
	m, _ := newSeqModel(t)
	if got := m.statusView().Chord; got != "" {
		t.Fatalf("a fresh model paints chord %q", got)
	}
	press(m, ctrl('x'))
	if got := m.statusView().Chord; got != "[Ctrl+X]" {
		t.Errorf("pending chord = %q, want %q", got, "[Ctrl+X]")
	}
	press(m, ctrl('s')) // resolves the sequence
	if got := m.statusView().Chord; got != "" {
		t.Errorf("after the sequence resolved chord = %q, want nothing", got)
	}
	press(m, ctrl('x'))
	press(m, typed("q")) // misses: the sequence is dropped
	if got := m.statusView().Chord; got != "" {
		t.Errorf("after a miss chord = %q, want nothing", got)
	}
}

// TestStatusClock comes from the injected clock, in Fresh's HH:MM form.
func TestStatusClock(t *testing.T) {
	t.Parallel()
	m := statusViewModel(t, &ViewOptions{
		ShowStatusBar: true,
		StatusBar:     StatusBarOptions{Left: []string{"{clock}"}},
	})
	if got, want := m.statusView().Clock, "09:00"; got != want {
		t.Errorf("clock = %q, want %q from the injected test clock", got, want)
	}
	later := time.Date(2026, 11, 5, 17, 42, 0, 0, time.UTC)
	m.now = func() time.Time { return later }
	if got, want := m.statusView().Clock, "17:42"; got != want {
		t.Errorf("clock after the injected time moved = %q, want %q", got, want)
	}
}

// TestStatusClockTickIsArmedOnlyWhenConfigured checks the redraw tick: it exists only
// while the configured row names {clock} and the row is visible, and each tick arms
// the next one without changing any state.
func TestStatusClockTickIsArmedOnlyWhenConfigured(t *testing.T) {
	t.Parallel()
	withClock := statusViewModel(t, &ViewOptions{
		ShowStatusBar: true,
		StatusBar:     StatusBarOptions{Left: []string{"{clock}", "{cursor}"}},
	})
	if !withClock.clockConfigured() {
		t.Fatal("a configured {clock} is not reported as configured")
	}
	if withClock.clockCmd() == nil {
		t.Fatal("a configured {clock} armed no tick")
	}
	_, cmd := withClock.Update(clockTickMsg{})
	if cmd == nil {
		t.Fatal("a clock tick did not arm the next tick")
	}

	without := statusViewModel(t, &ViewOptions{
		ShowStatusBar: true,
		StatusBar:     StatusBarOptions{Left: []string{"{cursor}"}},
	})
	if without.clockConfigured() || without.clockCmd() != nil {
		t.Error("a row without {clock} armed a tick")
	}

	// A palette on top hides the status row, so the clock needs no ticks then.
	hidden := statusViewModel(t, &ViewOptions{
		ShowStatusBar: true,
		StatusBar:     StatusBarOptions{Left: []string{"{clock}"}},
	})
	hidden.overlays = append(hidden.overlays, &palette.Palette{})
	if hidden.clockConfigured() || hidden.clockCmd() != nil {
		t.Error("a hidden status row armed a clock tick")
	}
}

// TestStatusRowPaintsTheResidualElements checks the configured-list wiring end to end:
// the three elements appear in the configured order on the model's row.
func TestStatusRowPaintsTheResidualElements(t *testing.T) {
	t.Parallel()
	m := statusViewModel(t, &ViewOptions{
		ShowStatusBar: true,
		StatusBar:     StatusBarOptions{Left: []string{"{filename}", "{chord}", "{clock}"}},
	})
	row := statusRowText(m)
	for _, want := range []string{" a.txt ", " 09:00 "} {
		if !strings.Contains(row, want) {
			t.Errorf("status row %q has no %q", row, want)
		}
	}
	if strings.Contains(row, "[") && strings.Contains(row, "Ctrl") {
		t.Errorf("status row %q paints a chord with none pending", row)
	}
}

// TestStatusFilenameResolvesSpellingDifferences is the regression for the macOS case
// the round-5 visual check found: the workspace root is canonicalised (/private/var)
// while the open document keeps the spelling it was opened with (/var), so a lexical
// relative computation saw an absolute name. Both paths are resolved before they are
// compared; a file outside the root keeps the document's original spelling.
func TestStatusFilenameResolvesSpellingDifferences(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "alpha.txt"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	canon, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	openPath := filepath.Join(root, "alpha.txt")
	if canon == root {
		// No spelling difference from the filesystem alone (a plain Linux temp
		// dir): make one with a symlink, and skip where symlinks are unavailable.
		link := root + "-spelled"
		if err := os.Symlink(root, link); err != nil {
			t.Skipf("no path spelling difference available: %v", err)
		}
		openPath = filepath.Join(link, "alpha.txt")
	}
	m := newTestModel(t, newFakeEditor("hello"), &statusRootTree{fakeTree: newFakeTree(3), root: canon}, 120, 30)
	doc := statusPathDoc{&fakeDoc{lines: []string{"hello"}}, openPath, false, false}
	if got := m.statusFilename(doc); got != "alpha.txt" {
		t.Errorf("statusFilename(%q with root %q) = %q, want the root-relative %q", openPath, canon, got, "alpha.txt")
	}
	// A file outside the root keeps its original spelling.
	outside := filepath.Join(t.TempDir(), "elsewhere.txt")
	if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := m.statusFilename(statusPathDoc{&fakeDoc{lines: []string{"x"}}, outside, false, false}); got != outside {
		t.Errorf("outside the root: statusFilename = %q, want the original spelling %q", got, outside)
	}
}

// TestStatusChordIsPaintedFaint checks the muted chord style on the painted cell, not
// just on the run: the chord's characters carry the faint attribute on the status bar.
func TestStatusChordIsPaintedFaint(t *testing.T) {
	t.Parallel()
	keys := seqKeymap{
		fakeKeymap: fakeKeymap{"normal|x|ctrl": {Action: "cut"}},
		seqs:       map[string]Command{"ctrl+x ctrl+s": {Action: "undo"}},
	}
	view := DefaultViewOptions()
	view.StatusBar = StatusBarOptions{Left: []string{"{chord}"}, Right: []string{}}
	m := New(Deps{Editor: newFakeEditor("hello"), Keys: keys, Theme: theme.Theme{}, Clock: testClock, View: &view})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	press(m, ctrl('x'))
	l := m.layout()
	var chord frame.Run
	found := false
	for _, r := range l.Status {
		if r.Style == frame.StyleMuted {
			chord, found = r, true
		}
	}
	if !found {
		t.Fatalf("no muted chord run on the row: %+v", l.Status)
	}
	if got := m.paint().At(chord.X+1, l.StatusY); !got.Style.Faint {
		t.Errorf("chord cell %q style = %+v, want faint", got.Text, got.Style)
	}
}

// TestStatusFilenameIsCachedBriefly checks that the symlink resolution behind the status
// file name is not repeated on every frame: a link swapped under the open file shows only
// once the cached name is older than statusNameTTL.
func TestStatusFilenameIsCachedBriefly(t *testing.T) {
	t.Parallel()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"dirA", "dirB"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, dir, "f.txt"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(filepath.Join(root, "dirA"), link); err != nil {
		t.Skipf("no symlinks: %v", err)
	}
	m := newTestModel(t, newFakeEditor("x"), &statusRootTree{fakeTree: newFakeTree(3), root: root}, 120, 30)
	now := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	doc := statusPathDoc{&fakeDoc{lines: []string{"x"}}, filepath.Join(link, "f.txt"), false, false}
	want := filepath.Join("dirA", "f.txt")
	if got := m.statusFilename(doc); got != want {
		t.Fatalf("statusFilename = %q, want %q", got, want)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "dirB"), link); err != nil {
		t.Fatal(err)
	}
	if got := m.statusFilename(doc); got != want {
		t.Fatalf("within the TTL statusFilename = %q, want the cached %q", got, want)
	}
	now = now.Add(statusNameTTL + time.Second)
	if got, fresh := m.statusFilename(doc), filepath.Join("dirB", "f.txt"); got != fresh {
		t.Fatalf("after the TTL statusFilename = %q, want %q", got, fresh)
	}
}
