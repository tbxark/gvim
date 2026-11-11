package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/apptest"
	"github.com/tbxark/gvim/internal/restore"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/session"
)

// trustTestTabSurface is the tab surface the root type-asserts on its editor
// port (the real interface is unexported in internal/tui).
type trustTestTabSurface interface {
	ActivateTab(int)
	CloseTab(int, bool) error
	CloseNeedsConfirm(int) (bool, error)
	Quit(bool) []editor.Dirty
}

// userConfigFile writes body as the user config file and returns its path.
func userConfigFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	writeTestFile(t, path, body)
	return path
}

// launchRuntime builds a launch and closes it when the test ends.
func launchRuntime(t *testing.T, opts Options) *runtime {
	t.Helper()
	rt, err := buildRuntime(t.Context(), opts, false)
	if err != nil {
		t.Fatalf("buildRuntime error = %v", err)
	}
	t.Cleanup(func() {
		rt.close()
		rt.ed.Close()
	})
	return rt
}

// seedSession saves a session with one clean tab and one edited tab, and returns
// the edited file's path.
func seedSession(t *testing.T, root string) (string, string) {
	t.Helper()
	dir := filepath.Join(root, "session")
	clean := filepath.Join(root, "clean.txt")
	edited := filepath.Join(root, "edited.txt")
	writeTestFile(t, clean, "clean\n")
	writeTestFile(t, edited, "on disk\n")
	seed := apptest.NewSessionEditor()
	apptest.Must(t, seed.OpenFile(clean))
	apptest.Must(t, seed.OpenFile(edited))
	if res := seed.Do("insert_char", map[string]any{"text": "Z"}); res.Err != nil {
		t.Fatal(res.Err)
	}
	apptest.Must(t, restore.SaveSession(seed, dir, tui.ExplorerState{Open: true, ShowHidden: true}))
	seed.Close()
	return dir, edited
}

// TestBuildRuntimeRestorePreviousSessionOffKeepsTheUnsavedText checks the
// restore of editor.restore_previous_session=false: the saved clean tab and the
// explorer state stay out, the edited file comes back with its unsaved text, and
// the session is still saved when the program ends.
func TestBuildRuntimeRestorePreviousSessionOffKeepsTheUnsavedText(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	sessDir, edited := seedSession(t, root)
	opts := Options{
		Root:       root,
		SessionDir: sessDir,
		Config:     userConfigFile(t, `{"editor":{"save_session":true,"restore_previous_session":false}}`),
	}
	rt := launchRuntime(t, opts)

	names, _ := apptest.TabNames(rt.ed)
	if len(names) != 1 || names[0] != "edited.txt" {
		t.Fatalf("tabs = %v, want only the edited file", names)
	}
	if doc := rt.ed.Active(); doc == nil || !doc.Modified() {
		t.Fatal("the unsaved text was not restored")
	}
	// The directory argument still opens the tree, but the saved tree settings
	// are part of the layout, so they are not restored.
	if st := rt.model.ExplorerState(); !st.ShowHidden || st.ShowIgnored {
		t.Errorf("explorer settings = %+v, want the layout's settings skipped", st)
	}
	if rt.sessionDir != sessDir {
		t.Fatalf("sessionDir = %q, want the session still saved", rt.sessionDir)
	}
	// The exit save keeps the hot content only.
	apptest.Must(t, rt.save())
	s, ok, err := session.Load(sessDir)
	apptest.Must(t, err)
	if !ok || len(s.Files) != 1 || s.Files[0].Path != edited || s.Files[0].StashFile == "" {
		t.Fatalf("saved session = %+v (found %v), want only the edited file with its stash", s, ok)
	}
}

// TestBuildRuntimeNoRestoreOverridesRestorePreviousSession checks --no-restore:
// it skips the saved layout even when editor.restore_previous_session is on, and
// still brings the unsaved text back.
func TestBuildRuntimeNoRestoreOverridesRestorePreviousSession(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	sessDir, _ := seedSession(t, root)
	rt := launchRuntime(t, Options{Root: root, SessionDir: sessDir, NoRestore: true})

	names, _ := apptest.TabNames(rt.ed)
	if len(names) != 1 || names[0] != "edited.txt" {
		t.Fatalf("tabs = %v, want only the edited file", names)
	}
	if rt.sessionDir != sessDir {
		t.Fatalf("sessionDir = %q, want the session still saved", rt.sessionDir)
	}
}

// TestBuildRuntimeWithFilesRestoresWhenTheSkipKeyIsOff checks
// editor.skip_session_restore_when_files_passed=false: a launch with files is a
// session launch, so the saved tabs come back and the named files follow them.
func TestBuildRuntimeWithFilesRestoresWhenTheSkipKeyIsOff(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	sessDir := filepath.Join(root, "session")
	saved := filepath.Join(root, "saved.txt")
	given := filepath.Join(root, "given.txt")
	writeTestFile(t, saved, "saved\n")
	writeTestFile(t, given, "given\n")
	seed := apptest.NewSessionEditor()
	apptest.Must(t, seed.OpenFile(saved))
	apptest.Must(t, restore.SaveSession(seed, sessDir, tui.ExplorerState{}))
	seed.Close()

	opts := Options{
		Root:       root,
		SessionDir: sessDir,
		Files:      []FileTarget{{Path: given}},
		Config:     userConfigFile(t, `{"editor":{"save_session":true,"skip_session_restore_when_files_passed":false}}`),
	}
	rt := launchRuntime(t, opts)

	names, active := apptest.TabNames(rt.ed)
	if len(names) != 2 || names[0] != "saved.txt" || names[1] != "given.txt" {
		t.Fatalf("tabs = %v, want the saved tab then the given file", names)
	}
	if active != 1 {
		t.Fatalf("active tab = %d, want the given file", active)
	}
	if rt.sessionDir != sessDir {
		t.Fatalf("sessionDir = %q, want the session saved", rt.sessionDir)
	}
}

// TestBuildRuntimeWithFilesSkipsTheSessionByDefault pins the default: a launch
// with files restores and saves no session.
func TestBuildRuntimeWithFilesSkipsTheSessionByDefault(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	sessDir, _ := seedSession(t, root)
	given := filepath.Join(root, "given.txt")
	writeTestFile(t, given, "given\n")
	rt := launchRuntime(t, Options{Root: root, SessionDir: sessDir, Files: []FileTarget{{Path: given}}})

	names, _ := apptest.TabNames(rt.ed)
	if len(names) != 1 || names[0] != "given.txt" {
		t.Fatalf("tabs = %v, want only the given file", names)
	}
	if rt.sessionDir != "" {
		t.Fatalf("sessionDir = %q, want the session not saved", rt.sessionDir)
	}
}

// TestBuildRuntimeRestoreFlagForcesTheSession checks --restore: it restores even
// when editor.restore_previous_session is off, and a named file that the session
// already has open is activated at the requested position instead of opening a
// second tab.
func TestBuildRuntimeRestoreFlagForcesTheSession(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	sessDir := filepath.Join(root, "session")
	saved := filepath.Join(root, "saved.txt")
	writeTestFile(t, saved, "one\ntwo\nthree\n")
	seed := apptest.NewSessionEditor()
	apptest.Must(t, seed.OpenFile(saved))
	apptest.Must(t, restore.SaveSession(seed, sessDir, tui.ExplorerState{}))
	seed.Close()

	opts := Options{
		Root:       root,
		SessionDir: sessDir,
		Files:      []FileTarget{{Path: saved, Line: 2}},
		Restore:    true,
		Config:     userConfigFile(t, `{"editor":{"save_session":true,"restore_previous_session":false}}`),
	}
	rt := launchRuntime(t, opts)

	names, active := apptest.TabNames(rt.ed)
	if len(names) != 1 || names[0] != "saved.txt" {
		t.Fatalf("tabs = %v, want the restored file only, not a duplicate", names)
	}
	if active != 0 {
		t.Fatalf("active tab = %d, want the restored tab", active)
	}
	if c := rt.ed.Active().Cursor; c.Line != 1 {
		t.Fatalf("cursor line = %d, want 1 (the requested line 2)", c.Line)
	}
	if rt.sessionDir != sessDir {
		t.Fatalf("sessionDir = %q, want the session saved", rt.sessionDir)
	}
}

// TestBuildRuntimeSimpleRestoresAndSavesNothing checks --simple: no session is
// restored or saved, and the explorer stays hidden even with a directory
// argument.
func TestBuildRuntimeSimpleRestoresAndSavesNothing(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	sessDir, _ := seedSession(t, root)

	got, err := buildCaptured(t, Options{Root: root, SessionDir: sessDir, Simple: true})
	if err != nil {
		t.Fatalf("buildCaptured error = %v", err)
	}
	if names, _ := apptest.TabNames(got.ed); len(names) != 0 {
		t.Fatalf("tabs = %v, want none: --simple restores no session", names)
	}
	if got.deps.ShowExplorer {
		t.Error("--simple shows the file explorer")
	}

	// An empty session directory shows whether the launch saves anything.
	freshDir := filepath.Join(root, "fresh-session")
	rt := launchRuntime(t, Options{Root: root, SessionDir: freshDir, Simple: true})
	if rt.sessionDir != "" {
		t.Fatalf("sessionDir = %q, want the session not saved", rt.sessionDir)
	}
	apptest.Must(t, rt.save())
	if _, err := os.Stat(filepath.Join(freshDir, "session.json")); err == nil {
		t.Fatal("--simple saved a session")
	}
	if rt.searchDir != freshDir {
		t.Errorf("searchDir = %q, want the find history kept in every launch", rt.searchDir)
	}
	// Crash recovery is not session state either: a real run (watching starts the
	// watcher and the recovery copy) keeps it in --simple.
	real, err := buildRuntime(t.Context(), Options{Root: root, SessionDir: freshDir, Simple: true}, true)
	apptest.Must(t, err)
	t.Cleanup(func() {
		real.close()
		real.ed.Close()
	})
	if real.recoveryWriter == nil {
		t.Error("--simple turned the crash-recovery copy off")
	}
}

// TestHotExitQuitBehavior drives the real quit key on the real model: with
// editor.hot_exit on, a session launch quits without asking and leaves the
// modified tabs for the exit save; with it off, and for a file launch, the quit
// asks through the unsaved-changes flow and grants nothing yet.
func TestHotExitQuitBehavior(t *testing.T) {
	tests := []struct {
		name     string
		hotExit  bool
		files    bool // a file launch: the session is not saved, so no hot exit
		wantQuit bool
	}{
		{name: "session launch asks even with hot exit", hotExit: true, wantQuit: false},
		{name: "session launch with hot exit off asks", hotExit: false, wantQuit: false},
		{name: "a file launch keeps the prompt even with hot exit on", hotExit: true, files: true, wantQuit: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateConfig(t)
			root := t.TempDir()
			file := filepath.Join(root, "a.txt")
			writeTestFile(t, file, "hello\n")
			opts := Options{
				Root:       root,
				SessionDir: filepath.Join(root, "session"),
				Keymap:     "default",
				Config:     userConfigFile(t, fmt.Sprintf(`{"editor":{"save_session":true,"hot_exit":%v}}`, tt.hotExit)),
			}
			if tt.files {
				opts.Files = []FileTarget{{Path: file}}
			}
			rt := launchRuntime(t, opts)
			if !tt.files {
				apptest.Must(t, rt.ed.OpenFile(file))
			}
			if res := rt.ed.Do("insert_char", map[string]any{"text": "Z"}); res.Err != nil {
				t.Fatal(res.Err)
			}
			rt.model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

			_, cmd := rt.model.Update(tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl})
			if got := cmdQuits(cmd); got != tt.wantQuit {
				t.Fatalf("the quit key produced tea.Quit = %v, want %v", got, tt.wantQuit)
			}
			if rt.ed.Quitting() != tt.wantQuit {
				t.Errorf("editor quitting = %v, want %v", rt.ed.Quitting(), tt.wantQuit)
			}
			if doc := rt.ed.Active(); doc == nil || !doc.Modified() {
				t.Fatal("the unsaved text was lost by the quit")
			}
			if tt.wantQuit {
				// The exit save keeps the unsaved text for the next start.
				apptest.Must(t, rt.save())
				s, ok, err := session.Load(opts.SessionDir)
				apptest.Must(t, err)
				if !ok || len(s.Files) != 1 || s.Files[0].StashFile == "" {
					t.Fatalf("saved session = %+v (found %v), want the stashed unsaved text", s, ok)
				}
			}
		})
	}
}

// TestHotExitEditorKeepsTheAdapterPorts checks the temporary wrapper satisfies
// the interfaces the root probes on its editor port, and that with hot exit on a
// plain quit is granted while the tabs stay modified.
func TestHotExitEditorKeepsTheAdapterPorts(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	file := filepath.Join(root, "a.txt")
	writeTestFile(t, file, "hello\n")
	opts := Options{
		Root:       root,
		SessionDir: filepath.Join(root, "session"),
		Keymap:     "default",
		Config:     userConfigFile(t, `{"editor":{"save_session":true,"hot_exit":true}}`),
	}
	got, err := buildCaptured(t, opts)
	if err != nil {
		t.Fatalf("buildCaptured error = %v", err)
	}
	wrapped, ok := got.deps.Editor.(hotExitEditor)
	if !ok {
		t.Fatalf("Deps.Editor is %T, want the hot-exit wrapper", got.deps.Editor)
	}
	if wrapped.Editor == nil {
		t.Fatal("the wrapper hides the real adapter")
	}
	if !wrapped.hot {
		t.Error("the wrapper is not in hot-exit mode for a session launch with hot_exit on")
	}
	// The root probes these on its editor port: the tab surface (Quit lives
	// there), the shell runner and the workspace root setter. The wrapper must
	// keep them promoted from *uiadapter.Editor.
	if _, ok := got.deps.Editor.(trustTestTabSurface); !ok {
		t.Error("the tab surface is hidden by the wrapper")
	}
	if _, ok := got.deps.Editor.(tui.ShellRunner); !ok {
		t.Error("the shell runner port is hidden by the wrapper")
	}
	if _, ok := got.deps.Editor.(interface{ SetRoot(string) }); !ok {
		t.Error("the workspace root port is hidden by the wrapper")
	}

	apptest.Must(t, got.ed.OpenFile(file))
	if res := got.ed.Do("insert_char", map[string]any{"text": "Z"}); res.Err != nil {
		t.Fatal(res.Err)
	}
	if dirty := wrapped.Quit(false); len(dirty) != 0 {
		t.Fatalf("Quit with hot exit asked about %v", dirty)
	}
	if !got.ed.Quitting() {
		t.Error("Quit with hot exit did not grant the quit")
	}
	if doc := got.ed.Active(); doc == nil || !doc.Modified() {
		t.Error("Quit with hot exit dropped the unsaved text")
	}
}

// TestHotExitOffQuitAsksForTheDirtyTabs pins the pass-through: with hot_exit
// off the wrapper returns the editor's dirty-tab list, so the root asks.
func TestHotExitOffQuitAsksForTheDirtyTabs(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	file := filepath.Join(root, "a.txt")
	writeTestFile(t, file, "hello\n")
	opts := Options{
		Root:       root,
		SessionDir: filepath.Join(root, "session"),
		Keymap:     "default",
		Config:     userConfigFile(t, `{"editor":{"hot_exit":false}}`),
	}
	got, err := buildCaptured(t, opts)
	if err != nil {
		t.Fatalf("buildCaptured error = %v", err)
	}
	wrapped, ok := got.deps.Editor.(hotExitEditor)
	if !ok || wrapped.hot {
		t.Fatalf("Deps.Editor = %T (%+v), want the wrapper with hot exit off", got.deps.Editor, wrapped)
	}
	apptest.Must(t, got.ed.OpenFile(file))
	if res := got.ed.Do("insert_char", map[string]any{"text": "Z"}); res.Err != nil {
		t.Fatal(res.Err)
	}
	dirty := wrapped.Quit(false)
	if len(dirty) != 1 || dirty[0].Name != "a.txt" {
		t.Fatalf("dirty tabs = %v, want a.txt", dirty)
	}
	if got.ed.Quitting() {
		t.Error("Quit with hot exit off granted the quit instead of asking")
	}
}

// cmdQuits reports whether cmd yields tea.QuitMsg. It waits at most two seconds,
// so a command that waits on a timer cannot hang the test.
func cmdQuits(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		return hasQuitMsg(msg)
	case <-time.After(2 * time.Second):
		return false
	}
}

// hasQuitMsg reports whether msg is tea.QuitMsg, or a batch that contains it.
func hasQuitMsg(msg tea.Msg) bool {
	if _, ok := msg.(tea.QuitMsg); ok {
		return true
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if c == nil {
				continue
			}
			if hasQuitMsg(c()) {
				return true
			}
		}
	}
	return false
}

// TestCrashRecoveryWorksInEveryLaunchKind checks for each launch kind: a
// session launch, a file launch and --simple. Crash recovery is not session
// state, so an abandoned copy is recovered before the launch mode matters.
func TestCrashRecoveryWorksInEveryLaunchKind(t *testing.T) {
	tests := []struct {
		name string
		opts func(root, sessionDir, file string) Options
	}{
		{
			name: "session launch",
			opts: func(root, sessionDir, _ string) Options {
				return Options{Root: root, SessionDir: sessionDir}
			},
		},
		{
			name: "file launch",
			opts: func(root, sessionDir, file string) Options {
				return Options{Root: root, SessionDir: sessionDir, Files: []FileTarget{{Path: file}}}
			},
		},
		{
			name: "simple",
			opts: func(root, sessionDir, _ string) Options {
				return Options{Root: root, SessionDir: sessionDir, Simple: true}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateConfig(t)
			root := t.TempDir()
			file := filepath.Join(root, "a.txt")
			writeTestFile(t, file, "on disk\n")
			sessDir := filepath.Join(root, "session")
			// An abandoned crash-recovery copy of an earlier run, with unsaved text.
			recDir := restore.OwnRecoveryDir(restore.RecoveryPath(sessDir))
			stash, err := session.StashContent(recDir, "recovered text\n")
			apptest.Must(t, err)
			apptest.Must(t, session.Save(recDir, session.Session{
				Files: []session.FileState{{Path: file, StashFile: stash}},
			}))

			rt, err := buildRuntime(t.Context(), tt.opts(root, sessDir, file), true)
			apptest.Must(t, err)
			t.Cleanup(func() {
				rt.close()
				rt.ed.Close()
			})
			// The recovered tab has the abandoned unsaved text, from the disk copy
			// changed in memory.
			recovered := false
			for i := range rt.ed.Tabs() {
				rt.ed.ActivateTab(i)
				doc := rt.ed.Active()
				if doc == nil || doc.Path != file {
					continue
				}
				if got := doc.Buffer.Text(); got == "recovered text\n" && doc.Modified() {
					recovered = true
				}
			}
			if !recovered {
				t.Fatalf("no tab of %q carries the recovered unsaved text; tabs: %v", file, editortabPaths(rt))
			}
		})
	}
}

// editortabPaths returns the paths of the tabs of rt, for failure messages.
func editortabPaths(rt *runtime) []string {
	var paths []string
	for i := range rt.ed.Tabs() {
		rt.ed.ActivateTab(i)
		if doc := rt.ed.Active(); doc != nil {
			paths = append(paths, doc.Path)
		}
	}
	return paths
}

// TestBuildRuntimeDiffIsAFileLaunch checks --diff: the compare launch restores
// and saves no session, even when a session exists.
func TestBuildRuntimeDiffIsAFileLaunch(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	sessDir, _ := seedSession(t, root)
	a := filepath.Join(root, "a.txt")
	b := filepath.Join(root, "b.txt")
	writeTestFile(t, a, "one\n")
	writeTestFile(t, b, "two\n")
	rt := launchRuntime(t, Options{
		SessionDir: sessDir,
		Diff:       true,
		Files:      []FileTarget{{Path: a}, {Path: b}},
	})
	names, _ := apptest.TabNames(rt.ed)
	if len(names) != 2 || names[0] != "a.txt" || names[1] != "b.txt" {
		t.Fatalf("tabs = %v, want the two compared files only", names)
	}
	if rt.sessionDir != "" {
		t.Fatalf("sessionDir = %q, want the session not saved in a compare launch", rt.sessionDir)
	}
}
