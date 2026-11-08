package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tbxark/gvim/theme"
)

// fakeWindows is a LargeWindowLoader the test drives.
type fakeWindows struct {
	async   bool
	pending []LargeWindow
	loaded  []LargeWindow
	err     error
	ctxErr  error
}

func (f *fakeWindows) AsyncLarge() bool { return f.async }
func (f *fakeWindows) PendingLargeWindow() (LargeWindow, bool) {
	if len(f.pending) == 0 {
		return LargeWindow{}, false
	}
	w := f.pending[0]
	f.pending = f.pending[1:]
	return w, true
}
func (f *fakeWindows) LoadLargeWindow(ctx context.Context, w LargeWindow) error {
	f.ctxErr = ctx.Err()
	if f.err != nil {
		return f.err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	f.loaded = append(f.loaded, w)
	return nil
}

func windowTestModel(loader LargeWindowLoader) *Model {
	return New(Deps{Editor: newFakeEditor("x"), Theme: theme.Theme{}, Clock: testClock, LargeWindows: loader})
}

func TestWindowSyncArmsOnePollAndStopsIdle(t *testing.T) {
	loader := &fakeWindows{async: true}
	m := windowTestModel(loader)
	cmd := m.windowSync()
	if cmd == nil {
		t.Fatal("no delayed check was armed")
	}
	if !m.large.windowPollPending {
		t.Fatal("the delayed check was not marked pending")
	}
	if second := m.windowSync(); second != nil {
		t.Fatal("a second delayed check was armed")
	}
	msg := cmd()
	if _, ok := msg.(windowPollMsg); !ok {
		t.Fatalf("poll command produced %T", msg)
	}
	m.Update(msg)
	if m.large.windowPollPending {
		t.Fatal("the poll is still marked pending after it fired")
	}
	// No miss: the check itself did not arm another (no idle spin).
	if loader.loaded != nil {
		t.Fatalf("an idle poll loaded windows: %+v", loader.loaded)
	}
}

func TestWindowSyncLoadsAPendingWindow(t *testing.T) {
	loader := &fakeWindows{async: true, pending: []LargeWindow{{Path: "/remote/big", Off: 1 << 20, N: 1 << 20}}}
	m := windowTestModel(loader)
	cmd := m.windowSync()
	if cmd == nil {
		t.Fatal("a pending window returned no load command")
	}
	if !m.large.windowBusy {
		t.Fatal("the load was not marked busy")
	}
	msg := cmd()
	fetched, ok := msg.(windowFetchedMsg)
	if !ok || fetched.err != nil {
		t.Fatalf("load command = %#v", msg)
	}
	if _, next := m.Update(fetched); next == nil {
		t.Fatal("the fetched window armed no next step/repaint")
	}
	if m.large.windowBusy {
		t.Fatal("the window is still marked busy")
	}
	if len(loader.loaded) != 1 || loader.loaded[0].Off != 1<<20 {
		t.Fatalf("loaded = %+v", loader.loaded)
	}
	if strings.Contains(m.notice, "Remote large file") {
		t.Fatalf("a successful load set a notice: %q", m.notice)
	}
}

func TestWindowLoadErrorIsAStatedNoticeNotAFailure(t *testing.T) {
	loader := &fakeWindows{async: true, pending: []LargeWindow{{Path: "/remote/big", Off: 0, N: 1}}, err: errors.New("carrier dropped")}
	m := windowTestModel(loader)
	cmd := m.windowSync()
	m.Update(cmd())
	if !strings.Contains(m.notice, "carrier dropped") {
		t.Fatalf("notice = %q, want the window error stated", m.notice)
	}
	if len(m.overlays) != 0 {
		t.Fatalf("a window error opened %d overlays; it must not be an error dialog", len(m.overlays))
	}
}

func TestWindowLoadCancelledOnStop(t *testing.T) {
	loader := &fakeWindows{async: true, pending: []LargeWindow{{Path: "/remote/big", Off: 0, N: 1}}}
	m := windowTestModel(loader)
	cmd := m.windowSync()
	if cmd == nil {
		t.Fatal("no load command")
	}
	gen := m.large.gen
	m.large.stop() // model closing: cancels the context and bumps the generation
	if m.large.gen == gen {
		t.Fatal("stop did not bump the generation")
	}
	msg := cmd()
	fetched, ok := msg.(windowFetchedMsg)
	if !ok || !errors.Is(fetched.err, context.Canceled) {
		t.Fatalf("load after stop = %#v, want context.Canceled", msg)
	}
	m.Update(fetched)
	if m.large.windowBusy {
		t.Fatal("a stale cancelled load left the window busy")
	}
	if strings.Contains(m.notice, "context canceled") {
		t.Fatalf("cancellation was reported as an error: %q", m.notice)
	}
}

func TestWindowSyncWithoutAsyncDocumentDoesNothing(t *testing.T) {
	m := windowTestModel(&fakeWindows{async: false})
	if cmd := m.windowSync(); cmd != nil {
		t.Fatalf("a local document armed a window check: %T", cmd)
	}
	m2 := windowTestModel(nil)
	if cmd := m2.windowSync(); cmd != nil {
		t.Fatalf("a nil loader armed a window check: %T", cmd)
	}
}
