package plugin_test

import (
	"testing"

	"github.com/tbxark/gvim/plugin/plugintest"
)

const compareBuffersSrc = `local gvim = require "gvim"

T.command("go", function()
  local bufs = gvim.buf.list()
  local a, b = bufs[1], bufs[2]
  local r, err = gvim.diff.compare_buffers(a, b)
  if not r then T.spy("err", err) return end
  T.spy("counts", r.left_lines, r.right_lines, r.left_len, r.right_len, #r.hunks, r.left_version)
  local h = r.hunks[1]
  T.spy("sides", h.left.start, h.left.count, h.left.from, h.left.to, h.left.jump,
        h.right.start, h.right.from, h.right.to, h.eol_only)
  T.spy("line", h.left.lines[1][1], h.left.lines[1][2])
  T.spy("span", h.left.spans[1][1], h.left.spans[1][2], h.right.spans[1][1] ~= nil)
  T.spy("replace", gvim.diff.replace_range(b, h.right.from, h.right.to, "b"))
  T.spy("stale", gvim.diff.replace_range(b, 2, 4, "Z", r.right_version))
  T.spy("out", gvim.diff.replace_range(b, -1, 3, "x"), gvim.diff.replace_range(b, 0, 9999, "x"))
end)`

// TestCompareBuffersAndReplaceRange is the Lua face of compare_buffers and
// replace_range: the geometry table the compare_files port paints from, the version
// tokens, and the false-not-error refusals.
func TestCompareBuffersAndReplaceRange(t *testing.T) {
	host := plugintest.NewFakeHost()
	host.AddBuffer("/w/a.txt", "a\nb\nc")
	host.AddBuffer("/w/b.txt", "a\nX\nc")
	h := plugintest.New(t, plugintest.Files{"t.lua": compareBuffersSrc}, plugintest.Options{
		Host: host, Services: &plugintest.FakeServices{},
	})
	h.Start()
	if err := h.Run("t.go", nil); err != nil {
		t.Fatal(err)
	}
	want := "counts 3 3 5 5 1 1.0|sides 1 1 2 4 2 1 2 4 false|line 2 3|span 2 3 true|replace true|stale false|out false false"
	if got := spyText(h); got != want {
		t.Fatalf("spy = %q\nwant %q\nlog %v", got, want, h.LogText(""))
	}
}

// TestCompareBuffersRefusesLargeFile checks the refusal: a large-file buffer rejects
// compare_buffers with CodeTooLarge instead of painting a partial diff.
func TestCompareBuffersRefusesLargeFile(t *testing.T) {
	host := plugintest.NewFakeHost()
	host.AddBuffer("/w/a.txt", "a")
	id := host.AddBuffer("/w/b.txt", "b")
	for _, b := range host.Buffers {
		if b.Info.ID == id {
			b.Info.Large = true
		}
	}
	const src = `local gvim = require "gvim"

T.command("go", function()
  local bufs = gvim.buf.list()
  local r, err = gvim.diff.compare_buffers(bufs[1], bufs[2])
  T.spy("cmp", r, err)
  T.spy("replace", gvim.diff.replace_range(bufs[2], 0, 1, "x"))
end)`
	h := plugintest.New(t, plugintest.Files{"t.lua": src}, plugintest.Options{
		Host: host, Services: &plugintest.FakeServices{},
	})
	h.Start()
	if err := h.Run("t.go", nil); err != nil {
		t.Fatal(err)
	}
	want := "cmp nil too_large: buffer 2 is in large-file mode|replace false"
	if got := spyText(h); got != want {
		t.Fatalf("spy = %q, want %q", got, want)
	}
}
