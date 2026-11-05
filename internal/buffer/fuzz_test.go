package buffer

import (
	"strings"
	"testing"
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
