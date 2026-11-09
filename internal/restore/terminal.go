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
	"sync"
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
	if reason := saveDisabledReason(dir); reason != "" {
		return fmt.Errorf("terminal: saving %s is disabled for this process: %s", terminalFileName, reason)
	}
	// A symlinked record is never followed for a write or replaced/removed: the link
	// (valid or dangling) is preserved as-is, so an attacker-controlled link cannot
	// redirect the record outside the session directory and the user's link is not
	// silently turned into a regular file. Restores quarantine an unusable symlink.
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("terminal: %s is a symlink; refusing to overwrite it (remove the link and use a regular record to enable saving)", terminalFileName)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("terminal: check %s: %w", terminalFileName, err)
	}
	// An unreadable record that could not be quarantined leaves a marker: never
	// write or remove the original in that case, so a later save cannot silently
	// destroy what the user may still want to recover.
	if _, err := os.Lstat(path + terminalProtectMarkerSuffix); err == nil {
		return fmt.Errorf("terminal: %s exists (an unreadable record could not be quarantined); refusing to overwrite it — inspect the record and remove the marker to continue", path+terminalProtectMarkerSuffix)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("terminal: check %s: %w", path+terminalProtectMarkerSuffix, err)
	}
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

// terminalProtectMarker is created when an unreadable terminal record cannot be
// quarantined (for example the directory is not writable). Its presence makes
// SaveTerminal refuse to write or remove the record, so a later normal exit can
// never silently destroy the unreadable original; the user is told to inspect it.
const terminalProtectMarkerSuffix = ".protected"

// maxQuarantineAttempts bounds the .rejected suffix search, so a directory full of
// colliding names cannot spin the loader; after it the record is protected instead.
var maxQuarantineAttempts = 100

// saveDisabledDirs remembers directories where an unreadable record could neither
// be quarantined nor get a protection marker (an unwritable directory). A process
// must then not write at all for that record: the marker may not exist, so the
// filesystem cannot be relied on, and the in-memory block keeps a later normal save
// from silently replacing the original in this process.
var (
	saveDisabledMu   sync.Mutex
	saveDisabledDirs = map[string]string{}
)

// disableSaveFor blocks SaveTerminal for dir for the rest of this process. The
// first reason wins.
func disableSaveFor(dir, reason string) {
	saveDisabledMu.Lock()
	defer saveDisabledMu.Unlock()
	if _, exists := saveDisabledDirs[dir]; !exists {
		saveDisabledDirs[dir] = reason
	}
}

// saveDisabledReason reports why SaveTerminal is blocked for dir ("" when it is not).
func saveDisabledReason(dir string) string {
	saveDisabledMu.Lock()
	defer saveDisabledMu.Unlock()
	return saveDisabledDirs[dir]
}

// resetSaveProtectionForTest clears the in-memory block for a test directory.
func resetSaveProtectionForTest(dir string) {
	saveDisabledMu.Lock()
	defer saveDisabledMu.Unlock()
	delete(saveDisabledDirs, dir)
}

// TerminalRecordProtected reports when a save must not touch a record: either the
// persistent .protected marker exists (checked with Lstat, so a dangling symlink at
// that name still counts) or this process disabled saves for the directory in memory
// (an unreadable record could neither be quarantined nor marked). Pure readers such
// as workspace extraction check it before migrating anything; the path is empty for
// the in-memory case.
func TerminalRecordProtected(dir string) (string, bool) {
	if dir == "" {
		return "", false
	}
	marker := filepath.Join(dir, terminalFileName) + terminalProtectMarkerSuffix
	if _, err := os.Lstat(marker); err == nil {
		return marker, true
	}
	if saveDisabledReason(dir) != "" {
		return "", true
	}
	return "", false
}

// TerminalRecordSymlink reports a symlinked terminal record whose target exists: the
// loader can read it, but SaveTerminal must refuse to replace the link. Workspace
// extraction calls this before migrating anything, because a mixed extraction would
// otherwise transfer the files first and only fail when the terminal accept tries to
// save — a predictably partial migration. A dangling symlink is reported by the
// loader itself (it is an unusable record, not this readable-link case).
func TerminalRecordSymlink(dir string) (string, bool) {
	if dir == "" {
		return "", false
	}
	path := filepath.Join(dir, terminalFileName)
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return "", false
	}
	if _, err := os.Stat(path); err != nil {
		return "", false
	}
	return path, true
}

// quarantinePath returns a free sibling name for the rejected record:
// terminal.json.rejected, then terminal.json.rejected.1, .2, ... It stops after
// maxQuarantineAttempts rather than scanning forever.
func quarantinePath(path string) (string, error) {
	for i := 0; i < maxQuarantineAttempts; i++ {
		candidate := path + ".rejected"
		if i > 0 {
			candidate = fmt.Sprintf("%s.rejected.%d", path, i)
		}
		if _, err := os.Lstat(candidate); errors.Is(err, fs.ErrNotExist) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no free quarantine name after %d attempts", maxQuarantineAttempts)
}

// protectUnquarantined handles a record that could not be renamed aside: it writes
// the persistent .protected marker when possible AND always disables saving for the
// directory in memory, so even when the marker write also fails (an unwritable
// directory) this process cannot overwrite the original on a later save.
func protectUnquarantined(dir, path string, cause error) error {
	marker := path + terminalProtectMarkerSuffix
	markerErr := os.WriteFile(marker, []byte("an unreadable terminal record could not be quarantined: "+cause.Error()+"\n"), 0o600)
	disableSaveFor(dir, cause.Error())
	if markerErr != nil {
		return fmt.Errorf("could not quarantine the unreadable terminal record (%v) or write the protection marker (%v); saving the terminal record is disabled for this process", cause, markerErr)
	}
	return fmt.Errorf("could not quarantine the unreadable terminal record: %v; wrote %s and disabled saving the terminal record for this process", cause, filepath.Base(marker))
}

// QuarantineTerminalRecord renames an unusable terminal record to a .rejected
// sibling so a later session save cannot silently overwrite it; the original bytes
// are preserved under the returned path. When the rename fails it writes a
// terminal.json.protected marker (when possible) so every SaveTerminal refuses to
// touch the record, and returns an error explaining the protection state.
func QuarantineTerminalRecord(dir string) (string, error) {
	if dir == "" {
		return "", nil
	}
	path := filepath.Join(dir, terminalFileName)
	// Lstat, not Stat: a dangling symlink is an existing record that must be
	// preserved/quarantined as the link it is, not treated as a missing file.
	if _, err := os.Lstat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		// A stat failure (permission denied, symlink loop, ...) means we cannot
		// prove the record absent or move it: protect it rather than risk a later
		// save silently replacing it once permissions recover.
		return "", protectUnquarantined(dir, path, err)
	}
	rejected, err := quarantinePath(path)
	if err != nil {
		return "", protectUnquarantined(dir, path, err)
	}
	if err := os.Rename(path, rejected); err != nil {
		return "", protectUnquarantined(dir, path, err)
	}
	return rejected, nil
}

// LoadTerminalForRestore is LoadTerminal for a session restore: a missing/empty
// record behaves like LoadTerminal, but an existing record the loader refuses
// (oversized, undecodable, or ignored by the loader as an unknown version/empty
// shape) is quarantined before returning, so the normal exit save writes a fresh
// record instead of silently replacing the unusable one. The returned error is the
// explicit warning (including the quarantine path) for the caller to show/log.
func LoadTerminalForRestore(dir string) (terminal.SavedState, bool, error) {
	st, ok, err := LoadTerminal(dir)
	if err == nil && ok {
		return st, true, nil
	}
	path := filepath.Join(dir, terminalFileName)
	if _, statErr := os.Lstat(path); statErr != nil {
		return st, false, err
	}
	rejected, qerr := QuarantineTerminalRecord(dir)
	if qerr != nil {
		if err == nil {
			return terminal.SavedState{}, false, fmt.Errorf("terminal: %s was refused by the loader and could not be moved aside: %w", terminalFileName, qerr)
		}
		return terminal.SavedState{}, false, fmt.Errorf("terminal: %w; additionally it could not be moved aside: %v", err, qerr)
	}
	if rejected == "" {
		return st, false, err
	}
	if err == nil {
		return terminal.SavedState{}, false, fmt.Errorf("terminal: %s was refused by the loader; the original was kept as %s", terminalFileName, rejected)
	}
	return terminal.SavedState{}, false, fmt.Errorf("terminal: %w (the original was kept as %s)", err, rejected)
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
	path := filepath.Join(dir, terminalFileName)
	// os.Open follows a symlink, so a dangling one would look like a missing file and
	// a later save could replace it. Inspect the link itself first: a dangling or
	// unfollowable symlink is an unusable record (an explicit error), never "no file".
	if info, lerr := os.Lstat(path); lerr == nil && info.Mode()&os.ModeSymlink != 0 {
		if _, serr := os.Stat(path); serr != nil {
			if errors.Is(serr, fs.ErrNotExist) {
				return terminal.SavedState{}, false, fmt.Errorf("terminal: %s is a dangling symlink; refusing to treat it as a missing record", terminalFileName)
			}
			return terminal.SavedState{}, false, fmt.Errorf("terminal: %s is a symlink that cannot be followed: %w", terminalFileName, serr)
		}
	} else if lerr != nil && !errors.Is(lerr, fs.ErrNotExist) {
		return terminal.SavedState{}, false, fmt.Errorf("terminal: inspect %s: %w", terminalFileName, lerr)
	}
	data, err := readTerminalFile(path)
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
