package editor

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/buffer"
)

// tempFiles lists the temporary files of a save that are left in dir.
func tempFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, ent := range entries {
		if strings.Contains(ent.Name(), ".gvim-") {
			out = append(out, ent.Name())
		}
	}
	return out
}

// docText returns the text of a large document, line by line.
func docText(d *Document) string {
	lines := make([]string, d.Buffer.Lines())
	for i := range lines {
		lines[i] = d.Buffer.Line(i)
	}
	return strings.Join(lines, "\n")
}

// crlfText is lineText with CRLF line breaks.
func crlfText(n int) string { return strings.ReplaceAll(lineText(n), "\n", "\r\n") }

// editBoth applies the same random edits to the buffers, which must hold the same text.
// The positions are chosen from the first buffer.
func editBoth(rng *rand.Rand, steps int, bufs ...*buffer.Buffer) {
	texts := []string{"x", "é世", "ab\ncd", "\n", "\n\n", "tail", " "}
	for range steps {
		ref := bufs[0]
		pos := func() buffer.Pos {
			line := rng.IntN(ref.Lines())
			return buffer.Pos{Line: line, Col: rng.IntN(ref.LineLen(line) + 1)}
		}
		switch rng.IntN(5) {
		case 0, 1:
			p, s := pos(), texts[rng.IntN(len(texts))]
			for _, b := range bufs {
				b.Insert(p, s)
			}
		case 2, 3:
			from, to := pos(), pos()
			if to.Line < from.Line || (to.Line == from.Line && to.Col < from.Col) {
				from, to = to, from
			}
			for _, b := range bufs {
				b.Delete(from, to)
			}
		default:
			for _, b := range bufs {
				b.Break()
			}
		}
	}
}

func TestLargeSaveWritesTheEditedText(t *testing.T) {
	t.Parallel()
	bom := "\xef\xbb\xbf"
	tests := []struct {
		name    string
		content string
		eol     string
	}{
		{"lf", lineText(150), "\n"},
		{"crlf", crlfText(150), "\r\n"},
		{"no final newline", strings.TrimSuffix(lineText(150), "\n"), "\n"},
		{"crlf no final newline", strings.TrimSuffix(crlfText(150), "\r\n"), "\r\n"},
		{"multibyte", strings.Repeat("héllo wörld 世界\n", 100), "\n"},
		{"bom", bom + lineText(150), "\n"},
	}
	for _, tt := range tests {
		for seed := range uint64(12) {
			t.Run(fmt.Sprintf("%s/%d", tt.name, seed), func(t *testing.T) {
				t.Parallel()
				e, d := openIndexed(t, largeOptions(), tt.content)
				body := strings.ReplaceAll(strings.TrimPrefix(tt.content, bom), "\r\n", "\n")
				oracle := buffer.New(body)
				editBoth(rand.New(rand.NewPCG(seed, 7)), 30, oracle, d.Buffer)
				if d.Buffer.Text() != oracle.Text() {
					t.Fatalf("the buffers differ before the save")
				}
				if err := e.Save(); err != nil {
					t.Fatalf("Save: %v", err)
				}
				want := strings.ReplaceAll(oracle.Text(), "\n", tt.eol)
				if strings.HasPrefix(tt.content, bom) {
					want = bom + want
				}
				if got := readFile(t, d.Path); got != want {
					t.Fatalf("file = %q, want %q", got, want)
				}
				if d.Modified() {
					t.Error("the document is modified after the save")
				}
				if d.Buffer.Text() != oracle.Text() {
					t.Error("the text changed by the save")
				}
				if left := tempFiles(t, filepath.Dir(d.Path)); len(left) != 0 {
					t.Errorf("temporary files left: %v", left)
				}
			})
		}
	}
}

func TestLargeSaveKeepsTheLineEndingsOfLinesThatWereNotEdited(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	var lines []string
	for i := range 150 {
		lines = append(lines, fmt.Sprintf("line %04d", i))
	}
	// Lines alternate LF and CRLF, with a CRLF majority missing, so the document is LF.
	for i, l := range lines {
		sb.WriteString(l)
		if i%2 == 1 {
			sb.WriteString("\r")
		}
		sb.WriteString("\n")
	}
	e, d := openIndexed(t, largeOptions(), sb.String())
	d.Buffer.Insert(buffer.Pos{Line: 3}, "!")
	d.Buffer.Insert(buffer.Pos{Line: 4}, "?")
	if err := e.Save(); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(sb.String(), "line 0003\r\n", "!line 0003\r\n", 1)
	want = strings.Replace(want, "line 0004\n", "?line 0004\n", 1)
	if got := readFile(t, d.Path); got != want {
		t.Fatalf("file differs from the original in more than the two lines:\n got %q\nwant %q", got[:80], want[:80])
	}
}

func TestLargeSaveFailureLeavesTheFile(t *testing.T) {
	t.Run("rename fails", func(t *testing.T) {
		// Not parallel: it replaces renameFn.
		e, d := openIndexed(t, largeOptions(), lineText(150))
		before := readFile(t, d.Path)
		d.Buffer.Insert(buffer.Pos{}, "edit ")
		orig := renameFn
		renameFn = func(string, string) error { return errors.New("no rename") }
		t.Cleanup(func() { renameFn = orig })
		if err := e.Save(); err == nil || !strings.Contains(err.Error(), "no rename") {
			t.Fatalf("Save = %v", err)
		}
		if got := readFile(t, d.Path); got != before {
			t.Fatal("the file changed")
		}
		if left := tempFiles(t, filepath.Dir(d.Path)); len(left) != 0 {
			t.Fatalf("temporary files left: %v", left)
		}
		if !d.Modified() || !strings.HasPrefix(d.Buffer.Line(0), "edit ") {
			t.Fatal("the edit is gone")
		}
	})
	t.Run("a read fails", func(t *testing.T) {
		t.Parallel()
		e, d, src := openMem(t, largeOptions(), lineText(150))
		indexAll(t, e)
		before := readFile(t, d.Path)
		d.Buffer.Insert(buffer.Pos{Line: 100}, "edit ")
		src.FailNext(1<<20, errors.New("disk on fire"))
		err := e.Save()
		if err == nil {
			t.Fatal("Save worked")
		}
		if got := readFile(t, d.Path); got != before {
			t.Fatal("the file changed")
		}
		if left := tempFiles(t, filepath.Dir(d.Path)); len(left) != 0 {
			t.Fatalf("temporary files left: %v", left)
		}
		if !d.Modified() {
			t.Fatal("the document lost its changes")
		}
	})
	t.Run("a read-only directory", func(t *testing.T) {
		t.Parallel()
		if runtime.GOOS == "windows" || os.Geteuid() == 0 {
			t.Skip("the directory stays writable")
		}
		e, d := openIndexed(t, largeOptions(), lineText(150))
		dir := filepath.Dir(d.Path)
		before, err := os.Stat(d.Path)
		if err != nil {
			t.Fatal(err)
		}
		bytesBefore := readFile(t, d.Path)
		d.Buffer.Insert(buffer.Pos{}, "edit ")
		if err := os.Chmod(dir, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
		// The file itself is writable. A write in place would work; the save must not
		// open the file for writing.
		if err := e.Save(); err == nil {
			t.Fatal("Save worked in a directory that cannot be written")
		}
		after, err := os.Stat(d.Path)
		if err != nil {
			t.Fatal(err)
		}
		if got := readFile(t, d.Path); got != bytesBefore || !after.ModTime().Equal(before.ModTime()) {
			t.Fatal("the file was touched")
		}
		if left := tempFiles(t, dir); len(left) != 0 {
			t.Fatalf("temporary files left: %v", left)
		}
	})
}

func TestLargeSaveKeepsModeAndSymlink(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("modes and links differ")
	}
	t.Run("mode", func(t *testing.T) {
		t.Parallel()
		e, d := openIndexed(t, largeOptions(), lineText(150))
		if err := os.Chmod(d.Path, 0o640); err != nil {
			t.Fatal(err)
		}
		d.Buffer.Insert(buffer.Pos{}, "x")
		if err := e.Save(); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(d.Path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o640 {
			t.Fatalf("mode = %v", info.Mode().Perm())
		}
	})
	t.Run("symlink", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		target := filepath.Join(dir, "target.txt")
		link := filepath.Join(dir, "link.txt")
		if err := os.WriteFile(target, []byte(lineText(150)), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		e := New(largeOptions())
		if err := e.OpenFile(link); err != nil {
			t.Fatal(err)
		}
		indexAll(t, e)
		d := e.Active()
		if !d.Large() {
			t.Fatal("not large")
		}
		d.Buffer.Insert(buffer.Pos{}, "x")
		if err := e.Save(); err != nil {
			t.Fatal(err)
		}
		info, err := os.Lstat(link)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("the link is gone: %v %v", info, err)
		}
		if got := readFile(t, target); !strings.HasPrefix(got, "xline 0000") {
			t.Fatalf("target = %q", got[:20])
		}
	})
}

func TestLargeSaveRefusesAFileThatAnotherProgramReplaced(t *testing.T) {
	t.Parallel()
	e, d := openIndexed(t, largeOptions(), lineText(150))
	other := filepath.Join(filepath.Dir(d.Path), "other.txt")
	replacement := "replaced\n" + lineText(120)
	if err := os.WriteFile(other, []byte(replacement), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(other, d.Path); err != nil {
		t.Fatal(err)
	}
	d.Buffer.Insert(buffer.Pos{}, "mine ")
	// The document still reads the file it opened: the display does not change.
	if d.Buffer.Line(1) != "line 0001" || d.Buffer.Lines() != 151 {
		t.Fatalf("the display changed: %q, %d lines", d.Buffer.Line(1), d.Buffer.Lines())
	}
	err := e.Save()
	if !errors.Is(err, ErrChangedOnDisk) {
		t.Fatalf("Save = %v, want ErrChangedOnDisk", err)
	}
	if readFile(t, d.Path) != replacement {
		t.Fatal("the file was overwritten")
	}
	// Overwrite is the user's answer.
	p, err := e.PlanSaveForce()
	if err != nil {
		t.Fatal(err)
	}
	if err := e.ApplySavePlan(p, p.Run()); err != nil {
		t.Fatalf("forced save: %v", err)
	}
	if got := readFile(t, d.Path); !strings.HasPrefix(got, "mine line 0000\n") || strings.Contains(got, "replaced") {
		t.Fatalf("file = %q", got[:30])
	}
	if d.Modified() {
		t.Fatal("modified after the forced save")
	}
}

func TestLargeSaveAfterAnEditDuringTheSave(t *testing.T) {
	t.Parallel()
	e, d := openIndexed(t, largeOptions(), lineText(150))
	d.Buffer.Insert(buffer.Pos{}, "A")
	plan, err := e.PlanSave()
	if err != nil {
		t.Fatal(err)
	}
	results := plan.Run()
	// An edit made while the file was written.
	d.Buffer.Insert(buffer.Pos{Line: 1}, "B")
	if err := e.ApplySavePlan(plan, results); err != nil {
		t.Fatal(err)
	}
	if !d.Modified() {
		t.Fatal("the edit made during the save is not unsaved")
	}
	if got := readFile(t, d.Path); !strings.HasPrefix(got, "Aline 0000\nline 0001\n") {
		t.Fatalf("file = %q", got[:30])
	}
	if err := e.Save(); err != nil {
		t.Fatal(err)
	}
	want := "Aline 0000\nBline 0001\n" + lineText(150)[len("line 0000\nline 0001\n"):]
	if got := readFile(t, d.Path); got != want {
		t.Fatalf("second save wrote %q", got[:40])
	}
	if d.Modified() {
		t.Fatal("modified after the second save")
	}
}

func TestLargeSaveMovesOntoTheNewFileWithoutAScan(t *testing.T) {
	t.Parallel()
	e, d := openIndexed(t, largeOptions(), lineText(150))
	d.Buffer.Insert(buffer.Pos{Line: 10}, "first ")
	plan, err := e.PlanSave()
	if err != nil {
		t.Fatal(err)
	}
	results := plan.Run()
	before := d.Buffer.Stats()
	if err := e.ApplySavePlan(plan, results); err != nil {
		t.Fatal(err)
	}
	after := d.Buffer.Stats()
	if read := after.ReadBytes - before.ReadBytes; read > 3*16 {
		t.Errorf("the rebase read %d bytes: only three chunks of 16 bytes are checked", read)
	}
	if !d.Index().Complete {
		t.Fatal("the index is not complete after the save")
	}
	if _, ok := e.PlanIndex(); ok {
		t.Fatal("the index is scanned again")
	}
	if after.HistoryGroups != 0 || after.Pieces != 0 {
		t.Errorf("history groups %d, pieces %d: the overlay is not empty", after.HistoryGroups, after.Pieces)
	}
	if want := "Saved (undo history cleared)"; d.status != want {
		t.Errorf("status = %q, want %q", d.status, want)
	}
	// The undo history is gone.
	text := docText(d)
	run(t, e, "undo")
	if docText(d) != text {
		t.Error("undo changed the text after the save")
	}
	// The next save streams from the new file.
	if err := os.Remove(d.Path); err != nil {
		t.Fatal(err)
	}
	d.Buffer.Insert(buffer.Pos{Line: 20}, "second ")
	d.disk = diskState{}
	if err := e.Save(); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(lineText(150), "line 0010", "first line 0010", 1)
	want = strings.Replace(want, "line 0020", "second line 0020", 1)
	if got := readFile(t, d.Path); got != want {
		t.Fatalf("second save wrote different bytes")
	}
}

func TestLargeSaveOfAnUnmodifiedDocumentIsANoop(t *testing.T) {
	t.Parallel()
	opts := largeOptions()
	opts.EnsureFinalNewline = true
	e, d := openIndexed(t, opts, strings.TrimSuffix(lineText(150), "\n"))
	before, err := os.Stat(d.Path)
	if err != nil {
		t.Fatal(err)
	}
	reads := d.Buffer.Stats().ReadBytes
	version := d.Buffer.Version()
	if err := e.Save(); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(d.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Error("the file was written")
	}
	if got := d.Buffer.Stats().ReadBytes; got != reads {
		t.Errorf("the save read %d bytes", got-reads)
	}
	if d.Buffer.Version() != version {
		t.Error("the save edited the document")
	}
	if left := tempFiles(t, filepath.Dir(d.Path)); len(left) != 0 {
		t.Errorf("temporary files: %v", left)
	}
	if d.status != "Nothing to save" {
		t.Errorf("status = %q", d.status)
	}
}

func TestLargeSaveOfAnUnmodifiedDocumentWritesOverAChangedFileOnForce(t *testing.T) {
	t.Parallel()
	e, d := openIndexed(t, largeOptions(), lineText(150))
	// Another program replaces the file with a copy that is newer.
	other := filepath.Join(filepath.Dir(d.Path), "other.txt")
	if err := os.WriteFile(other, []byte(lineText(150)), 0o600); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(other, later, later); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(other, d.Path); err != nil {
		t.Fatal(err)
	}
	if err := e.Save(); !errors.Is(err, ErrChangedOnDisk) {
		t.Fatalf("Save = %v, want ErrChangedOnDisk", err)
	}
	p, err := e.PlanSaveForce()
	if err != nil {
		t.Fatal(err)
	}
	if err := e.ApplySavePlan(p, p.Run()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(d.Path)
	if err != nil {
		t.Fatal(err)
	}
	if info.ModTime().After(time.Now().Add(time.Minute)) || d.status != "Saved" {
		t.Fatalf("the file was not written, status %q", d.status)
	}
}

func TestPlanSaveIncompleteIndexDoesNotTidy(t *testing.T) {
	t.Parallel()
	opts := largeOptions()
	opts.EnsureFinalNewline = true
	e, d := openLarge(t, opts, strings.TrimSuffix(lineText(150), "\n"))
	if d.Index().Complete {
		t.Fatal("the index is already complete")
	}
	d.Buffer.Insert(buffer.Pos{}, "x")
	version := d.Buffer.Version()
	plan, err := e.PlanSave()
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.jobs) != 1 || !errors.Is(plan.jobs[0].err, ErrIndexing) {
		t.Fatalf("job error = %v, want ErrIndexing", plan.jobs[0].err)
	}
	if !strings.Contains(plan.jobs[0].err.Error(), "Scanning...") {
		t.Errorf("error = %q", plan.jobs[0].err)
	}
	if d.Buffer.Version() != version {
		t.Error("planning the save edited the document")
	}
	if plan.jobs[0].large != nil {
		t.Error("the job holds a snapshot")
	}
	// A save-as is the same.
	if _, err := e.PlanSaveAs(filepath.Join(t.TempDir(), "copy.txt"), false); err != nil {
		t.Fatal(err)
	}
}

func TestPlanSaveReportsAnIndexFailure(t *testing.T) {
	t.Parallel()
	e, d, src := openMem(t, largeOptions(), lineText(150))
	src.FailNext(1<<20, errors.New("disk on fire"))
	job, ok := e.PlanIndex()
	if !ok {
		t.Fatal("no index job")
	}
	e.ApplyIndex(job.Run(context.Background()))
	if d.large.indexErr == nil {
		t.Fatal("no index error")
	}
	_, err := e.PlanSave()
	if err != nil {
		t.Fatal(err)
	}
	plan, _ := e.PlanSave()
	got := plan.jobs[0].err
	if got == nil || errors.Is(got, ErrIndexing) || !strings.Contains(got.Error(), "disk on fire") {
		t.Fatalf("error = %v, want the index failure", got)
	}
}

func TestPlanSaveLargeRefusedTidyStillSaves(t *testing.T) {
	t.Parallel()
	opts := largeOptions()
	opts.EnsureFinalNewline = true
	// The last line is not valid UTF-8, past the head that decides the encoding.
	content := withHead("bad \xff line")
	e, d := openIndexed(t, opts, content)
	d.Buffer.Insert(buffer.Pos{}, "x")
	if err := e.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := readFile(t, d.Path); got != "x"+content {
		t.Fatal("the saved bytes differ from the text plus the edit")
	}
	if want := "Saved without final newline: last line not editable"; d.status != want {
		t.Fatalf("status = %q, want %q", d.status, want)
	}
}

func TestLargeSaveEnsuresTheFinalNewline(t *testing.T) {
	t.Parallel()
	opts := largeOptions()
	opts.EnsureFinalNewline = true
	opts.TrimTrailingWhitespace = true
	content := "trail   \n" + strings.TrimSuffix(lineText(150), "\n")
	e, d := openIndexed(t, opts, content)
	d.Buffer.Insert(buffer.Pos{Line: 1}, "x")
	if err := e.Save(); err != nil {
		t.Fatal(err)
	}
	want := "trail   \nx" + lineText(150)
	if got := readFile(t, d.Path); got != want {
		t.Fatalf("file = %q...", got[:40])
	}
}

func TestLargeSaveAfterTheFileWasTruncated(t *testing.T) {
	t.Parallel()
	e, d := openIndexed(t, largeOptions(), lineText(150))
	d.Buffer.Insert(buffer.Pos{}, "x")
	if err := os.Truncate(d.Path, 100); err != nil {
		t.Fatal(err)
	}
	for name, run := range map[string]func() error{
		"save":    e.Save,
		"save as": func() error { return e.SaveAs(filepath.Join(filepath.Dir(d.Path), "copy.txt")) },
	} {
		err := run()
		if !errors.Is(err, buffer.ErrSourceChanged) || err.Error() != "File changed on disk: reload or Save As after reloading" {
			t.Errorf("%s = %v", name, err)
		}
	}
	if !d.ReadOnly() {
		t.Error("the document is not read-only")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(d.Path), "copy.txt")); err == nil {
		t.Error("the Save As wrote a file")
	}
	if len(readFile(t, d.Path)) != 100 {
		t.Error("the file was written")
	}
}

func TestAppendedLogStaysEditableSaveRefused(t *testing.T) {
	t.Parallel()
	e, d := openIndexed(t, largeOptions(), lineText(150))
	f, err := os.OpenFile(d.Path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	appended := "appended line\nanother\n"
	if _, err := f.WriteString(appended); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	d.Buffer.Insert(buffer.Pos{}, "x")
	err = e.Save()
	if !errors.Is(err, buffer.ErrSourceGrew) || !strings.Contains(err.Error(), "Save As to another path") {
		t.Fatalf("Save = %v", err)
	}
	if d.ReadOnly() {
		t.Fatal("a file that grew is still editable")
	}
	if got := readFile(t, d.Path); got != lineText(150)+appended {
		t.Fatal("the file changed")
	}
	copyPath := filepath.Join(filepath.Dir(d.Path), "copy.txt")
	if err := e.SaveAs(copyPath); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	if got := readFile(t, copyPath); got != "x"+lineText(150) {
		t.Fatalf("copy = %q...", got[:30])
	}
	if want := fmt.Sprintf("Saved without %d appended bytes", len(appended)); d.status != want {
		t.Fatalf("status = %q, want %q", d.status, want)
	}
	if d.Path != copyPath || d.Modified() {
		t.Fatalf("path %q, modified %v", d.Path, d.Modified())
	}
}

func TestLargeSaveAsMovesOntoTheNewFile(t *testing.T) {
	t.Parallel()
	e, d := openIndexed(t, largeOptions(), lineText(150))
	d.Buffer.Insert(buffer.Pos{Line: 5}, "moved ")
	oldPath := d.Path
	newPath := filepath.Join(filepath.Dir(oldPath), "new.txt")
	if err := e.SaveAs(newPath); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(lineText(150), "line 0005", "moved line 0005", 1)
	if readFile(t, newPath) != want || readFile(t, oldPath) != lineText(150) {
		t.Fatal("the files are not as expected")
	}
	if d.Path != newPath || d.Modified() || !d.Large() {
		t.Fatalf("path %q modified %v large %v", d.Path, d.Modified(), d.Large())
	}
	// The document reads the new file: the old one can go.
	if err := os.Remove(oldPath); err != nil {
		t.Fatal(err)
	}
	if docText(d) != strings.TrimSuffix(want, "\n")+"\n" {
		t.Fatal("the document does not read the new file")
	}
	if left := tempFiles(t, filepath.Dir(newPath)); len(left) != 0 {
		t.Errorf("temporary files: %v", left)
	}
}

func TestSaveRebaseForeignFileEqualSize(t *testing.T) {
	// Not parallel: it replaces renameFn.
	e, d := openIndexed(t, largeOptions(), lineText(150))
	d.Buffer.Insert(buffer.Pos{Line: 5}, "mine ")
	want := strings.Replace(lineText(150), "line 0005", "mine line 0005", 1)
	orig := renameFn
	renameFn = func(from, to string) error {
		if err := os.Rename(from, to); err != nil {
			return err
		}
		// Another program replaces the file at once, with a file of the same size.
		foreign := filepath.Join(filepath.Dir(to), "foreign")
		if err := os.WriteFile(foreign, []byte(strings.Repeat("z", len(want)-1)+"\n"), 0o600); err != nil {
			return err
		}
		return os.Rename(foreign, to)
	}
	t.Cleanup(func() { renameFn = orig })
	if err := e.Save(); err != nil {
		t.Fatal(err)
	}
	renameFn = orig
	if d.Modified() {
		t.Error("the file was written, so the document is saved")
	}
	if !d.large.rebaseFailed || d.status != "Saved; reload to continue editing" {
		t.Errorf("rebaseFailed %v, status %q", d.large.rebaseFailed, d.status)
	}
	if d.Buffer.Line(5) != "mine line 0005" || d.Buffer.Lines() != 151 {
		t.Error("the display changed")
	}
	// The next save still goes through the disk check, which sees the foreign file as a
	// changed one. The document is still correct, and saves once the user overwrites.
	d.Buffer.Insert(buffer.Pos{}, "more ")
	if err := e.Save(); !errors.Is(err, ErrChangedOnDisk) {
		t.Fatalf("second save = %v, want ErrChangedOnDisk", err)
	}
	p, err := e.PlanSaveForce()
	if err != nil {
		t.Fatal(err)
	}
	if err := e.ApplySavePlan(p, p.Run()); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, d.Path); got != "more "+want {
		t.Fatalf("file = %q...", got[:30])
	}
}

func TestSaveRebaseSameFileIdentity(t *testing.T) {
	t.Parallel()
	e, d := openIndexed(t, largeOptions(), lineText(150))
	d.Buffer.Insert(buffer.Pos{Line: 5}, "mine ")
	if err := e.Save(); err != nil {
		t.Fatal(err)
	}
	if d.large.rebaseFailed {
		t.Fatal("the rebase failed for the file that was just written")
	}
	// The default source reports the file behind its descriptor, so the identity of the
	// file was checked. The buffer reads the new file now: replace it and see.
	info, err := os.Stat(d.Path)
	if err != nil {
		t.Fatal(err)
	}
	if d.large.size != info.Size() {
		t.Errorf("size = %d, file has %d", d.large.size, info.Size())
	}
}

func TestSaveWithTheEncodingChanged(t *testing.T) {
	t.Parallel()
	bom := "\xef\xbb\xbf"
	setEncoding := func(t *testing.T, e *Editor, name string) {
		t.Helper()
		if r := e.Do("set_encoding", map[string]any{"name": name}); r.Err != nil {
			t.Fatalf("set_encoding %s: %v", name, r.Err)
		}
	}
	t.Run("add a mark", func(t *testing.T) {
		t.Parallel()
		e, d := openIndexed(t, largeOptions(), lineText(150))
		setEncoding(t, e, "utf-8-bom")
		if !d.Modified() {
			t.Fatal("a new encoding does not mark the document modified")
		}
		if err := e.Save(); err != nil {
			t.Fatal(err)
		}
		if got := readFile(t, d.Path); got != bom+lineText(150) {
			t.Fatalf("file = %q...", got[:20])
		}
		if d.Buffer.Line(0) != "line 0000" || d.Modified() {
			t.Fatalf("line 0 = %q, modified %v", d.Buffer.Line(0), d.Modified())
		}
		// The mark is not written twice, nor does it show in the text.
		d.Buffer.Insert(buffer.Pos{}, "x")
		if err := e.Save(); err != nil {
			t.Fatal(err)
		}
		if got := readFile(t, d.Path); got != bom+"x"+lineText(150) {
			t.Fatalf("file = %q...", got[:20])
		}
	})
	t.Run("remove the mark", func(t *testing.T) {
		t.Parallel()
		e, d := openIndexed(t, largeOptions(), bom+lineText(150))
		if d.codec != "utf-8-bom" {
			t.Fatalf("codec = %q", d.codec)
		}
		setEncoding(t, e, "utf-8")
		if err := e.Save(); err != nil {
			t.Fatal(err)
		}
		if got := readFile(t, d.Path); got != lineText(150) {
			t.Fatalf("file = %q...", got[:20])
		}
		if d.Buffer.Line(0) != "line 0000" || d.Buffer.Lines() != 151 {
			t.Fatalf("line 0 = %q", d.Buffer.Line(0))
		}
	})
}

func TestLargeSaveCancelledLeavesNothing(t *testing.T) {
	t.Parallel()
	e, d := openIndexed(t, largeOptions(), lineText(150))
	before := readFile(t, d.Path)
	d.Buffer.Insert(buffer.Pos{}, "x")
	plan, err := e.PlanSave()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	results := plan.RunContext(ctx)
	if err := e.ApplySavePlan(plan, results); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if readFile(t, d.Path) != before || len(tempFiles(t, filepath.Dir(d.Path))) != 0 {
		t.Fatal("a cancelled save left something")
	}
	if !d.Modified() {
		t.Fatal("the document lost its changes")
	}
	// The snapshot of the cancelled job was released: the document still saves.
	if err := e.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
}

func TestRedoAfterRebaseReturnsFalseCaretUnchanged(t *testing.T) {
	t.Parallel()
	e, d := openIndexed(t, largeOptions(), lineText(150))
	d.Buffer.Insert(buffer.Pos{Line: 1}, "A")
	d.Buffer.Break()
	d.Buffer.Insert(buffer.Pos{Line: 2}, "B")
	if _, ok := d.Buffer.Undo(); !ok {
		t.Fatal("no undo")
	}
	d.Cursor = buffer.Pos{Line: 7, Col: 3}
	d.lastMulti = &multiEditRecord{}
	d.findRange = &buffer.Selection{}
	if err := e.Save(); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.Buffer.Redo(); ok {
		t.Error("redo works after the rebase")
	}
	if d.Cursor != (buffer.Pos{Line: 7, Col: 3}) {
		t.Errorf("cursor = %v", d.Cursor)
	}
	if d.lastMulti != nil || d.findRange != nil {
		t.Error("the records of the old history are still there")
	}
}

func TestLargeSaveRacesWithEdits(t *testing.T) {
	t.Parallel()
	e, d := openIndexed(t, largeOptions(), lineText(150))
	d.Buffer.Insert(buffer.Pos{Line: 3}, "first ")
	plan, err := e.PlanSave()
	if err != nil {
		t.Fatal(err)
	}
	var results []SaveResult
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		results = plan.Run()
	}()
	for i := range 50 {
		d.Buffer.Insert(buffer.Pos{Line: i}, "e")
		_ = d.Buffer.Line(i)
	}
	wg.Wait()
	if err := e.ApplySavePlan(plan, results); err != nil {
		t.Fatal(err)
	}
	if !d.Modified() {
		t.Fatal("the edits made during the save are not unsaved")
	}
	if got := readFile(t, d.Path); !strings.Contains(got, "first line 0003") || strings.Contains(got, "eline") {
		t.Fatal("the file does not hold the text of the snapshot")
	}
	if err := e.Save(); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, d.Path); !strings.Contains(got, "eline 0010") {
		t.Fatal("the second save lost the edits")
	}
}

func TestCheckSourceInTheDiskPoll(t *testing.T) {
	t.Parallel()
	e, d := openIndexed(t, largeOptions(), lineText(150))
	check := e.PlanDiskCheck()
	if changes := check(); len(changes) != 1 || changes[0].Changed {
		t.Fatalf("changes = %+v", changes)
	}
	f, err := os.OpenFile(d.Path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("more\n")
	_ = f.Close()
	_ = e.PlanDiskCheck()
	if want := errFileGrew.Error(); d.status != want {
		t.Fatalf("status = %q, want %q", d.status, want)
	}
	// The poll says it once.
	d.status = "other"
	_ = e.PlanDiskCheck()
	if d.status != "other" {
		t.Fatalf("status = %q: the poll said it again", d.status)
	}
}

func TestLargeSaveOfAVirtualSourceWaitsForTheIndex(t *testing.T) {
	t.Parallel()
	e, d, src := openVirtual(t, 5<<30, 64)
	if d.Index().Complete {
		t.Fatal("the index of a 5 GiB source is complete")
	}
	d.Buffer.Insert(buffer.Pos{}, "x")
	plan, err := e.PlanSave()
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(plan.jobs[0].err, ErrIndexing) {
		t.Fatalf("err = %v, want ErrIndexing", plan.jobs[0].err)
	}
	if got := src.BytesRead(); got > 16<<20 {
		t.Fatalf("read %d bytes", got)
	}
}
