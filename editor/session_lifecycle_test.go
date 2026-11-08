package editor

import (
	"errors"
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/search"
)

// restoringDoc returns an editor with one large document whose stashed edit ("edited " at
// line 100) waits for the line index, which has not got there yet.
func restoringDoc(t *testing.T) (*Editor, *Document) {
	t.Helper()
	opts := largeOptions()
	e, d := openLarge(t, opts, lineText(150))
	indexAll(t, e)
	typeAt(t, e, 100, 0, "edited ")
	st := stashOf(t, d)
	e2, d2 := sameFileOpened(t, opts, d.Path)
	if err := e2.RestoreOverlay(d2, st); err != nil {
		t.Fatalf("RestoreOverlay: %v", err)
	}
	if !d2.restoring() {
		t.Fatal("the restore is not waiting for the index")
	}
	return e2, d2
}

// failedRestoreDoc returns an editor whose large document had a stash that could not be
// put back once the index was complete.
func failedRestoreDoc(t *testing.T) (*Editor, *Document) {
	t.Helper()
	opts := largeOptions()
	e, d := openLarge(t, opts, lineText(150))
	indexAll(t, e)
	typeAt(t, e, 5, 0, "x")
	st := stashOf(t, d)
	st.Overlay.TailFirst = 5000
	fresh, nd := sameFileOpened(t, opts, d.Path)
	if err := fresh.RestoreOverlay(nd, st); err != nil {
		t.Fatalf("RestoreOverlay: %v", err)
	}
	indexAll(t, fresh)
	if nd.restoring() || nd.large.restore == nil {
		t.Fatal("the restore did not fail late")
	}
	return fresh, nd
}

func TestAStashThatWaitsOrFailedIsUnsavedForCloseAndQuit(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		make func(*testing.T) (*Editor, *Document)
	}{
		{"waiting", restoringDoc},
		{"failed", failedRestoreDoc},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e, d := tc.make(t)
			if !d.Modified() || !e.Tabs()[0].Modified {
				t.Errorf("Modified() = %v, tab %v: want an unsaved document", d.Modified(), e.Tabs()[0].Modified)
			}
			if need, err := e.CloseNeedsConfirm(0); err != nil || !need {
				t.Errorf("CloseNeedsConfirm = %v, %v", need, err)
			}
			if err := e.CloseTab(0, false); !errors.Is(err, ErrUnsaved) {
				t.Errorf("CloseTab = %v, want ErrUnsaved", err)
			}
			if err := e.CloseByPath(d.Path, false); !errors.Is(err, ErrUnsaved) {
				t.Errorf("CloseByPath = %v, want ErrUnsaved", err)
			}
			if got := e.Quit(false); len(got) != 1 {
				t.Errorf("Quit(false) = %v, want the one tab", got)
			}
			if len(e.docs) != 1 {
				t.Fatalf("%d tabs left, want the document kept", len(e.docs))
			}
			if err := e.CloseTab(0, true); err != nil {
				t.Errorf("forced CloseTab = %v", err)
			}
		})
	}
}

func TestNothingEditsADocumentWhileItsStashWaits(t *testing.T) {
	t.Parallel()
	e, d := restoringDoc(t)
	ver := d.Buffer.Version()
	_, err := e.ReplaceMatch(search.Query{Text: "line 0000"}, "REPLACED",
		search.Match{Start: buffer.Pos{}, End: buffer.Pos{Col: 9}})
	if !errors.Is(err, ErrRestoring) {
		t.Errorf("ReplaceMatch = %v, want ErrRestoring", err)
	}
	if _, err := e.ReplaceMatches(search.Query{Text: "line"}, "x"); !errors.Is(err, ErrRestoring) {
		t.Errorf("ReplaceMatches = %v, want ErrRestoring", err)
	}
	e.SelectRange(buffer.Pos{}, buffer.Pos{Col: 4})
	if _, err := e.BeginShell("shell_command_replace", map[string]any{"command": "tr a-z A-Z"}); !errors.Is(err, ErrRestoring) {
		t.Errorf("BeginShell replace = %v, want ErrRestoring", err)
	}
	job := ShellJob{Replace: true, doc: d, to: buffer.Pos{Col: 4}, snapshot: d.contentMark()}
	if res := e.EndShell(job, "XXXX", nil); !errors.Is(res.Err, ErrRestoring) {
		t.Errorf("EndShell replace = %v, want ErrRestoring", res.Err)
	}
	err = e.ApplyLSPEdits(d.Path, []LSPEdit{{From: buffer.Pos{}, To: buffer.Pos{Col: 4}, Text: "LINE"}})
	if !errors.Is(err, ErrRestoring) {
		t.Errorf("ApplyLSPEdits = %v, want ErrRestoring", err)
	}
	if d.Buffer.Version() != ver || d.Buffer.Modified() {
		t.Fatal("an edit got into the document while its stash waited")
	}
	indexAll(t, e)
	if d.large.restoreErr != nil || !strings.HasPrefix(d.Buffer.Line(100), "edited ") {
		t.Errorf("restoreErr = %v, line 100 = %q: want the stash back", d.large.restoreErr, d.Buffer.Line(100))
	}
}

func TestEditsAfterAFailedRestoreAreTheStashOfTheSession(t *testing.T) {
	t.Parallel()
	e, d := failedRestoreDoc(t)
	typeAt(t, e, 0, 0, "NEWEDIT ")
	got, ok := d.OverlayStash()
	if !ok || got.Overlay.TailFirst == 5000 {
		t.Fatalf("OverlayStash() = tail %d, %v: want the live edits", got.Overlay.TailFirst, ok)
	}
	if text := restoredText(t, largeOptions(), d, got); !strings.Contains(text, "NEWEDIT line 0000") {
		t.Errorf("the stash lacks the new edit: %q", text[:min(len(text), 40)])
	}
	kept, ok := d.FailedStash()
	if !ok || kept.Overlay.TailFirst != 5000 {
		t.Errorf("FailedStash() = %+v, %v: want the stash that failed", kept, ok)
	}
}

func TestOverlayStashOfAFailedRestoreWithoutEditsIsTheFailedStash(t *testing.T) {
	t.Parallel()
	_, d := failedRestoreDoc(t)
	got, ok := d.OverlayStash()
	if !ok || got.Overlay.TailFirst != 5000 {
		t.Errorf("OverlayStash() = %+v, %v: want the failed stash", got, ok)
	}
	if _, ok := d.FailedStash(); ok {
		t.Error("FailedStash() lists the stash twice")
	}
}

func TestReplaceOverlayTakesTheNewerStashOverRestoredEdits(t *testing.T) {
	t.Parallel()
	opts := largeOptions()
	e, d := openLarge(t, opts, lineText(150))
	indexAll(t, e)
	typeAt(t, e, 100, 0, "older ")
	older := stashOf(t, d)
	typeAt(t, e, 50, 0, "newer ")
	newer := stashOf(t, d)

	e2, d2 := sameFileOpened(t, opts, d.Path)
	if err := e2.RestoreOverlay(d2, older); err != nil {
		t.Fatalf("RestoreOverlay: %v", err)
	}
	if err := e2.RestoreOverlay(d2, newer); err == nil {
		t.Fatal("RestoreOverlay over restored edits succeeded")
	}
	stale := newer
	stale.Size++
	if err := e2.ReplaceOverlay(d2, stale); !errors.Is(err, ErrStashStale) {
		t.Fatalf("ReplaceOverlay of a stash for another file = %v, want ErrStashStale", err)
	}
	if !d2.restoring() {
		t.Fatal("a refused ReplaceOverlay dropped the restore that was waiting")
	}
	if err := e2.ReplaceOverlay(d2, newer); err != nil {
		t.Fatalf("ReplaceOverlay: %v", err)
	}
	indexAll(t, e2)
	if d2.restoring() || d2.large.restoreErr != nil {
		t.Fatalf("restoring %v, err %v", d2.restoring(), d2.large.restoreErr)
	}
	if d2.Buffer.Text() != d.Buffer.Text() {
		t.Errorf("the document does not hold the newer edits: line 50 = %q, line 100 = %q", d2.Buffer.Line(50), d2.Buffer.Line(100))
	}
}

func TestOverlayStashLaterIsTheStashAsItWasWhenTaken(t *testing.T) {
	t.Parallel()
	e, d := openLarge(t, largeOptions(), lineText(150))
	indexAll(t, e)
	typeAt(t, e, 100, 0, "first ")
	later, ok := d.OverlayStashLater()
	if !ok {
		t.Fatal("OverlayStashLater() found nothing to stash")
	}
	want := stashOf(t, d)
	typeAt(t, e, 20, 0, "second ")
	got := later()
	if got.Path != want.Path || got.Size != want.Size || got.Overlay.TailFirst != want.Overlay.TailFirst ||
		len(got.Overlay.Pieces) != len(want.Overlay.Pieces) {
		t.Fatalf("later() = %+v, want %+v", got, want)
	}
	if text := restoredText(t, largeOptions(), d, got); strings.Contains(text, "second ") || !strings.Contains(text, "first ") {
		t.Errorf("the stash holds edits made after it was taken, or lacks the ones before")
	}
	if _, ok := (&Document{Buffer: buffer.New("")}).OverlayStashLater(); ok {
		t.Error("a document that is not large has a stash")
	}
}
