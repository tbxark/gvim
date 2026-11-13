package plugin

import (
	"context"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

// pollCtx is a context that counts how often it is asked, and reports itself cancelled from
// the fireAt-th question on.
type countingCtx struct {
	context.Context
	asked, fireAt int
}

func (c *countingCtx) Err() error {
	c.asked++
	if c.asked >= c.fireAt {
		return context.Canceled
	}
	return nil
}

// TestSortPollsTheContextEveryFewComparisons is the step-count half of sort_cancel: the default comparator asks the context once per sortPollEvery comparisons, so a
// cancel that arrives at the third question stops a 500 000 element sort after 3 x 4096
// comparisons. The bound is in comparisons, and does not depend on how fast the machine is.
func TestSortPollsTheContextEveryFewComparisons(t *testing.T) {
	const n, fireAt = MaxSortN, 3
	L := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer L.Close()
	ctx := &countingCtx{Context: context.Background(), fireAt: fireAt}
	L.SetContext(ctx)

	tab := L.CreateTable(n, 0)
	x := 12345
	for i := 1; i <= n; i++ {
		x = (x*1103515245 + 12345) & 0x7fffffff
		tab.RawSetInt(i, lua.LNumber(x%100003))
	}
	err := L.CallByParam(lua.P{Fn: L.NewFunction(tableSort), NRet: 0, Protect: true}, tab)
	if err == nil {
		t.Fatal("the sort of 500 000 elements finished although the context was cancelled")
	}
	// Each firing poll asks twice (ctxErr checks, then reads the cause), so the third poll is
	// the third or fourth question and there is no fourth poll.
	if ctx.asked < fireAt || ctx.asked > fireAt+1 {
		t.Fatalf("the context was asked %d times, want %d or %d: the sort went on after the cancel or never polled", ctx.asked, fireAt, fireAt+1)
	}
	if sortPollEvery != 4096 {
		t.Fatalf("sortPollEvery = %d: the docs (limits.md) and this test say 4096", sortPollEvery)
	}
}
