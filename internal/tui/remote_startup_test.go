package tui

import (
	"errors"
	"testing"

	"github.com/tbxark/gvim/document"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/theme"
)

// remoteStartupEditor is a fake editor with the asynchronous (large-file) open path and
// the remote path classifier: LoadFunc fails or succeeds on demand and OpenLoaded records
// what the root opened. The synchronous file methods are inert, so a test can prove the
// startup open only went through the async path.
type remoteStartupEditor struct {
	*fakeEditor
	loadErr   error
	loads     int
	opened    *editor.Document
	line, col int
}

func newRemoteStartupEditor() *remoteStartupEditor {
	return &remoteStartupEditor{fakeEditor: newFakeEditor("local")}
}

func (e *remoteStartupEditor) LoadFunc(path string) func() (*editor.Document, error) {
	return func() (*editor.Document, error) {
		e.loads++
		if e.loadErr != nil {
			return nil, e.loadErr
		}
		return document.NewDocument(path, "remote text"), nil
	}
}

func (e *remoteStartupEditor) OpenLoaded(d *editor.Document) { e.opened = d }

func (e *remoteStartupEditor) IsLargePath(string) bool { return true }

func (e *remoteStartupEditor) OpenFile(string) error { return nil }

func (e *remoteStartupEditor) OpenPreview(string) error { return nil }

func (e *remoteStartupEditor) GotoLine(line, col int) error {
	e.line, e.col = line, col
	return nil
}

// startupModel builds a model whose first command opens a remote file.
func startupModel(e *remoteStartupEditor) *Model {
	return New(Deps{Editor: e, StartupOpen: "/remote/a.txt", Theme: theme.Theme{}, Clock: testClock, Keys: fakeKeymap{}})
}

func TestStartupOpenUsesRequestedPositionAfterRetry(t *testing.T) {
	e := newRemoteStartupEditor()
	m := New(Deps{Editor: e, StartupOpen: "/remote/a.txt", StartupLine: 3, StartupCol: 5, Theme: theme.Theme{}, Clock: testClock, Keys: fakeKeymap{}})
	e.loadErr = errors.New("disconnected")
	m.Update(m.startupOpenCmd()())
	e.loadErr = nil
	_, retry := m.Update(authorityReconnectedMsg{})
	if retry == nil {
		t.Fatal("reconnect did not retry")
	}
	m.Update(retry())
	if e.opened == nil || e.line != 3 || e.col != 5 {
		t.Fatalf("open = %v, position = %d:%d; want opened at 3:5", e.opened, e.line, e.col)
	}
}

// TestStartupOpenRetriesAfterReconnect checks the remote startup-open path: a load failure
// while the authority is down is a notice (never a local empty buffer), repeated failed
// reconnects keep the open pending, and the first successful reconnect retries exactly
// once and opens the document.
func TestStartupOpenRetriesAfterReconnect(t *testing.T) {
	e := newRemoteStartupEditor()
	m := startupModel(e)
	if !m.startupPending {
		t.Fatal("the startup open is not pending")
	}
	// Before any connection the first command runs and fails: notice only.
	e.loadErr = errors.New("SSH: connection is down")
	cmd := m.startupOpenCmd()
	if cmd == nil {
		t.Fatal("no startup command was scheduled")
	}
	m.Update(cmd())
	if e.opened != nil {
		t.Fatal("a failed startup open opened a document")
	}
	if m.notice == "" {
		t.Fatal("a failed startup open left no notice")
	}
	if !m.startupPending {
		t.Fatal("a failed startup open stopped being retryable")
	}
	// A failed reconnect does not attempt the open.
	loads := e.loads
	if _, retry := m.Update(authorityReconnectedMsg{err: errors.New("dial failed")}); retry != nil {
		t.Fatal("a failed reconnect retried the open")
	}
	if e.loads != loads {
		t.Fatal("a failed reconnect ran the loader")
	}
	// Repeated failures keep the open pending.
	e.loadErr = errors.New("still down")
	if _, retry := m.Update(authorityReconnectedMsg{err: errors.New("dial failed again")}); retry != nil {
		t.Fatal("a repeated failed reconnect retried the open")
	}
	if !m.startupPending {
		t.Fatal("repeated failed reconnects dropped the retry")
	}
	// The first successful reconnect retries the open, off the update loop.
	e.loadErr = nil
	_, retry := m.Update(authorityReconnectedMsg{})
	if retry == nil {
		t.Fatal("a successful reconnect did not retry the startup open")
	}
	m.Update(retry())
	if e.opened == nil {
		t.Fatal("the retried startup open did not open the document")
	}
	if e.opened.Path != "/remote/a.txt" {
		t.Fatalf("opened %q, want the remote startup path", e.opened.Path)
	}
	if m.startupPending {
		t.Fatal("a successful open stayed pending")
	}
	if m.notice != "" {
		t.Fatalf("notice after a successful retry = %q", m.notice)
	}
	// A further reconnect does not re-open (the retry is done).
	opened := e.opened
	loads = e.loads
	if _, again := m.Update(authorityReconnectedMsg{}); again != nil {
		t.Fatal("a reconnect after a successful startup open retried it again")
	}
	if e.opened != opened || e.loads != loads {
		t.Fatal("a later reconnect re-ran the startup open")
	}
}
