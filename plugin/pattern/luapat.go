// Package luapat is a Lua 5.1 pattern matcher (lstrlib.c's str_find_aux,
// gmatch and gsub) with a step budget, a cancel hook and output caps, so that
// untrusted plugin code can use string patterns without being able to hang or
// exhaust the editor. It adds %f (frontier) and %b, back references %1-%9,
// position captures and the 5.1 gmatch/gsub empty-match rules.
//
// Offsets in results are 0-based, half-open byte offsets into the subject.
// Arguments named init follow Lua: 1-based, negative counts from the end.
//
// Budget rules (a call is one Find, Match, Gsub or one GMatch iterator step):
// the first match attempt at each start position is free; steps are charged
// for backtracking and recursion. The effective budget is Options.Budget (or
// DefaultBudget) plus StepsPerByte per subject byte; exceeding it returns
// ErrBudget. A pattern without special characters is searched with
// strings.Index and needs no budget.
//
// The package imports only the standard library.
package pattern

import (
	"errors"
	"strconv"
	"strings"
)

// ErrBudget is returned when a match exceeds its step budget.
var ErrBudget = errors.New("pattern too complex")

// ErrPatternTooLong is returned for a pattern longer than MaxPattern that
// needs the matcher.
var ErrPatternTooLong = errors.New("pattern too long")

// ErrSubjectTooLong is returned for a subject longer than MaxSubject that
// needs the matcher.
var ErrSubjectTooLong = errors.New("subject too long")

// ErrOutputTooLarge is returned by Gsub when the result would exceed MaxOut.
var ErrOutputTooLarge = errors.New("resulting string too large")

// PatternError reports a malformed pattern or an invalid capture reference,
// with the wording of the reference implementation. Like in C Lua it is only
// raised when the matcher reaches the faulty part of the pattern.
type PatternError struct{ Msg string }

func (e *PatternError) Error() string { return e.Msg }

// Options bounds one call.
type Options struct {
	// Budget is the base step budget; <= 0 means DefaultBudget. The matcher
	// adds StepsPerByte for every byte of the subject.
	Budget int
	// MaxOut caps the length of a Gsub result; <= 0 means DefaultMaxOut.
	MaxOut int
	// Cancel, when not nil, is polled every 16 384 start positions and every
	// 1<<16 matcher steps. A non-nil result aborts the call and is returned
	// unchanged.
	Cancel func() error
}

// Capture is one capture of a match.
type Capture struct {
	Str   string // text; empty for a position capture
	Pos   int    // 1-based position of a "()" capture
	IsPos bool
}

// Result is a successful match.
type Result struct {
	Start, End int       // [Start, End) in the subject
	Caps       []Capture // see the function that returned it
}

// Unlimited is the maxN argument of Gsub when the caller passed no limit.
const Unlimited = int(^uint(0) >> 1)

func (o Options) maxOut() int {
	if o.MaxOut > 0 {
		return o.MaxOut
	}
	return DefaultMaxOut
}

func (o Options) state(src, pat string) *matchState {
	b := o.Budget
	if b <= 0 {
		b = DefaultBudget
	}
	return &matchState{src: src, pat: pat, budget: b + StepsPerByte*len(src), cancel: o.Cancel}
}

func checkInputs(src, pat string) error {
	if len(pat) > MaxPattern {
		return ErrPatternTooLong
	}
	if len(src) > MaxSubject {
		return ErrSubjectTooLong
	}
	return nil
}

// noSpecials is lstrlib's nospecials: no byte of "^$*+?.([%-".
func noSpecials(pat string) bool { return !strings.ContainsAny(pat, "^$*+?.([%-") }

// startIndex converts Lua's init argument (1-based, negative from the end)
// into a 0-based offset clamped to [0, n], as Lua 5.1's str_find_aux does.
func startIndex(init, n int) int {
	if init < 0 {
		init += n + 1
		if init < 0 {
			init = 0
		}
	}
	init--
	if init < 0 {
		return 0
	}
	if init > n {
		return n
	}
	return init
}

// captures is push_captures: all captures, or the whole match when the
// pattern has none and whole is true.
func (ms *matchState) captures(s, e int, whole bool) ([]Capture, error) {
	n := ms.level
	if n == 0 && whole {
		n = 1
	}
	if n == 0 {
		return nil, nil
	}
	out := make([]Capture, n)
	for i := range out {
		c, err := ms.oneCapture(i, s, e)
		if err != nil {
			return nil, err
		}
		out[i] = c
	}
	return out, nil
}

// oneCapture is get_onecapture.
func (ms *matchState) oneCapture(i, s, e int) (Capture, error) {
	if i >= ms.level {
		if i == 0 {
			return Capture{Str: ms.src[s:e]}, nil
		}
		return Capture{}, &PatternError{Msg: "invalid capture index"}
	}
	c := ms.capture[i]
	switch c.len {
	case capUnfinished:
		return Capture{}, &PatternError{Msg: "unfinished capture"}
	case capPosition:
		return Capture{Pos: c.init + 1, IsPos: true}, nil
	}
	return Capture{Str: ms.src[c.init : c.init+c.len]}, nil
}

// Find is string.find. With plain true, or when pat has no special
// characters, it is a substring search. Result.Caps holds the captures only
// (empty when the pattern has none).
func Find(s, pat string, init int, plain bool, o Options) (Result, bool, error) {
	return find(s, pat, init, plain, true, o)
}

// Match is string.match. Result.Caps holds the captures, or the whole match
// when the pattern has none.
func Match(s, pat string, init int, o Options) (Result, bool, error) {
	return find(s, pat, init, false, false, o)
}

func find(s, pat string, init int, plain, isFind bool, o Options) (Result, bool, error) {
	i0 := startIndex(init, len(s))
	if isFind && (plain || noSpecials(pat)) {
		idx := strings.Index(s[i0:], pat)
		if idx < 0 {
			return Result{}, false, nil
		}
		return Result{Start: i0 + idx, End: i0 + idx + len(pat)}, true, nil
	}
	if err := checkInputs(s, pat); err != nil {
		return Result{}, false, err
	}
	ms := o.state(s, pat)
	anchor := len(pat) > 0 && pat[0] == '^'
	p := 0
	if anchor {
		p = 1
	}
	st, e := ms.scan(i0, p, anchor, ms.prepSkip(p))
	if ms.err != nil {
		return Result{}, false, ms.err
	}
	if st < 0 {
		return Result{}, false, nil
	}
	caps, err := ms.captures(st, e, !isFind)
	if err != nil {
		return Result{}, false, err
	}
	return Result{Start: st, End: e, Caps: caps}, true, nil
}

// Iter is the state of string.gmatch. Like Lua 5.1 it does not treat a
// leading '^' as an anchor.
type Iter struct {
	ms   *matchState
	k    skipper
	pos  int
	done bool
}

// GMatch starts an iteration over the matches of pat in s. Each Next call has
// its own budget.
func GMatch(s, pat string, o Options) (*Iter, error) {
	if err := checkInputs(s, pat); err != nil {
		return nil, err
	}
	ms := o.state(s, pat)
	return &Iter{ms: ms, k: ms.prepSkip(0)}, nil
}

// Next returns the next match; Result.Caps is as for Match. After the last
// match, or after an error, it returns false.
func (it *Iter) Next() (Result, bool, error) {
	if it.done || it.pos > len(it.ms.src) {
		it.done = true
		return Result{}, false, nil
	}
	ms := it.ms
	ms.steps, ms.err = 0, nil
	st, e := ms.scan(it.pos, 0, false, it.k)
	if ms.err != nil {
		it.done = true
		return Result{}, false, ms.err
	}
	if st < 0 {
		it.done = true
		return Result{}, false, nil
	}
	it.pos = e
	if e == st { // empty match: go at least one position further
		it.pos++
	}
	caps, err := ms.captures(st, e, true)
	if err != nil {
		it.done = true
		return Result{}, false, err
	}
	return Result{Start: st, End: e, Caps: caps}, true, nil
}

// Replacement is the third argument of Gsub. When Func is nil Template is
// used: "%0" is the whole match, "%1".."%9" captures, "%%" a percent sign and
// any other byte after '%' stands for itself. A table replacement is a Func
// that looks up Caps[0].
type Replacement struct {
	Template string
	// Func receives the match (Caps as for Match) and returns the
	// replacement text. ok=false keeps the matched text. Its error aborts
	// Gsub and is returned unchanged.
	Func func(Result) (text string, ok bool, err error)
}

// capWriter is a strings.Builder that refuses to grow beyond max.
type capWriter struct {
	b   strings.Builder
	max int
}

func (w *capWriter) write(piece string) error {
	if len(piece) > w.max-w.b.Len() {
		return ErrOutputTooLarge
	}
	w.b.WriteString(piece)
	return nil
}

func (w *capWriter) writeByte(c byte) error {
	if w.b.Len() >= w.max {
		return ErrOutputTooLarge
	}
	w.b.WriteByte(c)
	return nil
}

// Gsub is string.gsub. maxN is the limit of replacements (Unlimited when the
// caller passed none; like Lua, a negative value replaces nothing). It
// returns the new string and the number of matches. The output is built in
// linear time and capped at Options.MaxOut.
func Gsub(s, pat string, r Replacement, maxN int, o Options) (string, int, error) {
	if err := checkInputs(s, pat); err != nil {
		return "", 0, err
	}
	ms := o.state(s, pat)
	anchor := len(pat) > 0 && pat[0] == '^'
	p := 0
	if anchor {
		p = 1
	}
	k := ms.prepSkip(p)
	w := &capWriter{max: o.maxOut()}
	if len(s) <= w.max {
		w.b.Grow(len(s))
	}
	n, src := 0, 0
	for it := 1; n < maxN; it++ {
		last := false
		if !anchor && k.kind != 0 {
			c := ms.advance(k, src)
			if c < 0 {
				if ms.err != nil {
					return "", 0, ms.err
				}
				break
			}
			if c > src {
				if err := w.write(s[src:c]); err != nil {
					return "", 0, err
				}
				src = c
			}
		}
		ms.level = 0
		e := ms.match(src, p)
		if ms.err != nil {
			return "", 0, ms.err
		}
		if e != -1 {
			n++
			if err := ms.addValue(w, r, src, e); err != nil {
				return "", 0, err
			}
		}
		switch {
		case e != -1 && e > src:
			src = e
		case src < len(s):
			if err := w.writeByte(s[src]); err != nil {
				return "", 0, err
			}
			src++
		default:
			last = true
		}
		if last || anchor {
			break
		}
		if it&(cancelScanEvery-1) == 0 && ms.pollCancel() {
			return "", 0, ms.err
		}
	}
	if n == 0 {
		return s, 0, nil
	}
	if err := w.write(s[src:]); err != nil {
		return "", 0, err
	}
	return w.b.String(), n, nil
}

// addValue is add_value: it appends the replacement for the match s..e.
func (ms *matchState) addValue(w *capWriter, r Replacement, s, e int) error {
	if r.Func != nil {
		caps, err := ms.captures(s, e, true)
		if err != nil {
			return err
		}
		text, ok, err := r.Func(Result{Start: s, End: e, Caps: caps})
		if err != nil {
			return err
		}
		if !ok {
			text = ms.src[s:e]
		}
		return w.write(text)
	}
	t := r.Template
	if strings.IndexByte(t, '%') < 0 {
		return w.write(t)
	}
	for i := 0; i < len(t); i++ {
		c := t[i]
		if c != '%' {
			if err := w.writeByte(c); err != nil {
				return err
			}
			continue
		}
		i++
		if i >= len(t) {
			return &PatternError{Msg: "invalid use of '%' in replacement string"}
		}
		d := t[i]
		switch {
		case !isdigit(d):
			if err := w.writeByte(d); err != nil {
				return err
			}
		case d == '0':
			if err := w.write(ms.src[s:e]); err != nil {
				return err
			}
		default:
			c, err := ms.oneCapture(int(d-'1'), s, e)
			if err != nil {
				return err
			}
			text := c.Str
			if c.IsPos {
				text = strconv.Itoa(c.Pos)
			}
			if err := w.write(text); err != nil {
				return err
			}
		}
	}
	return nil
}
