package finder

import (
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

// rank is Rank with a limit. A limit of zero or less, or above MaxResults, means
// MaxResults.
func rank(files []string, query string, usage Usage, now time.Time, limit int) []Match {
	if limit <= 0 || limit > MaxResults {
		limit = MaxResults
	}
	var out []Match
	if query == "" {
		for _, f := range files {
			u := usage[f]
			out = append(out, Match{Path: f, Score: rawFrecency(u, now)})
		}
		slices.SortStableFunc(out, byScore)
		return truncate(out, limit)
	}
	pat := fuzzy.NewPattern(query)
	for _, f := range files {
		score, pos, ok := pat.Match(f)
		if !ok {
			continue
		}
		score += frecencyBoost(usage[f], now)
		if strings.HasPrefix(f, query) {
			score += prefixBoost
		}
		out = append(out, Match{Path: f, Score: score, Positions: pos})
	}
	slices.SortStableFunc(out, byScore)
	return truncate(out, limit)
}

func byScore(a, b Match) int {
	switch {
	case a.Score > b.Score:
		return -1
	case a.Score < b.Score:
		return 1
	}
	return 0
}

func truncate(m []Match, limit int) []Match {
	if len(m) > limit {
		return m[:limit]
	}
	return m
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
