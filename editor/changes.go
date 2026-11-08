package editor

import (
	"slices"

	"github.com/tbxark/gvim/buffer"
)

// Change tracking. While any key is tracked (TrackChanges), every document's
// Buffer reports its changes to the editor, which hands each one to the change
// listeners at once and keeps the recent ones in a ring per document for the
// consumers that poll them (TakeChanges).
//
// Changes are sequential: each is in the coordinates of the document just
// before it, so applying them first to last reproduces the text.

// Limits of the per-document ring and of what one TakeChanges hands out.
const (
	// changeRingRecords and changeRingBytes bound a ring: the most records, and
	// the most bytes of inserted and removed text, that it keeps. The newest
	// record always stays.
	changeRingRecords = 256
	changeRingBytes   = 1 << 20
	// changePayloadMax bounds one record, and the total of the records one
	// TakeChanges returns. A record over it is kept without its text, as a
	// coarse change; a total over it is reported as overflow.
	changePayloadMax = 256 << 10
)

// ChangeListener is called for every change of every document, synchronously,
// before the edit that caused it returns. It must not edit the document. doc is
// the id of the document that changed.
type ChangeListener func(doc DocID, c buffer.Change)

type listenerEntry struct {
	key string
	fn  ChangeListener
	// serial names the registration, so a remove function only removes its own.
	serial uint64
}

// seamState is the editor's part of the plugin seams. It is a field of Editor.
type seamState struct {
	nextID uint64
	// listenerSerial numbers the registrations of change listeners.
	listenerSerial uint64
	// track counts TrackChanges calls per key, and tracked is their sum.
	track   map[string]int
	tracked int
	// listeners is replaced, never changed in place, so a change delivered while
	// a listener is added or removed sees a consistent list.
	listeners []listenerEntry
}

// AddChangeListener registers fn under key and returns the function that removes
// it. Listeners are called in registration order. Registering a key again
// replaces its function and keeps its place. A listener only hears changes while
// some key is tracked (TrackChanges).
func (e *Editor) AddChangeListener(key string, fn ChangeListener) (remove func()) {
	e.seam.listenerSerial++
	serial := e.seam.listenerSerial
	next := slices.Clone(e.seam.listeners)
	if i := slices.IndexFunc(next, func(l listenerEntry) bool { return l.key == key }); i >= 0 {
		next[i].fn, next[i].serial = fn, serial
	} else {
		next = append(next, listenerEntry{key: key, fn: fn, serial: serial})
	}
	e.seam.listeners = next
	return func() {
		cur := e.seam.listeners
		// Removing a listener that was replaced since is left alone.
		i := slices.IndexFunc(cur, func(l listenerEntry) bool { return l.key == key && l.serial == serial })
		if i < 0 {
			return
		}
		e.seam.listeners = slices.Delete(slices.Clone(cur), i, i+1)
	}
}

// TrackChanges turns the tracking of one key on or off. It is reference counted
// per key: it takes as many off calls as on calls to stop tracking for a key.
// The buffer observer is installed on every current and future document while
// any key is on, and removed from all of them when the last key goes off. A key
// is also the name of a consumer of TakeChanges: switching it on starts that
// consumer at the present, switching it off forgets its position.
func (e *Editor) TrackChanges(key string, on bool) {
	e.adoptAll()
	s := &e.seam
	if on {
		if s.track == nil {
			s.track = make(map[string]int)
		}
		s.track[key]++
		s.tracked++
		if s.track[key] == 1 {
			for _, d := range e.docs {
				d.ringFor().cursors[key] = d.seam.ring.next()
			}
		}
		for _, d := range e.docs {
			if d.seam.obs == nil {
				e.observe(d)
			}
		}
		return
	}
	if s.track[key] == 0 {
		return
	}
	s.track[key]--
	s.tracked--
	if s.track[key] == 0 {
		delete(s.track, key)
		for _, d := range e.docs {
			if d.seam.ring != nil {
				delete(d.seam.ring.cursors, key)
			}
		}
	}
	if s.tracked > 0 {
		return
	}
	for _, d := range e.docs {
		if d.seam.obs != nil {
			d.seam.obs = nil
			d.Buffer.SetObserver(nil)
		}
		d.seam.ring = nil
	}
}

// observe installs the change observer on d's buffer.
func (e *Editor) observe(d *Document) {
	d.seam.obs = func(c buffer.Change) { e.changed(d, c) }
	d.Buffer.SetObserver(d.seam.obs)
}

// changed records c in the ring of d and calls the listeners.
func (e *Editor) changed(d *Document, c buffer.Change) {
	d.ringFor().add(c)
	for _, l := range e.seam.listeners {
		l.fn(d.seam.id, c)
	}
}

// TakeChanges returns the changes of the document that came after this
// consumer's last call, and moves the consumer to the present. Every consumer
// has its own position, so one never takes what another has yet to read. A
// consumer that was never tracked (see TrackChanges) starts at the oldest change
// the ring still holds.
//
// overflow is true, and no changes are returned, when the consumer missed
// changes (the ring of 256 records, or of 1 MiB of text, wrapped), when any of
// them is coarse (a large file, or a replaced buffer: the text must be read
// again), or when their text totals over 256 KiB. An unknown id returns nothing.
func (e *Editor) TakeChanges(id DocID, consumer string) (changes []buffer.Change, overflow bool) {
	d, _, ok := e.DocByID(id)
	if !ok || d.seam.ring == nil {
		return nil, false
	}
	return d.seam.ring.take(consumer)
}

// ringFor returns the change ring of d, made when first needed.
func (d *Document) ringFor() *changeRing {
	if d.seam.ring == nil {
		d.seam.ring = &changeRing{cursors: make(map[string]uint64)}
	}
	return d.seam.ring
}

// changeRing keeps the latest changes of one document. Each record has a
// sequence number, from base on, and each consumer a cursor: the sequence
// number of the next record it has to read.
type changeRing struct {
	recs    []buffer.Change
	start   int // index of the oldest record in recs
	base    uint64
	bytes   int
	cursors map[string]uint64
}

func payload(c buffer.Change) int { return len(c.Text) + len(c.Removed) }

// next is the sequence number the next record gets.
func (r *changeRing) next() uint64 { return r.base + uint64(len(r.recs)-r.start) }

func (r *changeRing) add(c buffer.Change) {
	if payload(c) > changePayloadMax {
		c = buffer.Change{Coarse: true, Undo: c.Undo, Version: c.Version}
	}
	r.recs = append(r.recs, c)
	r.bytes += payload(c)
	for n := len(r.recs) - r.start; n > 1 && (n > changeRingRecords || r.bytes > changeRingBytes); n-- {
		r.bytes -= payload(r.recs[r.start])
		r.recs[r.start] = buffer.Change{}
		r.start++
		r.base++
	}
	if r.start >= changeRingRecords {
		r.recs = append(r.recs[:0], r.recs[r.start:]...)
		r.start = 0
	}
}

func (r *changeRing) take(consumer string) ([]buffer.Change, bool) {
	cur := r.cursors[consumer]
	head := r.next()
	r.cursors[consumer] = head
	if cur < r.base {
		return nil, true
	}
	if cur >= head {
		return nil, false
	}
	recs := r.recs[r.start+int(cur-r.base):]
	total := 0
	for _, c := range recs {
		total += payload(c)
		if c.Coarse || total > changePayloadMax {
			return nil, true
		}
	}
	return slices.Clone(recs), false
}
