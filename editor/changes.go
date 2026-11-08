package editor

import (
	"github.com/tbxark/gvim/buffer"
	"slices"
)

// ChangeListener is called for every change of every document, synchronously,
// before the edit that caused it returns. It must not edit the document. doc is
// the id of the document that changed.
type ChangeListener func(doc DocID, c buffer.Change)

type listenerEntry struct {
	Key string
	fn  ChangeListener
	// serial names the registration, so a remove function only removes its own.
	Serial uint64
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
	if i := slices.IndexFunc(next, func(l listenerEntry) bool { return l.Key == key }); i >= 0 {
		next[i].fn, next[i].Serial = fn, serial
	} else {
		next = append(next, listenerEntry{Key: key, fn: fn, Serial: serial})
	}
	e.seam.listeners = next
	return func() {
		cur := e.seam.listeners
		// Removing a listener that was replaced since is left alone.
		i := slices.IndexFunc(cur, func(l listenerEntry) bool { return l.Key == key && l.Serial == serial })
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
				d.RingFor().Cursors[key] = d.Seam().Ring.Next()
			}
		}
		for _, d := range e.docs {
			if d.Seam().Obs == nil && !d.SharedMirror() {
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
			if d.Seam().Ring != nil {
				delete(d.Seam().Ring.Cursors, key)
			}
		}
	}
	if s.tracked > 0 {
		return
	}
	for _, d := range e.docs {
		if d.Seam().Obs != nil {
			d.Seam().Obs = nil
			d.Buffer.SetObserver(nil)
		}
		d.Seam().Ring = nil
	}
}

// observe installs the change observer on d's buffer.
func (e *Editor) observe(d *Document) {
	d.Seam().Obs = func(c buffer.Change) { e.changed(d, c) }
	d.Buffer.SetObserver(d.Seam().Obs)
}

// changed records c in the ring of d and calls the listeners.
func (e *Editor) changed(d *Document, c buffer.Change) {
	d.RingFor().Add(c)
	for _, l := range e.seam.listeners {
		l.fn(d.Seam().Id, c)
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
	if !ok || d.Seam().Ring == nil {
		return nil, false
	}
	return d.Seam().Ring.Take(consumer)
}
