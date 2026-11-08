package buffer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"unicode/utf8"
)

// writeStep is how many bytes WriteTo copies between checks of its context;
// tests lower it.
var writeStep int64 = 1 << 20

// errNotLarge is returned by operations that only a large buffer supports.
var errNotLarge = errors.New("buffer: not a large file buffer")

// WriteOptions control how Snapshot.WriteTo encodes a large snapshot.
type WriteOptions struct {
	// EOL ends a literal line that has no terminator of its own: "\n" (the
	// default when empty) or "\r\n". Lines read from the source keep the
	// terminator they have, and a line that replaced one keeps its flag.
	EOL string
	// Prefix is written first and is not part of the text, normally a UTF-8
	// BOM. It is not counted in WriteResult.Counts.
	Prefix []byte
	// AllowGrown lets the write go on when the source grew after it was opened;
	// only the first Size bytes are used. Otherwise a grown source fails with
	// ErrSourceGrew.
	AllowGrown bool
}

// WriteResult describes what Snapshot.WriteTo wrote, so Rebase can adopt the
// new file without scanning it.
type WriteResult struct {
	// Bytes is the number of bytes written, Prefix included.
	Bytes int64
	// Chunk is the chunk size Counts is cut in.
	Chunk int64
	// Counts is the number of '\n' bytes in each Chunk-sized piece of the
	// output after Prefix.
	Counts []int32

	huge bool // a line over MaxLine may be in the output
}

// WriteTo streams the snapshot's text to w. Lines read from the source and not
// edited are copied from it byte for byte, terminators included; edited and
// new lines are encoded as UTF-8 and end with their own terminator, or with
// o.EOL, except the last line of the document, which has none. The source is
// checked before and after, so a file that changed under the write fails
// instead of producing a mixed output. The snapshot must be complete: a
// partly indexed one fails with ErrIndexPending.
//
// It checks ctx between steps of about 1 MiB and returns ctx's error then. The
// result counts the output's newlines by chunk. On error w may have received
// part of the output.
//
// (The name follows the other streaming methods; Snapshot is not an
// io.WriterTo.)
func (s *Snapshot) WriteTo(ctx context.Context, w io.Writer, o WriteOptions) (WriteResult, error) {
	if s.big == nil {
		return WriteResult{}, errNotLarge
	}
	return s.big.writeTo(ctx, w, o)
}

// teeCounter writes to w and counts the '\n' bytes it passes per chunk.
type teeCounter struct {
	w      io.Writer
	chunk  int64
	pos    int64 // bytes written so far
	counts []int32
}

func (t *teeCounter) Write(p []byte) (int, error) {
	n, err := t.w.Write(p)
	if n > len(p) || n < 0 {
		n = len(p)
	}
	t.count(p[:n])
	if err == nil && n < len(p) {
		err = io.ErrShortWrite
	}
	return n, err
}

func (t *teeCounter) count(p []byte) {
	for len(p) > 0 {
		c := int(t.pos / t.chunk)
		if c == len(t.counts) {
			t.counts = append(t.counts, 0)
		}
		take := int(min(int64(len(p)), t.chunk-t.pos%t.chunk))
		t.counts[c] += int32(bytes.Count(p[:take], []byte{'\n'}))
		t.pos += int64(take)
		p = p[take:]
	}
}

// checkSource compares the source with its state at open time.
func (g *bigSnap) checkSource(allowGrown bool) error {
	err := g.r.src.src.Check()
	if allowGrown && errors.Is(err, ErrSourceGrew) {
		return nil
	}
	return err
}

func (g *bigSnap) writeTo(ctx context.Context, w io.Writer, o WriteOptions) (WriteResult, error) {
	r := &g.r
	switch {
	case !r.g.complete():
		return WriteResult{}, ErrIndexPending
	case r.frozen != nil:
		return WriteResult{}, r.frozen
	}
	eol := o.EOL
	if eol == "" {
		eol = "\n"
	}
	if err := g.checkSource(o.AllowGrown); err != nil {
		return WriteResult{}, err
	}
	if len(o.Prefix) > 0 {
		if _, err := w.Write(o.Prefix); err != nil {
			return WriteResult{}, err
		}
	}
	tee := &teeCounter{w: w, chunk: r.g.chunk}
	wr := &snapWriter{g: g, tee: tee, eol: eol, total: g.lines()}
	err := wr.writeAll(ctx)
	if err == nil {
		err = g.checkSource(o.AllowGrown)
	}
	res := WriteResult{
		Bytes:  int64(len(o.Prefix)) + tee.pos,
		Chunk:  r.g.chunk,
		Counts: tee.counts,
		huge:   g.hugeBase || wr.huge,
	}
	return res, err
}

// snapWriter holds the state of one WriteTo.
type snapWriter struct {
	g     *bigSnap
	tee   *teeCounter
	eol   string
	total int  // document lines
	line  int  // document lines written so far
	huge  bool // a literal line over MaxLine was written
	lit   []byte
	cp    []byte
}

func (wr *snapWriter) writeAll(ctx context.Context) error {
	if err := wr.walk(ctx, wr.g.st.root); err != nil {
		return err
	}
	if tail := wr.g.r.g.baseLines() - wr.g.st.tailFirst; tail > 0 {
		if err := wr.run(ctx, wr.g.st.tailFirst, tail); err != nil {
			return err
		}
	}
	return wr.flush()
}

// walk writes the pieces of n in document order.
func (wr *snapWriter) walk(ctx context.Context, n *node) error {
	if n == nil {
		return nil
	}
	if err := wr.walk(ctx, n.l); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var err error
	if n.p.isRun() {
		err = wr.run(ctx, n.p.first, n.p.count)
	} else {
		err = wr.literals(ctx, n.p)
	}
	if err != nil {
		return err
	}
	return wr.walk(ctx, n.r)
}

// flush writes the buffered literal lines.
func (wr *snapWriter) flush() error {
	if len(wr.lit) == 0 {
		return nil
	}
	_, err := wr.tee.Write(wr.lit)
	wr.lit = wr.lit[:0]
	return err
}

// literals writes the lines of a literal piece.
func (wr *snapWriter) literals(ctx context.Context, p piece) error {
	for k, runes := range p.lit {
		for _, r := range runes {
			wr.lit = utf8.AppendRune(wr.lit, r)
		}
		if p.nb[k] > int(wr.g.r.maxLine) {
			wr.huge = true
		}
		wr.line++
		if wr.line < wr.total {
			switch p.eolAt(k) {
			case eolLF:
				wr.lit = append(wr.lit, '\n')
			case eolCRLF:
				wr.lit = append(wr.lit, '\r', '\n')
			default:
				wr.lit = append(wr.lit, wr.eol...)
			}
		}
		if int64(len(wr.lit)) >= writeStep {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := wr.flush(); err != nil {
				return err
			}
		}
	}
	return nil
}

// runSpan returns the source bytes of base lines [first, first+count),
// terminators included. The index is complete.
func (r *baseReader) runSpan(first, count int) (start, end int64, err error) {
	g := r.g
	start = g.skip
	if first > 0 {
		off, err := r.newlineOffset(int64(first))
		if err != nil {
			return 0, 0, err
		}
		start = off + 1
	}
	end = g.skip + g.size
	if int64(first+count) <= g.nl[g.nChunks()] {
		off, err := r.newlineOffset(int64(first + count))
		if err != nil {
			return 0, 0, err
		}
		end = off + 1
	}
	return start, end, nil
}

// run copies base lines [first, first+count) from the source as they are.
func (wr *snapWriter) run(ctx context.Context, first, count int) error {
	if err := wr.flush(); err != nil {
		return err
	}
	r := &wr.g.r
	start, end, err := r.runSpan(first, count)
	if err != nil {
		return err
	}
	wr.line += count
	for off := start; off < end; {
		if err := ctx.Err(); err != nil {
			return err
		}
		n := min(writeStep, end-off)
		if int64(cap(wr.cp)) < n {
			wr.cp = make([]byte, n)
		}
		buf := wr.cp[:n]
		if err := readFull(r.src.src, r.stats, buf, off); err != nil {
			if !errors.Is(err, ErrSourcePending) {
				return err
			}
			// An asynchronous window source may not have this range yet. The write
			// runs in a command (never on the update loop), so loading the window
			// here is allowed; the read is then retried at the same offset.
			ws, ok := r.src.src.(WindowSource)
			if !ok {
				return err
			}
			poff, pn, pending := ws.TakePendingWindow()
			if !pending || pn <= 0 {
				return err
			}
			if loadErr := ws.LoadWindow(ctx, poff, pn); loadErr != nil {
				return loadErr
			}
			continue
		}
		if _, err := wr.tee.Write(buf); err != nil {
			return err
		}
		off += n
	}
	return nil
}
