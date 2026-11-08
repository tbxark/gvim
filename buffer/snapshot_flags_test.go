package buffer_test

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/buffer/buffertest"
)

func TestSnapshotIndexCompleteAndHugeLine(t *testing.T) {
	t.Parallel()
	geom := buffer.LargeOptions{Chunk: 16, CachePages: 2, MaxLine: 64, InitialBytes: 32}
	var rows []string
	for range 60 {
		rows = append(rows, "row of text")
	}
	short := strings.Join(rows, "\n")
	long := short + "\n" + strings.Repeat("y", 200) + "\nend"

	open := func(t *testing.T, text string) *buffer.Buffer {
		t.Helper()
		b, err := buffer.OpenLarge(buffertest.NewMemSource([]byte(text)), geom)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = b.Close() })
		return b
	}
	complete := func(t *testing.T, b *buffer.Buffer) {
		t.Helper()
		for !b.Index(0).Complete {
			sc := b.BeginIndexScan()
			batch, err := sc.Next(t.Context(), 1<<20)
			sc.Close()
			if err != nil {
				t.Fatal(err)
			}
			b.ApplyIndex(batch)
		}
	}

	tests := []struct {
		name         string
		buf          func(t *testing.T) *buffer.Buffer
		wantComplete bool
		wantHuge     bool
	}{
		{"memory", func(*testing.T) *buffer.Buffer { return buffer.New(long) }, true, false},
		{"large, index incomplete", func(t *testing.T) *buffer.Buffer { return open(t, short) }, false, false},
		{"large, complete", func(t *testing.T) *buffer.Buffer { b := open(t, short); complete(t, b); return b }, true, false},
		{"large, complete, with a huge line", func(t *testing.T) *buffer.Buffer { b := open(t, long); complete(t, b); return b }, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := tt.buf(t).Snapshot()
			defer s.Release()
			if got := s.IndexComplete(); got != tt.wantComplete {
				t.Errorf("IndexComplete = %v, want %v", got, tt.wantComplete)
			}
			if got := s.HugeLine(); got != tt.wantHuge {
				t.Errorf("HugeLine = %v, want %v", got, tt.wantHuge)
			}
		})
	}

	// A snapshot is frozen: indexing on does not change what it reports.
	b := open(t, short)
	s := b.Snapshot()
	defer s.Release()
	complete(t, b)
	if s.IndexComplete() {
		t.Error("a snapshot taken before the index was complete reports complete after it")
	}
	if !b.Snapshot().IndexComplete() {
		t.Error("a snapshot taken after the index was complete reports incomplete")
	}
}
