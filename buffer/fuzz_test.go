package buffer

import (
	"strings"
	"testing"
	"time"
)

// model is a naive reference: the whole document as runes, with '\n' between
// lines. Positions map to indexes by summing line lengths.
type model []rune

func (m model) index(p Pos) int {
	idx, line := 0, 0
	for i, r := range m {
		if line == p.Line {
			break
		}
		if r == '\n' {
			line++
		}
		idx = i + 1
	}
	return idx + p.Col
}

func (m model) insert(p Pos, s string) model {
	i := m.index(p)
	out := append(model{}, m[:i]...)
	out = append(out, []rune(s)...)
	return append(out, m[i:]...)
}

func (m model) remove(from, to int) model {
	return append(append(model{}, m[:from]...), m[to:]...)
}

// FuzzBufferEdits drives random inserts and deletes and checks the buffer
// against the naive model after every step. Then it undoes everything and
// checks that the original text comes back.
func FuzzBufferEdits(f *testing.F) {
	f.Add("hello\nworld", []byte{0, 1, 2, 3, 4, 5})
	f.Add("", []byte{0, 0, 0, 1, 1})
	f.Add("héllo 世界\n\nx", []byte{9, 8, 7, 6, 5, 4, 3, 2, 1, 0})
	f.Fuzz(func(t *testing.T, initial string, ops []byte) {
		if !validUTF8(initial) {
			t.Skip()
		}
		b := New(initial)
		m := model([]rune(initial))
		original := initial
		// seen holds every document state the sequence has produced. An undo
		// may only return to one of them.
		seen := map[string]bool{initial: true}

		for i := 0; i+2 < len(ops); i += 3 {
			lines := b.Lines()
			line := int(ops[i]) % lines
			col := int(ops[i+1]) % (b.LineLen(line) + 1)
			p := Pos{line, col}
			switch ops[i+2] % 4 {
			case 0:
				s := []string{"a", " ", "\n", "é", "世"}[ops[i+1]%5]
				b.Insert(p, s)
				m = m.insert(p, s)
			case 1:
				q := b.End()
				if ops[i]%2 == 0 {
					q = Pos{line, min(col+2, b.LineLen(line))}
				}
				from, to := m.index(p), m.index(q)
				if from > to {
					from, to = to, from
				}
				b.Delete(p, q)
				m = m.remove(from, to)
			case 2:
				b.Break()
			default:
				if _, ok := b.Undo(); ok {
					// The model does not replay history, so accept any earlier state.
					text := b.Text()
					if !seen[text] {
						t.Fatalf("step %d: undo produced unseen state %q", i/3, text)
					}
					m = model(text)
				}
			}
			if got := b.Text(); got != string(m) {
				t.Fatalf("step %d: Text() = %q, model = %q", i/3, got, string(m))
			}
			seen[string(m)] = true
		}

		// Undo must always restore a prior state; undoing all edits must
		// return to the original text.
		b.Break()
		for {
			if _, ok := b.Undo(); !ok {
				break
			}
		}
		if got := b.Text(); got != original {
			t.Fatalf("undo-all = %q, want %q", got, original)
		}
		if b.Modified() {
			t.Fatal("buffer back at original text with no save should match its initial version")
		}
	})
}

func validUTF8(s string) bool {
	return strings.ToValidUTF8(s, "") == s
}

// FuzzLargeBufferEdits drives the same random edits on a memory buffer and a
// large buffer over the same text and compares them after every step,
// including Group steps that a large buffer refuses and so must undo whole.
func FuzzLargeBufferEdits(f *testing.F) {
	f.Add("hello\nworld", []byte{0, 1, 2, 3, 4, 5, 6, 7, 8})
	f.Add("", []byte{0, 0, 0, 1, 1, 1, 5, 2, 0})
	f.Add("héllo 世界\n\nx\n", []byte{9, 8, 7, 6, 5, 4, 3, 2, 1, 0, 1, 2})
	f.Fuzz(func(t *testing.T, initial string, ops []byte) {
		if !validUTF8(initial) || strings.Contains(initial, "\r") {
			t.Skip()
		}
		for _, l := range strings.Split(initial, "\n") {
			if len(l) > 60 {
				t.Skip()
			}
		}
		clock := func() time.Time { return time.Unix(1_700_000_000, 0) }
		b := tinyOpen(t, initial)
		indexFully(t, b)
		m := New(initial)
		b.SetClock(clock)
		m.SetClock(clock)
		mdl := model([]rune(initial))

		for i := 0; i+2 < len(ops); i += 3 {
			line := int(ops[i]) % m.Lines()
			col := int(ops[i+1]) % (m.LineLen(line) + 1)
			p := Pos{line, col}
			resync := false // undo and redo move the memory buffer past the model
			switch ops[i+2] % 7 {
			case 0:
				s := []string{"a", " ", "\n", "é", "世", "xy\nz"}[ops[i+1]%6]
				if got, want := b.Insert(p, s), m.Insert(p, s); got != want {
					t.Fatalf("step %d: Insert returned %v, want %v", i/3, got, want)
				}
				mdl = mdl.insert(p, s)
			case 1:
				q := m.End()
				if ops[i]%2 == 0 {
					q = Pos{line, min(col+2, m.LineLen(line))}
				}
				from, to := mdl.index(p), mdl.index(q)
				if from > to {
					from, to = to, from
				}
				if got, want := b.Delete(p, q), m.Delete(p, q); got != want {
					t.Fatalf("step %d: Delete returned %q, want %q", i/3, got, want)
				}
				mdl = mdl.remove(from, to)
			case 2:
				b.Break()
				m.Break()
			case 3:
				gp, gok := b.Undo()
				wp, wok := m.Undo()
				if gp != wp || gok != wok {
					t.Fatalf("step %d: Undo = %v %v, want %v %v", i/3, gp, gok, wp, wok)
				}
				resync = gok
			case 4:
				gp, gok := b.Redo()
				wp, wok := m.Redo()
				if gp != wp || gok != wok {
					t.Fatalf("step %d: Redo = %v %v, want %v %v", i/3, gp, gok, wp, wok)
				}
				resync = gok
			case 5:
				// A group both buffers accept is one step on each.
				b.Group(func() { b.Insert(p, "g"); b.Insert(p, "h\n") })
				m.Group(func() { m.Insert(p, "g"); m.Insert(p, "h\n") })
				mdl = mdl.insert(p, "g").insert(p, "h\n")
			default:
				// A group the large buffer refuses (its second step reads more
				// than the cap) leaves it unchanged, and the memory buffer is
				// simply not asked to run it.
				old := materializeCap
				materializeCap = 1
				b.Group(func() { b.Insert(p, "zz"); _ = b.Text() })
				materializeCap = old
				_ = b.Refusal()
			}
			if got := b.Text(); got != m.Text() {
				t.Fatalf("step %d: large Text() = %q, memory = %q", i/3, got, m.Text())
			}
			if resync {
				mdl = model(m.Text())
			}
			if m.Text() != string(mdl) {
				t.Fatalf("step %d: memory Text() = %q, model = %q", i/3, m.Text(), string(mdl))
			}
			if b.Modified() != m.Modified() {
				t.Fatalf("step %d: Modified = %v, memory %v", i/3, b.Modified(), m.Modified())
			}
		}
		b.Break()
		m.Break()
		for {
			gp, gok := b.Undo()
			wp, wok := m.Undo()
			if gp != wp || gok != wok {
				t.Fatalf("undo-all: %v %v, want %v %v", gp, gok, wp, wok)
			}
			if !gok {
				break
			}
		}
		if b.Text() != initial || b.Modified() {
			t.Fatalf("undo-all = %q (modified %v), want %q", b.Text(), b.Modified(), initial)
		}
	})
}
