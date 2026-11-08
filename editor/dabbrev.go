package editor

// registerDabbrevHandlers registers dabbrev_expand.
func registerDabbrevHandlers(h map[Action]handler) {
	h["dabbrev_expand"] = dabbrevExpand
}

// dabbrevExpand completes the word before the caret from earlier words of the
// document. A repeated press, with the text untouched, moves to the next
// candidate.
func dabbrevExpand(e *Editor, _ map[string]any) error {
	d := e.Active()
	c := &e.extras.dabbrev
	if !c.Shows(d) {
		if err := c.Begin(d); err != nil {
			*c = dabbrevCycle{}
			return err
		}
	}
	c.Step(d)
	return nil
}
