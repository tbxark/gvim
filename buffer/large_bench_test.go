package buffer_test

import (
	"bufio"
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/tbxark/gvim/buffer"
)

// These benchmarks run on a real big file and never in CI: they skip unless
// GVIM_BIG_FILE names a UTF-8 text file (the manual acceptance of large file mode uses
// a file of about 2 GB). The file is only read. The save benchmark writes its copy to
// GVIM_BIG_TMP, or to the system temporary directory, and removes it.
//
//	GVIM_BIG_FILE=/tmp/gvim-big/big.txt go test ./buffer -run '^$' -bench Large -benchtime 1x
const bigFileEnv = "GVIM_BIG_FILE"

// bigFile returns the path in GVIM_BIG_FILE, or skips the benchmark.
func bigFile(b *testing.B) string {
	b.Helper()
	path := os.Getenv(bigFileEnv)
	if path == "" {
		b.Skipf("set %s to a big text file to run this benchmark", bigFileEnv)
	}
	return path
}

// openBigFile opens path as a large buffer with the default geometry.
func openBigFile(b *testing.B, path string) *buffer.Buffer {
	b.Helper()
	src, err := buffer.OpenFileSource(path)
	if err != nil {
		b.Fatal(err)
	}
	buf, err := buffer.OpenLarge(src, buffer.LargeOptions{})
	if err != nil {
		_ = src.Close()
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = buf.Close() })
	return buf
}

// indexedBigFile opens path and builds its whole index outside the timed part.
func indexedBigFile(b *testing.B, path string) *buffer.Buffer {
	b.Helper()
	buf := openBigFile(b, path)
	b.StopTimer()
	indexAll(b, buf)
	b.StartTimer()
	return buf
}

// heapMB reports the live heap after a collection, in MiB.
func heapMB() float64 {
	var ms runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&ms)
	return float64(ms.HeapAlloc) / (1 << 20)
}

// BenchmarkLargeOpen is the time from a path to a buffer that shows its first lines:
// the open, which indexes the first 4 MiB.
func BenchmarkLargeOpen(b *testing.B) {
	path := bigFile(b)
	for b.Loop() {
		src, err := buffer.OpenFileSource(path)
		if err != nil {
			b.Fatal(err)
		}
		buf, err := buffer.OpenLarge(src, buffer.LargeOptions{})
		if err != nil {
			_ = src.Close()
			b.Fatal(err)
		}
		_ = buf.Line(0)
		_ = buf.Close()
	}
}

// BenchmarkLargeIndexThroughput is the speed of the line index over the whole file.
func BenchmarkLargeIndexThroughput(b *testing.B) {
	path := bigFile(b)
	fi, err := os.Stat(path)
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(fi.Size())
	for b.Loop() {
		buf := openBigFile(b, path)
		for {
			sc := buf.BeginIndexScan()
			if sc == nil {
				break
			}
			batch, err := sc.Next(context.Background(), 8<<20)
			sc.Close()
			if err != nil {
				b.Fatal(err)
			}
			buf.ApplyIndex(batch)
		}
		if !buf.Index(0).Complete {
			b.Fatal("the index did not complete")
		}
	}
}

// BenchmarkLargeLineRandom reads a random line of a fully indexed file.
func BenchmarkLargeLineRandom(b *testing.B) {
	buf := indexedBigFile(b, bigFile(b))
	lines := buf.Lines()
	rng := rand.New(rand.NewPCG(1, 2))
	read := buf.Stats().ReadBytes
	for b.Loop() {
		_ = buf.Line(rng.IntN(lines))
	}
	b.ReportMetric(float64(buf.Stats().ReadBytes-read)/float64(b.N), "readB/op")
}

// BenchmarkLargeTypeChar types one character at a random line. The edit lands in the
// overlay, so its cost does not grow with the file.
func BenchmarkLargeTypeChar(b *testing.B) {
	buf := indexedBigFile(b, bigFile(b))
	lines := buf.Lines()
	rng := rand.New(rand.NewPCG(3, 4))
	for b.Loop() {
		buf.Insert(buffer.Pos{Line: rng.IntN(lines)}, "x")
	}
	st := buf.Stats()
	b.ReportMetric(float64(st.OverlayBytes), "overlayB")
	b.ReportMetric(float64(st.HistoryBytes), "historyB")
}

// BenchmarkLargeSave edits one line and streams the whole file, with the edit, to a
// copy, then moves the buffer onto the copy. The copy is as big as the file; the copy
// of the iteration before is removed once the buffer has left it.
func BenchmarkLargeSave(b *testing.B) {
	path := bigFile(b)
	dir := os.Getenv("GVIM_BIG_TMP")
	if dir == "" {
		dir = os.TempDir()
	}
	fi, err := os.Stat(path)
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(fi.Size())
	buf := indexedBigFile(b, path)
	prev := ""
	b.Cleanup(func() {
		if prev != "" {
			_ = os.Remove(prev)
		}
	})
	for i := 0; b.Loop(); i++ {
		buf.Insert(buffer.Pos{}, "edited ")
		copyPath := filepath.Join(dir, fmt.Sprintf("gvim-bench-save-%d.txt", i%2))
		saveBigCopy(b, buf, copyPath)
		if prev != "" {
			_ = os.Remove(prev)
		}
		prev = copyPath
	}
	b.StopTimer()
	b.ReportMetric(heapMB(), "heapMB")
}

// saveBigCopy writes the text of buf to copyPath and rebases buf onto it.
func saveBigCopy(b *testing.B, buf *buffer.Buffer, copyPath string) {
	b.Helper()
	f, err := os.Create(copyPath)
	if err != nil {
		b.Fatal(err)
	}
	ver := buf.Version()
	snap := buf.Snapshot()
	bw := bufio.NewWriterSize(f, 1<<20)
	res, err := snap.WriteTo(context.Background(), bw, buffer.WriteOptions{})
	snap.Release()
	if err == nil {
		err = bw.Flush()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		b.Fatal(err)
	}
	src, err := buffer.OpenFileSource(copyPath)
	if err != nil {
		b.Fatal(err)
	}
	if err := buf.Rebase(src, res, ver, 0); err != nil {
		_ = src.Close()
		b.Fatal(err)
	}
}
