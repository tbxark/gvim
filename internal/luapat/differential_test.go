package luapat

import (
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

// stockRun is the oracle: it runs the operation with gopher-lua's own string
// library and renders the outcome exactly like runRow.
const stockRun = `
local function q(v)
  if type(v) == "number" then return tostring(v) end
  local o = {}
  for i = 1, #v do
    local c = v:byte(i)
    if c == 34 then o[#o+1] = '\\"' elseif c == 92 then o[#o+1] = '\\\\'
    elseif c >= 32 and c < 127 then o[#o+1] = string.char(c)
    elseif c == 10 then o[#o+1] = '\\n' elseif c == 9 then o[#o+1] = '\\t'
    else o[#o+1] = string.format("\\x%02x", c) end
  end
  return '"' .. table.concat(o) .. '"'
end
local function join(...)
  local t = {}
  for i = 1, select('#', ...) do t[i] = q((select(i, ...))) end
  return table.concat(t, " ")
end
local fns = {
  upper = function(a) return tostring(a):upper() end,
  all = function(...) local t = {} for i = 1, select('#', ...) do t[i] = tostring((select(i, ...))) end return "<" .. table.concat(t, ",") .. ">" end,
  ["nil"] = function() return nil end,
  num = function() return 42 end,
}
function run(op, s, p, init, repl, fn, max)
  if op == "find" then
    local r = {pcall(string.find, s, p, init)}
    if not r[1] then return "error" end
    if r[2] == nil then return "nil" end
    return join(unpack(r, 2))
  elseif op == "match" then
    local r = {pcall(string.match, s, p, init)}
    if not r[1] then return "error" end
    if r[2] == nil then return "nil" end
    return join(unpack(r, 2))
  elseif op == "gmatch" then
    local out = {}
    local ok = pcall(function()
      local n = 0
      local it, st = string.gmatch(s, p)
      while true do
        local t = {it(st)}
        if #t == 0 then break end
        n = n + 1
        if n > 1000 then error("loop") end
        out[#out+1] = "[" .. join(unpack(t)) .. "]"
      end
    end)
    if not ok then return "error" end
    return table.concat(out)
  else
    local r
    if fn ~= "" then r = {pcall(string.gsub, s, p, fns[fn], max)}
    else r = {pcall(string.gsub, s, p, repl, max)} end
    if not r[1] then return "error" end
    return q(r[2]) .. " " .. tostring(r[3])
  end
end
`

// patGen builds well-formed random Lua patterns without %f (the stock engine
// does not implement it).
type patGen struct {
	r        *rand.Rand
	closed   []int // numbers of closed text captures, valid back references
	captures int
}

var (
	litAlphabet = "abc.-1 _"
	classes     = []string{"%a", "%d", "%s", "%w", "%l", "%u", "%p", "%A", "%D", "%S", "%W", "%x", "%c"}
	sets        = []string{"[ab]", "[^a]", "[a-c]", "[%d_]", "[^%s]", "[b-]", "[%a%d]", "[^ab1]", "[.]", "[%.-]"}
	quants      = []string{"", "", "", "*", "+", "-", "?"}
)

func (g *patGen) single() string {
	switch k := g.r.Intn(10); {
	case k < 4:
		c := litAlphabet[g.r.Intn(len(litAlphabet))]
		if strings.IndexByte(".-", c) >= 0 {
			return "%" + string(c)
		}
		return string(c)
	case k < 5:
		return "."
	case k < 7:
		return classes[g.r.Intn(len(classes))]
	default:
		return sets[g.r.Intn(len(sets))]
	}
}

func (g *patGen) seq(depth int) string {
	var b strings.Builder
	for n := 1 + g.r.Intn(4); n > 0; n-- {
		switch k := g.r.Intn(14); {
		case k == 0 && depth < 2 && g.captures < 4:
			g.captures++
			idx := g.captures
			b.WriteString("(" + g.seq(depth+1) + ")")
			g.closed = append(g.closed, idx)
		case k == 1 && g.captures < 4:
			g.captures++
			b.WriteString("()")
		case k == 2 && len(g.closed) > 0:
			b.WriteString("%" + strconv.Itoa(g.closed[g.r.Intn(len(g.closed))]))
		case k == 3 && depth == 0:
			b.WriteString("%b()")
		default:
			b.WriteString(g.single() + quants[g.r.Intn(len(quants))])
		}
	}
	return b.String()
}

func (g *patGen) pattern() string {
	g.closed, g.captures = g.closed[:0], 0
	p := g.seq(0)
	if g.r.Intn(5) == 0 {
		p = "^" + p
	}
	if g.r.Intn(6) == 0 {
		p += "$"
	}
	return p
}

func (g *patGen) subject() string {
	const alpha = "aabbc 1.-_()AB\n"
	b := make([]byte, g.r.Intn(20))
	for i := range b {
		b[i] = alpha[g.r.Intn(len(alpha))]
	}
	return string(b)
}

var templates = []string{"", "x", "%0", "[%0]", "%%", "<%1>", "%1%1", "a%0b", "%2-%1"}

type diffRow struct {
	convRow
	got, stock string
}

func stockCall(t *testing.T, L *lua.LState, r convRow) string {
	t.Helper()
	init := lua.LNil
	if r.init != 0 {
		init = lua.LNumber(r.init)
	}
	max := lua.LNil
	if r.max != 0 {
		max = lua.LNumber(r.max)
	}
	err := L.CallByParam(lua.P{Fn: L.GetGlobal("run"), NRet: 1, Protect: true},
		lua.LString(r.op), lua.LString(r.s), lua.LString(r.p), init, lua.LString(r.repl), lua.LString(r.fn), max)
	if err != nil {
		t.Fatalf("stock run: %v", err)
	}
	v := L.Get(-1)
	L.Pop(1)
	return v.String()
}

// TestDifferentialAgainstStock runs random well-formed patterns through
// luapat and through gopher-lua's string functions and demands equal results.
// The stock engine diverges from Lua 5.1 in a few documented places, so the
// comparison skips exactly those: the empty pattern (find ignores init), the
// anchor '^' in gmatch (5.1 treats it as a literal, stock as an anchor), the
// trailing-empty-match rules of gsub/gmatch (stock collects all matches up
// front), and replacement strings with an invalid capture index.
func TestDifferentialAgainstStock(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	if err := L.DoString(stockRun); err != nil {
		t.Fatal(err)
	}
	g := &patGen{r: rand.New(rand.NewSource(20260429))}
	n := 60000
	if testing.Short() || raceEnabled {
		n = 5000
	}
	var diffs []diffRow
	compared := 0
	kinds := map[string]int{}
	for i := 0; i < n; i++ {
		p, s := g.pattern(), g.subject()
		rows := []convRow{
			{op: "find", s: s, p: p},
			{op: "find", s: s, p: p, init: 1 + g.r.Intn(len(s)+1)},
			{op: "match", s: s, p: p},
			{op: "match", s: s, p: p, init: 1 + g.r.Intn(len(s)+1)},
			{op: "match", s: s, p: p, init: -1 - g.r.Intn(len(s)+1)},
			{op: "gmatch", s: s, p: strings.TrimPrefix(p, "^")},
			{op: "gsub", s: s, p: p, repl: templates[g.r.Intn(len(templates))]},
			{op: "gsub", s: s, p: p, repl: templates[g.r.Intn(len(templates))], max: 1 + g.r.Intn(3)},
			{op: "gsub", s: s, p: p, fn: []string{"upper", "all", "nil", "num"}[g.r.Intn(4)]},
		}
		for _, r := range rows {
			got, _ := runRow(r)
			stock := stockCall(t, L, r)
			compared++
			switch {
			case got == "error":
				kinds["error"]++
			case got == "nil" || got == "" || strings.HasPrefix(got, `"`+r.s+`" 0`):
				kinds["no match"]++
			default:
				kinds["match"]++
			}
			if got != stock {
				diffs = append(diffs, diffRow{r, got, stock})
			}
		}
	}
	t.Logf("compared %d: %v", compared, kinds)
	if kinds["match"] < compared/4 {
		t.Errorf("generator is too weak: %v", kinds)
	}
	if len(diffs) > 0 {
		if os.Getenv("LUAPAT_DIFF_DUMP") != "" {
			for _, d := range diffs[:min(len(diffs), 40)] {
				fmt.Printf("%s s=%q p=%q init=%d repl=%q fn=%q max=%d\n   luapat=%s\n   stock =%s\n", d.op, d.s, d.p, d.init, d.repl, d.fn, d.max, d.got, d.stock)
			}
		}
		t.Errorf("%d of %d comparisons differ", len(diffs), compared)
	}
}
