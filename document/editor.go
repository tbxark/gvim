package document

// Action is the name of an editor command, such as "move_left" or "save".
// Names match the action vocabulary in Fresh's keymaps.
type Action string

// Fail sets err as the status of d and marks it as an error.
func (d *Document) Fail(err error) {
	d.Status = err.Error()
	d.StatusErr = d.Status
}
