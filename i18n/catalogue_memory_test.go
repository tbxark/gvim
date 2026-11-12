package i18n

import (
	"runtime"
	"testing"
)

// TestCatalogueHeapBudget bounds what the embedded catalogues cost in live heap: it loads
// the English catalogue and the Chinese catalogue the way a session does (the lazy
// package cache) and compares the collected heap before and after. It is the runtime
// counterpart to the embedded-size cap: raising the JSON cap must not silently raise the
// idle heap budget. A normal English session loads only English, and a translated locale's
// map is allocated lazily when that locale is first used. Run in a fresh test process
// (-run TestCatalogueHeapBudget) for exact numbers; in the full package earlier tests may
// already have loaded one or both, in which case the deltas read zero and the cap still
// holds.
func TestCatalogueHeapBudget(t *testing.T) {
	heapDelta := func(load func()) int64 {
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		load()
		runtime.GC()
		runtime.ReadMemStats(&after)
		return int64(after.HeapAlloc) - int64(before.HeapAlloc)
	}
	en := heapDelta(func() {
		if catalogue(DefaultLocale) == nil {
			t.Fatal("the English catalogue did not load")
		}
	})
	zh := heapDelta(func() {
		if catalogue("zh-CN") == nil {
			t.Fatal("the Chinese catalogue did not load")
		}
	})
	t.Logf("catalogue heap: en %d KiB, zh-CN %d KiB, both %d KiB", en>>10, zh>>10, (en+zh)>>10)
	// The measured values are a few hundred KiB; the cap leaves room for the remaining
	// extraction but keeps the loaded maps from growing into a heap budget item.
	const budget = 2 << 20
	if total := en + zh; total > budget {
		t.Errorf("loaded en+zh-CN catalogues keep %d KiB, want under the %d KiB budget", total>>10, budget>>10)
	}
}
