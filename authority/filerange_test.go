package authority

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalReadRange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := Local(LocalOptions{})
	ctx := context.Background()
	got, err := a.Files().ReadRange(ctx, path, 2, 3)
	if err != nil || string(got) != "234" {
		t.Fatalf("range = %q, %v", got, err)
	}
	got, err = a.Files().ReadRange(ctx, path, 7, 100)
	if !errors.Is(err, io.EOF) || string(got) != "789" {
		t.Fatalf("short range = %q, %v; want 789, EOF", got, err)
	}
	if _, err := a.Files().ReadRange(ctx, path, -1, 1); err == nil {
		t.Fatal("a negative offset was accepted")
	}
	if _, err := a.Files().ReadRange(ctx, filepath.Join(dir, "missing"), 0, 1); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing file = %v, want os.ErrNotExist", err)
	}
	// The transport cap bounds one answer.
	big := filepath.Join(dir, "big.bin")
	data := make([]byte, localReadRangeCap+4096)
	for i := range data {
		data[i] = 'x'
	}
	if err := os.WriteFile(big, data, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = a.Files().ReadRange(ctx, big, 0, int64(len(data)))
	if err != nil || len(got) != localReadRangeCap {
		t.Fatalf("capped range = %d bytes, %v", len(got), err)
	}
}

func TestUnsupportedReadRange(t *testing.T) {
	if _, err := (UnsupportedFiles{}).ReadRange(context.Background(), "/x", 0, 1); !errors.Is(err, ErrUnsupportedFileIO) {
		t.Fatalf("unsupported range = %v", err)
	}
}
