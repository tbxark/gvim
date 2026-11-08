package plugin

import (
	"regexp"
	"regexp/syntax"
	"strconv"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

// re: Go RE2 for Lua (linear time), with work caps (AMENDMENTS C7).
//
// A pattern is compiled with regexp/syntax first; a program of more than reMaxProg
// instructions is refused at compile time. Before every operation
// work = len(subject) * len(program) must stay within reMaxWork, otherwise the call returns
// `nil, "limit: subject too large for this pattern; ..."`. A pattern that is a plain
// literal is a strings.Index and only the subject cap applies. Measured cost is about 6 ns
// per unit of work, so the cap is about 0.25 s worst case.
//
// Failures are returned as `nil, "<code>: <message>"` (invalid pattern, limit); re.gmatch
// raises instead, because it returns an iterator.
const (
	reMaxPattern = 4 << 10
	reMaxSubject = 16 << 20
	reMaxProg    = 2000
	reMaxWork    = 40_000_000
	reMaxMatches = 1_000_000
	reMaxIndexes = 4_000_000 // capture indexes held at once: matches x 2 x (groups + 1)
	reCacheSize  = 64
	kindRegex    = "Regex"
)

// reProg is a compiled regular expression with the numbers the work cap needs.
type reProg struct {
	re      *regexp.Regexp
	src     string
	nInst   int
	literal bool // the whole pattern is a literal
}

func (r *reProg) handleString() string { return strconv.Quote(r.src) }

func compileRE(pat, flags string) (*reProg, error) {
	if len(pat) > reMaxPattern {
		return nil, limitErr("pattern longer than %d bytes", reMaxPattern)
	}
	full := pat
	if flags != "" {
		full = "(?" + flags + ")" + pat
	}
	parsed, err := syntax.Parse(full, syntax.Perl)
	if err != nil {
		return nil, invalidErr("%s", err.Error())
	}
	prog, err := syntax.Compile(parsed.Simplify())
	if err != nil {
		return nil, invalidErr("%s", err.Error())
	}
	if len(prog.Inst) > reMaxProg {
		return nil, limitErr("pattern too complex")
	}
	re, err := regexp.Compile(full)
	if err != nil {
		return nil, invalidErr("%s", err.Error())
	}
	_, complete := re.LiteralPrefix()
	return &reProg{re: re, src: pat, nInst: len(prog.Inst), literal: complete}, nil
}

// cached compiles pat with flags through the per-state cache.
func (d *dataState) cached(pat, flags string) (*reProg, error) {
	key := flags + "\x00" + pat
	if r, ok := d.re[key]; ok {
		return r, nil
	}
	r, err := compileRE(pat, flags)
	if err != nil {
		return nil, err
	}
	if d.re == nil {
		d.re = map[string]*reProg{}
	}
	if len(d.re) >= reCacheSize {
		for k := range d.re { // arbitrary eviction
			delete(d.re, k)
			break
		}
	}
	d.re[key] = r
	return r, nil
}

// check applies the subject and work caps.
func (r *reProg) check(s string) error {
	if len(s) > reMaxSubject {
		return limitErr("subject larger than %d bytes", reMaxSubject)
	}
	if !r.literal && int64(len(s))*int64(r.nInst) > reMaxWork {
		return limitErr("subject too large for this pattern; use b:find_lines/b:find or scan line by line")
	}
	return nil
}

// reArg resolves argument n (a pattern string or a Regex) of a static re function.
func (p *plugin) reArg(L *lua.LState, n int) (*reProg, error) {
	switch v := L.Get(n).(type) {
	case lua.LString:
		return p.data.cached(string(v), "")
	case *lua.LUserData:
		if o, ok := v.Value.(*object); ok && o.kind == kindRegex {
			return o.value.(*reProg), nil
		}
	}
	L.ArgError(n, "pattern string or Regex expected")
	return nil, nil
}

func installRE(e *apiEnv) *lua.LTable {
	p, L := e.p, e.L
	methods := e.kind(kindRegex).methods
	t := L.NewTable()

	e.set(t, "compile", func(L *lua.LState) int {
		pat := L.CheckString(1)
		flags := L.OptString(2, "")
		for i := 0; i < len(flags); i++ {
			if !strings.ContainsRune("ims", rune(flags[i])) {
				L.ArgError(2, "flags may contain i, m and s")
			}
		}
		r, err := p.data.cached(pat, flags)
		if err != nil {
			return errReturn(L, err)
		}
		L.Push(p.newObject(kindRegex, r))
		return 1
	})
	e.set(t, "escape", func(L *lua.LState) int {
		s := L.CheckString(1)
		if len(s) > reMaxPattern {
			L.ArgError(1, "string too long")
		}
		L.Push(lua.LString(regexp.QuoteMeta(s)))
		return 1
	})

	// Every operation exists as re.<op>(subject, pattern|Regex, ...) and as Regex:<op>(subject, ...).
	type op func(L *lua.LState, r *reProg, subj int) int
	ops := map[string]op{
		"find": reFind, "match": reMatch, "test": reTest, "gmatch": reGMatch, "gsub": reGsub, "split": reSplit,
	}
	for name, f := range ops {
		f := f
		e.set(t, name, func(L *lua.LState) int {
			r, err := p.reArg(L, 2)
			if err != nil {
				return errReturn(L, err)
			}
			return f(L, r, 1)
		})
		e.set(methods, name, func(L *lua.LState) int {
			r := checkObject(L, 1, kindRegex).(*reProg)
			return f(L, r, 2)
		})
	}
	return t
}

// reCapture is the Lua value of group k of the match loc (false when it did not take part).
func reCapture(s string, loc []int, k int) lua.LValue {
	if loc[2*k] < 0 {
		return lua.LFalse
	}
	return lua.LString(s[loc[2*k]:loc[2*k+1]])
}

// pushCaptures pushes the groups of loc, or the whole match when the pattern has none.
func pushCaptures(L *lua.LState, r *reProg, s string, loc []int) int {
	n := r.re.NumSubexp()
	if n == 0 {
		L.Push(lua.LString(s[loc[0]:loc[1]]))
		return 1
	}
	for k := 1; k <= n; k++ {
		L.Push(reCapture(s, loc, k))
	}
	return n
}

// reStart converts a 1-based, possibly negative init into a byte offset in [0, len].
func reStart(L *lua.LState, n, l int) int {
	init := optI64(L, n, 1)
	if init < 0 {
		init += int64(l) + 1
		if init < 1 {
			init = 1
		}
	}
	if init == 0 {
		init = 1
	}
	return int(min(init-1, int64(l)))
}

// The functions below take the subject at argument subj (1 for re.<op>, 2 for Regex:<op>);
// the arguments after the pattern are at 3 and 4 in both forms.

// reFind: first match at or after init. Matching starts at init as if the string began
// there (so ^ and \b see the cut as the beginning).
func reFind(L *lua.LState, r *reProg, subj int) int {
	s := L.CheckString(subj)
	if err := r.check(s); err != nil {
		return errReturn(L, err)
	}
	i0 := reStart(L, 3, len(s))
	loc := r.re.FindStringSubmatchIndex(s[i0:])
	if loc == nil {
		L.Push(lua.LNil)
		return 1
	}
	L.Push(lua.LNumber(i0 + loc[0] + 1))
	L.Push(lua.LNumber(i0 + loc[1]))
	n := r.re.NumSubexp()
	for k := 1; k <= n; k++ {
		L.Push(reCapture(s[i0:], loc, k))
	}
	return 2 + n
}

func reMatch(L *lua.LState, r *reProg, subj int) int {
	s := L.CheckString(subj)
	if err := r.check(s); err != nil {
		return errReturn(L, err)
	}
	i0 := reStart(L, 3, len(s))
	loc := r.re.FindStringSubmatchIndex(s[i0:])
	if loc == nil {
		L.Push(lua.LNil)
		return 1
	}
	return pushCaptures(L, r, s[i0:], loc)
}

func reTest(L *lua.LState, r *reProg, subj int) int {
	s := L.CheckString(subj)
	if err := r.check(s); err != nil {
		return errReturn(L, err)
	}
	L.Push(lua.LBool(r.re.MatchString(s)))
	return 1
}

// allMatches returns every match (at most reMaxMatches, and at most reMaxIndexes capture
// indexes in all, so a pattern with many groups gets fewer matches) with its groups.
func allMatches(r *reProg, s string, n int) ([][]int, error) {
	maxMatches := min(reMaxMatches, reMaxIndexes/(2*(r.re.NumSubexp()+1)))
	limit := maxMatches + 1
	if n >= 0 && n < limit {
		limit = n
	}
	all := r.re.FindAllStringSubmatchIndex(s, limit)
	if len(all) > maxMatches {
		return nil, limitErr("more than %d matches; process the text in chunks", maxMatches)
	}
	return all, nil
}

func reGMatch(L *lua.LState, r *reProg, subj int) int {
	s := L.CheckString(subj)
	if err := r.check(s); err != nil {
		L.RaiseError("%s", err.Error())
	}
	all, err := allMatches(r, s, -1)
	if err != nil {
		L.RaiseError("%s", err.Error())
	}
	i := 0
	L.Push(L.NewFunction(func(L *lua.LState) int {
		if i >= len(all) {
			L.Push(lua.LNil)
			return 1
		}
		loc := all[i]
		i++
		return pushCaptures(L, r, s, loc)
	}))
	return 1
}

// tmplPart is a piece of a gsub replacement template: literal text or a group.
type tmplPart struct {
	lit   string
	group int // >= 0: the group; -1: literal
}

// parseTemplate splits a replacement like "$1-${name}$$" into parts. As in Go's
// Regexp.Expand, $name is the longest run of letters, digits and underscores; use ${1}
// to follow a group with a digit. A group that does not exist expands to nothing.
func parseTemplate(r *reProg, t string) []tmplPart {
	var parts []tmplPart
	var lit strings.Builder
	flush := func() {
		if lit.Len() > 0 {
			parts = append(parts, tmplPart{lit: lit.String(), group: -1})
			lit.Reset()
		}
	}
	for i := 0; i < len(t); i++ {
		if t[i] != '$' || i+1 >= len(t) {
			lit.WriteByte(t[i])
			continue
		}
		i++
		if t[i] == '$' {
			lit.WriteByte('$')
			continue
		}
		var name string
		if t[i] == '{' {
			end := strings.IndexByte(t[i:], '}')
			if end < 0 {
				lit.WriteString("${")
				continue
			}
			name = t[i+1 : i+end]
			i += end
		} else {
			j := i
			for j < len(t) && (t[j] == '_' || t[j] >= '0' && t[j] <= '9' || t[j] >= 'a' && t[j] <= 'z' || t[j] >= 'A' && t[j] <= 'Z') {
				j++
			}
			if j == i {
				lit.WriteByte('$')
				i--
				continue
			}
			name = t[i:j]
			i = j - 1
		}
		flush()
		g, err := strconv.Atoi(name)
		if err != nil {
			g = r.re.SubexpIndex(name)
		}
		if g < 0 || g > r.re.NumSubexp() {
			parts = append(parts, tmplPart{lit: "", group: -1})
			continue
		}
		parts = append(parts, tmplPart{group: g})
	}
	flush()
	return parts
}

// capBuilder builds a result of at most MaxString bytes. It grows by doubling, bounded by
// the cap, so a failing build allocates at most about twice what it had written.
type capBuilder struct {
	buf []byte
	L   *lua.LState
}

func (c *capBuilder) write(s string) {
	if len(s) > MaxString-len(c.buf) {
		tooLarge(c.L)
	}
	if need := len(c.buf) + len(s); need > cap(c.buf) {
		grown := make([]byte, len(c.buf), min(max(need, 2*cap(c.buf)), MaxString))
		copy(grown, c.buf)
		c.buf = grown
	}
	c.buf = append(c.buf, s...)
}

func (c *capBuilder) String() string { return string(c.buf) }

func reGsub(L *lua.LState, r *reProg, subj int) int {
	s := L.CheckString(subj)
	if err := r.check(s); err != nil {
		return errReturn(L, err)
	}
	repl := L.Get(3)
	n := -1
	if L.Get(4) != lua.LNil {
		n = int(checkI64(L, 4))
	}
	if n == 0 {
		L.Push(lua.LString(s))
		L.Push(lua.LNumber(0))
		return 2
	}
	var parts []tmplPart
	var tbl *lua.LTable
	var fn *lua.LFunction
	switch v := repl.(type) {
	case lua.LString:
		parts = parseTemplate(r, string(v))
	case lua.LNumber:
		parts = parseTemplate(r, v.String())
	case *lua.LTable:
		tbl = v
	case *lua.LFunction:
		fn = v
	default:
		L.ArgError(3, "string/function/table expected")
	}
	all, err := allMatches(r, s, n)
	if err != nil {
		return errReturn(L, err)
	}
	if len(all) == 0 {
		L.Push(lua.LString(s))
		L.Push(lua.LNumber(0))
		return 2
	}
	out := &capBuilder{L: L, buf: make([]byte, 0, len(s))}
	last := 0
	for _, loc := range all {
		out.write(s[last:loc[0]])
		last = loc[1]
		switch {
		case fn != nil:
			L.Push(fn)
			k := pushCaptures(L, r, s, loc)
			L.Call(k, 1)
			v := L.Get(-1)
			L.Pop(1)
			text, ok := replacementValue(L, v)
			if !ok {
				text = s[loc[0]:loc[1]]
			}
			out.write(text)
		case tbl != nil:
			var key lua.LValue = lua.LString(s[loc[0]:loc[1]])
			if r.re.NumSubexp() > 0 {
				key = reCapture(s, loc, 1)
			}
			text, ok := replacementValue(L, L.GetTable(tbl, key))
			if !ok {
				text = s[loc[0]:loc[1]]
			}
			out.write(text)
		default:
			for _, part := range parts {
				if part.group < 0 {
					out.write(part.lit)
				} else if loc[2*part.group] >= 0 {
					out.write(s[loc[2*part.group]:loc[2*part.group+1]])
				}
			}
		}
	}
	out.write(s[last:])
	L.Push(lua.LString(out.String()))
	L.Push(lua.LNumber(len(all)))
	return 2
}

// reSplit splits s around the matches (Go's Regexp.Split rules); n limits the number of
// pieces (<= 0: all). More than MaxSplitParts pieces is a limit error.
func reSplit(L *lua.LState, r *reProg, subj int) int {
	s := L.CheckString(subj)
	if err := r.check(s); err != nil {
		return errReturn(L, err)
	}
	n := -1
	if L.Get(3) != lua.LNil {
		n = int(checkI64(L, 3))
	}
	if n == 0 {
		L.Push(L.NewTable())
		return 1
	}
	limit := MaxSplitParts
	if n > 0 && n < limit {
		limit = n
	}
	if r.re.String() != "" && len(s) == 0 {
		t := L.NewTable()
		t.Append(lua.LString(""))
		L.Push(t)
		return 1
	}
	matches := r.re.FindAllStringIndex(s, limit)
	if n < 0 && len(matches) >= MaxSplitParts {
		return errReturn(L, limitErr("more than %d pieces", MaxSplitParts))
	}
	t := L.NewTable()
	beg, end, count := 0, 0, 0
	for _, m := range matches {
		if n > 0 && count == n-1 {
			break
		}
		end = m[0]
		if m[1] != 0 {
			t.Append(lua.LString(s[beg:end]))
			count++
		}
		beg = m[1]
	}
	if end != len(s) {
		t.Append(lua.LString(s[beg:]))
	}
	L.Push(t)
	return 1
}
