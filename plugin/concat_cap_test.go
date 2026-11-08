package plugin_test

import (
	"testing"
)

// Stock gopher-lua does not cap the `..` operator: it allocates the whole result in one VM
// instruction. This test keeps the caps that gvim applies itself, table.concat refusing a
// result over MaxString (64 MiB), and checks that a concatenation under the cap still works.
// The doubling case (s = s .. s) is measured by TestHeapGuardStopsADoublingString.
func TestConcatIsCapped(t *testing.T) {
	h := cmd(t, "", `
local s = string.rep("x", 40 * 1024 * 1024)
local ok, err = pcall(function() return table.concat({ s, s }) end)
T.spy("table.concat", tostring(ok), tostring(err):match("too large") or tostring(err))
local small = ("x"):rep(1000) .. "y" .. 5
T.spy("small", #small)
local half = s .. string.rep("y", 20 * 1024 * 1024)
T.spy("under the cap", #half)
`)
	want := "table.concat false too large|small 1002|under the cap 62914560"
	if got := spyText(h); got != want {
		t.Fatalf("spy = %q\nwant %q\nlog %v", got, want, h.LogText("t"))
	}
}
