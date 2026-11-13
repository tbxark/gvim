package buffer

import (
	"container/list"
	"sync"
	"sync/atomic"
)

// DefaultPageBudget is the byte budget for source pages shared by every open
// large buffer in the process.
const DefaultPageBudget = 64 << 20

// PageBudget bounds the page cache memory of all large buffers that use it.
// Each open buffer may keep total/open-buffers bytes of pages, so memory does
// not grow with the number of tabs. The count is atomic; buffers never lock
// each other.
type PageBudget struct {
	total  int64
	stores atomic.Int32
}

// NewPageBudget returns a budget of total bytes.
func NewPageBudget(total int64) *PageBudget { return &PageBudget{total: total} }

var defaultBudget = NewPageBudget(DefaultPageBudget)

// pages returns how many pages of chunk bytes one buffer may keep: want, or
// less when the budget is shared. A buffer always keeps at least two, because
// a line lookup touches the pages of both ends of a line.
func (b *PageBudget) pages(chunk int64, want int) int {
	stores := int64(max(b.stores.Load(), 1))
	share := b.total / stores / chunk
	return int(min(int64(want), max(share, 2)))
}

// page is one chunk of the source. It is immutable once published, so slices
// of data stay valid after the page is evicted.
type page struct {
	data  []byte
	nlPos []int32 // offsets of '\n' in data, ascending
}

type pageEnt struct {
	c int
	p *page
}

// pageCache is an LRU of chunk pages, safe for concurrent use.
type pageCache struct {
	mu     sync.Mutex
	chunk  int64
	want   int
	budget *PageBudget
	stats  *statCounters
	byChk  map[int]*list.Element
	lru    *list.List // front = most recently used
	// retired is set when the buffer moved on to another source.
	retired bool
}

func newPageCache(chunk int64, want int, budget *PageBudget, stats *statCounters) *pageCache {
	return &pageCache{
		chunk: chunk, want: want, budget: budget, stats: stats,
		byChk: make(map[int]*list.Element), lru: list.New(),
	}
}

// get returns the cached page for chunk c.
func (pc *pageCache) get(c int) (*page, bool) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	e, ok := pc.byChk[c]
	if !ok {
		return nil, false
	}
	pc.lru.MoveToFront(e)
	return e.Value.(*pageEnt).p, true
}

// put stores p as chunk c and returns the page now cached for c, which is the
// existing one when another goroutine filled it first.
func (pc *pageCache) put(c int, p *page) *page {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	if e, ok := pc.byChk[c]; ok {
		pc.lru.MoveToFront(e)
		return e.Value.(*pageEnt).p
	}
	pc.byChk[c] = pc.lru.PushFront(&pageEnt{c: c, p: p})
	pc.stats.cacheBytes.Add(int64(len(p.data)))
	for limit := pc.budget.pages(pc.chunk, pc.want); pc.lru.Len() > limit; {
		pc.evictOldest()
	}
	return p
}

func (pc *pageCache) evictOldest() {
	e := pc.lru.Back()
	ent := pc.lru.Remove(e).(*pageEnt)
	delete(pc.byChk, ent.c)
	pc.stats.cacheBytes.Add(-int64(len(ent.p.data)))
}

// clear drops every page.
func (pc *pageCache) clear() {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	pc.clearLocked()
}

func (pc *pageCache) clearLocked() {
	for pc.lru.Len() > 0 {
		pc.evictOldest()
	}
}

// retire drops every page and marks the cache as no longer used by its buffer
// (its source was replaced). Snapshots that still read through it refill it;
// each one empties it again when released.
func (pc *pageCache) retire() {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	pc.retired = true
	pc.clearLocked()
}

// clearIfRetired empties a retired cache.
func (pc *pageCache) clearIfRetired() {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	if pc.retired {
		pc.clearLocked()
	}
}

// lineSpan is where a terminated base line lies in the source and how it ends.
type lineSpan struct {
	start, end int64
	term       uint8
}

// rangeMemoMax is how many line spans the memo holds before it starts over.
const rangeMemoMax = 4096

// hugeMemoMax is how many huge indexes a hugeMemo keeps before it starts again. Each index
// holds a mark for every stride runes of its line, so the bound is small.
const hugeMemoMax = 64

// hugeMemo holds the indexes of the huge base lines of one source. The buffer and its
// snapshots share it: an index is immutable once built, and building one reads the whole
// line, so one is built once for all of them. A nil hugeMemo keeps nothing.
type hugeMemo struct {
	mu sync.Mutex
	m  map[int]*hugeLine
}

func newHugeMemo() *hugeMemo { return &hugeMemo{m: make(map[int]*hugeLine)} }

func (hm *hugeMemo) get(b int) (*hugeLine, bool) {
	if hm == nil {
		return nil, false
	}
	hm.mu.Lock()
	defer hm.mu.Unlock()
	h, ok := hm.m[b]
	return h, ok
}

func (hm *hugeMemo) put(b int, h *hugeLine) {
	if hm == nil {
		return
	}
	hm.mu.Lock()
	defer hm.mu.Unlock()
	if len(hm.m) >= hugeMemoMax {
		clear(hm.m)
	}
	hm.m[b] = h
}

// rangeMemo remembers the spans of the base lines that were looked up last. Finding a
// line needs the page that holds its newline, which for a long line is a page of the
// cache read just to place the line. A frame of long lines would need more pages than
// the cache holds and read them all again on every frame, an idle one too; the memo
// answers from the spans instead. The span of a terminated line never changes, so an
// entry is never stale. Safe for concurrent use.
type rangeMemo struct {
	mu sync.Mutex
	m  map[int]lineSpan
}

func newRangeMemo() *rangeMemo { return &rangeMemo{m: make(map[int]lineSpan)} }

func (rm *rangeMemo) get(b int) (lineSpan, bool) {
	if rm == nil {
		return lineSpan{}, false
	}
	rm.mu.Lock()
	defer rm.mu.Unlock()
	sp, ok := rm.m[b]
	return sp, ok
}

func (rm *rangeMemo) put(b int, sp lineSpan) {
	if rm == nil {
		return
	}
	rm.mu.Lock()
	defer rm.mu.Unlock()
	if len(rm.m) >= rangeMemoMax {
		clear(rm.m)
	}
	rm.m[b] = sp
}
