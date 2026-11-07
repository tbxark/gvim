//go:build race

package plugin_test

// stdlibRace scales the wall-clock bounds of the stdlib tests: the race detector makes the
// Lua VM and the Go builtins an order of magnitude slower.
const stdlibRace = true
