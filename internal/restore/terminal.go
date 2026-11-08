package restore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

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
	if !st.Open && len(st.Rows) == 0 {
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
	if st.Version != terminal.SavedStateVersion || len(st.Rows) == 0 {
		return terminal.SavedState{}, false, nil
	}
	if len(st.Rows) > maxSavedTerminalRows {
		st.Rows = st.Rows[len(st.Rows)-maxSavedTerminalRows:]
	}
	size := 0
	for i := len(st.Rows) - 1; i >= 0; i-- {
		size += len(st.Rows[i]) + 1
		if size > maxSavedTerminalBytes {
			st.Rows = st.Rows[i+1:]
			break
		}
	}
	for i := range st.Rows {
		st.Rows[i] = cleanTerminalText(st.Rows[i])
	}
	return st, len(st.Rows) > 0, nil
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
