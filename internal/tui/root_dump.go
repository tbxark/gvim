package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/editor"
)

// configDump is the JSON shape of Dump Config. Its keys are the names the config file
// uses. It has the settings the root is given, which is the snapshot the app takes
// when it starts.
type configDump struct {
	Theme               string `json:"theme"`
	Keymap              string `json:"keymap"`
	TabSize             int    `json:"tab_size"`
	InsertSpaces        bool   `json:"insert_spaces"`
	AutoIndent          bool   `json:"auto_indent"`
	LineNumbers         bool   `json:"line_numbers"`
	ShowMenuBar         bool   `json:"show_menu_bar"`
	ExplorerShowHidden  bool   `json:"explorer_show_hidden"`
	ExplorerShowIgnored bool   `json:"explorer_show_ignored"`
}

// dumpConfig returns the settings s as indented JSON, with a final newline.
func dumpConfig(s Settings) string {
	b, _ := json.MarshalIndent(configDump{
		Theme:               s.Theme,
		Keymap:              s.Keymap,
		TabSize:             s.TabSize,
		InsertSpaces:        s.InsertSpaces,
		AutoIndent:          s.AutoIndent,
		LineNumbers:         s.LineNumbers,
		ShowMenuBar:         s.MenuBar,
		ExplorerShowHidden:  s.ExplorerShowHidden,
		ExplorerShowIgnored: s.ExplorerShowIgnored,
	}, "", "  ")
	return string(b) + "\n"
}

// openScratch opens an untitled tab that holds text, with the caret at its top. The
// text is not saved to any file.
func (m *Model) openScratch(text string) tea.Cmd {
	m.focusEditor()
	if res := m.editorDo(editor.Action("new"), nil); res.Err != nil {
		m.notice = res.Err.Error()
		return nil
	}
	if res := m.editorDo(editor.Action("insert_char"), map[string]any{"text": text}); res.Err != nil {
		m.notice = res.Err.Error()
	}
	m.editorDo(editor.Action("move_document_start"), nil)
	m.syncViewport()
	return nil
}

// uiTree describes the model in text: the frame size, the focus, the explorer, the tabs,
// the panes, the overlays, the menu and the terminal dock.
func (m *Model) uiTree() string {
	var b strings.Builder
	b.WriteString("gvim ui tree\n")
	fmt.Fprintf(&b, "size %dx%d\n", m.width, m.height)
	fmt.Fprintf(&b, "focus %s\n", m.focusName())
	explorer := "closed"
	if m.explorerOpen {
		explorer = "open"
	}
	fmt.Fprintf(&b, "explorer %s\n", explorer)
	tabs := m.ed.Tabs()
	fmt.Fprintf(&b, "tabs %d active %d\n", len(tabs), m.activeTab()+1)
	for i, t := range tabs {
		fmt.Fprintf(&b, "  %d %s", i+1, t.Name)
		if t.Modified {
			b.WriteString(" modified")
		}
		if t.Preview {
			b.WriteString(" preview")
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "panes %d\n", len(m.splits.root.leaves()))
	fmt.Fprintf(&b, "overlays %d\n", len(m.overlays))
	for _, o := range m.overlays {
		fmt.Fprintf(&b, "  %T\n", o)
	}
	fmt.Fprintf(&b, "menu %s\n", openWord(m.menu.IsOpen()))
	fmt.Fprintf(&b, "dock %s\n", openWord(m.dock.open))
	return b.String()
}

// focusName names the part of the frame that takes the keys now.
func (m *Model) focusName() string {
	switch {
	case len(m.overlays) > 0:
		return "overlay"
	case m.explorerFocus:
		return "explorer"
	case m.tabFocus:
		return "tabs"
	case m.dock.focus:
		return "dock"
	case m.menu.IsOpen():
		return "menu"
	}
	return "editor"
}

// openWord is "open" or "closed" for a flag.
func openWord(open bool) string {
	if open {
		return "open"
	}
	return "closed"
}
