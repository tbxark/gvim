//go:build race

package bundled_test

// raceDetector scales the wall-clock budgets of the vi tests: the race detector makes the Lua
// VM an order of magnitude slower.
const raceDetector = true
