package plugin

import (
	"fmt"
	"strings"
)

// planOrder orders set for loading: topological over requires and after, ties by the
// position in set (callers pass source order, init last). A candidate that cannot load
// because of a dependency is not in order and has its reason in blocked:
//
//   - a required plugin is neither in set nor satisfied by ext (not installed, disabled,
//     failed);
//   - a required plugin of set is itself blocked;
//   - it is part of, or behind, a dependency cycle.
//
// "after" names only order: a missing or blocked one is ignored.
func planOrder(set []*candidate, ext func(name string) (ok bool, why string)) (order []*candidate, blocked map[*candidate]string) {
	blocked = map[*candidate]string{}
	in := make(map[string]*candidate, len(set))
	for _, c := range set {
		in[c.name] = c
	}
	for changed := true; changed; {
		changed = false
		for _, c := range set {
			if blocked[c] != "" {
				continue
			}
			for _, req := range c.man.Requires {
				if dep := in[req]; dep != nil {
					if blocked[dep] != "" {
						blocked[c] = fmt.Sprintf("requires %q, which cannot be loaded", req)
						changed = true
						break
					}
					continue
				}
				if ok, why := ext(req); !ok {
					blocked[c] = fmt.Sprintf("requires %q (%s)", req, why)
					changed = true
					break
				}
			}
		}
	}

	// Kahn's algorithm over what is left; the first ready candidate of set goes first.
	var left []*candidate
	for _, c := range set {
		if blocked[c] == "" {
			left = append(left, c)
		}
	}
	for len(left) > 0 {
		picked := -1
		for i, c := range left {
			if depsPlaced(c, left, in) {
				picked = i
				break
			}
		}
		if picked < 0 {
			break
		}
		order = append(order, left[picked])
		left = append(left[:picked:picked], left[picked+1:]...)
	}
	if len(left) > 0 {
		names := make([]string, len(left))
		for i, c := range left {
			names[i] = c.name
		}
		why := "in or behind a dependency cycle (" + strings.Join(names, ", ") + ")"
		for _, c := range left {
			blocked[c] = why
		}
	}
	return order, blocked
}

// depsPlaced reports whether no requires/after name of c is still waiting in left.
func depsPlaced(c *candidate, left []*candidate, in map[string]*candidate) bool {
	waiting := func(name string) bool {
		dep := in[name]
		if dep == nil || dep == c {
			return false
		}
		for _, l := range left {
			if l == dep {
				return true
			}
		}
		return false
	}
	for _, n := range c.man.Requires {
		if waiting(n) {
			return false
		}
	}
	for _, n := range c.man.After {
		if waiting(n) {
			return false
		}
	}
	return true
}
