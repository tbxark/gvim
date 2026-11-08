//go:build race

package pattern

// raceEnabled scales the wall-clock bounds and corpus sizes of the tests: the
// race detector makes the matcher an order of magnitude slower.
const raceEnabled = true
