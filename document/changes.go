package document

import (
	"github.com/tbxark/gvim/buffer"
	"slices"
)

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

// RingFor returns the change ring of d, made when first needed.
func (d *Document) RingFor() *changeRing {
	if d.seam.Ring == nil {
		d.seam.Ring = &changeRing{Cursors: make(map[string]uint64)}
	}
	return d.seam.Ring
}

// changeRing keeps the latest changes of one document. Each record has a
// sequence number, from base on, and each consumer a cursor: the sequence
// number of the next record it has to read.
type changeRing struct {
	Recs    []buffer.Change
	Start   int // index of the oldest record in recs
	Base    uint64
	Bytes   int
	Cursors map[string]uint64
}

func payload(c buffer.Change) int { return len(c.Text) + len(c.Removed) }

// Next is the sequence number the next record gets.
func (r *changeRing) Next() uint64 { return r.Base + uint64(len(r.Recs)-r.Start) }

func (r *changeRing) Add(c buffer.Change) {
	if payload(c) > changePayloadMax {
		c = buffer.Change{Coarse: true, Undo: c.Undo, Version: c.Version}
	}
	r.Recs = append(r.Recs, c)
	r.Bytes += payload(c)
	for n := len(r.Recs) - r.Start; n > 1 && (n > changeRingRecords || r.Bytes > changeRingBytes); n-- {
		r.Bytes -= payload(r.Recs[r.Start])
		r.Recs[r.Start] = buffer.Change{}
		r.Start++
		r.Base++
	}
	if r.Start >= changeRingRecords {
		r.Recs = append(r.Recs[:0], r.Recs[r.Start:]...)
		r.Start = 0
	}
}

func (r *changeRing) Take(consumer string) ([]buffer.Change, bool) {
	cur := r.Cursors[consumer]
	head := r.Next()
	r.Cursors[consumer] = head
	if cur < r.Base {
		return nil, true
	}
	if cur >= head {
		return nil, false
	}
	recs := r.Recs[r.Start+int(cur-r.Base):]
	total := 0
	for _, c := range recs {
		total += payload(c)
		if c.Coarse || total > changePayloadMax {
			return nil, true
		}
	}
	return slices.Clone(recs), false
}
