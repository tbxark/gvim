package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/ui/icons"
)

// iconLabels names each icon use in the preview.
var iconLabels = map[icons.Icon]string{
	icons.Refresh:      "Refresh",
	icons.Close:        "Close",
	icons.New:          "New tab",
	icons.DirCollapsed: "Folder, collapsed",
	icons.DirExpanded:  "Folder, expanded",
	icons.ScrollLeft:   "Tabs scroll left",
	icons.ScrollRight:  "Tabs scroll right",
	icons.FoldClosed:   "Fold, collapsed",
	icons.FoldOpen:     "Fold, expanded",
	icons.Folder:       "Folder icon",
	icons.FolderOpen:   "Folder icon, open",
	icons.File:         "File icon",
}

// previewFiles are the file names whose icons the preview samples, and previewGit the
// git status letters.
var (
	previewFiles = []string{"a.go", "a.py", "a.js", "a.ts", "a.md", "a.json", "a.yaml", "Makefile", "a.sh", "a.png"}
	previewGit   = "AMDRUC"
)

// spaced joins the cells of the icons, one blank between them.
func spaced(cells []string) string { return strings.Join(cells, " ") }

// iconPreviewLines is the body of the icon preview. It draws the glyphs the
// interface uses in both sets, whatever the current mode is, so a user can judge
// whether the terminal font draws the Nerd Font set before turning it on.
func iconPreviewLines(s Settings, current icons.Mode) []string {
	row := func(label, plain, nerd, name string) string {
		return fmt.Sprintf("  %-18s %-11s %-19s %s", label, plain, nerd, name)
	}
	lines := []string{
		"Current ui_icons: " + iconsSettingText(s, current),
		"  " + orNone(s.IconsWhy),
		row("", "plain", "nerd", "name, NF "+icons.NerdFontsVersion),
	}
	for _, i := range icons.All() {
		plain := icons.Plain.Glyph(i)
		if plain == "" {
			plain = "-"
		}
		lines = append(lines, row(iconLabels[i], plain, icons.Nerd.Glyph(i), icons.GlyphName(i)))
	}
	var files, git, gitPlain []string
	for _, f := range previewFiles {
		files = append(files, icons.Nerd.FileIcon(f, false, false))
	}
	for _, r := range previewGit {
		git = append(git, icons.Nerd.GitMark(r))
		gitPlain = append(gitPlain, icons.Plain.GitMark(r))
	}
	return append(lines,
		row("File types", "-", spaced(files), "tree and tabs"),
		row("Git marks", strings.Join(gitPlain, ""), spaced(git), "tree"),
		"Boxes, gaps or overlaps in the nerd column: the font lacks the glyphs.",
		`ui_icons: "auto" (default), "nerd" or "plain"; restart gvim to apply.`,
		`ui_icons_font: the terminal's font, e.g. "JetBrainsMono Nerd Font Mono"`,
	)
}

// iconsSettingText is the ui_icons value as the settings list shows it: "auto" shows
// the mode it resolved to.
func iconsSettingText(s Settings, current icons.Mode) string {
	set := s.UIIcons
	if set == "" {
		set = icons.AutoName
	}
	if set == icons.AutoName {
		return set + " -> " + current.String()
	}
	return set
}

// openIconPreviewMsg asks the root to open the icon preview.
type openIconPreviewMsg struct{}

// openIconPreview shows the icon sets side by side, and how to set ui_icons. Enter
// opens the config file, where the setting is edited.
func (m *Model) openIconPreview() {
	v := &boxView{active: true, chrome: m.listChrome()}
	v.content = func() (string, []string, string) {
		return "Interface icons", iconPreviewLines(m.util.settings, m.view.icons), "Enter opens the config file, Esc closes"
	}
	v.onKey = func(k tea.Key) bool {
		if k.Code != tea.KeyEnter {
			return false
		}
		m.openConfigFile()
		return true
	}
	m.pushOverlay(boxOverlay{v: v})
}
