package restore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/terminal"
)

// terminalExitMarkerForTest mirrors the dock's Fresh marker line.
const terminalExitMarkerForTest = "[Terminal process exited]"

// TestTerminalScreenSnapshotRoundTrip checks the additive styled snapshot in the
// terminal record: it round-trips per tab (and the top-level mirror), and an old
// record without it loads with no snapshot.
func TestTerminalScreenSnapshotRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	screen := &terminal.SavedScreen{Cols: 4, Rows: []terminal.SavedRow{{Cells: []terminal.SavedCell{
		{Rune: 'o', Fg: "#ff0000", Bold: true},
		{Rune: 'k', Bg: "#0000ff"},
	}}}}
	st := terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true, Rows: []string{"ok"},
		Screen: screen,
		Tabs: []terminal.SavedTab{
			{Name: "a", Open: true, Rows: []string{"ok"}, Screen: screen},
			{Name: "b", Open: true, Rows: []string{"b"}},
		},
		ActiveTab: 0,
	}
	if err := SaveTerminal(dir, st); err != nil {
		t.Fatal(err)
	}
	got, ok, err := LoadTerminal(dir)
	if err != nil || !ok {
		t.Fatalf("LoadTerminal = %v, %v", ok, err)
	}
	if got.Screen == nil || len(got.Screen.Rows) != 1 || got.Screen.Rows[0].Cells[0].Fg != "#ff0000" {
		t.Fatalf("top-level snapshot = %+v", got.Screen)
	}
	if got.Tabs[0].Screen == nil || got.Tabs[1].Screen != nil {
		t.Fatalf("tab snapshots = %+v / %+v", got.Tabs[0].Screen, got.Tabs[1].Screen)
	}

	// An old record has no screen field anywhere and loads fine (text only).
	legacy := terminal.SavedState{Version: terminal.SavedStateVersion, Open: true, Rows: []string{"old"}}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, terminalFileName)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	old, ok, err := LoadTerminal(dir)
	if err != nil || !ok || old.Screen != nil {
		t.Fatalf("legacy record = %+v, %v, %v", old, ok, err)
	}
}

// TestLoadTerminalBoundsScreenSnapshot checks that a corrupt/hand-edited snapshot
// cannot make the loader paint garbage: rows/cols are bounded, control characters
// are replaced, invalid colours dropped and over-wide rows clipped, per tab too.
func TestLoadTerminalBoundsScreenSnapshot(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	rows := make([]terminal.SavedRow, terminal.MaxSavedScreenRows+20)
	for i := range rows {
		rows[i] = terminal.SavedRow{Cells: []terminal.SavedCell{
			{Rune: '\n', Fg: "not-a-color"},
			{Rune: 'x', Bg: "#00ff00"},
		}}
	}
	overCols := make([]terminal.SavedCell, terminal.MaxSavedScreenCols+10)
	for i := range overCols {
		overCols[i] = terminal.SavedCell{Rune: 'y'}
	}
	rows[0] = terminal.SavedRow{Cells: overCols}
	bad := &terminal.SavedScreen{Cols: 3, Rows: rows}
	st := terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true, Rows: []string{"x"},
		Screen: bad,
		Tabs:   []terminal.SavedTab{{Name: "t", Open: true, Rows: []string{"x"}, Screen: bad}},
	}
	if err := SaveTerminal(dir, st); err != nil {
		if !strings.Contains(err.Error(), "") {
			t.Fatal(err)
		}
		return
	}
	got, ok, err := LoadTerminal(dir)
	if err != nil || !ok {
		t.Fatalf("LoadTerminal = %v, %v", ok, err)
	}
	for name, snap := range map[string]*terminal.SavedScreen{"top": got.Screen, "tab": got.Tabs[0].Screen} {
		if snap == nil {
			t.Fatalf("%s snapshot missing", name)
		}
		if len(snap.Rows) > terminal.MaxSavedScreenRows || snap.Cols > terminal.MaxSavedScreenCols {
			t.Fatalf("%s snapshot not bounded: cols %d rows %d", name, snap.Cols, len(snap.Rows))
		}
		for _, row := range snap.Rows {
			if len(row.Cells) > snap.Cols {
				t.Fatalf("%s row too wide: %d > %d", name, len(row.Cells), snap.Cols)
			}
			for _, cell := range row.Cells {
				if cell.Rune == '\n' || cell.Rune == '\t' {
					t.Fatalf("%s control character survived: %q", name, cell.Rune)
				}
				if cell.Fg == "not-a-color" {
					t.Fatalf("%s invalid colour survived", name)
				}
			}
		}
	}
}

// TestLoadTerminalRemainderBounds checks the cursor/alt/history remainder of the
// screen snapshot is bounded and sanitized on load: history rows capped, control
// characters stripped, invalid colours dropped, the cursor clamped and an unknown
// shape cleared (a corrupt record cannot paint garbage).
func TestLoadTerminalRemainderBounds(t *testing.T) {
	t.Parallel()
	history := make([]terminal.SavedRow, terminal.MaxSavedHistoryRows+30)
	for i := range history {
		history[i] = terminal.SavedRow{Cells: []terminal.SavedCell{
			{Rune: '\t', Fg: "bogus"},
			{Rune: 'h'},
		}}
	}
	screen := &terminal.SavedScreen{
		Cols:    4,
		History: history,
		Rows: []terminal.SavedRow{{Cells: []terminal.SavedCell{
			{Rune: '\n', Bg: "#00ff00"},
		}}},
		Cursor: terminal.SavedCursor{Row: 99, Col: -1, Visible: true, Shape: "star"},
		Alt:    true,
	}
	dir := t.TempDir()
	st := terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true, Rows: []string{"x"},
		Screen: screen,
		Tabs:   []terminal.SavedTab{{Name: "t", Open: true, Rows: []string{"x"}, Screen: screen}},
	}
	if err := SaveTerminal(dir, st); err != nil {
		t.Fatal(err)
	}
	got, ok, err := LoadTerminal(dir)
	if err != nil || !ok {
		t.Fatalf("LoadTerminal = %v, %v", ok, err)
	}
	for name, snap := range map[string]*terminal.SavedScreen{"top": got.Screen, "tab": got.Tabs[0].Screen} {
		if snap == nil {
			t.Fatalf("%s snapshot missing", name)
		}
		if !snap.Alt || len(snap.History) > terminal.MaxSavedHistoryRows {
			t.Fatalf("%s history/alt = %d/%v", name, len(snap.History), snap.Alt)
		}
		if snap.Cursor.Row < 0 || snap.Cursor.Row >= len(snap.Rows) || snap.Cursor.Col != 0 || snap.Cursor.Shape != "" {
			t.Fatalf("%s cursor = %+v", name, snap.Cursor)
		}
		for _, row := range append(append([]terminal.SavedRow{}, snap.Rows...), snap.History...) {
			for _, cell := range row.Cells {
				if cell.Rune == '\t' || cell.Rune == '\n' {
					t.Fatalf("%s control character survived: %q", name, cell.Rune)
				}
				if cell.Fg == "bogus" {
					t.Fatalf("%s invalid colour survived", name)
				}
			}
		}
	}
}

// TestLoadTerminalOldRecordNoRemainder checks an old record (no cursor/alt/history)
// loads with the zero remainder and does not invent state.
func TestLoadTerminalOldRecordNoRemainder(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st := terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true, Rows: []string{"old"},
		Screen: &terminal.SavedScreen{Cols: 3, Rows: []terminal.SavedRow{{Cells: []terminal.SavedCell{{Rune: 'o'}}}}},
	}
	if err := SaveTerminal(dir, st); err != nil {
		t.Fatal(err)
	}
	got, ok, err := LoadTerminal(dir)
	if err != nil || !ok || got.Screen == nil {
		t.Fatalf("LoadTerminal = %+v, %v, %v", got, ok, err)
	}
	if got.Screen.Cursor != (terminal.SavedCursor{}) || got.Screen.Alt || len(got.Screen.History) != 0 {
		t.Fatalf("old record gained remainder: %+v", got.Screen)
	}
}

// TestTerminalExitStatusRoundTrip checks the additive exit status survives the
// record per tab and at the top level, while an old record loads as unknown.
func TestTerminalExitStatusRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st := terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true, Rows: []string{"out", terminalExitMarkerForTest},
		ExitCode: 3, ExitKnown: true,
		Tabs: []terminal.SavedTab{
			{Name: "a", Open: true, Rows: []string{"out", terminalExitMarkerForTest}, ExitCode: 3, ExitKnown: true},
			{Name: "b", Open: true, Rows: []string{"out"}},
		},
	}
	if err := SaveTerminal(dir, st); err != nil {
		t.Fatal(err)
	}
	got, ok, err := LoadTerminal(dir)
	if err != nil || !ok {
		t.Fatalf("LoadTerminal = %v, %v", ok, err)
	}
	if !got.ExitKnown || got.ExitCode != 3 {
		t.Fatalf("top-level exit status = %d/%v", got.ExitCode, got.ExitKnown)
	}
	if !got.Tabs[0].ExitKnown || got.Tabs[0].ExitCode != 3 || got.Tabs[1].ExitKnown {
		t.Fatalf("tab exit statuses = %+v", got.Tabs)
	}
	// The old-record direction: no fields means "exited, status unknown".
	legacy := terminal.SavedState{Version: terminal.SavedStateVersion, Open: true, Rows: []string{"out"}}
	if err := SaveTerminal(dir, legacy); err != nil {
		t.Fatal(err)
	}
	old, ok, err := LoadTerminal(dir)
	if err != nil || !ok || old.ExitKnown || old.ExitCode != 0 {
		t.Fatalf("old record = %+v, %v, %v", old, ok, err)
	}
}

// TestTerminalTitleRoundTrip checks the additive OSC 0/2 title survives the record
// per tab and at the top level, is cleaned/bounded on load, and an old record has
// none.
func TestTerminalTitleRoundTrip(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", terminal.MaxTitleRunes+20)
	dir := t.TempDir()
	st := terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true, Rows: []string{"out"},
		Title: "editor title \033bad\n",
		Tabs: []terminal.SavedTab{
			{Name: "a", Open: true, Rows: []string{"out"}, Title: "tab title"},
			{Name: "b", Open: true, Rows: []string{"out"}, Title: long},
		},
	}
	if err := SaveTerminal(dir, st); err != nil {
		t.Fatal(err)
	}
	got, ok, err := LoadTerminal(dir)
	if err != nil || !ok {
		t.Fatalf("LoadTerminal = %v, %v", ok, err)
	}
	if got.Title != "editor title bad" {
		t.Fatalf("top-level title = %q", got.Title)
	}
	if got.Tabs[0].Title != "tab title" {
		t.Fatalf("tab title = %q", got.Tabs[0].Title)
	}
	if n := len([]rune(got.Tabs[1].Title)); n != terminal.MaxTitleRunes {
		t.Fatalf("long tab title = %d runes, want %d", n, terminal.MaxTitleRunes)
	}
	// Old record: no title, nothing invented.
	legacy := terminal.SavedState{Version: terminal.SavedStateVersion, Open: true, Rows: []string{"out"}}
	if err := SaveTerminal(dir, legacy); err != nil {
		t.Fatal(err)
	}
	old, ok, err := LoadTerminal(dir)
	if err != nil || !ok || old.Title != "" || len(old.Tabs) != 0 {
		t.Fatalf("old record = %+v, %v, %v", old, ok, err)
	}
}

// TestLoadTerminalAltScreenContent checks an alt frame with a (corrupt) history
// window is bounded on load: the frame content and alt flag stay, the history is
// dropped rather than making the whole record paint wrong rows.
func TestLoadTerminalAltScreenContent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	screen := terminal.SavedScreen{
		Cols:    4,
		Alt:     true,
		History: []terminal.SavedRow{{Cells: []terminal.SavedCell{{Rune: 'h'}}}},
		Rows: []terminal.SavedRow{{Cells: []terminal.SavedCell{
			{Rune: 'A', Bold: true}, {Rune: 'L'}, {Rune: 'T'},
		}}},
		Cursor: terminal.SavedCursor{Row: 0, Col: 0, Visible: true},
	}
	st := terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true, Rows: []string{"ALT"}, Title: "alt-app",
		Screen: &screen,
		Tabs:   []terminal.SavedTab{{Name: "a", Open: true, Rows: []string{"ALT"}, Screen: &screen, Title: "alt-app"}},
	}
	if err := SaveTerminal(dir, st); err != nil {
		t.Fatal(err)
	}
	got, ok, err := LoadTerminal(dir)
	if err != nil || !ok || got.Screen == nil || got.Tabs[0].Screen == nil {
		t.Fatalf("LoadTerminal = %+v, %v, %v", got, ok, err)
	}
	for name, snap := range map[string]*terminal.SavedScreen{"top": got.Screen, "tab": got.Tabs[0].Screen} {
		if !snap.Alt || len(snap.History) != 0 || len(snap.Rows) == 0 {
			t.Fatalf("%s alt frame = %+v", name, snap)
		}
		if snap.Rows[0].Cells[0].Rune != 'A' || !snap.Rows[0].Cells[0].Bold {
			t.Fatalf("%s alt content = %+v", name, snap.Rows)
		}
	}
	if got.Title != "alt-app" || got.Tabs[0].Title != "alt-app" {
		t.Fatalf("alt title = %q / %q", got.Title, got.Tabs[0].Title)
	}
}

// TestLoadTerminalCwdIsBounded checks a hand-edited cwd is cleaned and bounded on
// load, while an ordinary/old record keeps its directory exactly.
func TestLoadTerminalCwdIsBounded(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	long := "/" + strings.Repeat("a", maxSavedTerminalDirBytes+20)
	st := terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true, Rows: []string{"out"},
		Cwd: "/ws/ok\x00\x1b\n",
		Tabs: []terminal.SavedTab{
			{Name: "a", Open: true, Rows: []string{"out"}, Cwd: "/tab/ok\x07"},
			{Name: "b", Open: true, Rows: []string{"out"}, Cwd: long},
		},
	}
	if err := SaveTerminal(dir, st); err != nil {
		t.Fatal(err)
	}
	got, ok, err := LoadTerminal(dir)
	if err != nil || !ok {
		t.Fatalf("LoadTerminal = %v, %v", ok, err)
	}
	if got.Cwd != "/ws/ok" || got.Tabs[0].Cwd != "/tab/ok" {
		t.Fatalf("cleaned cwds = %q / %q", got.Cwd, got.Tabs[0].Cwd)
	}
	if n := len(got.Tabs[1].Cwd); n != maxSavedTerminalDirBytes {
		t.Fatalf("bounded tab cwd = %d, want %d", n, maxSavedTerminalDirBytes)
	}
}

// TestSearchHistoryPersistenceBoundsAndCompat checks the additive search-query
// history in the terminal record: it is cleaned and bounded on load, capped to the
// newest 100 queries of at most 512 bytes, and an old record without the field
// loads with an empty history (no migration, no process involvement).
func TestSearchHistoryPersistenceBoundsAndCompat(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	long := strings.Repeat("x", terminal.MaxSearchQueryBytes+40)
	st := terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true, Rows: []string{"out"},
		SearchHistory: []string{"", "  ok  ", "a\x01b\n", long},
	}
	if err := SaveTerminal(dir, st); err != nil {
		t.Fatal(err)
	}
	got, ok, err := LoadTerminal(dir)
	if err != nil || !ok {
		t.Fatalf("LoadTerminal = %v, %v", ok, err)
	}
	want := []string{"ok", "ab", long[:terminal.MaxSearchQueryBytes]}
	if len(got.SearchHistory) != len(want) {
		t.Fatalf("search history = %q, want %q", got.SearchHistory, want)
	}
	for i := range want {
		if got.SearchHistory[i] != want[i] {
			t.Fatalf("search history[%d] = %q, want %q", i, got.SearchHistory[i], want[i])
		}
	}
	// The cap keeps the newest entries in order.
	var many []string
	for i := 0; i < terminal.MaxSearchHistoryEntries+20; i++ {
		many = append(many, fmt.Sprintf("q%d", i))
	}
	st.SearchHistory = many
	if err := SaveTerminal(dir, st); err != nil {
		t.Fatal(err)
	}
	got, ok, err = LoadTerminal(dir)
	if err != nil || !ok {
		t.Fatalf("LoadTerminal(cap) = %v, %v", ok, err)
	}
	if len(got.SearchHistory) != terminal.MaxSearchHistoryEntries {
		t.Fatalf("history length = %d, want %d", len(got.SearchHistory), terminal.MaxSearchHistoryEntries)
	}
	if got.SearchHistory[0] != "q20" || got.SearchHistory[len(got.SearchHistory)-1] != fmt.Sprintf("q%d", terminal.MaxSearchHistoryEntries+19) {
		t.Fatalf("bounded history = %q..%q", got.SearchHistory[0], got.SearchHistory[len(got.SearchHistory)-1])
	}
	// Old record compatibility: no field means no history (and the record still
	// restores its transcript exactly as before).
	old := terminal.SavedState{Version: terminal.SavedStateVersion, Open: true, Rows: []string{"out"}}
	if err := SaveTerminal(dir, old); err != nil {
		t.Fatal(err)
	}
	legacy, ok, err := LoadTerminal(dir)
	if err != nil || !ok || len(legacy.SearchHistory) != 0 {
		t.Fatalf("legacy history = %v, %v, %v", legacy.SearchHistory, ok, err)
	}
}

// TestLoadTerminalRejectsOversizedFile checks the hard read cap: a terminal.json
// larger than the cap is refused (reported as an error) before/while reading, so a
// malicious record cannot make the loader allocate without bound; a normal record
// under the cap still loads. Not parallel: it lowers the cap for the test.
func TestLoadTerminalRejectsOversizedFile(t *testing.T) {
	dir := t.TempDir()
	old := maxTerminalFileBytes
	maxTerminalFileBytes = 1024
	t.Cleanup(func() { maxTerminalFileBytes = old })
	if err := os.WriteFile(filepath.Join(dir, terminalFileName), bytes.Repeat([]byte("x"), 2048), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := LoadTerminal(dir); err == nil || ok {
		t.Fatalf("oversized record: ok %v err %v, want a size error", ok, err)
	} else if !strings.Contains(err.Error(), "limit") {
		t.Fatalf("oversized record error = %v, want the size limit named", err)
	}
	// Under the lowered cap a legitimate bounded record still loads.
	st := terminal.SavedState{Version: terminal.SavedStateVersion, Open: true, Rows: []string{"out"}}
	if err := SaveTerminal(dir, st); err != nil {
		t.Fatal(err)
	}
	got, ok, err := LoadTerminal(dir)
	if err != nil || !ok || len(got.Rows) != 1 || got.Rows[0] != "out" {
		t.Fatalf("normal record = %+v, %v, %v", got, ok, err)
	}
}

// TestSaveTerminalBoundsTheGenericEntryPoint checks a direct SaveTerminal call
// cannot write an unbounded record: tabs, transcript rows, styled screens, argv,
// labels and the search history are all bounded before encoding, and the written
// file still loads with those bounds.
func TestSaveTerminalBoundsTheGenericEntryPoint(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	huge := strings.Repeat("x", 600)
	longArg := strings.Repeat("a", 5000)
	tabs := make([]terminal.SavedTab, 0, 40)
	for i := 0; i < 40; i++ {
		rows := make([]string, 0, 1000)
		for j := 0; j < 1000; j++ {
			rows = append(rows, huge)
		}
		args := make([]string, 0, 200)
		for j := 0; j < 200; j++ {
			args = append(args, longArg)
		}
		tabs = append(tabs, terminal.SavedTab{
			Name: strings.Repeat("n", 1000), Open: true, Cwd: "/tmp/x",
			Rows: rows, LaunchArgv: args, ResumeArgv: args,
			Agent: strings.Repeat("a", 1000), Source: strings.Repeat("s", 1000),
			Title: strings.Repeat("t", 1000),
		})
	}
	history := make([]string, 0, 300)
	for i := 0; i < 300; i++ {
		history = append(history, strings.Repeat("q", 2000))
	}
	st := terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true, Rows: []string{"out"}, Tabs: tabs,
		ActiveTab: 1, SearchHistory: history,
	}
	if err := SaveTerminal(dir, st); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(dir, terminalFileName)); err != nil {
		t.Fatal(err)
	} else if info.Size() > maxTerminalFileBytes {
		t.Fatalf("written file = %d bytes, over the %d-byte cap", info.Size(), maxTerminalFileBytes)
	}
	got, ok, err := LoadTerminal(dir)
	if err != nil || !ok {
		t.Fatalf("LoadTerminal = %v, %v", ok, err)
	}
	if len(got.Tabs) != maxSavedTerminalTabs {
		t.Fatalf("tabs = %d, want %d", len(got.Tabs), maxSavedTerminalTabs)
	}
	tab := got.Tabs[0]
	if len(tab.Rows) > maxSavedTerminalRows || len(tab.Name) > maxSavedLabelBytes ||
		len(tab.Agent) > maxSavedLabelBytes || len(tab.Source) > maxSavedLabelBytes ||
		len(tab.Title) > terminal.MaxTitleRunes*4 {
		t.Fatalf("tab bounded fields = rows %d name %d agent %d source %d title %d",
			len(tab.Rows), len(tab.Name), len(tab.Agent), len(tab.Source), len(tab.Title))
	}
	if len(tab.LaunchArgv) > maxSavedArgvEntries || len(tab.ResumeArgv) > maxSavedArgvEntries {
		t.Fatalf("argv entries = %d/%d", len(tab.LaunchArgv), len(tab.ResumeArgv))
	}
	for _, argv := range [][]string{tab.LaunchArgv, tab.ResumeArgv} {
		total := 0
		for _, arg := range argv {
			if len(arg) > maxSavedArgvArgBytes {
				t.Fatalf("argv argument = %d bytes, over %d", len(arg), maxSavedArgvArgBytes)
			}
			total += len(arg)
		}
		if total > maxSavedArgvTotalBytes {
			t.Fatalf("argv total = %d bytes, over %d", total, maxSavedArgvTotalBytes)
		}
	}
	if len(got.SearchHistory) != terminal.MaxSearchHistoryEntries {
		t.Fatalf("history = %d, want %d", len(got.SearchHistory), terminal.MaxSearchHistoryEntries)
	}
	for _, query := range got.SearchHistory {
		if len(query) > terminal.MaxSearchQueryBytes {
			t.Fatalf("query = %d bytes, over %d", len(query), terminal.MaxSearchQueryBytes)
		}
	}
}

// TestSaveTerminalRefusesToExceedTheReaderCap checks the last-resort writer guard:
// when the encoded bounded state would exceed the reader cap, SaveTerminal returns
// an error and leaves the previous record byte-identical (never a self-generated
// record the loader would refuse). Not parallel: it lowers the cap.
func TestSaveTerminalRefusesToExceedTheReaderCap(t *testing.T) {
	dir := t.TempDir()
	oldCap := maxTerminalFileBytes
	maxTerminalFileBytes = 2048
	t.Cleanup(func() { maxTerminalFileBytes = oldCap })
	original := terminal.SavedState{Version: terminal.SavedStateVersion, Open: true, Rows: []string{"kept"}}
	if err := SaveTerminal(dir, original); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, terminalFileName)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	big := terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true,
		Rows: []string{strings.Repeat("a", 2000), strings.Repeat("b", 1000)},
	}
	err = SaveTerminal(dir, big)
	if err == nil || !strings.Contains(err.Error(), "reader cap") {
		t.Fatalf("SaveTerminal error = %v, want the reader-cap refusal", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("a refused save changed the previous record")
	}
	got, ok, err := LoadTerminal(dir)
	if err != nil || !ok || len(got.Rows) != 1 || got.Rows[0] != "kept" {
		t.Fatalf("previous record after refusal = %+v, %v, %v", got, ok, err)
	}
}

// TestBoundedStateFitsTheReaderCap is the evidence for the 64 MiB cap: a state at
// the documented worst case (16 tabs, each transcript at its 1 MiB raw / 500-row
// bound with maximal JSON escaping, both argv lists at their per-list budget of
// control-heavy arguments, 256-byte labels/dirs, plus the 100x512 search history
// and the mirrored top-level launch record) encodes below the cap. Styled screens
// are separately hard-capped by terminal.MaxSavedScreenBytes (512 KiB each).
func TestBoundedStateFitsTheReaderCap(t *testing.T) {
	t.Parallel()
	// 500 rows x 2000 backslashes = 1,000,500 raw bytes per transcript; JSON
	// escapes each backslash, so this is the 2x worst case at the byte bound.
	row := strings.Repeat("\\", 1000)
	rows := make([]string, 0, 500)
	for i := 0; i < 500; i++ {
		rows = append(rows, row)
	}
	// Sixfold JSON escape worst case: 32 KiB of control bytes per argv list.
	arg := strings.Repeat("\x01", maxSavedArgvTotalBytes/2)
	argv := []string{arg}
	labels := strings.Repeat("n", 4096)
	tabs := make([]terminal.SavedTab, 0, 16)
	for i := 0; i < 16; i++ {
		tabs = append(tabs, terminal.SavedTab{
			Name: labels, Open: true, Cwd: "/" + strings.Repeat("c", 4096),
			Rows: rows, LaunchArgv: argv, ResumeArgv: argv,
			Agent: labels, Source: labels, LaunchDir: "/" + strings.Repeat("d", 4096),
			Title: strings.Repeat("T", 2000),
		})
	}
	history := make([]string, terminal.MaxSearchHistoryEntries)
	for i := range history {
		history[i] = strings.Repeat("q", terminal.MaxSearchQueryBytes)
	}
	st := boundTerminalState(terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true, Rows: rows[:1], Tabs: tabs,
		ActiveTab: 3, SearchHistory: history,
		LaunchArgv: argv, ResumeArgv: argv, Agent: labels, Source: labels,
	})
	data, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(data)) >= maxTerminalFileBytes {
		t.Fatalf("worst-case bounded state = %d bytes, over the %d-byte cap", len(data), maxTerminalFileBytes)
	}
}
