package restore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/tbxark/gvim/atomicfile"
	"github.com/tbxark/gvim/internal/terminal"
)

// maxTerminalFileBytes is a hard cap on the terminal record read: LoadTerminal
// checks the file size and also limits the read, so a malicious or corrupt
// terminal.json cannot make the loader allocate without bound. 64 MiB is above the
// largest record boundTerminalState can produce: at most 16 tabs (per transcript
// 500 rows / 1 MiB cleaned text, JSON at most 2x; a 512 KiB serialized screen; argv
// at most 64 entries / 4096 bytes each / 32 KiB text per list, JSON at most 6x;
// labels 256 bytes) plus the 100x512 search history and the mirrored top-level
// record; TestBoundedStateFitsTheReaderCap checks the worst case stays under it.
// SaveTerminal also measures the exact encoded size and refuses to write over the
// cap (keeping the previous record). It is a variable only so tests can exercise
// the cap without writing 64 MiB.
var maxTerminalFileBytes int64 = 64 << 20

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
	// Launch/resume argv and the display labels are bounded too, so SaveTerminal's
	// generic entry point cannot write an unbounded record: at most 64 entries per
	// argv, 4096 bytes per argument and 32 KiB of argument text per argv (encoded
	// JSON can expand control bytes sixfold, which the loader cap below covers).
	maxSavedArgvEntries    = 64
	maxSavedArgvArgBytes   = 4096
	maxSavedArgvTotalBytes = 32 << 10
	// maxSavedLabelBytes bounds a display label (tab name, agent name, source id).
	maxSavedLabelBytes = 256
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
	// The generic entry point bounds the whole state, not only the dock's own
	// callers: a direct SaveTerminal cannot write a record larger than LoadTerminal
	// (or the documented bounds) would accept.
	st = boundTerminalState(st)
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
	// Last-resort invariant: the writer must never emit a record the reader's hard
	// cap would refuse. A bounded state is far below this (see boundTerminalState
	// and the cap's derivation in LoadTerminal/readTerminalFile), but the encoded
	// size is checked exactly and the previous record is kept untouched on refusal.
	if int64(len(data)) > maxTerminalFileBytes {
		return fmt.Errorf("terminal: %s would be %d bytes, over the %d-byte reader cap; keeping the previous record",
			terminalFileName, len(data), maxTerminalFileBytes)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("terminal: session dir: %w", err)
	}
	if _, err := atomicfile.WriteFileMode(path, data, 0o600); err != nil {
		return fmt.Errorf("terminal: write %s: %w", terminalFileName, err)
	}
	return nil
}

// readTerminalFile reads the terminal record under the hard size cap. It checks
// the size before the read and limits the read too, so a file that grew between the
// stat and the read is still refused instead of allocated.
func readTerminalFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	if info, err := f.Stat(); err != nil {
		return nil, err
	} else if info.Size() > maxTerminalFileBytes {
		return nil, fmt.Errorf("file is %d bytes, limit %d", info.Size(), maxTerminalFileBytes)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxTerminalFileBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxTerminalFileBytes {
		return nil, fmt.Errorf("file exceeds the %d-byte limit", maxTerminalFileBytes)
	}
	return data, nil
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
	data, err := readTerminalFile(filepath.Join(dir, terminalFileName))
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
	st = boundTerminalState(st)
	return st, len(st.Rows) > 0 || len(st.Tabs) > 0, nil
}

// boundSavedText cleans one saved display label/string: invalid UTF-8 is replaced,
// control characters are dropped, outer space is trimmed and the result is cut on a
// rune boundary at max bytes.
func boundSavedText(text string, maxBytes int) string {
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.ToValidUTF8(text, ""))
	text = strings.TrimSpace(text)
	if len(text) <= maxBytes {
		return text
	}
	cut := text[:maxBytes]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return strings.TrimSpace(cut)
}

// boundSavedArgv bounds a launch/resume argv list: the first at most
// maxSavedArgvEntries entries, each valid UTF-8 and at most maxSavedArgvArgBytes,
// and at most maxSavedArgvTotalBytes of argument text in total (later entries are
// dropped once the budget is spent). Controls are kept (they can be legitimate in
// command arguments); JSON escapes them at most sixfold, which the loader cap
// accounts for.
func boundSavedArgv(argv []string) []string {
	if len(argv) == 0 {
		return nil
	}
	out := make([]string, 0, min(len(argv), maxSavedArgvEntries))
	total := 0
	for _, arg := range argv[:min(len(argv), maxSavedArgvEntries)] {
		arg = strings.ToValidUTF8(arg, "")
		if len(arg) > maxSavedArgvArgBytes {
			cut := arg[:maxSavedArgvArgBytes]
			for len(cut) > 0 && !utf8.ValidString(cut) {
				cut = cut[:len(cut)-1]
			}
			arg = cut
		}
		if total+len(arg) > maxSavedArgvTotalBytes {
			break
		}
		total += len(arg)
		out = append(out, arg)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// boundSavedLaunch bounds the launch-record strings of one state/tab.
func boundSavedLaunch(name, agent, source, launchDir, title string, launchArgv, resumeArgv []string) (string, string, string, string, string, []string, []string) {
	return boundSavedText(name, maxSavedLabelBytes),
		boundSavedText(agent, maxSavedLabelBytes),
		boundSavedText(source, maxSavedLabelBytes),
		boundTerminalDir(launchDir),
		terminal.CleanTitle(title),
		boundSavedArgv(launchArgv),
		boundSavedArgv(resumeArgv)
}

// boundTerminalState applies the record's bounds/cleaning to one state. LoadTerminal
// and SaveTerminal both go through it, so the write path can never emit a record the
// loader would have to trim (and a direct library caller is bounded too): titles are
// cleaned, the search history and directories are bounded, transcript rows are
// capped, the styled screen snapshot is bounded and at most the newest
// maxSavedTerminalTabs tabs survive with their per-tab transcript bound. A tab that
// ends up with neither rows nor the open flag is dropped; the active index is
// clamped. It is pure value processing: no IO and no process.
func boundTerminalState(st terminal.SavedState) terminal.SavedState {
	st.Title = terminal.CleanTitle(st.Title)
	st.SearchHistory = boundSearchHistory(st.SearchHistory)
	st.Cwd = boundTerminalDir(st.Cwd)
	// The mirrored top-level launch record is bounded like a tab's.
	_, st.Agent, st.Source, st.LaunchDir, _, st.LaunchArgv, st.ResumeArgv =
		boundSavedLaunch("", st.Agent, st.Source, st.LaunchDir, "", st.LaunchArgv, st.ResumeArgv)
	st.Rows = boundTerminalRows(st.Rows)
	st.Screen = st.Screen.Bounded()
	if len(st.Tabs) > maxSavedTerminalTabs {
		st.Tabs = st.Tabs[len(st.Tabs)-maxSavedTerminalTabs:]
	}
	kept := make([]terminal.SavedTab, 0, len(st.Tabs))
	for _, tab := range st.Tabs {
		tab.Name, tab.Agent, tab.Source, tab.LaunchDir, tab.Title, tab.LaunchArgv, tab.ResumeArgv =
			boundSavedLaunch(tab.Name, tab.Agent, tab.Source, tab.LaunchDir, tab.Title, tab.LaunchArgv, tab.ResumeArgv)
		tab.Cwd = boundTerminalDir(tab.Cwd)
		tab.Rows = boundTerminalRows(tab.Rows)
		tab.Screen = tab.Screen.Bounded()
		if len(tab.Rows) == 0 && !tab.Open {
			continue
		}
		kept = append(kept, tab)
	}
	st.Tabs = kept
	if len(st.Tabs) == 0 {
		st.ActiveTab = 0
	} else {
		st.ActiveTab = min(max(st.ActiveTab, 0), len(st.Tabs)-1)
	}
	return st
}

// boundSearchHistory keeps the newest terminal.MaxSearchHistoryEntries search queries and
// cleans each one: control characters/invalid UTF-8 are dropped, outer space is
// trimmed, empty queries are skipped and a query is cut on a rune boundary at
// terminal.MaxSearchQueryBytes. It only sanitizes data (no process is involved).
func boundSearchHistory(queries []string) []string {
	out := make([]string, 0, min(len(queries), terminal.MaxSearchHistoryEntries))
	start := max(len(queries)-terminal.MaxSearchHistoryEntries, 0)
	for _, query := range queries[start:] {
		query = strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, strings.ToValidUTF8(query, ""))
		query = strings.TrimSpace(query)
		if query == "" {
			continue
		}
		if len(query) > terminal.MaxSearchQueryBytes {
			cut := query[:terminal.MaxSearchQueryBytes]
			for len(cut) > 0 && !utf8.ValidString(cut) {
				cut = cut[:len(cut)-1]
			}
			query = strings.TrimSpace(cut)
		}
		out = append(out, query)
	}
	return out
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
	// Clean first, then bound, so the stored text is valid UTF-8 and the JSON
	// encoder's worst case is at most a doubling (quotes/backslashes); this is what
	// makes the record's maximum encoded size computable.
	cleaned := make([]string, len(rows))
	for i := range rows {
		cleaned[i] = cleanTerminalText(rows[i])
	}
	rows = cleaned
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
