package editor

import (
	"fmt"
	"strings"
)

// registerTabsExtraHandlers registers close and save_all.
func registerTabsExtraHandlers(h map[Action]handler) {
	// Fresh's close action runs the same code as close_tab, so in a single
	// pane it closes the active tab.
	h["close"] = closeTab
	h["save_all"] = saveAll
}

// saveAll saves every named tab with unsaved edits. Untitled tabs have no file
// name, and read-only tabs must not change their file, so both are left alone and
// counted. A file that changed on disk is not overwritten, and it is counted as
// failed. The status says what happened.
func saveAll(e *Editor, _ map[string]any) error {
	p, err := e.PlanSaveAll()
	if err != nil {
		return err
	}
	return e.ApplySavePlan(p, p.Run())
}

// saveAllStatus sets the status of the active tab from the results of a Save All:
// the summary, or the summary with the first failure, which is also returned.
func (e *Editor) saveAllStatus(results []SaveResult, p SavePlan) error {
	var first error
	failed := 0
	for _, r := range results {
		if r.Err() != nil {
			failed++
			if first == nil {
				first = r.Err()
			}
		}
	}
	msg := saveAllMessage(len(results)-failed, p.untitled, p.readOnly, failed)
	if first != nil {
		err := fmt.Errorf("%s: %w", msg, first)
		e.setError(err)
		return err
	}
	e.setStatus(msg)
	return nil
}

// saveAllMessage is the status line of save_all.
func saveAllMessage(saved, untitled, readOnly, failed int) string {
	var parts []string
	switch {
	case saved > 0:
		noun := "files"
		if saved == 1 {
			noun = "file"
		}
		parts = append(parts, fmt.Sprintf("Saved %d %s", saved, noun))
	case untitled == 0 && readOnly == 0 && failed == 0:
		return "No modified files to save"
	default:
		parts = append(parts, "No files saved")
	}
	if untitled > 0 {
		parts = append(parts, fmt.Sprintf("skipped %d untitled", untitled))
	}
	if readOnly > 0 {
		parts = append(parts, fmt.Sprintf("skipped %d read-only", readOnly))
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", failed))
	}
	return strings.Join(parts, "; ")
}
