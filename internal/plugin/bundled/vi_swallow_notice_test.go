package bundled_test

import (
	"strings"
	"testing"
)

// The keys of a command that vi_mode does not have are dropped until Esc, and the status row says
// so for each one: a notice said once is gone after the next key, and the row falls back to the
// stale text under it (RV-05).
func TestViSwallowedKeysSayTheyAreIgnored(t *testing.T) {
	t.Parallel()
	const want = "keys are ignored until Esc"
	count := func(v *vi) int {
		n := 0
		for _, s := range v.h.NoticeTexts() {
			if strings.Contains(s, want) {
				n++
			}
		}
		return n
	}
	v := newVi(t, "alpha\n")
	v.keys("cgn")
	before := count(v)
	if before == 0 {
		t.Fatalf("no notice says the keys are ignored: %q", v.notices())
	}
	for _, k := range []string{"X", "Y"} {
		v.keys(k)
		if got := count(v); got <= before {
			t.Fatalf("after %q the notice is not said again: %q", k, v.notices())
		}
		before = count(v)
	}
}
