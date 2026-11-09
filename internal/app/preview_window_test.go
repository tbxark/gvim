package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPreviewReaderReadWindow checks the Live Grep preview window is bounded around the
// hit line, reports the first line, and honors context cancellation.
func TestPreviewReaderReadWindow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.txt")
	var b strings.Builder
	for i := 1; i <= 100; i++ {
		b.WriteString("line ")
		b.WriteString(strings.Repeat("x", i%7))
		b.WriteString("\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	data, first, truncated, err := previewReader{}.ReadWindow(context.Background(), path, 50, 4096)
	if err != nil {
		t.Fatalf("ReadWindow: %v", err)
	}
	if first != 45 {
		t.Fatalf("firstLine = %d, want 45 (five before the hit)", first)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) != 12 {
		t.Fatalf("window lines = %d, want 12", len(lines))
	}
	if !truncated {
		t.Fatal("a 100-line file's middle window is not marked truncated")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, _, err := (previewReader{}).ReadWindow(ctx, path, 50, 4096); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled ReadWindow err = %v, want context.Canceled", err)
	}
}
