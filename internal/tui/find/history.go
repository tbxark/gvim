package find

// historyCap is the number of entries a history keeps.
const historyCap = 100

// history is the ring of past texts of one field: the entries in the order they
// were confirmed, and the place Up and Down have walked to. It outlives the bar
// that filled it (see Bar.Inherit).
type history struct {
	items []string
	pos   int    // the entry shown, or len(items) when the field shows the draft
	draft string // what the field held before Up first moved off it
}

// push records s as the newest entry. An empty text is not kept, and an equal
// older entry moves to the end instead of repeating. The place resets to the
// draft.
func (h *history) push(s string) {
	if s != "" {
		for i, it := range h.items {
			if it == s {
				h.items = append(h.items[:i], h.items[i+1:]...)
				break
			}
		}
		h.items = append(h.items, s)
		if len(h.items) > historyCap {
			h.items = h.items[len(h.items)-historyCap:]
		}
	}
	h.pos = len(h.items)
}

// last returns the newest entry, or "" when there is none.
func (h *history) last() string {
	if len(h.items) == 0 {
		return ""
	}
	return h.items[len(h.items)-1]
}

// prev steps back to an older entry. cur is the text now in the field, kept as
// the draft when the walk starts. It reports false when there is no entry.
func (h *history) prev(cur string) (string, bool) {
	if len(h.items) == 0 {
		return "", false
	}
	if h.pos >= len(h.items) {
		h.pos = len(h.items)
		h.draft = cur
	}
	if h.pos > 0 {
		h.pos--
	}
	return h.items[h.pos], true
}

// next steps forward to a newer entry, and past the newest back to the draft. It
// reports false when the walk is not on an entry.
func (h *history) next() (string, bool) {
	if h.pos >= len(h.items) {
		return "", false
	}
	h.pos++
	if h.pos == len(h.items) {
		return h.draft, true
	}
	return h.items[h.pos], true
}

// load replaces the entries with items, newest last, and keeps the newest
// historyCap of them. Empty entries are dropped.
func (h *history) load(items []string) {
	h.items = h.items[:0]
	for _, it := range items {
		if it != "" {
			h.items = append(h.items, it)
		}
	}
	if len(h.items) > historyCap {
		h.items = h.items[len(h.items)-historyCap:]
	}
	h.pos = len(h.items)
}
