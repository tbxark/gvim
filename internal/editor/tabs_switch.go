package editor

import (
	"errors"
	"fmt"
	"slices"
)

// ErrNoPreviousTab is returned by switch_to_previous_tab when the tab used before
// the current one is not open, or there was none.
var ErrNoPreviousTab = errors.New("no previous tab")

// registerTabSwitchHandlers registers switch_to_previous_tab and switch_to_tab_by_name.
func registerTabSwitchHandlers(h map[Action]handler) {
	h["switch_to_previous_tab"] = func(e *Editor, _ map[string]any) error {
		i := slices.Index(e.docs, e.prevDoc)
		if i < 0 || e.docs[i] == e.Active() {
			return ErrNoPreviousTab
		}
		e.switchTo(i)
		e.setStatus("Switched to " + e.Active().Name())
		return nil
	}
	// switch_to_tab_by_name takes the name argument. The name is the file name of the
	// tab, without the preview suffix, and the first tab with that name is activated.
	h["switch_to_tab_by_name"] = func(e *Editor, args map[string]any) error {
		name, ok := args["name"].(string)
		if !ok {
			return errors.New("switch_to_tab_by_name: missing string argument \"name\"")
		}
		i := slices.IndexFunc(e.docs, func(d *Document) bool { return d.Name() == name })
		if i < 0 {
			return fmt.Errorf("switch_to_tab_by_name: no tab named %q", name)
		}
		e.switchTo(i)
		e.setStatus("Switched to " + name)
		return nil
	}
}
