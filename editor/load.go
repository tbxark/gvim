package editor

import "fmt"

// LoadFunc returns a function that reads path into a document for OpenLoaded. The
// new-file line ending and the large file settings are taken from the options now. The
// function reads no other editor state, so it may run on another goroutine, as a
// command does. Its errors are the ones OpenFile returns, but it does not write them
// to the status row. A large file that is not UTF-8 returns an ErrLargeNeedsFullLoad;
// LoadFullFunc loads it after the user agrees.
func (e *Editor) LoadFunc(path string) func() (*Document, error) {
	spec := e.loadSpec()
	return func() (*Document, error) {
		if path == "" {
			return nil, fmt.Errorf("open: %w", ErrEmptyPath)
		}
		abs, err := absPath(path)
		if err != nil {
			return nil, err
		}
		return loadDocument(abs, spec)
	}
}

// OpenLoaded opens a document from LoadFunc in a new tab and makes it active, as
// OpenFile does. If its path is already open, that tab is activated and d is
// dropped, and released, so the text of the open tab is kept. It changes editor state,
// so it runs on the UI goroutine.
func (e *Editor) OpenLoaded(d *Document) {
	if i := e.pathIndex(d.Path); i >= 0 {
		if e.docs[i] != d {
			e.releaseDoc(d)
		}
		e.docs[i].preview = false
		if i != e.active {
			e.pushJump()
		}
		e.switchTo(i)
		e.setStatus("Opened " + e.docs[i].Name())
		return
	}
	e.pushJump()
	if e.pristineUntitled() {
		// The untouched startup buffer is replaced, not kept as a tab.
		e.releaseDoc(e.docs[0])
		e.setDoc(0, d)
		e.switchTo(0)
	} else {
		e.addDoc(d)
		e.switchTo(len(e.docs) - 1)
	}
	e.setStatus(d.openStatus())
}
