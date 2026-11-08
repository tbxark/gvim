package buffer

import (
	"slices"
	"time"
)

// undoBucket is the wall-clock window that groups edits into one undo step.
// Edits stamped in the same bucket undo together. Micro uses the same width
// (undoThreshold in micro's eventhandler.go).
const undoBucket = time.Second

// bucketOf returns the index of the undoBucket window that contains t.
func bucketOf(t time.Time) int64 {
	return t.UnixMilli() / undoBucket.Milliseconds()
}

// opKind distinguishes the two primitive edits that history records.
type opKind uint8

const (
	opInsert opKind = iota
	opDelete
)

// op is one recorded edit. For opInsert, text was inserted at from. For
// opDelete, text was removed starting at from. Positions are in the document
// as it was before the edit, so an op can be inverted without extra state.
type op struct {
	kind opKind
	from Pos
	text string
	// big marks an op whose text was too large to keep (text is empty), and
	// end is the position Redo reports. Both are zero in a memory buffer.
	big bool
	end Pos
}

// group is one undo step. Its ops are in the order they were applied. Undo
// reverts them newest first and Redo replays them oldest first.
type group struct {
	ops []op
	// before and after are the content versions on either side of the group.
	// The saved marker compares against them.
	before, after uint64
	// bucket is the undoBucket window of the first edit. Non-Group edits
	// stamped in the same window join the group. Group steps leave it zero
	// and are never joined.
	bucket int64
	// sb and sa are the large-buffer states on either side of the group and
	// cost is the estimated bytes the group keeps alive (lastCost is the
	// share of the newest edit). All are zero in a memory buffer.
	sb, sa         bigState
	cost, lastCost int64
}

// add appends o to the group. When o continues the last op as one run of
// typing or deleting, it is folded into that op instead. Undo is unchanged
// because the folded op's inverse equals the inverses of the pair.
func (g *group) add(o op) {
	if n := len(g.ops); n > 0 && mergeable(&g.ops[n-1], &o) {
		merge(&g.ops[n-1], &o)
		return
	}
	g.ops = append(g.ops, o)
}

// record appends an edit to history. It clears the redo stack. Inside Group
// the edit joins the group being built. Otherwise it joins the newest undo
// group when that group is still open and was stamped in the same wall-clock
// bucket. An edit in a later bucket, or after Break or MarkSaved, starts a
// new group.
func (b *Buffer) record(kind opKind, from Pos, text string) {
	b.redo = nil
	b.seq++
	next := b.seq
	o := op{kind: kind, from: from, text: text}

	if b.depth > 0 {
		b.txn.add(o)
		b.ver = next
		return
	}
	bucket := bucketOf(b.now())
	if n := len(b.undo); n > 0 && b.open && b.undo[n-1].bucket == bucket {
		g := b.undo[n-1]
		g.add(o)
		g.after = next
		b.ver = next
		return
	}
	g := &group{before: b.ver, after: next, bucket: bucket}
	g.add(o)
	b.undo = append(b.undo, g)
	b.open = true
	b.ver = next
}

// mergeable reports whether next continues prev as one run of typing or
// deleting. Runs are not broken at whitespace; grouping is by time.
func mergeable(prev, next *op) bool {
	if prev.big || next.big {
		return false
	}
	if prev.kind != next.kind {
		return false
	}
	switch next.kind {
	case opInsert:
		return advance(prev.from, prev.text) == next.from
	case opDelete:
		// Backspace repeats: next ends where prev began.
		// Forward delete repeats: next starts where prev began.
		return advance(next.from, next.text) == prev.from || next.from == prev.from
	}
	return false
}

// merge folds next into prev. The caller has checked mergeable.
func merge(prev, next *op) {
	switch {
	case prev.kind == opInsert:
		prev.text += next.text
		prev.end = next.end
	case advance(next.from, next.text) == prev.from:
		prev.from = next.from
		prev.text = next.text + prev.text
		prev.end = prev.from
	default:
		prev.text += next.text
	}
}

// addBounded is add for large buffers: ops are not merged past InlineOpMax of
// stored text. It reports whether o was folded into the last op.
func (g *group) addBounded(o op) (merged bool) {
	if n := len(g.ops); n > 0 && len(g.ops[n-1].text)+len(o.text) <= InlineOpMax && mergeable(&g.ops[n-1], &o) {
		merge(&g.ops[n-1], &o)
		return true
	}
	g.ops = append(g.ops, o)
	return false
}

// noteEdit adds a large-buffer edit to g and makes ns its end state. An edit
// folded into the previous op replaces that op's share of the cost instead of
// adding to it, because the literal line it replaced is no longer reachable
// from the group's end state.
func (g *group) noteEdit(o op, ns bigState, cost int64) {
	if g.addBounded(o) {
		g.cost += max(0, cost-g.lastCost)
	} else {
		g.cost += cost
	}
	g.lastCost = cost
	g.sa = ns
}

// recordBig appends a large-buffer edit to history and makes ns the current
// state. It follows record: the edit clears the redo stack and joins the
// group being built, or the open undo group of the same wall-clock bucket, or
// starts a new group. big says the edit text was too large to keep inline;
// cost is the estimated bytes the edit's literal lines add.
func (b *Buffer) recordBig(kind opKind, from, end Pos, text string, big bool, ns bigState, cost int64) {
	s := b.big
	old := s.st
	if big {
		text = ""
	}
	o := op{kind: kind, from: from, text: text, big: big, end: end}
	cost += int64(len(text))
	b.redo = nil
	b.seq++
	next := b.seq
	s.st = ns

	if b.depth > 0 {
		b.txn.noteEdit(o, ns, cost)
		b.ver = next
		return
	}
	bucket := bucketOf(b.now())
	if n := len(b.undo); n > 0 && b.open && b.undo[n-1].bucket == bucket {
		g := b.undo[n-1]
		g.noteEdit(o, ns, cost)
		g.after = next
		b.ver = next
		b.trimHistory()
		return
	}
	g := &group{before: b.ver, after: next, bucket: bucket, sb: old}
	g.noteEdit(o, ns, cost)
	b.undo = append(b.undo, g)
	b.open = true
	b.ver = next
	b.trimHistory()
}

// historyBytes is the estimated memory the undo and redo stacks keep alive.
func (b *Buffer) historyBytes() int64 {
	var n int64
	for _, g := range b.undo {
		n += g.cost
	}
	for _, g := range b.redo {
		n += g.cost
	}
	return n
}

// trimHistory drops the oldest undo groups while a large buffer's history is
// over its group or byte cap. The newest group always stays, so the last edit
// can be undone however large it was.
func (b *Buffer) trimHistory() {
	s := b.big
	if s == nil {
		return
	}
	for len(b.undo) > 1 && (len(b.undo) > s.maxGroups || b.historyBytes() > s.maxHistory) {
		b.undo[0] = nil
		b.undo = b.undo[1:]
	}
}

// Break ends the current undo group, so the next edit starts a new one. The
// editor calls it when the user moves the cursor or runs a non-typing command.
func (b *Buffer) Break() {
	b.open = false
}

// Group runs fn so that every edit it makes is one undo step. Undo reverts
// all of them together, and Redo reapplies them together. Group does not
// merge with typing before or after it, so the next edit starts a new step.
//
// Group nests. An inner Group inside fn joins the outer group, and the outer
// group is the one undo step. If fn makes no edits, nothing is recorded.
//
// Inside fn, Undo and Redo report false and change nothing. If fn panics, the
// edits already made stay in the buffer and form one undo step, and the panic
// continues.
//
// Example: a replace is Group(func() { b.Delete(from, to); b.Insert(from, s) }).
// One Undo then restores the old text, and one Redo puts the new text back.
func (b *Buffer) Group(fn func()) {
	outer := b.depth == 0
	var snap txnSnap
	if outer {
		snap = txnSnap{redo: b.redo, open: b.open, ver: b.ver}
		if b.big != nil {
			snap.sb = b.big.st
		}
		b.Break()
		b.txn = &group{before: b.ver, sb: snap.sb}
		b.txnRefused = false
	}
	b.depth++
	completed := false
	defer func() {
		b.depth--
		if !outer {
			return
		}
		g := b.txn
		b.txn = nil
		refused := b.txnRefused
		b.txnRefused = false
		if b.big != nil && completed && refused {
			// A large buffer refused an edit (or a read) inside the group, so
			// the group is dropped whole: the steps that did succeed would be
			// half of an action. After a panic the edits stay instead.
			b.big.st, b.redo, b.open, b.ver = snap.sb, snap.redo, snap.open, snap.ver
			if len(g.ops) > 0 {
				// The edits that were reported are gone.
				b.notifyCoarse(false)
			}
			return
		}
		b.open = false
		if len(g.ops) == 0 {
			return
		}
		g.after = b.ver
		b.undo = append(b.undo, g)
		b.trimHistory()
	}()
	fn()
	completed = true
}

// txnSnap is what an outermost Group saves so a refused group can be undone
// without trace. record replaces redo instead of changing it, so keeping the
// slice header is enough.
type txnSnap struct {
	sb   bigState
	redo []*group
	open bool
	ver  uint64
}

// Undo reverts the newest undo group and reports the position where the
// reverted text begins, which is the position of the group's first edit. It
// returns false when there is nothing to undo, or when called inside Group.
func (b *Buffer) Undo() (Pos, bool) {
	n := len(b.undo)
	if n == 0 || b.depth > 0 {
		return Pos{}, false
	}
	g := b.undo[n-1]
	b.undo = b.undo[:n-1]

	var seen []Change
	if b.big != nil {
		// A large buffer swaps in the immutable state from before the group.
		b.big.st = g.sb
	} else {
		// Revert newest first so each op sees the document it produced.
		for _, o := range slices.Backward(g.ops) {
			if o.kind == opInsert {
				to := advance(o.from, o.text)
				if b.obs != nil {
					seen = append(seen, b.changeOf(o.from, to, "", o.text))
				}
				b.deleteRaw(o.from, to)
			} else {
				if b.obs != nil {
					seen = append(seen, b.changeOf(o.from, o.from, o.text, ""))
				}
				b.insertRaw(o.from, o.text)
			}
		}
	}
	b.ver = g.before
	b.redo = append(b.redo, g)
	b.open = false
	b.notifyStep(seen)
	return g.ops[0].from, true
}

// Redo reapplies the newest undone group and reports the position after the
// group's last edit (or the deletion point, when that edit was a delete). It
// returns false when there is nothing to redo, or when called inside Group.
func (b *Buffer) Redo() (Pos, bool) {
	n := len(b.redo)
	if n == 0 || b.depth > 0 {
		return Pos{}, false
	}
	g := b.redo[n-1]
	b.redo = b.redo[:n-1]

	var at Pos
	var seen []Change
	if b.big != nil {
		b.big.st = g.sa
		at = g.ops[len(g.ops)-1].end
	} else {
		for _, o := range g.ops {
			if o.kind == opInsert {
				if b.obs != nil {
					seen = append(seen, b.changeOf(o.from, o.from, o.text, ""))
				}
				at = b.insertRaw(o.from, o.text)
			} else {
				to := advance(o.from, o.text)
				if b.obs != nil {
					seen = append(seen, b.changeOf(o.from, to, "", o.text))
				}
				b.deleteRaw(o.from, to)
				at = o.from
			}
		}
	}
	b.undo = append(b.undo, g)
	b.ver = g.after
	b.open = false
	b.notifyStep(seen)
	return at, true
}
