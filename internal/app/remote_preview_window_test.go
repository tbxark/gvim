package app

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestRemotePreviewReadWindow: the remote Live Grep preview returns the bounded complete
// line window around the hit through the authority's ReadRange, never the local disk, and
// honors its context.
func TestRemotePreviewReadWindow(t *testing.T) {
	var b strings.Builder
	for i := 1; i <= 60; i++ {
		fmt.Fprintf(&b, "line %02d\n", i)
	}
	fs := &previewFS{data: []byte(b.String())}
	p := newRemotePreviewer(previewSlot(fs, true))
	data, first, truncated, err := p.ReadWindow(context.Background(), "/remote/w/big.txt", 30, 4096)
	if err != nil {
		t.Fatalf("ReadWindow: %v", err)
	}
	if first != 25 {
		t.Fatalf("firstLine = %d, want 25 (five before the hit)", first)
	}
	if !truncated {
		t.Fatal("a middle window of a 60-line file is not marked truncated")
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) != 12 || lines[0] != "line 25" || lines[11] != "line 36" {
		t.Fatalf("window = %q", lines)
	}
	if sizes := fs.readSizes(); len(sizes) == 0 {
		t.Fatal("the remote preview read nothing")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, _, err := p.ReadWindow(ctx, "/remote/w/big.txt", 30, 4096); err == nil {
		t.Fatal("a canceled window reported no error")
	}
}

// TestRemotePreviewReadWindowDisconnected checks a disconnected authority reads nothing and
// never falls back locally.
func TestRemotePreviewReadWindowDisconnected(t *testing.T) {
	fs := &previewFS{data: []byte("secret\n")}
	p := newRemotePreviewer(previewSlot(fs, false))
	if _, _, _, err := p.ReadWindow(context.Background(), "/remote/w/a.txt", 1, 16); err == nil || !strings.Contains(err.Error(), "disconnected") {
		t.Fatalf("disconnected ReadWindow err = %v", err)
	}
	if sizes := fs.readSizes(); len(sizes) != 0 {
		t.Fatalf("a disconnected preview read %v", sizes)
	}
}
