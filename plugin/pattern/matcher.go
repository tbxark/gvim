package pattern

import (
	"strings"
)

// Limits of the matcher. They are the numbers of the plugin sandbox spec
// (pattern <= 256 bytes, subject <= 16 MiB, 2 M steps plus 8 per subject byte).
const (
	// DefaultBudget is the base step budget used when Options.Budget <= 0.
	DefaultBudget = 2_000_000
	// StepsPerByte is added to the base budget for every byte of the subject.
	StepsPerByte = 8
	// MaxPattern is the longest pattern the matcher accepts.
	MaxPattern = 256
	// MaxSubject is the longest subject the matcher accepts.
	MaxSubject = 16 << 20
	// DefaultMaxOut is the Gsub output cap used when Options.MaxOut <= 0.
	DefaultMaxOut = 64 << 20

	maxCaptures   = 32
	capUnfinished = -1
	capPosition   = -2

	// cancelScanEvery is how many start positions the scan loops visit between
	// two polls of Options.Cancel. It must be a power of two.
	cancelScanEvery = 1 << 14
	// balanceChunk is how many bytes a %b scan covers between two charges and
	// cancel polls. It must be a power of two.
	balanceChunk = 1 << 10
	// balanceCost is how many %b-scanned bytes cost one step.
	balanceCost = 8
	// cancelStepMask makes the matcher poll Options.Cancel every 1<<16 steps.
	cancelStepMask = 1<<16 - 1
)

type capture struct{ init, len int }

// matchState is a port of lstrlib.c's MatchState. Offsets are 0-based indexes
// into src and pat; -1 plays the role of C's NULL.
//
// Recursion depth of match is bounded by len(pat)+1 because every recursive
// call advances p by at least one byte, and the public entry points cap
// len(pat) at MaxPattern; no depth counter is needed.
type matchState struct {
	src, pat string
	level    int
	capture  [maxCaptures]capture

	steps  int
	budget int
	cancel func() error
	err    error
}

func (ms *matchState) fail(msg string) {
	if ms.err == nil {
		ms.err = &PatternError{Msg: msg}
	}
}

// tick charges one step. It reports false when the budget is exhausted or the
// cancel hook fired; ms.err is set in that case.
func (ms *matchState) tick() bool {
	ms.steps++
	if ms.steps > ms.budget {
		if ms.err == nil {
			ms.err = ErrBudget
		}
		return false
	}
	if ms.steps&cancelStepMask == 0 && ms.cancel != nil {
		if err := ms.cancel(); err != nil {
			if ms.err == nil {
				ms.err = err
			}
			return false
		}
	}
	return true
}

// chargeScan charges n scanned bytes of a %b scan (balanceCost bytes per
// step). It reports false when the budget is exhausted or the cancel hook
// fired; ms.err is set in that case.
func (ms *matchState) chargeScan(n int) bool {
	ms.steps += n / balanceCost
	if ms.steps > ms.budget {
		if ms.err == nil {
			ms.err = ErrBudget
		}
		return false
	}
	if n >= balanceChunk && ms.pollCancel() {
		return false
	}
	return true
}

// pollCancel is called by the scan loops; it reports true when the scan must stop.
func (ms *matchState) pollCancel() bool {
	if ms.cancel == nil {
		return false
	}
	if err := ms.cancel(); err != nil {
		if ms.err == nil {
			ms.err = err
		}
		return true
	}
	return false
}

// sub is a charged recursive call into match.
func (ms *matchState) sub(s, p int) int {
	if !ms.tick() {
		return -1
	}
	return ms.match(s, p)
}

func (ms *matchState) classEnd(p int) int {
	pat := ms.pat
	c := pat[p]
	p++
	switch c {
	case '%':
		if p >= len(pat) {
			ms.fail("malformed pattern (ends with '%')")
			return len(pat)
		}
		return p + 1
	case '[':
		if p < len(pat) && pat[p] == '^' {
			p++
		}
		for {
			if p >= len(pat) {
				ms.fail("malformed pattern (missing ']')")
				return len(pat)
			}
			c := pat[p]
			p++
			if c == '%' && p < len(pat) {
				p++
			}
			if p < len(pat) && pat[p] == ']' {
				break
			}
		}
		return p + 1
	}
	return p
}

func isalpha(c byte) bool  { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func isdigit(c byte) bool  { return c >= '0' && c <= '9' }
func islower(c byte) bool  { return c >= 'a' && c <= 'z' }
func isupper(c byte) bool  { return c >= 'A' && c <= 'Z' }
func isspace(c byte) bool  { return c == ' ' || c >= 9 && c <= 13 }
func iscntrl(c byte) bool  { return c < 32 || c == 127 }
func ispunct(c byte) bool  { return c > 32 && c < 127 && !isalpha(c) && !isdigit(c) }
func isxdigit(c byte) bool { return isdigit(c) || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }

// matchClass is match_class with the C locale: classes a c d l p s u w x z and
// their upper-case complements (z, the NUL byte, included); any other byte after '%' matches itself.
func matchClass(c, cl byte) bool {
	var res bool
	switch cl | 0x20 {
	case 'a':
		res = isalpha(c)
	case 'c':
		res = iscntrl(c)
	case 'd':
		res = isdigit(c)
	case 'l':
		res = islower(c)
	case 'p':
		res = ispunct(c)
	case 's':
		res = isspace(c)
	case 'u':
		res = isupper(c)
	case 'w':
		res = isalpha(c) || isdigit(c)
	case 'x':
		res = isxdigit(c)
	case 'z': // deprecated in 5.1 but still there
		res = c == 0
	default:
		return cl == c
	}
	if isupper(cl) {
		return !res
	}
	return res
}

// matchBracketClass tests c against the set pat[p:ec+1] where pat[p] is '['
// and pat[ec] is the closing ']'.
func (ms *matchState) matchBracketClass(c byte, p, ec int) bool {
	pat := ms.pat
	sig := true
	if pat[p+1] == '^' {
		sig = false
		p++
	}
	for p++; p < ec; p++ {
		switch {
		case pat[p] == '%':
			p++
			if matchClass(c, pat[p]) {
				return sig
			}
		case pat[p+1] == '-' && p+2 < ec:
			p += 2
			if pat[p-2] <= c && c <= pat[p] {
				return sig
			}
		case pat[p] == c:
			return sig
		}
	}
	return !sig
}

func (ms *matchState) singleMatch(s, p, ep int) bool {
	if s >= len(ms.src) {
		return false
	}
	c := ms.src[s]
	switch ms.pat[p] {
	case '.':
		return true
	case '%':
		return matchClass(c, ms.pat[p+1])
	case '[':
		return ms.matchBracketClass(c, p, ep-1)
	default:
		return ms.pat[p] == c
	}
}

func (ms *matchState) matchBalance(s, p int) int {
	if p+1 >= len(ms.pat) {
		ms.fail("malformed pattern (missing arguments to '%b')")
		return -1
	}
	if s >= len(ms.src) || ms.src[s] != ms.pat[p] {
		return -1
	}
	b, e := ms.pat[p], ms.pat[p+1]
	cont := 1
	from := s
	for s++; s < len(ms.src); s++ {
		switch ms.src[s] {
		case e:
			cont--
			if cont == 0 {
				return s + 1
			}
		case b:
			cont++
		}
		if (s-from)&(balanceChunk-1) == 0 && !ms.chargeScan(balanceChunk) {
			return -1
		}
	}
	ms.chargeScan((s - from) & (balanceChunk - 1))
	return -1
}

// nextLiteral reports the byte the pattern item at p must match when that
// item is a plain, unquantified literal (an ordinary byte or an escaped
// punctuation byte). It lets maxExpand skip backtrack positions that cannot
// match without recursing; the result of the match is unchanged.
func (ms *matchState) nextLiteral(p int) (byte, bool) {
	pat := ms.pat
	if p >= len(pat) {
		return 0, false
	}
	c := pat[p]
	n := p + 1
	switch c {
	case '(', ')', '[', '.', '^':
		return 0, false
	case '$':
		if n == len(pat) {
			return 0, false
		}
	case '%':
		if n >= len(pat) {
			return 0, false
		}
		c = pat[n]
		if isalpha(c) || isdigit(c) {
			return 0, false
		}
		n++
	}
	if n < len(pat) && (pat[n] == '*' || pat[n] == '?' || pat[n] == '-') {
		return 0, false
	}
	return c, true
}

func (ms *matchState) maxExpand(s, p, ep int) int {
	i := 0
	for ms.singleMatch(s+i, p, ep) {
		if !ms.tick() {
			return -1
		}
		i++
	}
	lit, hasLit := ms.nextLiteral(ep + 1)
	for ; i >= 0; i-- {
		if hasLit && (s+i >= len(ms.src) || ms.src[s+i] != lit) {
			continue
		}
		if res := ms.sub(s+i, ep+1); res != -1 || ms.err != nil {
			return res
		}
	}
	return -1
}

func (ms *matchState) minExpand(s, p, ep int) int {
	for {
		if res := ms.sub(s, ep+1); res != -1 || ms.err != nil {
			return res
		}
		if !ms.singleMatch(s, p, ep) {
			return -1
		}
		s++
		if !ms.tick() {
			return -1
		}
	}
}

func (ms *matchState) startCapture(s, p, what int) int {
	if ms.level >= maxCaptures {
		ms.fail("too many captures")
		return -1
	}
	ms.capture[ms.level] = capture{s, what}
	ms.level++
	res := ms.sub(s, p)
	if res == -1 {
		ms.level--
	}
	return res
}

func (ms *matchState) captureToClose() int {
	for l := ms.level - 1; l >= 0; l-- {
		if ms.capture[l].len == capUnfinished {
			return l
		}
	}
	ms.fail("invalid pattern capture")
	return -1
}

func (ms *matchState) endCapture(s, p int) int {
	l := ms.captureToClose()
	if l < 0 {
		return -1
	}
	ms.capture[l].len = s - ms.capture[l].init
	res := ms.sub(s, p)
	if res == -1 {
		ms.capture[l].len = capUnfinished
	}
	return res
}

// matchCapture implements the back reference %1..%9 inside a pattern.
func (ms *matchState) matchCapture(s int, d byte) int {
	l := int(d) - '1'
	if l < 0 || l >= ms.level || ms.capture[l].len == capUnfinished {
		ms.fail("invalid capture index")
		return -1
	}
	c := ms.capture[l]
	if c.len < 0 { // a position capture is never equal to text
		return -1
	}
	if len(ms.src)-s >= c.len && ms.src[c.init:c.init+c.len] == ms.src[s:s+c.len] {
		return s + c.len
	}
	return -1
}

// match tries to match pat[p:] at src[s:] and returns the end of the match or
// -1. A pattern error or an exhausted budget sets ms.err.
func (ms *matchState) match(s, p int) int {
	pat := ms.pat
	for {
		if p == len(pat) {
			return s
		}
		switch pat[p] {
		case '(':
			if p+1 < len(pat) && pat[p+1] == ')' {
				return ms.startCapture(s, p+2, capPosition)
			}
			return ms.startCapture(s, p+1, capUnfinished)
		case ')':
			return ms.endCapture(s, p+1)
		case '$':
			if p+1 == len(pat) {
				if s == len(ms.src) {
					return s
				}
				return -1
			}
		case '%':
			if p+1 < len(pat) {
				switch d := pat[p+1]; {
				case d == 'b':
					if s = ms.matchBalance(s, p+2); s == -1 {
						return -1
					}
					p += 4
					continue
				case d == 'f':
					p += 2
					if p >= len(pat) || pat[p] != '[' {
						ms.fail("missing '[' after '%f' in pattern")
						return -1
					}
					ep := ms.classEnd(p)
					if ms.err != nil {
						return -1
					}
					var prev, cur byte
					if s > 0 {
						prev = ms.src[s-1]
					}
					if s < len(ms.src) {
						cur = ms.src[s]
					}
					if !ms.matchBracketClass(prev, p, ep-1) && ms.matchBracketClass(cur, p, ep-1) {
						p = ep
						continue
					}
					return -1
				case isdigit(d):
					if s = ms.matchCapture(s, d); s == -1 {
						return -1
					}
					p += 2
					continue
				}
			}
		}
		// default: a single character class, possibly quantified
		ep := ms.classEnd(p)
		if ms.err != nil {
			return -1
		}
		var q byte
		if ep < len(pat) {
			q = pat[ep]
		}
		if !ms.singleMatch(s, p, ep) {
			if q == '*' || q == '?' || q == '-' {
				p = ep + 1
				continue
			}
			return -1
		}
		switch q {
		case '?':
			if res := ms.sub(s+1, ep+1); res != -1 || ms.err != nil {
				return res
			}
			p = ep + 1
		case '+':
			return ms.maxExpand(s+1, p, ep)
		case '*':
			return ms.maxExpand(s, p, ep)
		case '-':
			return ms.minExpand(s, p, ep)
		default:
			s++
			p = ep
		}
	}
}

// skipper lets the scan loops jump to the next start position that can begin
// a match when the pattern starts with a plain single-character item.
type skipper struct {
	kind uint8 // 0 none, 1 literal byte, 2 single-character class
	b    byte
	p    int
	ep   int
}

// prepSkip inspects the first item of pat[p:]. It never reports an error: a
// malformed first item simply disables skipping and the matcher reports it.
func (ms *matchState) prepSkip(p int) skipper {
	pat := ms.pat
	if p >= len(pat) {
		return skipper{}
	}
	c := pat[p]
	switch c {
	case '(', ')', '$', '^', '.':
		return skipper{}
	case '%':
		if p+1 >= len(pat) {
			return skipper{}
		}
		if d := pat[p+1]; d == 'b' || d == 'f' || isdigit(d) {
			return skipper{}
		}
	}
	ep := ms.classEnd(p)
	if ms.err != nil {
		ms.err = nil
		return skipper{}
	}
	if ep < len(pat) && (pat[ep] == '*' || pat[ep] == '?' || pat[ep] == '-') {
		return skipper{}
	}
	if c == '%' || c == '[' {
		return skipper{kind: 2, p: p, ep: ep}
	}
	return skipper{kind: 1, b: c}
}

// advance returns the first start position >= s that can begin a match, or -1.
func (ms *matchState) advance(k skipper, s int) int {
	if s > len(ms.src) {
		return -1
	}
	if k.kind == 1 {
		i := strings.IndexByte(ms.src[s:], k.b)
		if i < 0 {
			return -1
		}
		return s + i
	}
	for n := 1; s < len(ms.src); s++ {
		if ms.singleMatch(s, k.p, k.ep) {
			return s
		}
		if n++; n&(cancelScanEvery-1) == 0 && ms.pollCancel() {
			return -1
		}
	}
	return -1
}

// scan looks for the first match starting at or after s. It returns the match
// bounds, or -1,-1 when there is none or ms.err is set.
func (ms *matchState) scan(s, p int, anchor bool, k skipper) (int, int) {
	for n := 1; ; n++ {
		if !anchor && k.kind != 0 {
			if s = ms.advance(k, s); s < 0 {
				return -1, -1
			}
		}
		ms.level = 0
		// The first attempt at each start position is free; only
		// backtracking and recursion are charged.
		if e := ms.match(s, p); e != -1 {
			return s, e
		}
		if ms.err != nil || anchor || s >= len(ms.src) {
			return -1, -1
		}
		s++
		if n&(cancelScanEvery-1) == 0 && ms.pollCancel() {
			return -1, -1
		}
	}
}
