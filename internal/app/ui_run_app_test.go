package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestBuildModelRendersAFrame(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, files, err := buildModel(t.Context(), Options{Files: []FileTarget{{Path: path}}, Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	v := m.View()
	rows := strings.Split(v.Content, "\n")
	if len(rows) != 24 {
		t.Fatalf("frame has %d rows, want 24", len(rows))
	}
	if !strings.Contains(ansi.Strip(v.Content), "Ln 1, Col 1") {
		t.Error("status row has no cursor position")
	}
	// Ctrl+S returns the write as a command, so the file is written when it runs.
	_, cmd := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("save returned no command for the write")
	}
	for _, msg := range drainCmd(cmd) {
		m.Update(msg)
	}
	if got, _ := os.ReadFile(path); string(got) != "hello\n" {
		t.Errorf("file after save of a clean document = %q, want it unchanged", got)
	}
}

func TestRunStartsNoProgramOnBuildFailure(t *testing.T) {
	isolateConfig(t)
	if err := Run(t.Context(), Options{Keymap: "nope"}); err == nil {
		t.Fatal("Run with an unknown keymap returned no error")
	}
}

// drainCmd runs the command and returns every message it yields, following the commands
// of the messages.
func drainCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if msg == nil {
		return nil
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, drainCmd(c)...)
		}
		return out
	}
	return append([]tea.Msg{msg}, drainCmd(nil)...)
}
