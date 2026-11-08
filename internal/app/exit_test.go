package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/internal/lsphost"
	"github.com/tbxark/gvim/internal/tui"
)

// stopRecorder is a language-server port that runs onStop before it stops the servers.
type stopRecorder struct {
	tui.LSPSession
	onStop func()
}

func (s stopRecorder) Stop() error {
	s.onStop()
	return s.LSPSession.Stop()
}

// TestExitSavesTheSessionBeforeStoppingTheServers checks the order of the exit. The
// session is saved first, because it is local and fast. Only then do the language
// servers stop, so a hung server delays the exit and never the save. The dock closes
// after the servers, as ui's Close checks.
func TestExitSavesTheSessionBeforeStoppingTheServers(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	must(t, os.WriteFile(file, []byte("a\n"), 0o644))
	sessDir := filepath.Join(dir, "session")
	sessionFile := filepath.Join(sessDir, "session.json")

	sawSession := false
	prevUI := newUI
	t.Cleanup(func() { newUI = prevUI })
	newUI = func(d tui.Deps) *tui.Model {
		servers := map[string]config.LSPServer{"go": {Command: "gopls"}}
		d.LSP = stopRecorder{LSPSession: lsphost.New(servers), onStop: func() {
			_, err := os.Stat(sessionFile)
			sawSession = err == nil
		}}
		return prevUI(d)
	}

	rt, err := buildRuntime(t.Context(), Options{SessionDir: sessDir}, false)
	must(t, err)
	defer rt.close()
	must(t, rt.ed.OpenFile(file))
	if _, err := os.Stat(sessionFile); err == nil {
		t.Fatal("the session file exists before the exit, so the check would prove nothing")
	}

	rt.exit()
	if !sawSession {
		t.Fatal("the language servers stopped before the session was saved")
	}
}
