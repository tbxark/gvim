package tui

import (
	"context"
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Windows of a remote large document. A remote large file opens in large-file mode
// over an asynchronous window source (document/remote_large.go): reads outside the
// loaded windows return buffer.ErrSourcePending at once, so the paint path never
// waits on the remote host. This file is the root's side: after each update it arms
// one short delayed check, and when the paint has recorded a missed window it loads
// that window in a command. The command's message makes the next update repaint with
// the loaded text. The poll exists only while the active document is a windowed
// remote large file, and the large-state context cancels a load when the model is
// closed.

// LargeWindow is one pending window of a remote large document.
type LargeWindow struct {
	Path string
	Off  int64
	N    int64
}

// LargeWindowLoader is the app's windowed large-file view of the active document:
// whether it is a remote large file, the window its last paint missed, and the
// blocking load of one window (only ever called from a command).
type LargeWindowLoader interface {
	AsyncLarge() bool
	PendingLargeWindow() (LargeWindow, bool)
	LoadLargeWindow(ctx context.Context, w LargeWindow) error
}

const (
	// windowPollDelay lets the frame that follows an update record any missed window
	// before the root checks again. It is a repaint delay, not a network timeout.
	windowPollDelay = 25 * time.Millisecond
	// windowLoadTimeout bounds one window load; the remote access has its own
	// timeout, this one also covers a stuck carrier.
	windowLoadTimeout = 30 * time.Second
)

// windowFetchedMsg is one loaded window (or its error). The generation makes a reply
// from before the large state was restarted discardable.
type windowFetchedMsg struct {
	gen uint64
	err error
}

// windowPollMsg is the delayed check for a window missed by the last paint.
type windowPollMsg struct{ gen uint64 }

// asyncLargeDoc reports whether the active document is a windowed remote large file.
func (m *Model) asyncLargeDoc() bool {
	return m.largeWindows != nil && m.largeWindows.AsyncLarge()
}

// windowSync starts one window load when the active remote large document has a
// missed window, or arms one delayed check on the first update after the document
// became active. It is called after every update and returns at once; the remote
// read itself happens in the command this returns.
func (m *Model) windowSync() tea.Cmd {
	if !m.asyncLargeDoc() {
		return nil
	}
	if m.large.windowBusy {
		return nil
	}
	loader := m.largeWindows
	if w, ok := loader.PendingLargeWindow(); ok {
		return m.windowLoadCmd(loader, w)
	}
	// The paint that runs after this update may record the miss: check once shortly
	// after it. Do not arm another check from the check itself, so an idle remote
	// large document does not poll.
	if m.large.windowSkipArm {
		m.large.windowSkipArm = false
		return nil
	}
	if m.large.windowPollPending {
		return nil
	}
	m.large.windowPollPending = true
	gen := m.large.gen
	return tea.Tick(windowPollDelay, func(time.Time) tea.Msg { return windowPollMsg{gen: gen} })
}

// windowLoadCmd loads window w in a command, bounded by windowLoadTimeout.
func (m *Model) windowLoadCmd(loader LargeWindowLoader, w LargeWindow) tea.Cmd {
	m.large.windowBusy = true
	ctx, gen := m.large.context(), m.large.gen
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, windowLoadTimeout)
		defer cancel()
		return windowFetchedMsg{gen: gen, err: loader.LoadLargeWindow(ctx, w)}
	}
}

// windowPolled handles the delayed check: a missed window is loaded, and otherwise
// the chain stops until the next update.
func (m *Model) windowPolled(msg windowPollMsg) tea.Cmd {
	if msg.gen != m.large.gen {
		return nil
	}
	m.large.windowPollPending = false
	if !m.asyncLargeDoc() {
		return nil
	}
	loader := m.largeWindows
	if w, ok := loader.PendingLargeWindow(); ok {
		return m.windowLoadCmd(loader, w)
	}
	// No miss: the next update arms the next check. Marking this so the batch added
	// after this message does not arm one immediately (the idle spin).
	m.large.windowSkipArm = true
	return nil
}

// windowDone handles a loaded window: a stale reply is dropped, and a valid one lets
// the next update's paint read the window (and record the next miss, if any).
func (m *Model) windowDone(msg windowFetchedMsg) {
	m.large.windowBusy = false
	if msg.gen != m.large.gen {
		return // the large state was stopped or restarted: the reply is stale
	}
	if msg.err != nil && !errors.Is(msg.err, context.Canceled) && !errors.Is(msg.err, context.DeadlineExceeded) && m.asyncLargeDoc() {
		// A failed window read is stated but is not a document failure: the file is
		// read-only and the next paint can retry the same window (e.g. after a
		// reconnect), so no error dialog and no read-only flip. A cancellation, or a
		// reply after the user left the document, stays silent.
		m.notice = uiTextVars("gvim.remote.large_file_error", "Remote large file: %{error}", map[string]any{"error": msg.err.Error()})
	}
}
