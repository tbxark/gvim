package plugin

// SetCompileHook makes f run in the discovery Work just before a plugin is compiled; it
// returns the function that restores the previous hook. Tests use it to see on which
// goroutine plugins are compiled.
func SetCompileHook(f func(plugin string)) (restore func()) {
	old := compileHook
	compileHook = f
	return func() { compileHook = old }
}
