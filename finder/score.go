package finder

import (
	"container/heap"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/tbxark/gvim/fuzzy"
)

// MaxResults is how many matches Rank returns, as Fresh's max_results does.
const MaxResults = 100

// prefixBoost is added to a file whose path starts with the raw query. It
// ranks a literal path prefix above fuzzy matches (Fresh's PREFIX_PROBE_BOOST).
const prefixBoost = 200

// frecencyCap bounds the frecency bonus so it cannot outrank a strong match.
const frecencyCap = 20

// Use records how often and when a file was opened from the picker.
type Use struct {
	Count int
	Last  time.Time
}

// Usage maps a path to its Use. Record needs a non-nil Usage; create it with
// make.
type Usage map[string]Use

// Record notes one open of path at now.
func (u Usage) Record(path string, now time.Time) {
	x := u[path]
	x.Count++
	x.Last = now
	u[path] = x
}

// Match is one ranked file.
type Match struct {
	Path string
	// Score is the fuzzy score plus the prefix and frecency bonuses. With an
	// empty query it is the raw frecency score and the order is by frecency.
	Score int
	// Positions are the rune indexes in Path that the query matched. They are
	// nil for an empty query.
	Positions []int
}

// Ranker ranks files for the picker and keeps the frecency of the files the user
// opened. It is safe for concurrent use: the palette ranks on a command goroutine
// while the UI records an open.
type Ranker struct {
	mu    sync.Mutex
	usage Usage
	now   func() time.Time
}

// NewRanker returns a Ranker with no usage yet. The zero Ranker is also usable. now is the clock frecency is
// measured on; nil means time.Now.
func NewRanker(now func() time.Time) *Ranker {
	if now == nil {
		now = time.Now
	}
	return &Ranker{usage: Usage{}, now: now}
}

// Record notes that path was opened now.
func (r *Ranker) Record(path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.usage == nil {
		r.usage = Usage{}
	}
	r.usage.Record(path, r.clock())
}

// clock returns the time frecency is measured on. The zero Ranker uses time.Now.
func (r *Ranker) clock() time.Time {
	if r.now == nil {
		return time.Now()
	}
	return r.now()
}

// Rank is Rank over the usage recorded so far and the clock of r. It returns at
// most limit matches, and at most MaxResults; a limit of zero or less means
// MaxResults. The usage is copied under the lock, so Rank does not block Record
// while it scores.
func (r *Ranker) Rank(query string, files []string, limit int) []Match {
	r.mu.Lock()
	usage := maps.Clone(r.usage)
	now := r.clock()
	r.mu.Unlock()
	return rank(files, query, usage, now, limit)
}

// Rank scores files against query and returns at most MaxResults matches, best
// first. Ties keep the order of files. The fuzzy scorer and its weights are
// those of the fuzzy package, which follow Fresh's input/fuzzy constants. The bonuses are:
// +frecency, capped at 20; and +200 when the path starts with the raw query.
// now is the time frecency is measured from.
func Rank(files []string, query string, usage Usage, now time.Time) []Match {
	return rank(files, query, usage, now, MaxResults)
}

type rankedMatch struct {
	m   Match
	idx int
}

// matchMinHeap keeps the best matches on top-K selection by putting the worst
// element at index 0: lower Score is worse; ties put higher file idx at root so
// earlier files win ties cleanly.
type matchMinHeap []rankedMatch

func (h matchMinHeap) Len() int { return len(h) }
func (h matchMinHeap) Less(i, j int) bool {
	if h[i].m.Score != h[j].m.Score {
		return h[i].m.Score < h[j].m.Score
	}
	return h[i].idx > h[j].idx
}
func (h matchMinHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *matchMinHeap) Push(x any)   { *h = append(*h, x.(rankedMatch)) }
func (h *matchMinHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

func betterMatch(a, b rankedMatch) bool {
	if a.m.Score != b.m.Score {
		return a.m.Score > b.m.Score
	}
	return a.idx < b.idx
}

// rank is Rank with a limit. A limit of zero or less, or above MaxResults, means
// MaxResults.
func rank(files []string, query string, usage Usage, now time.Time, limit int) []Match {
	if limit <= 0 || limit > MaxResults {
		limit = MaxResults
	}
	if len(files) == 0 {
		return nil
	}
	if strings.TrimSpace(query) == "" {
		return rankEmptyQuery(files, usage, now, limit)
	}
	matcher := fuzzy.NewMatcher(query)
	h := make(matchMinHeap, 0, min(limit, len(files)))
	for idx, f := range files {
		score, ok := matcher.MatchScore(f)
		if !ok {
			continue
		}
		score += frecencyBoost(usage[f], now)
		if strings.HasPrefix(f, query) {
			score += prefixBoost
		}
		cand := rankedMatch{m: Match{Path: f, Score: score}, idx: idx}
		if len(h) < limit {
			h = append(h, cand)
			if len(h) == limit {
				heap.Init(&h)
			}
		} else if betterMatch(cand, h[0]) {
			h[0] = cand
			heap.Fix(&h, 0)
		}
	}
	if len(h) == 0 {
		return nil
	}
	slices.SortFunc(h, func(a, b rankedMatch) int {
		if a.m.Score != b.m.Score {
			if a.m.Score > b.m.Score {
				return -1
			}
			return 1
		}
		return a.idx - b.idx
	})
	out := make([]Match, len(h))
	for i := range h {
		m := h[i].m
		_, m.Positions, _ = matcher.Match(m.Path)
		out[i] = m
	}
	return out
}

func rankEmptyQuery(files []string, usage Usage, now time.Time, limit int) []Match {
	if len(usage) == 0 {
		n := min(limit, len(files))
		out := make([]Match, n)
		for i := range n {
			out[i] = Match{Path: files[i]}
		}
		return out
	}
	h := make(matchMinHeap, 0, min(limit, len(usage)))
	for idx, f := range files {
		u := usage[f]
		if u.Count == 0 {
			continue
		}
		sc := rawFrecency(u, now)
		if sc <= 0 {
			continue
		}
		cand := rankedMatch{m: Match{Path: f, Score: sc}, idx: idx}
		if len(h) < limit {
			heap.Push(&h, cand)
		} else if betterMatch(cand, h[0]) {
			h[0] = cand
			heap.Fix(&h, 0)
		}
	}
	slices.SortFunc(h, func(a, b rankedMatch) int {
		if a.m.Score != b.m.Score {
			if a.m.Score > b.m.Score {
				return -1
			}
			return 1
		}
		return a.idx - b.idx
	})
	out := make([]Match, 0, min(limit, len(files)))
	used := make(map[int]bool, len(h))
	for _, r := range h {
		out = append(out, r.m)
		used[r.idx] = true
	}
	for idx, f := range files {
		if len(out) >= limit {
			break
		}
		if !used[idx] {
			out = append(out, Match{Path: f})
		}
	}
	return out
}

// frecencyBoost is the bonus added to a matching file: its frecency score
// divided by 100, capped at frecencyCap, as Fresh does.
func frecencyBoost(u Use, now time.Time) int {
	return min(frecencyCap, rawFrecency(u, now)/100)
}

// rawFrecency is the access count times a weight that falls with the age of
// the last access: 100 within 4 hours, 70 within a day, 50 within a week, 30
// within a month, 10 within three months, and 1 after that.
func rawFrecency(u Use, now time.Time) int {
	if u.Count == 0 {
		return 0
	}
	hours := now.Sub(u.Last).Hours()
	var w int
	switch {
	case hours < 4:
		w = 100
	case hours < 24:
		w = 70
	case hours < 24*7:
		w = 50
	case hours < 24*30:
		w = 30
	case hours < 24*90:
		w = 10
	default:
		w = 1
	}
	return u.Count * w
}
