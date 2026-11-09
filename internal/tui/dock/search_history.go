package dock

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/tbxark/gvim/internal/terminal"
)

// searchHistoryQueryMax is the byte bound a recalled query is cut to; it matches
// the terminal record bound (terminal.MaxSearchQueryBytes) so what the dock recalls
// and what a restore loads stay consistent.
const searchHistoryQueryMax = terminal.MaxSearchQueryBytes

// searchHistoryStore is the dock-wide scrollback-search query history: Fresh keeps
// its search prompt InputHistory per window and persists it with the workspace, so
// gvim shares one bounded history across every tab in the dock and saves it with
// the terminal state. It is pure data: writing or restoring it never starts a
// process or touches a PTY.
type searchHistoryStore struct {
	queries []string // oldest first, newest last
}

// remember appends a committed query with Fresh's InputHistory rules: the query
// is cleaned/bounded first (so the SAVE side also honors the record's 512-byte
// limit, not only the loader), empty queries and an exact repeat of the newest
// entry are skipped, and past the terminal record's cap the oldest entry is
// dropped.
func (h *searchHistoryStore) remember(query string) {
	query = sanitizeSearchQuery(query)
	if query == "" {
		return
	}
	if n := len(h.queries); n > 0 && h.queries[n-1] == query {
		return
	}
	h.queries = append(h.queries, query)
	if over := len(h.queries) - terminal.MaxSearchHistoryEntries; over > 0 {
		h.queries = append([]string(nil), h.queries[over:]...)
	}
}

// entries returns the queries oldest first. The slice is owned by the store; the
// search input only reads it while holding the store.
func (h *searchHistoryStore) entries() []string { return h.queries }

// snapshot returns a copy for a saved terminal record, defensively re-bounding it
// to the record's limits (newest 100 queries, each cleaned/cut to 512 bytes) so
// the write side can never emit a field the loader would have to truncate.
func (h *searchHistoryStore) snapshot() []string {
	if len(h.queries) == 0 {
		return nil
	}
	start := max(len(h.queries)-terminal.MaxSearchHistoryEntries, 0)
	out := make([]string, 0, len(h.queries)-start)
	for _, query := range h.queries[start:] {
		if query = sanitizeSearchQuery(query); query != "" {
			out = append(out, query)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// restore installs a loaded history, cleaning and bounding it again like the
// restore loader did (defense in depth for callers that construct a state by hand):
// queries keep their order, empty/invalid entries are dropped and only the newest
// cap entries survive.
func (h *searchHistoryStore) restore(queries []string) {
	h.queries = nil
	start := max(len(queries)-terminal.MaxSearchHistoryEntries, 0)
	for _, query := range queries[start:] {
		query = sanitizeSearchQuery(query)
		if query != "" {
			h.queries = append(h.queries, query)
		}
	}
}

// sanitizeSearchQuery drops control characters/invalid UTF-8, trims and cuts a
// query to the record bound on a rune boundary.
func sanitizeSearchQuery(query string) string {
	query = strings.ToValidUTF8(query, "")
	query = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, query)
	query = strings.TrimSpace(query)
	if len(query) > searchHistoryQueryMax {
		cut := query[:searchHistoryQueryMax]
		for len(cut) > 0 && !utf8.ValidString(cut) {
			cut = cut[:len(cut)-1]
		}
		query = strings.TrimSpace(cut)
	}
	return query
}
