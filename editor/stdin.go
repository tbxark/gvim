package editor

// StdinOptions shapes the untitled buffer that holds standard input.
type StdinOptions struct {
	// Label is the tab name shown for the buffer, e.g. "[stdin]".
	Label string
	// Language is the lexer name (from --stdin-name or content sniffing); empty
	// leaves the buffer with no language.
	Language string
}

// OpenStdinText opens text as an untitled buffer in a new tab (replacing the untouched
// placeholder, as New does) and makes it active. The buffer is unmodified, exactly like
// Fresh's file-backed stdin buffer: its saved state is the piped text, so typing marks it
// modified. It has no path, so Save goes through Save As; the label names it (the stdin
// buffer is "[stdin]") instead of Untitled-N. Returns the document.
func (e *Editor) OpenStdinText(text string, opts StdinOptions) *Document {
	d := newStdinDocument(text)
	if opts.Label != "" {
		d.SetLabel(opts.Label)
	}
	if opts.Language != "" {
		d.SetLanguage(opts.Language)
	}
	if e.opts.ReadOnly {
		d.SetReadOnly(true)
	}
	if e.pristineUntitled() && e.docs[0].UntitledNumber() == 0 {
		e.releaseDoc(e.docs[0])
		e.setDoc(0, d)
		e.switchTo(0)
	} else {
		e.addDoc(d)
		e.switchTo(len(e.docs) - 1)
	}
	return d
}
