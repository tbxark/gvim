package editor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tbxark/gvim/buffer"
)

// sourceLedger counts the sources that a test's editor opens and closes.
type sourceLedger struct {
	opened, closed atomic.Int32
}

// open opens path as the editor does and counts it.
func (l *sourceLedger) open(path string) (buffer.Source, error) {
	src, err := buffer.OpenFileSource(path)
	if err != nil {
		return nil, err
	}
	l.opened.Add(1)
	return &trackedSource{Source: src, ledger: l}, nil
}

// live is the number of sources that are open.
func (l *sourceLedger) live() int { return int(l.opened.Load() - l.closed.Load()) }

type trackedSource struct {
	buffer.Source
	ledger *sourceLedger
	done   atomic.Bool
}

func (s *trackedSource) Close() error {
	if s.done.CompareAndSwap(false, true) {
		s.ledger.closed.Add(1)
	}
	return s.Source.Close()
}

// ledgerOptions returns large file options whose sources are counted by the ledger.
func ledgerOptions() (Options, *sourceLedger) {
	l := &sourceLedger{}
	opts := largeOptions()
	opts.openSrc = &sourceOpener{open: l.open}
	return opts, l
}

// openLargeNamed opens a large file called name, with content, in e and returns the document.
func openLargeNamed(t *testing.T, e *Editor, name, content string) *Document {
	t.Helper()
	if err := e.OpenFile(writeLargeFile(t, name, content)); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	return e.Active()
}

func TestReleaseDocClosesSourceOnEverySite(t *testing.T) {
	t.Parallel()
	// Each case opens one large document next to a small one, makes the editor drop the
	// large one in a different way, and expects its source to be closed.
	cases := []struct {
		name string
		// scan is how far the scan of the document has got when it is dropped.
		scan string
		drop func(t *testing.T, e *Editor, d *Document)
	}{
		{"close_tab force", "none", func(t *testing.T, e *Editor, d *Document) {
			if err := e.CloseTab(e.indexOfDoc(d), true); err != nil {
				t.Fatal(err)
			}
		}},
		{"close_tab with a scan that is not running", "idle", func(t *testing.T, e *Editor, d *Document) {
			if err := e.CloseTab(e.indexOfDoc(d), true); err != nil {
				t.Fatal(err)
			}
		}},
		{"close_tab action", "idle", func(t *testing.T, e *Editor, d *Document) {
			e.switchTo(e.indexOfDoc(d))
			if err := e.Do("close_tab", map[string]any{"force": true}).Err; err != nil {
				t.Fatal(err)
			}
		}},
		{"CloseByPath", "idle", func(t *testing.T, e *Editor, d *Document) {
			if err := e.CloseByPath(d.Path, true); err != nil {
				t.Fatal(err)
			}
		}},
		{"CloseByPath on the folder", "idle", func(t *testing.T, e *Editor, d *Document) {
			if err := e.CloseByPath(filepath.Dir(d.Path), true); err != nil {
				t.Fatal(err)
			}
		}},
		{"preview replaced", "idle", func(t *testing.T, e *Editor, d *Document) {
			// A large document is never a preview, so the test makes one.
			d.preview = true
			small := writeLargeFile(t, "small.txt", "tiny\n")
			if err := e.OpenPreview(small); err != nil {
				t.Fatal(err)
			}
			if e.indexOfDoc(d) >= 0 {
				t.Fatal("the preview tab was not replaced")
			}
		}},
		{"Editor.Close", "idle", func(_ *testing.T, e *Editor, _ *Document) {
			e.Close()
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts, ledger := ledgerOptions()
			e := New(opts)
			small := writeLargeFile(t, "other.txt", "other\n")
			if err := e.OpenFile(small); err != nil {
				t.Fatal(err)
			}
			d := openLargeNamed(t, e, "big.txt", lineText(400))
			if tc.scan == "idle" {
				if ran, _ := indexStep(e); !ran {
					t.Fatal("no index step ran")
				}
			}
			if ledger.live() != 1 {
				t.Fatalf("live sources before the drop = %d, want 1", ledger.live())
			}
			tc.drop(t, e, d)
			if got := ledger.live(); got != 0 {
				t.Fatalf("live sources after the drop = %d, want 0", got)
			}
			if _, more := indexStep(e); more {
				t.Fatal("the index of a dropped document is still being built")
			}
		})
	}
}

func TestReleaseDocWithAJobRunningClosesWhenTheResultArrives(t *testing.T) {
	t.Parallel()
	opts, ledger := ledgerOptions()
	e := New(opts)
	d := openLargeNamed(t, e, "big.txt", lineText(400))
	e.docs = append(e.docs, newDocument("", "other"))
	job, ok := e.PlanIndex()
	if !ok {
		t.Fatal("no job")
	}
	res := job.Run(context.Background())
	if err := e.CloseTab(0, true); err != nil {
		t.Fatal(err)
	}
	if e.indexOfDoc(d) >= 0 {
		t.Fatal("the tab is still open")
	}
	// The job holds the scan, which holds the file: it is not closed under the job.
	if ledger.live() != 1 {
		t.Fatalf("live sources with a result pending = %d, want 1", ledger.live())
	}
	if e.ApplyIndex(res) {
		t.Fatal("ApplyIndex asked for more after the tab closed")
	}
	if ledger.live() != 0 {
		t.Fatalf("live sources after the result was dropped = %d, want 0", ledger.live())
	}
}

func TestReleaseDocDropsPendingWork(t *testing.T) {
	t.Parallel()
	e, d := openLarge(t, largeOptions(), lineText(400))
	e.docs = append(e.docs, newDocument("", "other"))
	if err := e.GotoLine(390, 1); err != nil {
		t.Fatal(err)
	}
	if d.large.pending == nil {
		t.Fatal("the goto did not wait")
	}
	e.releaseDoc(d)
	e.releaseDoc(d) // twice is safe
	e.releaseDoc(nil)
	if d.large.pending != nil || d.large.scan != nil {
		t.Fatalf("pending = %v, scan = %v after the release", d.large.pending, d.large.scan)
	}
	if e.docs[0] != d {
		t.Fatal("releaseDoc removed the document from the tabs")
	}
}

func TestReleaseDocOnDocumentInMemory(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	d := e.Active()
	e.releaseDoc(d)
	if d.Buffer.Text() != "" || d.Large() {
		t.Fatal("a release changed a document in memory")
	}
	e.Close()
	e.Close()
}

func TestEditorCloseKeepsTheTabsAndStopsTheFiles(t *testing.T) {
	t.Parallel()
	opts, ledger := ledgerOptions()
	e := New(opts)
	openLargeNamed(t, e, "a.txt", lineText(300))
	d := openLargeNamed(t, e, "b.txt", lineText(300))
	if ledger.live() != 2 {
		t.Fatalf("live = %d, want 2", ledger.live())
	}
	if got := d.Buffer.Line(1); got != "line 0001" {
		t.Fatalf("Line(1) = %q", got)
	}
	e.Close()
	if len(e.docs) != 2 || ledger.live() != 0 {
		t.Fatalf("tabs = %d, live = %d", len(e.docs), ledger.live())
	}
	if job, ok := e.PlanIndex(); ok {
		t.Fatalf("PlanIndex planned a job %v for a closed document", job)
	}
}

func TestOpenLoadedOfAnOpenPathReleasesTheDuplicate(t *testing.T) {
	t.Parallel()
	opts, ledger := ledgerOptions()
	e := New(opts)
	path := writeLargeFile(t, "big.txt", lineText(300))
	first, err := e.LoadFunc(path)()
	if err != nil {
		t.Fatal(err)
	}
	e.OpenLoaded(first)
	second, err := e.LoadFunc(path)()
	if err != nil {
		t.Fatal(err)
	}
	if ledger.live() != 2 {
		t.Fatalf("live = %d, want 2", ledger.live())
	}
	e.OpenLoaded(second)
	if len(e.docs) != 1 || e.docs[0] != first {
		t.Fatal("the open tab was not kept")
	}
	if ledger.live() != 1 {
		t.Fatalf("live = %d after the duplicate was dropped, want 1", ledger.live())
	}
}

func TestPreviewReplacementDoesNotLeakFD(t *testing.T) {
	t.Parallel()
	opts, ledger := ledgerOptions()
	e := New(opts)
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	for i := range 20 {
		suffix := string(rune('a' + i))
		// A large file is not previewed: nothing is opened for it.
		if err := e.OpenPreview(write("big-"+suffix+".txt", lineText(150))); err != nil {
			t.Fatal(err)
		}
		if err := e.OpenPreview(write("small-"+suffix+".txt", "tiny\n")); err != nil {
			t.Fatal(err)
		}
	}
	if ledger.opened.Load() != 0 {
		t.Fatalf("%d sources were opened by previews", ledger.opened.Load())
	}
	// A preview that is a large document, which a test can only force, is closed when
	// the next preview replaces it.
	e = New(opts)
	for i := range 20 {
		d := openLargeNamed(t, e, "forced-"+string(rune('a'+i))+".txt", lineText(150))
		for _, x := range e.docs {
			x.preview = false
		}
		d.preview = true
		if err := e.OpenPreview(write("next-"+string(rune('a'+i))+".txt", "tiny\n")); err != nil {
			t.Fatal(err)
		}
		if ledger.live() != 0 {
			t.Fatalf("round %d: %d sources are still open", i, ledger.live())
		}
	}
	if st := e.Active().Buffer.Stats(); st.CacheBytes != 0 {
		t.Fatalf("CacheBytes = %d for a document in memory", st.CacheBytes)
	}
}

func TestPreviewOfLargePathDoesNotOpen(t *testing.T) {
	t.Parallel()
	opts, ledger := ledgerOptions()
	e := New(opts)
	big := writeLargeFile(t, "big.txt", lineText(150))
	if err := e.OpenPreview(big); err != nil {
		t.Fatal(err)
	}
	if ledger.opened.Load() != 0 || e.Status() != "Large file: open it to load" {
		t.Fatalf("opened = %d, status = %q", ledger.opened.Load(), e.Status())
	}
}

func TestReloadWithEncodingRefusesLarge(t *testing.T) {
	t.Parallel()
	t.Run("large document", func(t *testing.T) {
		t.Parallel()
		opts, ledger := ledgerOptions()
		e := New(opts)
		d := openLargeNamed(t, e, "big.txt", lineText(300))
		before := d.Buffer
		read := ledger.opened.Load()
		err := e.Do("reload_with_encoding", map[string]any{"name": "iso-8859-1"}).Err
		if !errors.Is(err, ErrLargeFile) {
			t.Fatalf("err = %v, want ErrLargeFile", err)
		}
		if d.Buffer != before || !d.Large() || d.Buffer.Line(1) != "line 0001" || ledger.opened.Load() != read {
			t.Fatal("a refused reload changed the document")
		}
	})
	t.Run("file grown past the threshold", func(t *testing.T) {
		t.Parallel()
		e := New(largeOptions())
		path := writeLargeFile(t, "grows.txt", "small\n")
		if err := e.OpenFile(path); err != nil {
			t.Fatal(err)
		}
		d := e.Active()
		if d.Large() {
			t.Fatal("the small file opened as a large one")
		}
		if err := os.WriteFile(path, []byte(lineText(300)), 0o600); err != nil {
			t.Fatal(err)
		}
		err := e.Do("reload_with_encoding", map[string]any{"name": "utf-8"}).Err
		if !errors.Is(err, ErrLargeFile) {
			t.Fatalf("err = %v, want ErrLargeFile", err)
		}
		if d.Buffer.Text() != "small\n" {
			t.Fatalf("text = %q", d.Buffer.Text())
		}
		if _, err := e.PlanRevert(false); !errors.Is(err, ErrLargeFile) {
			t.Fatalf("PlanRevert err = %v, want ErrLargeFile", err)
		}
	})
	t.Run("small file still reloads", func(t *testing.T) {
		t.Parallel()
		e := New(largeOptions())
		path := writeLargeFile(t, "small.txt", "small\n")
		if err := e.OpenFile(path); err != nil {
			t.Fatal(err)
		}
		if err := e.Do("reload_with_encoding", map[string]any{"name": "iso-8859-1"}).Err; err != nil {
			t.Fatalf("reload: %v", err)
		}
	})
}

func TestApplyRevertRefusesALargeDocument(t *testing.T) {
	t.Parallel()
	e, d := openLarge(t, largeOptions(), lineText(300))
	indexAll(t, e)
	version := d.Buffer.Version()
	plan := RevertPlan{Path: d.Path}
	res := RunRevert(plan)
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if err := e.ApplyRevert(plan, res); !errors.Is(err, ErrLargeFile) {
		t.Fatalf("ApplyRevert err = %v, want ErrLargeFile", err)
	}
	if d.Buffer.Version() != version || d.Buffer.Modified() || !d.Large() {
		t.Fatal("a refused revert changed the document")
	}
	if st := d.Buffer.Stats(); st.OverCap != 0 {
		t.Fatalf("OverCap = %d", st.OverCap)
	}
}

func TestFollowDiskLargeNoReadFile(t *testing.T) {
	t.Parallel()
	e, d := openLarge(t, largeOptions(), lineText(300))
	old := filepath.Join(filepath.Dir(d.Path), "renamed.txt")
	if err := os.Rename(d.Path, old); err != nil {
		t.Fatal(err)
	}
	recorded := d.disk
	if err := e.RenamePath(d.Path, old); err != nil {
		t.Fatal(err)
	}
	if d.Path != old || d.Name() != "renamed.txt" {
		t.Fatalf("path = %q", d.Path)
	}
	// The record is the one taken at open, and a move that keeps size and time still
	// agrees with it, so the next save does not take the move for an outside change.
	if !d.disk.sameAs(recorded) || !diskStateOf(old).sameAs(d.disk) {
		t.Fatalf("disk record = %+v, file = %+v", d.disk, diskStateOf(old))
	}
	// A file that changed is not adopted: the old record stays, and a save refuses it.
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(old, later, later); err != nil {
		t.Fatal(err)
	}
	d.followDisk(old)
	if !d.disk.sameAs(recorded) {
		t.Fatalf("disk record = %+v after an outside change", d.disk)
	}
	if st := d.Buffer.Stats(); st.OverCap != 0 || st.MaterializedBytes != 0 {
		t.Fatalf("stats = %+v: the move read the text", st)
	}
	if !strings.HasPrefix(d.Buffer.Line(2), "line") {
		t.Fatal("the document is not readable after the move")
	}
}
