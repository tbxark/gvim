package restore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/tbxark/gvim/atomicfile"
	"github.com/tbxark/gvim/internal/terminal"
)

// terminalFileName is the terminal dock state kept next to session.json. It is a
// separate file because the terminal's transcript is presentation state, bounded
// and rewritten on every save, not part of the editor's file list.
const terminalFileName = "terminal.json"

// The bounds LoadTerminal applies to a saved transcript, so a hand-edited or
// corrupt file cannot make a start allocate without end.
const (
	maxSavedTerminalRows  = 500
	maxSavedTerminalBytes = 1 << 20
	maxSavedTerminalTabs  = 16
	// maxSavedTerminalDirBytes bounds a persisted terminal working directory.
	maxSavedTerminalDirBytes = 4096
)

// SaveTerminal writes the terminal dock state to dir/terminal.json. A state with
// no dock and no transcript removes the file the previous save left, so a closed
// dock does not come back. The write is atomic, so a crash never leaves a partial
// state behind. An empty dir does nothing.
func SaveTerminal(dir string, st terminal.SavedState) error {
	if dir == "" {
		return nil
	}
	path := filepath.Join(dir, terminalFileName)
	if !st.Open && len(st.Rows) == 0 && len(st.Tabs) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("terminal: remove %s: %w", terminalFileName, err)
		}
		return nil
	}
	data, err := json.Marshal(st)
	if err != nil {
		return fmt.Errorf("terminal: encode: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("terminal: session dir: %w", err)
	}
	if _, err := atomicfile.WriteFileMode(path, data, 0o600); err != nil {
		return fmt.Errorf("terminal: write %s: %w", terminalFileName, err)
	}
	return nil
}

// LoadTerminal reads dir/terminal.json. A missing file returns false and no
// error. A state from another version is ignored, so an old session cannot break
// a start; so is one with no rows. The transcript is bounded to the newest rows
// and bytes, and control characters are stripped so a corrupt file cannot move
// the loader's cursor.
func LoadTerminal(dir string) (terminal.SavedState, bool, error) {
	if dir == "" {
		return terminal.SavedState{}, false, nil
	}
	data, err := os.ReadFile(filepath.Join(dir, terminalFileName))
	if errors.Is(err, fs.ErrNotExist) {
		return terminal.SavedState{}, false, nil
	}
	if err != nil {
		return terminal.SavedState{}, false, fmt.Errorf("terminal: read %s: %w", terminalFileName, err)
	}
	var st terminal.SavedState
	if err := json.Unmarshal(data, &st); err != nil {
		return terminal.SavedState{}, false, fmt.Errorf("terminal: decode %s: %w", terminalFileName, err)
	}
	if st.Version != terminal.SavedStateVersion || (len(st.Rows) == 0 && len(st.Tabs) == 0) {
		return terminal.SavedState{}, false, nil
	}
	st.Title = terminal.CleanTitle(st.Title)
	st.Cwd = boundTerminalDir(st.Cwd)
	st.Rows = boundTerminalRows(st.Rows)
	// The styled screen snapshot is bounded/cleaned the same way: a corrupt or
	// hand-edited snapshot is dropped rather than painted.
	st.Screen = st.Screen.Bounded()
	// Tabs are bounded the same way: at most maxSavedTerminalTabs, each with the
	// single-transcript row/byte bound. A tab with no rows and not open is dropped.
	if len(st.Tabs) > maxSavedTerminalTabs {
		st.Tabs = st.Tabs[len(st.Tabs)-maxSavedTerminalTabs:]
	}
	kept := st.Tabs[:0]
	for _, tab := range st.Tabs {
		tab.Title = terminal.CleanTitle(tab.Title)
		tab.Cwd = boundTerminalDir(tab.Cwd)
		tab.Rows = boundTerminalRows(tab.Rows)
		tab.Screen = tab.Screen.Bounded()
		if len(tab.Rows) == 0 && !tab.Open {
			continue
		}
		kept = append(kept, tab)
	}
	st.Tabs = kept
	return st, len(st.Rows) > 0 || len(st.Tabs) > 0, nil
}

// boundTerminalDir bounds a saved working directory and strips control characters.
// It is deliberately not POSIX-only: a local Windows path is a legitimate saved
// cwd (OSC 7 itself is POSIX-only and is cleaned in the terminal package).
func boundTerminalDir(dir string) string {
	dir = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.ToValidUTF8(dir, ""))
	dir = strings.TrimSpace(dir)
	if len(dir) <= maxSavedTerminalDirBytes {
		return dir
	}
	cut := dir[:maxSavedTerminalDirBytes]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut
}

// boundTerminalRows applies the saved-transcript bounds and strips control
// characters from each row.
func boundTerminalRows(rows []string) []string {
	if len(rows) > maxSavedTerminalRows {
		rows = rows[len(rows)-maxSavedTerminalRows:]
	}
	size := 0
	for i := len(rows) - 1; i >= 0; i-- {
		size += len(rows[i]) + 1
		if size > maxSavedTerminalBytes {
			rows = rows[i+1:]
			break
		}
	}
	for i := range rows {
		rows[i] = cleanTerminalText(rows[i])
	}
	return rows
}

// cleanTerminalText strips control characters from a saved transcript row.
func cleanTerminalText(text string) string {
	out := make([]rune, 0, len(text))
	for _, r := range text {
		if r < 0x20 || r == 0x7f {
			continue
		}
		out = append(out, r)
	}
	return string(out)
}
