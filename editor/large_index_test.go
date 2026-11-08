package editor

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/tbxark/gvim/buffer"
)

func TestIndexLoopBuildsTheWholeIndex(t *testing.T) {
	t.Parallel()
	content := lineText(400)
	e, d := openLarge(t, largeOptions(), content)
	if d.Index().Complete {
		t.Fatal("complete before the first job")
	}
	prev := d.Buffer.Lines()
	steps := 0
	for {
		ran, more := indexStep(e)
		if !ran {
			t.Fatal("no job planned although the index is incomplete")
		}
		steps++
		lines := d.Buffer.Lines()
		if lines < prev {
			t.Fatalf("Lines() fell from %d to %d", prev, lines)
		}
		prev = lines
		for _, i := range []int{0, lines / 2, lines - 1} {
			if got, want := d.Buffer.Line(i), fmt.Sprintf("line %04d", i); i < 400 && got != want {
				t.Fatalf("after %d jobs Line(%d) = %q, want %q", steps, i, got, want)
			}
		}
		if !more {
			break
		}
	}
	if steps < 10 {
		t.Fatalf("%d jobs: the batch size did not bound a job", steps)
	}
	info := d.Index()
	if !info.Complete || info.Lines != 401 || info.Indexed != int64(len(content)) || info.Size != int64(len(content)) {
		t.Fatalf("index = %+v", info)
	}
	if e.Status() != "Line index built: 401 lines" {
		t.Fatalf("status = %q", e.Status())
	}
	if _, ok := e.PlanIndex(); ok {
		t.Fatal("PlanIndex returned a job for a complete index")
	}
	if got := d.Buffer.Text(); got != content {
		t.Fatalf("Text() differs from the file: %d bytes, want %d", len(got), len(content))
	}
	if d.LargeState().Scan != nil || d.LargeState().InFlight {
		t.Fatal("the scan is not released")
	}
}

func TestPlanIndexChoosesDocuments(t *testing.T) {
	t.Parallel()
	t.Run("only large documents with an incomplete index", func(t *testing.T) {
		t.Parallel()
		e := docWith(largeOptions(), "small\n")
		if _, ok := e.PlanIndex(); ok {
			t.Fatal("a job for a document in memory")
		}
		if err := e.OpenFile(writeLargeFile(t, "tiny.txt", "tiny\n")); err != nil {
			t.Fatal(err)
		}
		if _, ok := e.PlanIndex(); ok {
			t.Fatal("a job for a small file")
		}
	})
	t.Run("one job at a time per document", func(t *testing.T) {
		t.Parallel()
		e, _ := openLarge(t, largeOptions(), lineText(400))
		job, ok := e.PlanIndex()
		if !ok {
			t.Fatal("no job")
		}
		if _, again := e.PlanIndex(); again {
			t.Fatal("a second job while the first runs")
		}
		e.ApplyIndex(job.Run(context.Background()))
		if _, ok := e.PlanIndex(); !ok {
			t.Fatal("no job after the first was applied")
		}
	})
	t.Run("a preview tab is not scanned", func(t *testing.T) {
		t.Parallel()
		e, d := openLarge(t, largeOptions(), lineText(400))
		d.SetPreview(true)
		if _, ok := e.PlanIndex(); ok {
			t.Fatal("a job for a preview tab")
		}
		d.SetPreview(false)
		if _, ok := e.PlanIndex(); !ok {
			t.Fatal("no job once the tab is permanent")
		}
	})
	t.Run("the active document goes first", func(t *testing.T) {
		t.Parallel()
		e, a := openLarge(t, largeOptions(), lineText(400))
		path := writeLargeFile(t, "second.txt", lineText(500))
		if err := e.OpenFile(path); err != nil {
			t.Fatal(err)
		}
		b := e.Active()
		if a == b {
			t.Fatal("no second tab")
		}
		job, _ := e.PlanIndex()
		if job.Doc != b {
			t.Fatal("the active tab is not scanned first")
		}
		e.ApplyIndex(job.Run(context.Background()))
		e.switchTo(0)
		job, ok := e.PlanIndex()
		if !ok || job.Doc != a {
			t.Fatalf("after switching, ok = %v, doc is a: %v", ok, job.Doc == a)
		}
		e.ApplyIndex(job.Run(context.Background()))
		// Both documents finish, whichever is active.
		for _, d := range []*Document{a, b} {
			for !d.Index().Complete {
				indexStep(e)
			}
			if d.Index().Lines < 401 {
				t.Fatalf("%s has %d lines", d.Name(), d.Index().Lines)
			}
		}
	})
}

func TestApplyIndexDropsStaleResults(t *testing.T) {
	t.Parallel()
	t.Run("tab closed", func(t *testing.T) {
		t.Parallel()
		e, d := openLarge(t, largeOptions(), lineText(400))
		e.docs = append(e.docs, newDocument("", "other"))
		job, _ := e.PlanIndex()
		res := job.Run(context.Background())
		if err := e.CloseTab(0, true); err != nil {
			t.Fatal(err)
		}
		before := d.Index().Indexed
		if e.ApplyIndex(res) {
			t.Fatal("ApplyIndex asked for more after the tab closed")
		}
		if d.Index().Indexed != before {
			t.Fatal("a result was applied to a closed document")
		}
	})
	t.Run("buffer replaced", func(t *testing.T) {
		t.Parallel()
		e, d := openLarge(t, largeOptions(), lineText(400))
		job, _ := e.PlanIndex()
		res := job.Run(context.Background())
		old := d.Buffer
		before := old.Index(0).Indexed
		d.Buffer = buffer.New("fresh")
		d.SetLargeState(nil)
		if e.ApplyIndex(res) {
			t.Fatal("ApplyIndex asked for more after the swap")
		}
		if old.Index(0).Indexed != before || d.Buffer.Text() != "fresh" {
			t.Fatal("a stale result changed a buffer")
		}
	})
	t.Run("scan replaced", func(t *testing.T) {
		t.Parallel()
		e, d := openLarge(t, largeOptions(), lineText(400))
		job, _ := e.PlanIndex()
		res := job.Run(context.Background())
		d.LargeState().Scan = nil
		d.LargeState().InFlight = false
		before := d.Index().Indexed
		if e.ApplyIndex(res) || d.Index().Indexed != before {
			t.Fatal("a result of an old scan was applied")
		}
	})
	t.Run("zero result", func(t *testing.T) {
		t.Parallel()
		e := New(Options{})
		if e.ApplyIndex(IndexResult{}) {
			t.Fatal("ApplyIndex asked for more after a zero result")
		}
	})
}

func TestIndexJobRunsBesideTheEditor(t *testing.T) {
	t.Parallel()
	e, d := openLarge(t, largeOptions(), lineText(400))
	for {
		job, ok := e.PlanIndex()
		if !ok {
			break
		}
		var res IndexResult
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			res = job.Run(context.Background())
		}()
		// The editor's goroutine keeps reading while the job reads.
		for i := range d.Buffer.Lines() {
			if got, want := d.Buffer.Line(i), fmt.Sprintf("line %04d", i); got != want {
				t.Errorf("Line(%d) = %q", i, got)
			}
		}
		wg.Wait()
		if !e.ApplyIndex(res) {
			break
		}
	}
	if !d.Index().Complete {
		t.Fatal("not complete")
	}
}

func TestIndexJobHonoursCancel(t *testing.T) {
	t.Parallel()
	e, d := openLarge(t, largeOptions(), lineText(400))
	job, _ := e.PlanIndex()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e.ApplyIndex(job.Run(ctx)) {
		t.Fatal("a cancelled job asks for more")
	}
	if d.LargeState().IndexErr != nil {
		t.Fatalf("a cancel is not an index failure: %v", d.LargeState().IndexErr)
	}
	if indexAll(t, e); !d.Index().Complete {
		t.Fatal("the scan does not resume after a cancel")
	}
}

var errBoom = errors.New("disk on fire")

func TestIndexFailureStopsTheChainAndRetries(t *testing.T) {
	t.Parallel()
	content := lineText(400)
	e, d, src := openMem(t, largeOptions(), content)
	src.FailNext(1, errBoom)
	ran, more := indexStep(e)
	if !ran || more {
		t.Fatalf("ran = %v, more = %v, want a job that ends the chain", ran, more)
	}
	if !errors.Is(d.LargeState().IndexErr, errBoom) {
		t.Fatalf("indexErr = %v", d.LargeState().IndexErr)
	}
	if !strings.HasPrefix(e.Status(), "Index failed: ") || !strings.Contains(e.Status(), errBoom.Error()) {
		t.Fatalf("status = %q", e.Status())
	}
	if _, ok := e.PlanIndex(); ok {
		t.Fatal("PlanIndex returned a job after a failure, before a retry was asked for")
	}
	run(t, e, "scan_line_index")
	if !strings.HasPrefix(e.Status(), "Retrying line index... Scanning...") || d.LargeState().IndexErr != nil {
		t.Fatalf("status = %q, indexErr = %v", e.Status(), d.LargeState().IndexErr)
	}
	indexAll(t, e)
	if got := d.Buffer.Text(); got != content {
		t.Fatalf("Text() after the retry differs: %d bytes, want %d", len(got), len(content))
	}
	if d.ReadOnly() {
		t.Fatal("a read error does not make the document read-only")
	}
}

func TestIndexFailureOfAChangedSourceFreezesTheDocument(t *testing.T) {
	t.Parallel()
	e, d, src := openMem(t, largeOptions(), lineText(400))
	src.Truncate(40)
	if ran, more := indexStep(e); !ran || more {
		t.Fatalf("ran = %v, more = %v", ran, more)
	}
	if !errors.Is(d.LargeState().IndexErr, buffer.ErrSourceChanged) || !d.ReadOnly() {
		t.Fatalf("indexErr = %v, read-only = %v", d.LargeState().IndexErr, d.ReadOnly())
	}
	run(t, e, "scan_line_index")
	if _, ok := e.PlanIndex(); ok {
		t.Fatal("a changed source is scanned again")
	}
}

func TestScanLineIndexReportsProgress(t *testing.T) {
	t.Parallel()
	e, _ := openLarge(t, largeOptions(), lineText(400))
	progress := regexp.MustCompile(`^Scanning\.\.\. (\d+)%$`)
	last := -1
	for {
		run(t, e, "scan_line_index")
		m := progress.FindStringSubmatch(e.Status())
		if m == nil {
			break
		}
		var pct int
		if _, err := fmt.Sscan(m[1], &pct); err != nil || pct < last || pct > 99 {
			t.Fatalf("status %q after %d%%", e.Status(), last)
		}
		last = pct
		if _, more := indexStep(e); !more {
			break
		}
	}
	run(t, e, "scan_line_index")
	if e.Status() != "Line index built: 401 lines" {
		t.Fatalf("status = %q", e.Status())
	}
	if last < 50 {
		t.Fatalf("progress ended at %d%%", last)
	}
}

func TestDocumentIndexOfADocumentInMemory(t *testing.T) {
	t.Parallel()
	d := docWith(Options{}, "a\nb\nc").Active()
	info := d.Index()
	if d.Large() || !info.Complete || info.Lines != 3 || info.ApproxLines != 3 {
		t.Fatalf("Large() = %v, index = %+v", d.Large(), info)
	}
}

func TestLargeIndexEstimateUsesTheEstimatedLineLength(t *testing.T) {
	t.Parallel()
	opts := largeOptions()
	opts.EstimatedLineLength = 10
	_, d := openLarge(t, opts, lineText(400))
	info := d.Index()
	if info.Complete || info.ApproxLines < 390 || info.ApproxLines > 410 {
		t.Fatalf("ApproxLines = %d, want about 400 (index %+v)", info.ApproxLines, info)
	}
}
