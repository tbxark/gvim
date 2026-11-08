package bundled_test

import "testing"

// ctrl+c is an Esc in vi: it ends the insert, the visual mode and a pending command, as it does
// in Vim. It must not reach the editor, where it copies.
func TestViCtrlCIsEscape(t *testing.T) {
	t.Parallel()
	runViCases(t, []viCase{
		{"leaves insert", "abc\n", [2]int{0, 0}, "iX<C-c>x", "abc\n", [2]int{0, 0}, ""},
		{"leaves visual", "abc\n", [2]int{0, 0}, "vl<C-c>x", "ac\n", [2]int{0, 1}, ""},
		{"ends a pending operator", "abc\n", [2]int{0, 0}, "d<C-c>x", "bc\n", [2]int{0, 0}, ""},
		{"ends a pending count and register", "abc\n", [2]int{0, 0}, "3\"a<C-c>x", "bc\n", [2]int{0, 0}, ""},
		{"ends a pending find", "abc\n", [2]int{0, 0}, "f<C-c>x", "bc\n", [2]int{0, 0}, ""},
	})
}
