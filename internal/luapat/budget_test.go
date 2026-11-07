package luapat

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// wordsSubject returns n bytes of "lorem ipsum ... " ending in a non-space,
// with a TODO marker only at the very end when marker is set.
func wordsSubject(n int, marker bool) string {
	const unit = "lorem ipsum dolor sit amet, consectetur adipiscing elit\n"
	s := strings.Repeat(unit, n/len(unit)+1)[:n]
	if marker {
		return s[:n-4] + "TODO"
	}
	return s[:n-1] + "x"
}

// sanityLimit is the one wall-clock bound the budget tests keep. The budgets are counted in
// steps, and the tests assert on the steps (deterministic); the wall-clock bound only
// catches a step that has become pathologically slow, so it is generous enough to hold on
// a machine that is running the whole test suite in parallel, with or without -race.
func sanityLimit() time.Duration {
	limit := 10 * time.Second
	if raceEnabled {
		limit *= 5
	}
	return limit
}

// scanOf runs the scan of Find for s and pat on a fresh match state, as find does for a
// pattern with special characters, and returns the state: steps is what the call charged,
// budget what it was allowed, err why it stopped.
func scanOf(s, pat string, o Options) *matchState {
	ms := o.state(s, pat)
	anchor := len(pat) > 0 && pat[0] == '^'
	p := 0
	if anchor {
		p = 1
	}
	ms.scan(0, p, anchor, ms.prepSkip(p))
	return ms
}

// TestCatastrophicBounded: patterns whose backtracking is exponential or
// quadratic must end with ErrBudget after exactly the budget's worth of steps and
// leave the package usable. The bound is in steps, so it does not depend on the
// speed of the machine.
func TestCatastrophicBounded(t *testing.T) {
	longA := strings.Repeat("a", 30) + "b"
	cases := []struct {
		name string
		s, p string
	}{
		{"nested_stars_12", strings.Repeat("a", 20) + "b", strings.Repeat("a*", 12) + "c"},
		{"nested_stars_30", longA, strings.Repeat("a*", 30) + "c"},
		{"lazy_chain", strings.Repeat("a", 40), strings.Repeat("a-", 40) + "b"},
		{"dot_star_chain", strings.Repeat("x", 60) + "y", strings.Repeat(".*", 20) + "z"},
		{"quadratic_spaces_then_x", strings.Repeat(" ", 50000) + "x", "%s+$"},
		{"quadratic_class_run", strings.Repeat("a", 200000), "[a]*[a]*b"},
		{"captures_backtrack", strings.Repeat("ab", 30), strings.Repeat("(a*b*)", 10) + "c"},
	}
	for _, c := range cases {
		t.Run(c.name+"/steps", func(t *testing.T) {
			ms := scanOf(c.s, c.p, Options{})
			if !errors.Is(ms.err, ErrBudget) {
				t.Fatalf("err = %v, want pattern too complex", ms.err)
			}
			if want := DefaultBudget + StepsPerByte*len(c.s); ms.budget != want {
				t.Fatalf("budget = %d, want the default %d for a %d byte subject", ms.budget, want, len(c.s))
			}
			// The matcher charges one step at a time and stops at the first one over the
			// budget: the work is the budget, and not a step more.
			if ms.steps != ms.budget+1 {
				t.Fatalf("steps = %d, want %d (budget %d plus the step that failed)", ms.steps, ms.budget+1, ms.budget)
			}
		})
		for _, op := range []string{"find", "match", "gmatch", "gsub"} {
			t.Run(c.name+"/"+op, func(t *testing.T) {
				start := time.Now()
				var err error
				switch op {
				case "find":
					_, _, err = Find(c.s, c.p, 1, false, Options{})
				case "match":
					_, _, err = Match(c.s, c.p, 1, Options{})
				case "gmatch":
					var it *Iter
					if it, err = GMatch(c.s, c.p, Options{}); err == nil {
						_, _, err = it.Next()
					}
				case "gsub":
					_, _, err = Gsub(c.s, c.p, Replacement{Template: "x"}, Unlimited, Options{})
				}
				if !errors.Is(err, ErrBudget) || err.Error() != "pattern too complex" {
					t.Fatalf("err = %v, want pattern too complex", err)
				}
				if d := time.Since(start); d > sanityLimit() {
					t.Fatalf("took %v, want < %v even on a loaded machine", d, sanityLimit())
				}
			})
		}
	}
	// the package keeps working after the aborts
	if r, ok, err := Find("hello world", "w%a+", 1, false, Options{}); err != nil || !ok || r.Start != 6 {
		t.Fatalf("after abort: %+v %v %v", r, ok, err)
	}
}

func TestBudgetOptions(t *testing.T) {
	s, p := strings.Repeat("a", 30)+"b", strings.Repeat("a*", 4)+"c"
	// a tiny budget fails what the default budget completes
	if _, _, err := Find(s, p, 1, false, Options{}); err != nil {
		t.Fatalf("default budget: %v", err)
	}
	if _, _, err := Find(s, p, 1, false, Options{Budget: 1}); !errors.Is(err, ErrBudget) {
		t.Fatalf("Budget=1: err = %v, want ErrBudget", err)
	}
	// the subject adds StepsPerByte per byte: the same Budget=1 succeeds on a
	// trivial pattern because the free first attempt charges nothing
	if _, ok, err := Find("abc", "b+", 1, false, Options{Budget: 1}); err != nil || !ok {
		t.Fatalf("free first attempt: %v %v", ok, err)
	}
	// a malicious unit of work per Next call does not accumulate across calls
	it, err := GMatch(strings.Repeat("ab", 1000), "a(b)", Options{Budget: 100})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for {
		_, ok, err := it.Next()
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		n++
	}
	if n != 1000 {
		t.Fatalf("gmatch yielded %d matches, want 1000", n)
	}
}

func TestLimits(t *testing.T) {
	long := strings.Repeat("a", MaxPattern+1)
	if _, _, err := Find("a", long+"*", 1, false, Options{}); !errors.Is(err, ErrPatternTooLong) {
		t.Errorf("long pattern: %v", err)
	}
	if _, _, err := Find("a", strings.Repeat("a", MaxPattern-1)+"*", 1, false, Options{}); err != nil {
		t.Errorf("pattern of exactly MaxPattern bytes: %v", err)
	}
	if _, err := GMatch("a", long+"*", Options{}); !errors.Is(err, ErrPatternTooLong) {
		t.Errorf("gmatch long pattern: %v", err)
	}
	big := strings.Repeat("x", MaxSubject+1)
	if _, _, err := Match(big, "x", 1, Options{}); !errors.Is(err, ErrSubjectTooLong) {
		t.Errorf("match big subject: %v", err)
	}
	if _, _, err := Gsub(big, "x", Replacement{Template: "y"}, 1, Options{}); !errors.Is(err, ErrSubjectTooLong) {
		t.Errorf("gsub big subject: %v", err)
	}
	if _, err := GMatch(big, "x", Options{}); !errors.Is(err, ErrSubjectTooLong) {
		t.Errorf("gmatch big subject: %v", err)
	}
}

func TestPlainFind(t *testing.T) {
	t.Run("plain_find_has_no_budget", plainFindHasNoBudget)
}

func plainFindHasNoBudget(t *testing.T) {
	big := strings.Repeat("x", MaxSubject+(1<<20)) // above the matcher's subject cap
	for _, tc := range []struct {
		name  string
		pat   string
		plain bool
	}{
		{"plain_flag", "needle", true},
		{"plain_flag_specials", "a.*b", true},
		{"no_specials", "needle", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, ok, err := Find(big, tc.pat, 1, tc.plain, Options{Budget: 1, Cancel: func() error { return errors.New("never polled") }})
			if err != nil || ok {
				t.Fatalf("ok=%v err=%v, want a clean miss", ok, err)
			}
			r, ok, err := Find(big+"needle", tc.pat, 1, tc.plain, Options{Budget: 1})
			if tc.pat == "needle" && (err != nil || !ok || r.Start != len(big)) {
				t.Fatalf("hit: %+v %v %v", r, ok, err)
			}
		})
	}
}

// TestCancel: the cancel hook interrupts the linear scan and the matcher.
func TestCancel(t *testing.T) {
	boom := errors.New("context canceled")
	t.Run("cancel_interrupts_linear_scan", func(t *testing.T) {
		// 16 MiB of identifier characters and no '(': the pattern is quadratic, so only
		// the cancel poll can end the call early. The poll comes every 1<<16 steps, so a
		// cancel that fires at the third poll stops the call after at most 3<<16 steps:
		// the bound is in steps, and does not depend on how fast the machine is.
		s := strings.Repeat("a", MaxSubject)
		const p = "[%a_][%w_]*%("
		const fireAt = 3
		polls := 0
		ms := scanOf(s, p, Options{Cancel: func() error {
			if polls++; polls == fireAt {
				return boom
			}
			return nil
		}})
		if !errors.Is(ms.err, boom) {
			t.Fatalf("err = %v, want the cancel error", ms.err)
		}
		if polls != fireAt {
			t.Fatalf("polled %d times, want %d: the scan went on after the cancel", polls, fireAt)
		}
		if ms.steps > fireAt<<16 {
			t.Fatalf("charged %d steps before the cancel was noticed, want <= %d", ms.steps, fireAt<<16)
		}
	})
	t.Run("cancel_interrupts_class_skip", func(t *testing.T) {
		// no byte can start a match, so the whole pass is the class skip loop, which
		// polls every cancelScanEvery positions: a never-firing hook is polled once per
		// that many bytes, and one that fires at the third poll ends the call there.
		s := strings.Repeat(" ", MaxSubject)
		for _, p := range []string{"[%a_][%w_]*%(", "%d+x", "[^ ]x"} {
			for _, op := range []string{"find", "gsub"} {
				run := func(fireAt int) (polls int, err error) {
					cancel := func() error {
						if polls++; polls == fireAt {
							return boom
						}
						return nil
					}
					if op == "find" {
						_, _, err = Find(s, p, 1, false, Options{Cancel: cancel})
					} else {
						_, _, err = Gsub(s, p, Replacement{Template: "x"}, Unlimited, Options{Cancel: cancel})
					}
					return polls, err
				}
				polls, err := run(0)
				if err != nil {
					t.Fatalf("%s %q: %v", op, p, err)
				}
				if want := len(s) / cancelScanEvery; polls != want {
					t.Fatalf("%s %q: %d polls over %d bytes, want one per %d bytes (%d)", op, p, polls, len(s), cancelScanEvery, want)
				}
				if polls, err := run(3); !errors.Is(err, boom) || polls != 3 {
					t.Fatalf("%s %q: err = %v after %d polls, want the cancel error at the third", op, p, err, polls)
				}
			}
		}
	})
	t.Run("scan_loop_polls", func(t *testing.T) {
		// every start position fails in O(1) steps, so only the scan-loop
		// poll (every 16384 positions) can notice the cancel
		s := strings.Repeat("a ", 1<<20)
		var calls int
		_, _, err := Find(s, "[ab][ab]x", 1, false, Options{Cancel: func() error {
			calls++
			if calls == 3 {
				return boom
			}
			return nil
		}})
		if !errors.Is(err, boom) || calls != 3 {
			t.Fatalf("err = %v after %d polls", err, calls)
		}
	})
	t.Run("gsub_and_gmatch_poll", func(t *testing.T) {
		s := strings.Repeat("ab", 1<<20)
		fire := func() func() error {
			n := 0
			return func() error {
				if n++; n >= 2 {
					return boom
				}
				return nil
			}
		}
		if _, _, err := Gsub(s, "(a)(b)", Replacement{Template: "%2%1"}, Unlimited, Options{Cancel: fire()}); !errors.Is(err, boom) {
			t.Errorf("gsub: %v", err)
		}
		// gmatch: a miss-heavy scan inside one Next call
		it, err := GMatch(strings.Repeat("a ", 1<<20), "[ab][ab]x", Options{Cancel: fire()})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := it.Next(); !errors.Is(err, boom) {
			t.Errorf("gmatch: %v", err)
		}
	})
	t.Run("matcher_polls_steps", func(t *testing.T) {
		// one start position, huge backtracking: the 1<<16 step poll fires
		var calls int
		_, _, err := Find(strings.Repeat("a", 5000)+"b", "^"+strings.Repeat("a*", 6)+"c", 1, false, Options{
			Budget: 1 << 30,
			Cancel: func() error {
				calls++
				return boom
			},
		})
		if !errors.Is(err, boom) || calls != 1 {
			t.Fatalf("err = %v after %d polls", err, calls)
		}
	})
}

func TestLargeSubjects(t *testing.T) {
	t.Run("find_TODO_10MiB", func(t *testing.T) {
		s := wordsSubject(10<<20, true)
		start := time.Now()
		r, ok, err := Find(s, "TODO", 1, false, Options{})
		if err != nil || !ok || r.Start != len(s)-4 {
			t.Fatalf("%+v %v %v", r, ok, err)
		}
		t.Logf("took %v", time.Since(start))
	})
	t.Run("find_%s+$_2_5M_chars", func(t *testing.T) {
		s := wordsSubject(5<<19, false) + " " // 2.5M chars, one trailing space
		start := time.Now()
		r, ok, err := Find(s, "%s+$", 1, false, Options{})
		if err != nil || !ok || r.Start != len(s)-1 {
			t.Fatalf("%+v %v %v", r, ok, err)
		}
		t.Logf("took %v", time.Since(start))
		// and without the trailing space the scan completes with a miss
		if _, ok, err := Find(s[:len(s)-1], "%s+$", 1, false, Options{}); err != nil || ok {
			t.Fatalf("miss: %v %v", ok, err)
		}
	})
	t.Run("gsub_literal_10MiB", func(t *testing.T) {
		s := wordsSubject(10<<20, false)
		out, n, err := Gsub(s, "ipsum", Replacement{Template: "IPSUM"}, Unlimited, Options{})
		want := strings.Count(s, "ipsum")
		if err != nil || n != want || len(out) != len(s) || strings.Count(out, "IPSUM") != want {
			t.Fatalf("n=%d (want %d) len=%d err=%v", n, want, len(out), err)
		}
	})
	t.Run("gmatch_over_10MiB", func(t *testing.T) {
		s := wordsSubject(10<<20, false)
		it, err := GMatch(s, "%a+", Options{})
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for {
			_, ok, err := it.Next()
			if err != nil {
				t.Fatal(err)
			}
			if !ok {
				break
			}
			n++
		}
		if want := len(strings.Fields(strings.NewReplacer(",", "").Replace(s))); n != want {
			t.Fatalf("n=%d want %d", n, want)
		}
	})
}

func TestGsubMaxOut(t *testing.T) {
	s := strings.Repeat("x", 1000)
	cases := []struct {
		name string
		pat  string
		rep  Replacement
		max  int
		err  error
	}{
		{"template_over_cap", "x", Replacement{Template: "yyyy"}, 0, ErrOutputTooLarge},
		{"capture_expansion_over_cap", "(x)", Replacement{Template: "%1%1%1%1%0"}, 0, ErrOutputTooLarge},
		{"func_over_cap", "x", Replacement{Func: func(Result) (string, bool, error) { return "yyyy", true, nil }}, 0, ErrOutputTooLarge},
		{"empty_matches_over_cap", "", Replacement{Template: "-----"}, 0, ErrOutputTooLarge},
		{"exactly_at_cap", "x", Replacement{Template: "y"}, 0, nil},
		{"limited_stays_under", "x", Replacement{Template: "yyyyyyyy"}, 10, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			max := c.max
			if max == 0 {
				max = Unlimited
			}
			out, _, err := Gsub(s, c.pat, c.rep, max, Options{MaxOut: 2000})
			if !errors.Is(err, c.err) {
				t.Fatalf("err = %v, want %v", err, c.err)
			}
			if err == nil && len(out) > 2000 {
				t.Fatalf("output %d bytes exceeds the cap", len(out))
			}
		})
	}
	t.Run("gsub_long_repl_16MiB", func(t *testing.T) {
		// default cap is 64 MiB: 16 MiB x 5 bytes passes through the matcher in time
		big := strings.Repeat("x", MaxSubject)
		start := time.Now()
		if _, _, err := Gsub(big, "x", Replacement{Template: "yyyyy"}, Unlimited, Options{}); !errors.Is(err, ErrOutputTooLarge) {
			t.Fatalf("err = %v", err)
		}
		if d := time.Since(start); d > sanityLimit() {
			t.Fatalf("took %v, want < %v even on a loaded machine", d, sanityLimit())
		}
	})
}

// TestBalanceBudget: %b scans are charged, so an unbalanced run ends with
// ErrBudget after the budget's worth of steps, and Cancel interrupts a single
// long scan within one chunk of it.
func TestBalanceBudget(t *testing.T) {
	s := strings.Repeat("(", 100000)
	for _, p := range []string{"%b()", ".-%b()x"} {
		t.Run(p+"/steps", func(t *testing.T) {
			ms := scanOf(s, p, Options{})
			if !errors.Is(ms.err, ErrBudget) {
				t.Fatalf("err = %v, want pattern too complex", ms.err)
			}
			// A %b scan charges balanceChunk/balanceCost steps per chunk and a tick one
			// step, so the call ends within one charge of the budget.
			if over := ms.steps - ms.budget; over < 1 || over > balanceChunk/balanceCost {
				t.Fatalf("steps = %d, budget %d: ended %d steps over, want 1..%d", ms.steps, ms.budget, over, balanceChunk/balanceCost)
			}
		})
		for _, op := range []string{"find", "match", "gmatch", "gsub"} {
			t.Run(p+"/"+op, func(t *testing.T) {
				start := time.Now()
				var err error
				switch op {
				case "find":
					_, _, err = Find(s, p, 1, false, Options{})
				case "match":
					_, _, err = Match(s, p, 1, Options{})
				case "gmatch":
					var it *Iter
					if it, err = GMatch(s, p, Options{}); err == nil {
						_, _, err = it.Next()
					}
				case "gsub":
					_, _, err = Gsub(s, p, Replacement{Template: "x"}, Unlimited, Options{})
				}
				if !errors.Is(err, ErrBudget) {
					t.Fatalf("err = %v, want pattern too complex", err)
				}
				if d := time.Since(start); d > sanityLimit() {
					t.Fatalf("took %v, want < %v even on a loaded machine", d, sanityLimit())
				}
			})
		}
	}
	t.Run("cancel_interrupts_balance", func(t *testing.T) {
		// One %b scan over 300000 bytes polls the hook once per balanceChunk bytes. A
		// hook that fires at the fifth poll ends the scan there: five chunks were
		// charged and no more.
		boom := errors.New("context canceled")
		big := strings.Repeat("(", 300000)
		const fireAt = 5
		polls := 0
		ms := scanOf(big, "%b()", Options{Budget: 1 << 40, Cancel: func() error {
			if polls++; polls == fireAt {
				return boom
			}
			return nil
		}})
		if !errors.Is(ms.err, boom) {
			t.Fatalf("err = %v, want the cancel error", ms.err)
		}
		if polls != fireAt {
			t.Fatalf("polled %d times, want %d: the scan went on after the cancel", polls, fireAt)
		}
		if want := fireAt * balanceChunk / balanceCost; ms.steps != want {
			t.Fatalf("charged %d steps, want %d (%d chunks of %d bytes)", ms.steps, want, fireAt, balanceChunk)
		}
	})
	t.Run("balanced_still_matches", func(t *testing.T) {
		in := "x" + strings.Repeat("(", 5000) + strings.Repeat(")", 5000) + "y"
		r, ok, err := Find(in, "%b()", 1, false, Options{})
		if err != nil || !ok || r.Start != 1 || r.End != len(in)-1 {
			t.Fatalf("%+v %v %v", r, ok, err)
		}
	})
}
