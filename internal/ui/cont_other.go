//go:build !unix

package ui

// onContinue does nothing here: there is no stop signal to continue from.
func onContinue(func()) (stop func()) { return func() {} }
