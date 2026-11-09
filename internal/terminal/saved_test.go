package terminal

import (
	"strconv"
	"testing"
)

// TestMergeSearchHistory checks the migration merge: target queries stay first,
// source-only queries append, exact duplicates are dropped, empty entries are
// ignored and only the newest MaxSearchHistoryEntries survive.
func TestMergeSearchHistory(t *testing.T) {
	t.Parallel()
	if got := MergeSearchHistory(nil, nil); got != nil {
		t.Fatalf("empty merge = %v", got)
	}
	got := MergeSearchHistory([]string{"target", "shared"}, []string{"shared", "", "alpha", "target", "beta"})
	want := []string{"target", "shared", "alpha", "beta"}
	if len(got) != len(want) {
		t.Fatalf("merge = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("merge[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	// Only the newest cap entries survive; the target's older entries fall off first.
	emptyTarget := make([]string, 0, MaxSearchHistoryEntries+20)
	for i := 0; i < MaxSearchHistoryEntries+20; i++ {
		emptyTarget = append(emptyTarget, "q"+strconv.Itoa(i))
	}
	merged := MergeSearchHistory(nil, emptyTarget)
	if len(merged) != MaxSearchHistoryEntries {
		t.Fatalf("merged length = %d, want %d", len(merged), MaxSearchHistoryEntries)
	}
	if merged[len(merged)-1] != emptyTarget[len(emptyTarget)-1] {
		t.Fatalf("merged newest = %q, want %q", merged[len(merged)-1], emptyTarget[len(emptyTarget)-1])
	}
}
