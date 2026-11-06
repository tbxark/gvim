// Package icons holds the glyphs of the interface controls: the buttons of the tab
// strip and of the explorer title, the expand and fold marks, the scroll markers,
// the file type icons of the tree and the tabs, and the git status marks. Each
// control has a plain glyph, which every terminal draws, and a Nerd Font glyph. A
// Mode picks which of the two a session shows. Resolve picks the Mode from the
// config and the terminal.
//
// The package covers interface controls only. It is not used for the editor text,
// for user input or for file contents.
//
// Every glyph in both modes is one terminal cell wide, so swapping the mode never
// moves a button. The Nerd Font glyphs are single-cell glyphs of Nerd Fonts in the
// Basic Multilingual Plane, so "Nerd Font Mono" is the family to recommend. The code
// points do not name a font family. The file type icons and the git marks have no
// plain form for some uses: plain draws the letter of a git mark, and no file icon.
package icons

import (
	"path"
	"strings"
)

// NerdFontsVersion is the Nerd Fonts release the code points below were checked
// against: each is present in JetBrainsMono Nerd Font Mono 3.5.1. They are Font
// Awesome glyphs, which Nerd Fonts has kept at these code points across releases.
const NerdFontsVersion = "3.5.1"

// Mode selects the glyph set. The zero value is Plain, so a caller that sets no mode
// keeps the interface gvim had before the Nerd Font set existed.
type Mode uint8

const (
	Plain Mode = iota // characters every terminal draws
	Nerd              // Nerd Font glyphs, in the private use area
)

// Names of the modes as the ui_icons setting writes them. AutoName is the setting
// that leaves the choice to Resolve.
const (
	PlainName = "plain"
	NerdName  = "nerd"
	AutoName  = "auto"
)

// Parse returns the mode that name writes, and false when name is not one.
func Parse(name string) (Mode, bool) {
	switch name {
	case PlainName:
		return Plain, true
	case NerdName:
		return Nerd, true
	}
	return Plain, false
}

// String is the name of the mode as the ui_icons setting writes it.
func (m Mode) String() string {
	if m == Nerd {
		return NerdName
	}
	return PlainName
}

// Icon names one use of a glyph. Components ask for a use, never for a code point.
type Icon uint8

const (
	Close        Icon = iota // closes a tab or hides the explorer
	Refresh                  // reloads the explorer tree
	New                      // opens a new tab
	DirExpanded              // a directory whose children are listed
	DirCollapsed             // a directory whose children are hidden
	ScrollLeft               // tabs are scrolled off the left of the strip
	ScrollRight              // tabs are scrolled off the right of the strip
	FoldOpen                 // an expanded fold in the gutter
	FoldClosed               // a collapsed fold in the gutter
	Folder                   // a directory, before its name; plain draws none
	FolderOpen               // an expanded directory, before its name; plain draws none
	File                     // a file of no known type, before its name; plain draws none
	count
)

// entry is one row of the table: the glyph of each mode, and the Nerd Fonts name of
// the Nerd Font glyph, for whoever checks or replaces a code point.
type entry struct {
	plain    string
	nerd     string
	nerdName string
}

// table is indexed by Icon. Both modes cover every use.
var table = [count]entry{
	Close:        {"×", "\uf00d", "nf-fa-times"},
	Refresh:      {"⟳", "\uf021", "nf-fa-refresh"},
	New:          {"+", "\uf067", "nf-fa-plus"},
	DirExpanded:  {"▼", "\uf078", "nf-fa-chevron_down"},
	DirCollapsed: {"▶", "\uf054", "nf-fa-chevron_right"},
	ScrollLeft:   {"<", "\uf053", "nf-fa-chevron_left"},
	ScrollRight:  {">", "\uf054", "nf-fa-chevron_right"},
	FoldOpen:     {"▾", "\uf078", "nf-fa-chevron_down"},
	FoldClosed:   {"▸", "\uf054", "nf-fa-chevron_right"},
	Folder:       {"", "\uf07b", "nf-fa-folder"},
	FolderOpen:   {"", "\uf07c", "nf-fa-folder_open"},
	File:         {"", "\uf15b", "nf-fa-file"},
}

// Glyph returns the text that draws i in mode m. It is one cell wide, or empty for a
// use that plain mode does not draw (Folder, FolderOpen and File).
func (m Mode) Glyph(i Icon) string {
	if i >= count {
		return ""
	}
	if m == Nerd {
		return table[i].nerd
	}
	return table[i].plain
}

// GlyphName returns the Nerd Fonts name of the Nerd Font glyph of i, such as
// "nf-fa-times".
func GlyphName(i Icon) string {
	if i >= count {
		return ""
	}
	return table[i].nerdName
}

// All lists every use, in the order the preview shows them.
func All() []Icon {
	return []Icon{Refresh, Close, New, DirCollapsed, DirExpanded, ScrollLeft, ScrollRight, FoldClosed, FoldOpen, Folder, FolderOpen, File}
}

// fileIcon is the Nerd Font glyph of a file type, with its Nerd Fonts name.
type fileIcon struct{ glyph, name string }

// fileNames holds the icons of whole file names, keyed by the lower-case base name.
// It is tried before fileExts.
var fileNames = map[string]fileIcon{
	"makefile":       {"\ue779", "nf-dev-gnu"},
	"dockerfile":     {"\uf308", "nf-linux-docker"},
	"go.mod":         {"\ue627", "nf-seti-go"},
	"go.sum":         {"\ue627", "nf-seti-go"},
	"package.json":   {"\ue71e", "nf-dev-npm"},
	"cargo.toml":     {"\ue7a8", "nf-dev-rust"},
	"license":        {"\uf0a3", "nf-fa-certificate"},
	".gitignore":     {"\ue702", "nf-dev-git"},
	".gitattributes": {"\ue702", "nf-dev-git"},
	".gitmodules":    {"\ue702", "nf-dev-git"},
	".bashrc":        {"\ue795", "nf-dev-terminal"},
	".zshrc":         {"\ue795", "nf-dev-terminal"},
}

// fileExts holds the icons of file extensions, keyed by the lower-case extension
// without its dot.
var fileExts = map[string]fileIcon{
	"go":       {"\ue627", "nf-seti-go"},
	"py":       {"\ue73c", "nf-dev-python"},
	"js":       {"\ue74e", "nf-dev-javascript"},
	"mjs":      {"\ue74e", "nf-dev-javascript"},
	"cjs":      {"\ue74e", "nf-dev-javascript"},
	"ts":       {"\ue628", "nf-seti-typescript"},
	"jsx":      {"\ue7ba", "nf-dev-react"},
	"tsx":      {"\ue7ba", "nf-dev-react"},
	"rs":       {"\ue7a8", "nf-dev-rust"},
	"c":        {"\ue61e", "nf-custom-c"},
	"h":        {"\ue61e", "nf-custom-c"},
	"cpp":      {"\ue61d", "nf-custom-cpp"},
	"cc":       {"\ue61d", "nf-custom-cpp"},
	"hpp":      {"\ue61d", "nf-custom-cpp"},
	"java":     {"\ue738", "nf-dev-java"},
	"kt":       {"\ue634", "nf-seti-kotlin"},
	"swift":    {"\ue755", "nf-dev-swift"},
	"rb":       {"\ue739", "nf-dev-ruby"},
	"php":      {"\ue73d", "nf-dev-php"},
	"lua":      {"\ue620", "nf-seti-lua"},
	"vim":      {"\ue62b", "nf-custom-vim"},
	"vue":      {"\ue6a0", "nf-seti-vue"},
	"sh":       {"\ue795", "nf-dev-terminal"},
	"bash":     {"\ue795", "nf-dev-terminal"},
	"zsh":      {"\ue795", "nf-dev-terminal"},
	"html":     {"\ue736", "nf-dev-html5"},
	"htm":      {"\ue736", "nf-dev-html5"},
	"css":      {"\ue749", "nf-dev-css3"},
	"scss":     {"\ue74b", "nf-dev-sass"},
	"md":       {"\ue73e", "nf-dev-markdown"},
	"markdown": {"\ue73e", "nf-dev-markdown"},
	"json":     {"\ue60b", "nf-seti-json"},
	"yaml":     {"\ue615", "nf-seti-config"},
	"yml":      {"\ue615", "nf-seti-config"},
	"toml":     {"\ue615", "nf-seti-config"},
	"ini":      {"\ue615", "nf-seti-config"},
	"conf":     {"\ue615", "nf-seti-config"},
	"xml":      {"\uf1c9", "nf-fa-file_code_o"},
	"txt":      {"\uf15c", "nf-fa-file_text"},
	"lock":     {"\uf023", "nf-fa-lock"},
	"sql":      {"\uf1c0", "nf-fa-database"},
	"png":      {"\uf1c5", "nf-fa-file_image_o"},
	"jpg":      {"\uf1c5", "nf-fa-file_image_o"},
	"jpeg":     {"\uf1c5", "nf-fa-file_image_o"},
	"gif":      {"\uf1c5", "nf-fa-file_image_o"},
	"svg":      {"\uf1c5", "nf-fa-file_image_o"},
	"pdf":      {"\uf1c1", "nf-fa-file_pdf_o"},
	"zip":      {"\uf1c6", "nf-fa-file_archive_o"},
	"tar":      {"\uf1c6", "nf-fa-file_archive_o"},
	"gz":       {"\uf1c6", "nf-fa-file_archive_o"},
}

// FileIcon returns the icon drawn before a file name, or "" when mode m draws none:
// plain mode draws no file icons. dir selects the folder icon, open or closed by
// expanded. A file is looked up by its whole name, then by its extension, and a file
// of no known type gets the generic file icon. name may be a path.
func (m Mode) FileIcon(name string, dir, expanded bool) string {
	if m != Nerd {
		return ""
	}
	switch {
	case dir && expanded:
		return table[FolderOpen].nerd
	case dir:
		return table[Folder].nerd
	}
	base := strings.ToLower(path.Base(strings.ReplaceAll(name, "\\", "/")))
	if f, ok := fileNames[base]; ok {
		return f.glyph
	}
	if ext := strings.TrimPrefix(path.Ext(base), "."); ext != "" {
		if f, ok := fileExts[ext]; ok {
			return f.glyph
		}
	}
	return table[File].nerd
}

// gitMarks holds the Nerd Font glyph of each git status letter the explorer shows.
var gitMarks = map[rune]fileIcon{
	'A': {"\uf457", "nf-oct-diff_added"},
	'M': {"\uf459", "nf-oct-diff_modified"},
	'D': {"\uf458", "nf-oct-diff_removed"},
	'R': {"\uf45a", "nf-oct-diff_renamed"},
	'U': {"\uf128", "nf-fa-question"},
	'C': {"\uf071", "nf-fa-warning"},
}

// GitMark returns the text that draws the git status letter r in mode m: the letter
// itself in plain mode, and for a letter with no glyph.
func (m Mode) GitMark(r rune) string {
	if m == Nerd {
		if g, ok := gitMarks[r]; ok {
			return g.glyph
		}
	}
	return string(r)
}

// IsNerdFontName reports whether a font family name is one of a Nerd Font: it holds
// "Nerd Font" or "NerdFont", or it ends in the patched-font suffixes NF, NFM or NFP,
// as in "JetBrainsMono NFM". The match ignores case.
func IsNerdFontName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if strings.Contains(n, "nerd font") || strings.Contains(n, "nerdfont") || strings.Contains(n, "nerd-font") {
		return true
	}
	fields := strings.Fields(n)
	if len(fields) < 2 {
		return false
	}
	switch fields[len(fields)-1] {
	case "nf", "nfm", "nfp":
		return true
	}
	return false
}

// Resolution is the mode a session uses, and the reason for it, for the preview.
type Resolution struct {
	Mode Mode
	Why  string
}

// Resolve picks the mode. The setting is the ui_icons value: "nerd" and "plain" are
// taken as written. Any other value, which is "auto", looks for a Nerd Font, which
// gvim cannot see directly because the terminal draws the text, not gvim:
//
//  1. font, the ui_icons_font value, is the font the user says the terminal uses.
//     It wins when it is set: a Nerd Font name gives nerd, any other name gives plain.
//  2. A terminal that draws Nerd Font glyphs without a patched font gives nerd: kitty,
//     WezTerm and Ghostty ship the Nerd Font symbols as a fallback font.
//  3. Otherwise the mode is plain.
//
// getenv reads an environment variable, so the caller can pass a fake in tests.
func Resolve(setting, font string, getenv func(string) string) Resolution {
	switch setting {
	case NerdName:
		return Resolution{Nerd, "set by ui_icons"}
	case PlainName:
		return Resolution{Plain, "set by ui_icons"}
	}
	if font != "" {
		if IsNerdFontName(font) {
			return Resolution{Nerd, `ui_icons_font "` + font + `" is a Nerd Font`}
		}
		return Resolution{Plain, `ui_icons_font "` + font + `" is not a Nerd Font`}
	}
	if t := nerdTerminal(getenv); t != "" {
		return Resolution{Nerd, t + " draws Nerd Font symbols itself"}
	}
	return Resolution{Plain, "no Nerd Font detected; set ui_icons_font or ui_icons"}
}

// nerdTerminal names the terminal that getenv shows, when it is one that draws the
// Nerd Font symbols with any font. It is "" for any other terminal.
func nerdTerminal(getenv func(string) string) string {
	switch {
	case getenv("KITTY_WINDOW_ID") != "" || getenv("TERM") == "xterm-kitty":
		return "kitty"
	case getenv("TERM_PROGRAM") == "WezTerm" || getenv("WEZTERM_EXECUTABLE") != "":
		return "WezTerm"
	case getenv("TERM_PROGRAM") == "ghostty" || getenv("TERM") == "xterm-ghostty" || getenv("GHOSTTY_RESOURCES_DIR") != "":
		return "Ghostty"
	}
	return ""
}
