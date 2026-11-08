package editor

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestNewBufferIsNamedUntitledAndNotModified(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	run(t, e, "new")
	run(t, e, "new")
	tabs := e.Tabs()
	if len(tabs) != 2 {
		t.Fatalf("tabs = %+v, want 2", tabs)
	}
	if tabs[0].Name != "Untitled-1" || tabs[1].Name != "Untitled-2" {
		t.Fatalf("names = %q, %q; want Untitled-1, Untitled-2", tabs[0].Name, tabs[1].Name)
	}
	if tabs[1].Modified {
		t.Fatal("a new untitled buffer must not be modified before typing")
	}
}

func TestFirstNewReplacesPlaceholderButSecondAppends(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	if got := e.Tabs()[0].Name; got != "[No Name]" {
		t.Fatalf("placeholder name = %q, want [No Name]", got)
	}
	run(t, e, "new")
	if n := len(e.Tabs()); n != 1 {
		t.Fatalf("first new should replace the placeholder, got %d tabs", n)
	}
	run(t, e, "new")
	names := tabNames(e)
	if !equalStrings(names, []string{"Untitled-1", "Untitled-2"}) {
		t.Fatalf("names = %q, want [Untitled-1 Untitled-2]", names)
	}
}

func TestNewAfterTypingKeepsPlaceholderTab(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	typeText(t, e, "x")
	run(t, e, "new")
	if !equalStrings(tabNames(e), []string{"[No Name]", "Untitled-1"}) {
		t.Fatalf("names = %q, want the typed placeholder kept", tabNames(e))
	}
}

func TestUntitledSaveReturnsErrNoPath(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	run(t, e, "new")
	typeText(t, e, "hello")
	if err := e.Save(); !errors.Is(err, ErrNoPath) {
		t.Fatalf("Save on untitled err = %v, want ErrNoPath", err)
	}
	if !e.Active().Untitled() {
		t.Fatal("Untitled() = false for a buffer with no path")
	}
	if !e.Active().Modified() {
		t.Fatal("a refused save must leave the buffer modified")
	}
}

func TestSaveAsTransitionsUntitledToNamedTab(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "new.txt")
	e := New(Options{})
	run(t, e, "new")
	typeText(t, e, "hello")
	if err := e.SaveAs(target); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	if got := readFile(t, target); got != "hello" {
		t.Fatalf("file = %q, want hello", got)
	}
	if e.Active().Untitled() {
		t.Fatal("after SaveAs the buffer has a path")
	}
	if got := e.Tabs()[0].Name; got != "new.txt" {
		t.Fatalf("tab name = %q, want new.txt", got)
	}
	if e.Active().Modified() {
		t.Fatal("SaveAs must mark the buffer saved")
	}
	if err := e.Save(); err != nil {
		t.Fatalf("Save after SaveAs: %v", err)
	}
	if e.Status() != "Saved" {
		t.Fatalf("status = %q, want Saved", e.Status())
	}
}

func TestAtomicSaveKeepsMode0600(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	e := New(Options{})
	if err := e.OpenFile(path); err != nil {
		t.Fatal(err)
	}
	typeText(t, e, "new ")
	run(t, e, "save")

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode after save = %o, want 600", got)
	}
	if got := readFile(t, path); got != "new old" {
		t.Fatalf("file = %q, want %q", got, "new old")
	}
	if e.Active().Modified() {
		t.Fatal("save must clear the modified state")
	}
	assertNoTempFiles(t, dir)
}

func TestAtomicSaveNewFileGetsDefaultMode(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "made.txt")
	e := New(Options{})
	if err := e.OpenFile(path); err != nil {
		t.Fatal(err)
	}
	typeText(t, e, "made")
	run(t, e, "save")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("mode of new file = %o, want 644", got)
	}
	assertNoTempFiles(t, dir)
}

func TestFailedAtomicSaveLeavesOriginalAndDirty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := New(Options{})
	if err := e.OpenFile(path); err != nil {
		t.Fatal(err)
	}
	typeText(t, e, "x")
	// A read-only directory stops the temporary file from being created.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if probe, err := os.CreateTemp(dir, "probe"); err == nil {
		_ = probe.Close()
		_ = os.Remove(probe.Name())
		t.Skip("directory stays writable for this user, cannot force a failed save")
	}

	err := e.Save()
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("Save err = %v, want a permission error", err)
	}
	if got := readFile(t, path); got != "keep" {
		t.Fatalf("file = %q after failed save, want original", got)
	}
	if !e.Active().Modified() {
		t.Fatal("a failed save must leave the buffer modified")
	}
	if e.Status() == "" || !strings.Contains(e.Status(), path) {
		t.Fatalf("status = %q, want the save error naming the path", e.Status())
	}
}

func TestAtomicSaveThroughSymlinkKeepsLink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "real.txt")
	link := filepath.Join(dir, "link.txt")
	putFile(t, target, "old")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	e := New(Options{})
	if err := e.OpenFile(link); err != nil {
		t.Fatal(err)
	}
	typeText(t, e, "new ")
	run(t, e, "save")
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&fs.ModeSymlink == 0 {
		t.Fatal("save replaced the symbolic link with a regular file")
	}
	if got := readFile(t, target); got != "new old" {
		t.Fatalf("target = %q, want %q", got, "new old")
	}
}

func TestCloseModifiedTabReturnsErrUnsaved(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	putFile(t, a, "a")
	putFile(t, b, "b")
	e := openTwo(t, a, b)
	activateByPath(t, e, a)
	typeText(t, e, "!")

	err := e.CloseTab(0, false)
	if !errors.Is(err, ErrUnsaved) {
		t.Fatalf("CloseTab modified err = %v, want ErrUnsaved", err)
	}
	if n := len(e.Tabs()); n != 2 {
		t.Fatalf("refused close changed the tab count to %d", n)
	}
	if e.Status() != err.Error() {
		t.Fatalf("status = %q, want %q", e.Status(), err.Error())
	}
	if err := e.CloseTab(0, true); err != nil {
		t.Fatalf("forced CloseTab: %v", err)
	}
	if got := tabNames(e); !equalStrings(got, []string{"b.txt"}) {
		t.Fatalf("tabs after forced close = %q", got)
	}
	if got := readFile(t, a); got != "a" {
		t.Fatalf("forced close wrote the file: %q", got)
	}
}

func TestOpenPathAlreadyOpenActivatesThatTab(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	putFile(t, a, "alpha")
	putFile(t, b, "beta")
	e := openTwo(t, a, b)
	activateByPath(t, e, a)
	typeText(t, e, "X")

	if err := e.OpenFile(a); err != nil {
		t.Fatalf("OpenFile on open path: %v", err)
	}
	if n := len(e.Tabs()); n != 2 {
		t.Fatalf("opening an open path added a tab: %q", tabNames(e))
	}
	if e.Active().Path != a {
		t.Fatalf("active path = %q, want %q", e.Active().Path, a)
	}
	if got := e.Active().Buffer.Text(); got != "Xalpha" {
		t.Fatalf("open path reloaded the buffer: %q, want the unsaved text", got)
	}
	if e.Status() != "Opened a.txt" {
		t.Fatalf("status = %q", e.Status())
	}
}

func TestOpenMissingPathBindsEmptyBufferToPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "later.txt")
	e := New(Options{})
	if err := e.OpenFile(path); err != nil {
		t.Fatalf("OpenFile missing: %v", err)
	}
	if e.Active().Path != path || e.Active().Modified() || e.Active().Buffer.Text() != "" {
		t.Fatalf("missing path should open an empty, clean buffer bound to it: %+v", e.Tabs())
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("opening a missing path must not create the file")
	}
	typeText(t, e, "made")
	run(t, e, "save")
	if got := readFile(t, path); got != "made" {
		t.Fatalf("file = %q, want made", got)
	}
}

func TestOpenEmptyPathFails(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	if err := e.OpenFile(""); !errors.Is(err, ErrEmptyPath) {
		t.Fatalf("OpenFile(\"\") err = %v, want ErrEmptyPath", err)
	}
}

func TestStatusIsPerTab(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	putFile(t, a, "a")
	e := New(Options{})
	if err := e.OpenFile(a); err != nil {
		t.Fatal(err)
	}
	if e.Status() != "Opened a.txt" {
		t.Fatalf("status on a = %q", e.Status())
	}
	run(t, e, "new")
	if e.Status() != "New buffer" {
		t.Fatalf("status on new tab = %q, want its own message", e.Status())
	}
	e.ActivateTab(0)
	if e.Status() != "Opened a.txt" {
		t.Fatalf("switching back kept the other tab's status: %q", e.Status())
	}

	// An error on one tab stays on that tab.
	run(t, e, "next_buffer")
	if r := e.Do("save", nil); !errors.Is(r.Err, ErrNoPath) {
		t.Fatalf("save untitled err = %v", r.Err)
	}
	run(t, e, "prev_buffer")
	if e.Status() != "Opened a.txt" {
		t.Fatalf("status leaked from the untitled tab: %q", e.Status())
	}
	run(t, e, "next_buffer")
	if e.Status() != ErrNoPath.Error() {
		t.Fatalf("untitled tab lost its status: %q", e.Status())
	}
}

func TestQuitWithoutDirtyTabsQuitsNow(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	putFile(t, a, "a")
	e := New(Options{})
	if err := e.OpenFile(a); err != nil {
		t.Fatal(err)
	}
	if dirty := e.Quit(false); dirty != nil {
		t.Fatalf("Quit with no dirty tabs listed %+v", dirty)
	}
	if !e.Quitting() {
		t.Fatal("Quit with no dirty tabs must mark the editor for quit")
	}
}

func TestQuitListsDirtyTabsInOrder(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	putFile(t, a, "a")
	putFile(t, b, "b")
	e := New(Options{})
	if err := e.OpenFile(a); err != nil {
		t.Fatal(err)
	}
	typeText(t, e, "1")
	if err := e.OpenFile(b); err != nil {
		t.Fatal(err)
	}
	run(t, e, "new")
	typeText(t, e, "2")
	run(t, e, "new")
	// Tabs: a (dirty), b (clean), Untitled-1 (dirty), Untitled-2 (clean).
	dirty := e.Quit(false)
	var idx []int
	for _, d := range dirty {
		idx = append(idx, d.Index)
	}
	if !slices.Equal(idx, []int{0, 2}) {
		t.Fatalf("dirty indexes = %v, want [0 2]; tabs %+v", idx, e.Tabs())
	}
	if dirty[0].Path != a || dirty[0].Untitled {
		t.Fatalf("first dirty = %+v, want a.txt", dirty[0])
	}
	if !dirty[1].Untitled || dirty[1].Name != "Untitled-1" {
		t.Fatalf("second dirty = %+v, want untitled", dirty[1])
	}
	if e.Quitting() {
		t.Fatal("a quit that lists dirty tabs must not quit")
	}

	// Once each dirty tab is resolved, Quit grants the quit.
	for i := range e.docs {
		e.docs[i].Buffer.MarkSaved()
	}
	if dirty := e.Quit(false); dirty != nil || !e.Quitting() {
		t.Fatalf("after resolving tabs, Quit = %+v, quitting %v", dirty, e.Quitting())
	}
}

func TestQuitForcedIgnoresDirtyTabs(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	typeText(t, e, "dirty")
	if dirty := e.Quit(true); dirty != nil || !e.Quitting() {
		t.Fatalf("forced Quit = %+v, quitting %v", dirty, e.Quitting())
	}
}

func TestTabLanguageIsSetAndClearedOnRename(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	putFile(t, path, "package main\n")
	e := New(Options{})
	if err := e.OpenFile(path); err != nil {
		t.Fatal(err)
	}
	e.SetLanguage("Go")
	if got := e.Tabs()[0].Language; got != "Go" {
		t.Fatalf("Tab.Language = %q, want Go", got)
	}
	if err := e.RenamePath(path, filepath.Join(dir, "main.txt")); err != nil {
		t.Fatal(err)
	}
	if got := e.Tabs()[0].Language; got != "" {
		t.Fatalf("rename kept language %q, want it cleared", got)
	}
}

func TestNextAndPrevBufferWrap(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	run(t, e, "new", "new", "new")
	if len(e.Tabs()) != 3 || !e.Tabs()[2].Active {
		t.Fatal("setup: last tab should be active")
	}
	run(t, e, "next_buffer")
	if !e.Tabs()[0].Active {
		t.Fatal("next_buffer should wrap to the first tab")
	}
	run(t, e, "prev_buffer")
	if !e.Tabs()[2].Active {
		t.Fatal("prev_buffer should wrap to the last tab")
	}
}

// activateByPath activates the tab whose path is path and fails the test when
// no tab matches.
func activateByPath(t *testing.T, e *Editor, path string) {
	t.Helper()
	i, ok := e.DocumentIndexByPath(path)
	if !ok {
		t.Fatalf("no tab for %s", path)
	}
	e.ActivateTab(i)
}

// assertNoTempFiles fails when dir holds a leftover temporary save file.
func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, en := range entries {
		if strings.Contains(en.Name(), ".gvim-") {
			t.Fatalf("temporary file left behind: %s", en.Name())
		}
	}
}
