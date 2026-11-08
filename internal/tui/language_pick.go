package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/list"
	"github.com/tbxark/gvim/syntax"
)

// plainTextLanguage is the language that Plain Text sets. The editor keeps it as the
// tab's language, the highlighter finds the plaintext lexer for it, and the status row
// shows it as "Text".
const plainTextLanguage = "plaintext"

// languageAction runs set_language. Without a name it opens the language list. A name
// must be a language the highlighter knows, so a typo is refused instead of becoming
// the language of the buffer: an alias such as "py" is set as its language, "Python".
// An empty name goes back to the language the file name gives. It reports the command
// the action starts and whether it took the action.
func (m *Model) languageAction(args map[string]any) (tea.Cmd, bool) {
	raw, ok := args["name"]
	if !ok {
		m.openLanguages()
		return nil, true
	}
	name, ok := raw.(string)
	if !ok || strings.TrimSpace(name) == "" {
		return nil, false
	}
	if strings.EqualFold(strings.TrimSpace(name), plainTextLanguage) {
		name = plainTextLanguage
	} else if canonical, known := syntax.Lookup(name); !known {
		m.notice = fmt.Sprintf("Unknown language %q: choose one from the list (Set Language)", name)
		return nil, true
	} else {
		name = canonical
	}
	if name == raw {
		return nil, false
	}
	return m.dispatch(Command{Action: "set_language", Args: map[string]any{"name": name}}), true
}

// openLanguages lists Plain Text and the languages of the highlighter, filterable by
// typing. The language of the active document is marked, and choosing a row runs
// set_language with its name.
func (m *Model) openLanguages() {
	current := ""
	if doc := m.ed.Active(); doc != nil {
		if l, ok := doc.(interface{ Language() string }); ok {
			current = l.Language()
		}
	}
	names := append([]string{"Plain Text"}, syntax.Languages()...)
	rows := make([]list.Row, len(names))
	for i, n := range names {
		value := n
		if i == 0 {
			value = plainTextLanguage
		}
		right := ""
		if strings.EqualFold(current, value) {
			right = "current"
		}
		rows[i] = list.Row{Left: n, Right: right, Value: value}
	}
	v := list.New("Select Language", rows, m.listChrome(), func(v *list.View, r list.Row) tea.Cmd {
		v.Close()
		return m.dispatch(Command{Action: "set_language", Args: map[string]any{"name": r.Value}})
	})
	v.BestFirst = true
	m.pushOverlay(v)
}

// languageLabel is the language as the status row shows it: plain text is "Text".
func languageLabel(language string) string {
	if strings.EqualFold(language, plainTextLanguage) {
		return "Text"
	}
	return language
}
