package plugin

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeEditor struct {
	notes     []string
	inserted  []string
	insertErr error
	path      string
	status    []string

	// insertEntered and insertRelease, when set, make InsertText signal
	// insertEntered and then block until insertRelease is closed. They model a
	// native call that does not finish on its own.
	insertEntered chan struct{}
	insertRelease chan struct{}
}

// codedError is a Go error with an exported method. Plugin code must not reach
// the method through the error it catches.
type codedError struct{ msg string }

func (e codedError) Error() string { return e.msg }
func (e codedError) Code() int     { return 7 }

func (f *fakeEditor) Notify(msg string) { f.notes = append(f.notes, msg) }

func (f *fakeEditor) SetStatus(text string) { f.status = append(f.status, text) }

func (f *fakeEditor) ActivePath() string { return f.path }

func (f *fakeEditor) InsertText(text string) error {
	if f.insertEntered != nil {
		f.insertEntered <- struct{}{}
		<-f.insertRelease
	}
	if f.insertErr != nil {
		return f.insertErr
	}
	f.inserted = append(f.inserted, text)
	return nil
}

// writeDir creates a temp directory holding files keyed by relative path.
func writeDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func newHost(t *testing.T, dir string, ed Editor, timeout time.Duration) *Host {
	t.Helper()
	h, err := New(Options{Dir: dir, Editor: ed, Timeout: timeout})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	return h
}

func loadClean(t *testing.T, h *Host) {
	t.Helper()
	if errs := h.Load(); len(errs) != 0 {
		t.Fatalf("Load() errors = %v, want none", errs)
	}
}

func TestJavaScriptCommandRunsAndNotifies(t *testing.T) {
	ed := &fakeEditor{}
	dir := writeDir(t, map[string]string{
		"hello.js": `gvim.registerCommand("hello", "Say Hello", function () { gvim.notify("hi from js"); });`,
	})
	h := newHost(t, dir, ed, time.Second)
	loadClean(t, h)

	if err := h.Run("hello"); err != nil {
		t.Fatalf("Run(hello) error = %v", err)
	}
	if want := []string{"hi from js"}; !reflect.DeepEqual(ed.notes, want) {
		t.Errorf("notes = %q, want %q", ed.notes, want)
	}
}

func TestTypeScriptWithAnnotationsLoadsAndRuns(t *testing.T) {
	ed := &fakeEditor{}
	dir := writeDir(t, map[string]string{
		"shout.ts": `
interface Greeting { text: string }
const shout = (g: Greeting): string => g.text.toUpperCase();
gvim.registerCommand("shout", "Shout", (): void => {
  const g: Greeting = { text: "typed" };
  gvim.notify(shout(g));
});
`,
	})
	h := newHost(t, dir, ed, time.Second)
	loadClean(t, h)

	if err := h.Run("shout"); err != nil {
		t.Fatalf("Run(shout) error = %v", err)
	}
	if want := []string{"TYPED"}; !reflect.DeepEqual(ed.notes, want) {
		t.Errorf("notes = %q, want %q", ed.notes, want)
	}
}

func TestBrokenFileIsReportedByNameAndOthersStillLoad(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"broken.js", `gvim.registerCommand("x", `},
		{"broken.ts", `const x: number = ;`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ed := &fakeEditor{}
			dir := writeDir(t, map[string]string{
				tt.name:   tt.body,
				"good.js": `gvim.registerCommand("good", "Good", function () { gvim.notify("ok"); });`,
			})
			h := newHost(t, dir, ed, time.Second)

			errs := h.Load()
			if len(errs) != 1 {
				t.Fatalf("Load() returned %d errors, want 1: %v", len(errs), errs)
			}
			if !strings.Contains(errs[0].Error(), tt.name) {
				t.Errorf("error %v does not name file %q", errs[0], tt.name)
			}
			if err := h.Run("good"); err != nil {
				t.Errorf("Run(good) error = %v", err)
			}
		})
	}
}

func TestInfiniteLoopTimesOutAndLaterRunsWork(t *testing.T) {
	ed := &fakeEditor{}
	dir := writeDir(t, map[string]string{
		"spin.js": `
gvim.registerCommand("spin", "Spin", function () { while (true) {} });
gvim.registerCommand("swallow", "Swallow", function () { for (;;) { try { while (true) {} } catch (e) {} } });
gvim.registerCommand("quick", "Quick", function () { gvim.notify("ok"); });
`,
	})
	h := newHost(t, dir, ed, 50*time.Millisecond)
	loadClean(t, h)

	for _, name := range []string{"spin", "swallow"} {
		err := h.Run(name)
		if err == nil || !strings.Contains(err.Error(), "timed out") {
			t.Fatalf("Run(%s) error = %v, want a timed out error", name, err)
		}
	}
	if err := h.Run("quick"); err != nil {
		t.Fatalf("Run(quick) after a timeout error = %v", err)
	}
	if want := []string{"ok"}; !reflect.DeepEqual(ed.notes, want) {
		t.Errorf("notes = %q, want %q", ed.notes, want)
	}
}

func TestTopLevelLoopTimesOutDuringLoad(t *testing.T) {
	dir := writeDir(t, map[string]string{"spin.js": `while (true) {}`})
	h := newHost(t, dir, &fakeEditor{}, 50*time.Millisecond)

	errs := h.Load()
	if len(errs) != 1 {
		t.Fatalf("Load() returned %d errors, want 1: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0].Error(), "timed out") || !strings.Contains(errs[0].Error(), "spin.js") {
		t.Errorf("error = %v, want a timed out error naming spin.js", errs[0])
	}
}

func TestThrowingCommandReturnsError(t *testing.T) {
	dir := writeDir(t, map[string]string{
		"boom.js": `gvim.registerCommand("boom", "Boom", function () { throw new Error("kaboom"); });`,
	})
	h := newHost(t, dir, &fakeEditor{}, time.Second)
	loadClean(t, h)

	err := h.Run("boom")
	if err == nil || !strings.Contains(err.Error(), "kaboom") {
		t.Fatalf("Run(boom) error = %v, want an error containing kaboom", err)
	}
}

func TestGlobalsOtherThanGvimAreUndefined(t *testing.T) {
	for _, expr := range []string{"require", "process", "module", "exports", "console", "fs", "Buffer", "globalThis.require", "globalThis.process"} {
		t.Run(expr, func(t *testing.T) {
			ed := &fakeEditor{}
			dir := writeDir(t, map[string]string{
				"probe.js": `gvim.registerCommand("probe", "Probe", function () { gvim.notify(typeof ` + expr + `); });`,
			})
			h := newHost(t, dir, ed, time.Second)
			loadClean(t, h)

			if err := h.Run("probe"); err != nil {
				t.Fatalf("Run(probe) error = %v", err)
			}
			if want := []string{"undefined"}; !reflect.DeepEqual(ed.notes, want) {
				t.Errorf("typeof %s: notes = %q, want %q", expr, ed.notes, want)
			}
		})
	}
}

func TestGvimExposesOnlyDocumentedFunctions(t *testing.T) {
	ed := &fakeEditor{}
	dir := writeDir(t, map[string]string{
		"api.js": `gvim.registerCommand("keys", "Keys", function () { gvim.notify(Object.keys(gvim).sort().join(",")); });`,
	})
	h := newHost(t, dir, ed, time.Second)
	loadClean(t, h)

	if err := h.Run("keys"); err != nil {
		t.Fatalf("Run(keys) error = %v", err)
	}
	want := []string{"activePath,insertText,notify,registerCommand,setStatus"}
	if !reflect.DeepEqual(ed.notes, want) {
		t.Errorf("gvim keys = %q, want %q", ed.notes, want)
	}
}

func TestEditorBridge(t *testing.T) {
	ed := &fakeEditor{path: "/work/main.go"}
	dir := writeDir(t, map[string]string{
		"bridge.js": `
gvim.registerCommand("bridge", "Bridge", function () {
  gvim.setStatus("editing " + gvim.activePath());
  gvim.insertText("abc");
});
`,
	})
	h := newHost(t, dir, ed, time.Second)
	loadClean(t, h)

	if err := h.Run("bridge"); err != nil {
		t.Fatalf("Run(bridge) error = %v", err)
	}
	if want := []string{"editing /work/main.go"}; !reflect.DeepEqual(ed.status, want) {
		t.Errorf("status = %q, want %q", ed.status, want)
	}
	if want := []string{"abc"}; !reflect.DeepEqual(ed.inserted, want) {
		t.Errorf("inserted = %q, want %q", ed.inserted, want)
	}
}

func TestInsertTextErrorIsCatchableByPlugin(t *testing.T) {
	ed := &fakeEditor{insertErr: errors.New("read only")}
	dir := writeDir(t, map[string]string{
		"insert.js": `
gvim.registerCommand("insert", "Insert", function () {
  try { gvim.insertText("x"); } catch (e) { gvim.notify("caught: " + e.message); }
});
`,
	})
	h := newHost(t, dir, ed, time.Second)
	loadClean(t, h)

	if err := h.Run("insert"); err != nil {
		t.Fatalf("Run(insert) error = %v", err)
	}
	if want := []string{"caught: read only"}; !reflect.DeepEqual(ed.notes, want) {
		t.Errorf("notes = %q, want %q", ed.notes, want)
	}
}

func TestCommandsListsNamesAndLabelsSorted(t *testing.T) {
	dir := writeDir(t, map[string]string{
		"one.js": `gvim.registerCommand("zeta", "Zeta Label", function () {});`,
		"two.ts": `gvim.registerCommand("alpha", "Alpha Label", (): void => {});`,
	})
	h := newHost(t, dir, &fakeEditor{}, time.Second)
	loadClean(t, h)

	want := []Command{{Name: "alpha", Label: "Alpha Label"}, {Name: "zeta", Label: "Zeta Label"}}
	if got := h.Commands(); !reflect.DeepEqual(got, want) {
		t.Errorf("Commands() = %v, want %v", got, want)
	}
}

func TestRunUnknownCommandReturnsError(t *testing.T) {
	h := newHost(t, writeDir(t, nil), &fakeEditor{}, time.Second)
	loadClean(t, h)

	if err := h.Run("missing"); err == nil {
		t.Fatal("Run(missing) returned nil error, want an unknown command error")
	}
}

func TestEachFileRunsInItsOwnRuntime(t *testing.T) {
	ed := &fakeEditor{}
	dir := writeDir(t, map[string]string{
		"a.js": `var shared = 1; gvim.registerCommand("a", "A", function () { gvim.notify(typeof shared); });`,
		"b.js": `gvim.registerCommand("b", "B", function () { gvim.notify(typeof shared); });`,
	})
	h := newHost(t, dir, ed, time.Second)
	loadClean(t, h)

	for _, name := range []string{"a", "b"} {
		if err := h.Run(name); err != nil {
			t.Fatalf("Run(%s) error = %v", name, err)
		}
	}
	if want := []string{"number", "undefined"}; !reflect.DeepEqual(ed.notes, want) {
		t.Errorf("notes = %q, want %q", ed.notes, want)
	}
}

func TestLoadRejectsInvalidRegistrationsAndRollsBackFile(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"not-a-function.js", `gvim.registerCommand("bad", "Bad", 42);`},
		{"empty-name.js", `gvim.registerCommand("", "Empty", function () {});`},
		{"throws-late.js", `gvim.registerCommand("partial", "Partial", function () {}); throw new Error("late");`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := writeDir(t, map[string]string{tt.name: tt.body})
			h := newHost(t, dir, &fakeEditor{}, time.Second)

			errs := h.Load()
			if len(errs) != 1 || !strings.Contains(errs[0].Error(), tt.name) {
				t.Fatalf("Load() = %v, want one error naming %s", errs, tt.name)
			}
			if got := h.Commands(); len(got) != 0 {
				t.Errorf("Commands() = %v, want none from a failed file", got)
			}
		})
	}
}

func TestDuplicateCommandNameIsRejected(t *testing.T) {
	dir := writeDir(t, map[string]string{
		"a.js": `gvim.registerCommand("dup", "First", function () {});`,
		"b.js": `gvim.registerCommand("dup", "Second", function () {});`,
	})
	h := newHost(t, dir, &fakeEditor{}, time.Second)

	errs := h.Load()
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "b.js") {
		t.Fatalf("Load() = %v, want one error naming b.js", errs)
	}
	want := []Command{{Name: "dup", Label: "First"}}
	if got := h.Commands(); !reflect.DeepEqual(got, want) {
		t.Errorf("Commands() = %v, want %v", got, want)
	}
}

func TestLoadReadsOnlyTopLevelJSAndTSFiles(t *testing.T) {
	dir := writeDir(t, map[string]string{
		"top.js":          `gvim.registerCommand("top", "Top", function () {});`,
		"side.ts":         `gvim.registerCommand("side", "Side", (): void => {});`,
		"notes.txt":       `gvim.registerCommand("txt", "Txt", function () {});`,
		"sub/inner.js":    `gvim.registerCommand("inner", "Inner", function () {});`,
		"sub/nested.ts":   `gvim.registerCommand("nested", "Nested", (): void => {});`,
		"README.markdown": `not a plugin`,
	})
	h := newHost(t, dir, &fakeEditor{}, time.Second)
	loadClean(t, h)

	want := []Command{{Name: "side", Label: "Side"}, {Name: "top", Label: "Top"}}
	if got := h.Commands(); !reflect.DeepEqual(got, want) {
		t.Errorf("Commands() = %v, want %v", got, want)
	}
}

func TestLoadMissingDirReportsOneError(t *testing.T) {
	h := newHost(t, filepath.Join(t.TempDir(), "missing"), &fakeEditor{}, time.Second)

	if errs := h.Load(); len(errs) != 1 {
		t.Errorf("Load() returned %d errors, want 1: %v", len(errs), errs)
	}
}

func TestNewRequiresEditorAndDefaultsTimeout(t *testing.T) {
	if _, err := New(Options{Dir: t.TempDir()}); err == nil {
		t.Error("New without an Editor returned nil error")
	}

	tests := []struct {
		name        string
		timeout     time.Duration
		budget      time.Duration
		wantTimeout time.Duration
		wantBudget  time.Duration
	}{
		{"zero means the defaults", 0, 0, 500 * time.Millisecond, 2 * time.Second},
		{"explicit values are kept", 5 * time.Second, 7 * time.Second, 5 * time.Second, 7 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, err := New(Options{Dir: t.TempDir(), Editor: &fakeEditor{}, Timeout: tt.timeout, LoadBudget: tt.budget})
			if err != nil {
				t.Fatal(err)
			}
			if h.timeout != tt.wantTimeout {
				t.Errorf("timeout = %v, want %v", h.timeout, tt.wantTimeout)
			}
			if h.loadBudget != tt.wantBudget {
				t.Errorf("load budget = %v, want %v", h.loadBudget, tt.wantBudget)
			}
		})
	}
}

// spinning is top-level code that never returns, so a load of it runs until the
// host interrupts it.
const spinning = `while (true) {}`

// TestLoadBudgetCoversEveryFile checks that the top-level code of all the files
// shares one load budget. A file that starts after the budget ran out is not run,
// and it is reported as not loaded. The sum of the per-file caps is never paid.
func TestLoadBudgetCoversEveryFile(t *testing.T) {
	tests := []struct {
		name       string
		files      map[string]string
		timeout    time.Duration
		budget     time.Duration
		maxElapsed time.Duration // a bound that the sum of the per-file caps would exceed
		wantLoaded []string      // commands that must be registered, sorted
		wantSlow   []string      // files whose top-level code is interrupted
		wantLate   []string      // files that are not run because the budget ran out
	}{
		{
			name: "every file loads inside the budget",
			files: map[string]string{
				"a.js": `gvim.registerCommand("a", "A", function () {});`,
				"b.js": `gvim.registerCommand("b", "B", function () {});`,
			},
			timeout:    time.Second,
			budget:     time.Second,
			maxElapsed: time.Second,
			wantLoaded: []string{"a", "b"},
		},
		{
			name: "a slow file uses up the budget and the rest are reported",
			files: map[string]string{
				"a_spin.js": spinning,
				"b.js":      `gvim.registerCommand("b", "B", function () {});`,
				"c.js":      `gvim.registerCommand("c", "C", function () {});`,
			},
			timeout:    time.Second,
			budget:     100 * time.Millisecond,
			maxElapsed: 800 * time.Millisecond,
			wantSlow:   []string{"a_spin.js"},
			wantLate:   []string{"b.js", "c.js"},
		},
		{
			name: "each slow file gets only what is left of the budget",
			files: map[string]string{
				"a_spin.js": spinning,
				"b_spin.js": spinning,
				"c_spin.js": spinning,
				"d.js":      `gvim.registerCommand("d", "D", function () {});`,
			},
			timeout:    300 * time.Millisecond,
			budget:     400 * time.Millisecond,
			maxElapsed: 700 * time.Millisecond,
			wantSlow:   []string{"a_spin.js", "b_spin.js"},
			wantLate:   []string{"c_spin.js", "d.js"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, err := New(Options{Dir: writeDir(t, tt.files), Editor: &fakeEditor{}, Timeout: tt.timeout, LoadBudget: tt.budget})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(h.Close)

			start := time.Now()
			errs := h.Load()
			if elapsed := time.Since(start); elapsed > tt.maxElapsed {
				t.Errorf("Load took %v, want at most %v", elapsed, tt.maxElapsed)
			}

			msgs := make(map[string]string)
			for _, err := range errs {
				for name := range tt.files {
					if strings.Contains(err.Error(), name) {
						msgs[name] = err.Error()
					}
				}
			}
			if want := len(tt.wantSlow) + len(tt.wantLate); len(errs) != want {
				t.Errorf("Load() returned %d errors, want %d: %v", len(errs), want, errs)
			}
			for _, name := range tt.wantSlow {
				if !strings.Contains(msgs[name], "timed out") {
					t.Errorf("%s: error = %q, want a timed out error", name, msgs[name])
				}
			}
			for _, name := range tt.wantLate {
				if !strings.Contains(msgs[name], "load budget") {
					t.Errorf("%s: error = %q, want a not loaded error for the load budget", name, msgs[name])
				}
			}
			var got []string
			for _, c := range h.Commands() {
				got = append(got, c.Name)
			}
			if !slices.Equal(got, tt.wantLoaded) {
				t.Errorf("registered commands = %q, want %q", got, tt.wantLoaded)
			}
		})
	}
}

func TestCloseDropsCommands(t *testing.T) {
	dir := writeDir(t, map[string]string{
		"a.js": `gvim.registerCommand("a", "A", function () {});`,
	})
	h := newHost(t, dir, &fakeEditor{}, time.Second)
	loadClean(t, h)

	h.Close()
	if got := h.Commands(); len(got) != 0 {
		t.Errorf("Commands() after Close = %v, want none", got)
	}
	if err := h.Run("a"); err == nil {
		t.Error("Run(a) after Close returned nil error")
	}
}

// waitLimit bounds every wait in the blocking-call test.
const waitLimit = 5 * time.Second

// within runs fn and fails the test if fn does not return within waitLimit.
func within(t *testing.T, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		fn()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(waitLimit):
		t.Fatalf("%s did not return within %v", what, waitLimit)
	}
}

func TestErrorsReachJavaScriptAsPlainMessages(t *testing.T) {
	// probe describes a caught value using only plain values.
	const probe = `function probe(e) { return [typeof e.value, typeof e.code, e.message, e instanceof Error].join("|"); }`
	tests := []struct {
		name   string
		editor *fakeEditor
		files  map[string]string
		run    string
		want   string
	}{
		{
			name:   "insertText failure",
			editor: &fakeEditor{insertErr: codedError{msg: "read only"}},
			files: map[string]string{"ins.js": probe + `
gvim.registerCommand("ins", "Ins", function () {
  try { gvim.insertText("x"); } catch (e) { gvim.notify(probe(e)); }
});`},
			run:  "ins",
			want: "undefined|undefined|read only|true",
		},
		{
			name:   "duplicate command name",
			editor: &fakeEditor{},
			files: map[string]string{
				"a.js": `gvim.registerCommand("dup", "First", function () {});`,
				"b.js": probe + `
gvim.registerCommand("tryDup", "Try", function () {
  try { gvim.registerCommand("dup", "Second", function () {}); } catch (e) { gvim.notify(probe(e)); }
});`,
			},
			run:  "tryDup",
			want: `undefined|undefined|registerCommand: command "dup" is already registered|true`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHost(t, writeDir(t, tt.files), tt.editor, time.Second)
			loadClean(t, h)

			if err := h.Run(tt.run); err != nil {
				t.Fatalf("Run(%s) error = %v", tt.run, err)
			}
			if want := []string{tt.want}; !reflect.DeepEqual(tt.editor.notes, want) {
				t.Errorf("notes = %q, want %q", tt.editor.notes, want)
			}
		})
	}
}

func TestNativeCallDoesNotBlockCommandsOrClose(t *testing.T) {
	ed := &fakeEditor{insertEntered: make(chan struct{}, 1), insertRelease: make(chan struct{})}
	dir := writeDir(t, map[string]string{
		"block.js": `gvim.registerCommand("block", "Block", function () { gvim.insertText("x"); });`,
	})
	const timeout = 20 * time.Millisecond
	h := newHost(t, dir, ed, timeout)
	loadClean(t, h)
	// Registered after newHost, so it runs before the host's Close cleanup and
	// unblocks the native call if the test fails early.
	release := sync.OnceFunc(func() { close(ed.insertRelease) })
	t.Cleanup(release)

	runErr := make(chan error, 1)
	go func() { runErr <- h.Run("block") }()
	select {
	case <-ed.insertEntered:
	case <-time.After(waitLimit):
		t.Fatal("native InsertText was not entered")
	}

	var got []Command
	within(t, "Commands during a blocked native call", func() { got = h.Commands() })
	if want := []Command{{Name: "block", Label: "Block"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("Commands() = %v, want %v", got, want)
	}

	var busyErr error
	within(t, "second Run during a blocked native call", func() { busyErr = h.Run("block") })
	if busyErr == nil || !strings.Contains(busyErr.Error(), "busy") {
		t.Errorf("second Run error = %v, want a busy error", busyErr)
	}

	within(t, "Close during a blocked native call", h.Close)

	select {
	case err := <-runErr:
		t.Fatalf("Run returned %v before the native call finished", err)
	default:
	}

	// Let the host timeout pass while the native call is still blocked, so the
	// timeout has fired before the call returns.
	time.Sleep(10 * timeout)
	release()
	select {
	case err := <-runErr:
		if err == nil || !strings.Contains(err.Error(), "timed out") {
			t.Errorf("Run(block) error = %v, want a timed out error once the native call returns", err)
		}
	case <-time.After(waitLimit):
		t.Fatal("Run did not return after the native call finished")
	}
}
