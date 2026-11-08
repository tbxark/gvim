package bundled_test

import (
	"math/rand/v2"
	"strings"
	"testing"
	"time"
)

// TestViRandomKeysNeverBreakThePlugin plays random key sequences against several texts and
// checks the plugin never logs an error (a Lua error inside a command), that the caret is
// always inside the buffer, and that vi always ends in one of its modes. A failing seed
// reproduces the session.
func TestViRandomKeysNeverBreakThePlugin(t *testing.T) {
	t.Parallel()
	texts := []string{
		"",
		"x",
		"hello world\nsecond line here\n\n  indented (paren [bracket] {brace}) \"quoted\" 'single'\nlast",
		"one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten",
		"héllo wörld 世界 ünï\ncafé 12 -3 0x1f\n\t\ttabbed line",
		"{\n  a: [1, 2, 3],\n  b: (x + y) * 2,\n}\n\nend",
		"one\ntwo\n\nthree (x)\n",
	}
	keys := []string{
		"h", "j", "k", "l", "w", "b", "e", "W", "B", "E", "0", "^", "$", "G", "g", "z", "H", "M", "L",
		"{", "}", "(", ")", "%", "f", "F", "t", "T", ";", ",", "n", "N", "*", "#", "'", "`", "m", "a",
		"d", "c", "y", ">", "<", "x", "X", "D", "C", "s", "S", "Y", "p", "P", "J", "~", "r", "u", ".",
		"i", "a", "I", "A", "o", "O", "v", "V", "q", "@", "\"", "_", "+", "-", "=", "&", "|", "?", "/", ":",
		"1", "2", "3", "5", "9", "0", "w", "e", "o", "p", "(", "[", "]", "\"", "'", "<", ">", "\\",
		"<Esc>", "<CR>", "<BS>", "<Tab>", "<Del>", "<Space>", "<Left>", "<Right>", "<Up>", "<Down>",
		"<Home>", "<End>", "<PageDown>", "<PageUp>", "<C-v>", "<C-r>", "<C-a>", "<C-x>", "<C-d>", "<C-u>",
		"<C-f>", "<C-b>", "<C-e>", "<C-y>", "<C-g>", "é", "世",
	}
	answers := []string{
		"a", "o", "x.*", "\\<o", "s/o/0/g", "%s/e/E/g", "1,2d", "g/e/d", "v/e/d", "sort", "$", "2", "noh", "set nu",
		"m0", "t.", "j", ">", "<", "d", "y", "undo", "redo", "'<,'>d", "%j", "1,3>", "2,3m$", "reg", "marks",
		"ls", "f", "s/\\(a\\)/\\1\\1/", "%s/x*/-/g", "g/./s/^/>/", "k a", "normal", "frob", "", "99", ".,+2y",
	}
	for seed := range 150 {
		text := texts[seed%len(texts)]
		t.Run("", func(t *testing.T) {
			t.Parallel()
			rng := rand.New(rand.NewPCG(uint64(seed), 7))
			v := newViTimeout(t, text, 10*time.Second)
			var played []string
			t.Cleanup(func() {
				if t.Failed() {
					t.Logf("seed %d, text %q, keys: %q", seed, text, played)
				}
			})
			for range 80 {
				k := keys[rng.IntN(len(keys))]
				played = append(played, k)
				v.keys(k)
				if o := v.host.UI().Last("input"); o != nil && o.Open {
					ans := answers[rng.IntN(len(answers))]
					played = append(played, "«"+ans+"»")
					if rng.IntN(6) == 0 {
						v.answerInput(nil)
					} else {
						v.answerInput(&ans)
					}
				}
				v.h.CompleteBuffers() // saves, opens and reverts the commands waited for
				v.check()
				if v.mode() != "vi_normal" && v.mode() != "vi_insert" && v.mode() != "vi_visual" {
					t.Fatalf("seed %d: mode %q after %s", seed, v.mode(), strings.Join(played, ""))
				}
				c := v.cursor()
				lines := v.buf().Lines
				if c.Line < 0 || c.Line >= len(lines) || c.Col < 0 || c.Col > len(lines[c.Line]) {
					t.Fatalf("seed %d: caret %+v outside the buffer %q after %s", seed, c, lines, strings.Join(played, ""))
				}
			}
			for _, l := range v.h.LogText(viPlugin) {
				if strings.HasPrefix(l, "error") {
					t.Fatalf("seed %d: %s\nkeys: %s", seed, l, strings.Join(played, ""))
				}
			}
			// every session ends back in normal mode with two Esc
			v.keys("<Esc><Esc>")
			if v.mode() != "vi_normal" {
				t.Fatalf("seed %d: mode %q after Esc Esc", seed, v.mode())
			}
		})
	}
}

// TestViRandomCommandsNeverBreakThePlugin plays random whole commands, which reach much
// deeper into the editing code than random keys do.
func TestViRandomCommandsNeverBreakThePlugin(t *testing.T) {
	t.Parallel()
	texts := []string{
		"hello world\nsecond line here\n\n  indented (paren [bracket] {brace}) \"quoted\" 'single'\nlast",
		"one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten",
		"héllo wörld 世界 ünï\ncafé 12 -3 0x1f\n\t\ttabbed line\n\n\nx",
		"{\n  a: [1, 2, 3],\n  b: (x + y) * 2,\n  c: <b>bold</b>,\n}\n\nend",
		"one\ntwo\n\nthree (x)\nfour\n",
	}
	commands := []string{
		"dw", "dd", "3dd", "cwX<Esc>", "ciwY<Esc>", "yiw", "p", "P", "dap", "yap", "yy", "di(", "da(", "ci\"Z<Esc>", "x", "X",
		"u", "<C-r>", ".", "3.", "vjd", "Vjy", "Vjd", "<C-v>jd", "<C-v>jIxx<Esc>", "<C-v>jlcQ<Esc>", "<C-v>j$A;<Esc>",
		"v$y", "vipd", "gUU", "g~w", "guiw", "gJ", "J", "5J", ">>", "<<", ">ip", "3>>", "ma", "'a", "`a", "d'a", "*", "#", "n", "N",
		"/", "?", ":", "dG", "dgg", "d}", "y{", "%", "d%", "fa", "dfa", "ta", "dta", ";", ",", "10x", "ggVGd", "viwp", "xp", "ddp",
		"o<Esc>", "O<Esc>", "A;<Esc>", "I-<Esc>", "3ix<Esc>", "3oy<Esc>", "\"ayy\"ap", "\"Ayw", "\"_dd", "<C-a>", "<C-x>",
		"gv", "vo", "voy", "vlllohd", "5~", "r<CR>", "3rz", "S<Esc>", "C<Esc>", "s<Esc>", "D", "Y", "gp", "gP", "2yyjp",
		"zz", "<C-d>", "<C-u>", "H", "M", "L", "G", "gg", "w", "b", "e", "ge", "W", "B", "E", "0", "$", "^", "_", "+", "-",
		"j", "k", "h", "l", "<Esc>", "ci{<Esc>", "yit", "dit", "vit", "va{d", "vi\"y", "vipJ", "V>", "Vj<", "v~", "vU", "vu", "vr!",
		"vjp", "Vp", "<C-v>jy$p", "<C-v>jy$P", ":", "&", "gi", "gI",
	}
	answers := []string{
		"a", "o", "e", "\\<o", "(", "x.*", "s/o/0/g", "%s/e/E/g", "1,2d", "g/e/d", "v/e/d", "sort", "$", "2", "noh",
		"m0", "t.", "j", ">", "<", "d", "y", "undo", "redo", "'<,'>d", "%j", "1,3>", "2,3m$", "reg", "marks", "ls", "f",
		"s/\\(a\\)/\\1\\1/", "%s/x*/-/g", "g/./s/^/>/", "k a", "", "99", ".,+2y", "+1", "-1,.d", "'a,'bd", "%s/\\n//",
	}
	for seed := range 300 {
		text := texts[seed%len(texts)]
		t.Run("", func(t *testing.T) {
			t.Parallel()
			rng := rand.New(rand.NewPCG(uint64(seed), 11))
			v := newViTimeout(t, text, 10*time.Second)
			var played []string
			t.Cleanup(func() {
				if t.Failed() {
					t.Logf("seed %d, text %q, commands: %q", seed, text, played)
				}
			})
			for range 60 {
				c := commands[rng.IntN(len(commands))]
				played = append(played, c)
				v.keys(c)
				if o := v.host.UI().Last("input"); o != nil && o.Open {
					ans := answers[rng.IntN(len(answers))]
					played = append(played, "«"+ans+"»")
					if rng.IntN(8) == 0 {
						v.answerInput(nil)
					} else {
						v.answerInput(&ans)
					}
				}
				v.h.CompleteBuffers()
				v.check()
				if m := v.mode(); m != "vi_normal" && m != "vi_insert" && m != "vi_visual" {
					t.Fatalf("seed %d: mode %q", seed, m)
				}
				cur := v.cursor()
				lines := v.buf().Lines
				if cur.Line < 0 || cur.Line >= len(lines) || cur.Col < 0 || cur.Col > len(lines[cur.Line]) {
					t.Fatalf("seed %d: caret %+v outside the buffer %q", seed, cur, lines)
				}
			}
			v.keys("<Esc><Esc>")
			if v.mode() != "vi_normal" {
				t.Fatalf("seed %d: mode %q after Esc Esc", seed, v.mode())
			}
		})
	}
}
