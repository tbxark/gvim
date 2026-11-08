package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// LanguageSettings is one languages.<id> entry. A pointer field is an override: nil
// means the entry leaves the value to the global editor.* option. Layers merge field
// by field, so a project file can set languages.go.tab_size and keep the user file's
// languages.go.use_tabs. A list field is replaced as a whole by the later layer.
//
// The names are Fresh's. Settings that gvim does not use yet are parsed and kept
// (see the contract).
type LanguageSettings struct {
	Extensions []string // file extensions without the dot, matched case-insensitively
	Filenames  []string // base names; a name that starts with "*" matches a suffix
	Grammar    string   // grammar name, kept as written

	CommentPrefix      *string // comment_prefix
	AutoIndent         *bool   // auto_indent
	AutoClose          *bool   // auto_close; only turns it off when editor.auto_close is on
	AutoSurround       *bool   // auto_surround; same rule as AutoClose
	TextmateGrammar    *string // textmate_grammar: path of a custom grammar
	ShowWhitespaceTabs *bool   // show_whitespace_tabs: false hides the tab marks
	LineWrap           *bool   // line_wrap
	WrapColumn         *int    // wrap_column; 0 in the file means no override
	PageView           *bool   // page_view
	PageWidth          *int    // page_width
	UseTabs            *bool   // use_tabs
	TabSize            *int    // tab_size; 0 in the file means no override
	Formatter          *Formatter
	FormatOnSave       *bool          // format_on_save
	OnSave             []OnSaveAction // on_save
	WordCharacters     *string        // word_characters: extra word characters
	Indent             *IndentRules   // indent
	IndentationGuide   *bool          // indentation_guide
}

// Formatter is languages.<id>.formatter.
type Formatter struct {
	Command   string   `json:"command"`
	Args      []string `json:"args,omitempty"`
	Stdin     bool     `json:"stdin"`      // default true
	TimeoutMS int      `json:"timeout_ms"` // default 10000
}

// OnSaveAction is one entry of languages.<id>.on_save.
type OnSaveAction struct {
	Command    string   `json:"command"`
	Args       []string `json:"args,omitempty"`
	WorkingDir string   `json:"working_dir,omitempty"`
	Stdin      bool     `json:"stdin"`      // default false
	TimeoutMS  int      `json:"timeout_ms"` // default 10000
	Enabled    bool     `json:"enabled"`    // default true
}

// IndentRules is languages.<id>.indent: regular expressions that tell when the next
// line is indented or dedented. An empty pattern is unset.
type IndentRules struct {
	IncreaseIndentPattern string `json:"increase_indent_pattern,omitempty"`
	DecreaseIndentPattern string `json:"decrease_indent_pattern,omitempty"`
	IndentNextLinePattern string `json:"indent_next_line_pattern,omitempty"`
	DedentNextLinePattern string `json:"dedent_next_line_pattern,omitempty"`
	SelfClosePattern      string `json:"self_close_pattern,omitempty"`
}

// Defaults of the Formatter and OnSaveAction fields that a file may leave out.
const (
	DefaultFormatterTimeoutMS = 10000
	DefaultOnSaveTimeoutMS    = 10000
)

// textLanguage is the id of a file whose type is not detected.
const textLanguage = "text"

func ptr[T any](v T) *T { return &v }

// builtinLanguages holds the languages.<id> entries that Fresh ships with and that
// differ from the global options, plus the file names and extensions that find them
// when a document has no language name. Entries that would change nothing are left
// out. A user entry is merged over these field by field.
var builtinLanguages = map[string]LanguageSettings{
	"makefile": {
		Extensions: []string{"mk"}, Filenames: []string{"Makefile", "makefile", "GNUmakefile"},
		AutoIndent: ptr(false), UseTabs: ptr(true), TabSize: ptr(8),
	},
	// Markdown is prose: quotes and brackets are mostly literal text, so neither
	// auto_close nor auto_surround is on, and each can be turned back on alone.
	"markdown": {
		Extensions: []string{"md", "markdown"}, Filenames: []string{"README"},
		AutoIndent: ptr(false), AutoClose: ptr(false), AutoSurround: ptr(false),
	},
	"go": {
		Extensions: []string{"go"},
		UseTabs:    ptr(true), TabSize: ptr(8), ShowWhitespaceTabs: ptr(false),
	},
	"odin": {
		Extensions: []string{"odin"},
		UseTabs:    ptr(true), TabSize: ptr(8), ShowWhitespaceTabs: ptr(false),
	},
	"justfile": {Filenames: []string{"justfile", "Justfile", ".justfile"}, UseTabs: ptr(true)},
	"gomod":    {Filenames: []string{"go.mod", "go.sum"}, UseTabs: ptr(true)},

	"po":         {Extensions: []string{"po", "pot"}, AutoIndent: ptr(false)},
	"git-rebase": {Filenames: []string{"git-rebase-todo"}, AutoIndent: ptr(false)},
	"git-commit": {Filenames: []string{"COMMIT_EDITMSG", "MERGE_MSG", "SQUASH_MSG", "TAG_EDITMSG"}, AutoIndent: ptr(false)},
	"gitignore":  {Extensions: []string{"gitignore"}, Filenames: []string{".gitignore", ".dockerignore", ".npmignore", ".hgignore"}, AutoIndent: ptr(false)},
	"gitattributes": {
		Extensions: []string{"gitattributes"}, Filenames: []string{".gitattributes"}, AutoIndent: ptr(false),
	},
	"cmake-cache":    {Filenames: []string{"CMakeCache.txt", "cmakecache.txt"}, AutoIndent: ptr(false)},
	"pkg-config":     {Extensions: []string{"pc"}, Filenames: []string{"*.pc.in"}, AutoIndent: ptr(false)},
	"wavefront-obj":  {Extensions: []string{"obj"}, AutoIndent: ptr(false)},
	"doxygen":        {Extensions: []string{"dox", "doxy"}, AutoIndent: ptr(false)},
	"doxygen-config": {Filenames: []string{"Doxyfile", "Doxyfile.in"}, AutoIndent: ptr(false)},
	"ini":            {Extensions: []string{"ini", "cfg"}, AutoIndent: ptr(false)},
}

// languageAliases maps the lowercased language names that gvim's highlighter gives
// (chroma lexer names) to Fresh's language ids, where the two differ. A name that is
// not here is its own id, lowercased. Names that cover several Fresh ids (TeX is both
// latex and tex, JSON is json and jsonc) are left out; the file name decides those.
var languageAliases = map[string]string{
	"c++":                     "cpp",
	"c#":                      "csharp",
	"f#":                      "fsharp",
	"docker":                  "dockerfile",
	"go mod":                  "gomod",
	"gdscript3":               "gdscript",
	"mysql":                   "sql",
	"protocol buffer":         "protobuf",
	"webgpu shading language": "wgsl",
	"fortranfixed":            "fortran",
	"llvm":                    "llvm-ir",
	"nasm":                    "asm",
	"armasm":                  "gas",
	"gettext":                 "po",
	"pkgconfig":               "pkg-config",
	"nu":                      "nushell",
	"phtml":                   "php",
	"plaintext":               textLanguage,
	"plain text":              textLanguage,
	"text only":               textLanguage,
	"fallback":                textLanguage,
}

// LanguageID returns the id under which languages.<id> entries are looked up for a
// language name such as gvim's highlighter gives ("Go", "C++"). The id is the name
// lowercased, with Fresh's spelling for the names in languageAliases. An empty name
// gives "".
func LanguageID(name string) string {
	id := strings.ToLower(strings.TrimSpace(name))
	if a, ok := languageAliases[id]; ok {
		return a
	}
	return id
}

// languageFields maps each key of a languages.<id> entry to the code that applies it.
// A null value clears an override field and is an error elsewhere.
var languageFields = map[string]func(*LanguageSettings, json.RawMessage) error{
	"extensions":           func(l *LanguageSettings, r json.RawMessage) error { return setStrings(&l.Extensions, r) },
	"filenames":            func(l *LanguageSettings, r json.RawMessage) error { return setStrings(&l.Filenames, r) },
	"grammar":              func(l *LanguageSettings, r json.RawMessage) error { return setString(&l.Grammar, r) },
	"comment_prefix":       func(l *LanguageSettings, r json.RawMessage) error { return setOptString(&l.CommentPrefix, r) },
	"auto_indent":          func(l *LanguageSettings, r json.RawMessage) error { return setOptBool(&l.AutoIndent, r) },
	"auto_close":           func(l *LanguageSettings, r json.RawMessage) error { return setOptBool(&l.AutoClose, r) },
	"auto_surround":        func(l *LanguageSettings, r json.RawMessage) error { return setOptBool(&l.AutoSurround, r) },
	"textmate_grammar":     func(l *LanguageSettings, r json.RawMessage) error { return setOptString(&l.TextmateGrammar, r) },
	"show_whitespace_tabs": func(l *LanguageSettings, r json.RawMessage) error { return setOptBool(&l.ShowWhitespaceTabs, r) },
	"line_wrap":            func(l *LanguageSettings, r json.RawMessage) error { return setOptBool(&l.LineWrap, r) },
	"wrap_column":          func(l *LanguageSettings, r json.RawMessage) error { return setOptInt(&l.WrapColumn, r, 0, 0, true) },
	"page_view":            func(l *LanguageSettings, r json.RawMessage) error { return setOptBool(&l.PageView, r) },
	"page_width":           func(l *LanguageSettings, r json.RawMessage) error { return setOptInt(&l.PageWidth, r, 0, 0, false) },
	"use_tabs":             func(l *LanguageSettings, r json.RawMessage) error { return setOptBool(&l.UseTabs, r) },
	"tab_size": func(l *LanguageSettings, r json.RawMessage) error {
		// 0 means "use editor.tab_size", as in Fresh.
		return setOptInt(&l.TabSize, r, MinTabSize, MaxTabSize, true)
	},
	"formatter":         func(l *LanguageSettings, r json.RawMessage) error { return setFormatter(&l.Formatter, r) },
	"format_on_save":    func(l *LanguageSettings, r json.RawMessage) error { return setOptBool(&l.FormatOnSave, r) },
	"on_save":           func(l *LanguageSettings, r json.RawMessage) error { return setOnSave(&l.OnSave, r) },
	"word_characters":   func(l *LanguageSettings, r json.RawMessage) error { return setOptString(&l.WordCharacters, r) },
	"indent":            func(l *LanguageSettings, r json.RawMessage) error { return setIndent(&l.Indent, r) },
	"indentation_guide": func(l *LanguageSettings, r json.RawMessage) error { return setOptBool(&l.IndentationGuide, r) },
	// highlighter is a field that older Fresh versions wrote. It is accepted and ignored.
	"highlighter": func(*LanguageSettings, json.RawMessage) error { return nil },
}

func setStrings(dst *[]string, raw json.RawMessage) error {
	if isNull(raw) {
		return errors.New("null is not a valid value")
	}
	var v []string
	if err := json.Unmarshal(raw, &v); err != nil {
		return errors.New("must be a list of strings")
	}
	*dst = v
	return nil
}

func setString(dst *string, raw json.RawMessage) error {
	if isNull(raw) {
		return errors.New("null is not a valid value")
	}
	s, err := decodeString(raw)
	if err != nil {
		return err
	}
	*dst = s
	return nil
}

func setOptString(dst **string, raw json.RawMessage) error {
	if isNull(raw) {
		*dst = nil
		return nil
	}
	s, err := decodeString(raw)
	if err != nil {
		return err
	}
	*dst = &s
	return nil
}

func setOptBool(dst **bool, raw json.RawMessage) error {
	if isNull(raw) {
		*dst = nil
		return nil
	}
	var v bool
	if err := json.Unmarshal(raw, &v); err != nil {
		return errors.New("must be true or false")
	}
	*dst = &v
	return nil
}

// setOptInt stores an integer override. null clears it. With zeroClears, 0 clears it
// too; lo and hi then bound the non-zero values (hi 0: no upper bound).
func setOptInt(dst **int, raw json.RawMessage, lo, hi int, zeroClears bool) error {
	if isNull(raw) {
		*dst = nil
		return nil
	}
	var n int
	if err := json.Unmarshal(raw, &n); err != nil {
		return errors.New("must be an integer")
	}
	if zeroClears && n == 0 {
		*dst = nil
		return nil
	}
	if _, err := decodeInt(raw, lo, hi); err != nil {
		return err
	}
	*dst = &n
	return nil
}

func setFormatter(dst **Formatter, raw json.RawMessage) error {
	if isNull(raw) {
		*dst = nil
		return nil
	}
	f := Formatter{Stdin: true, TimeoutMS: DefaultFormatterTimeoutMS}
	if err := decodeStrict(raw, &f); err != nil {
		return err
	}
	if strings.TrimSpace(f.Command) == "" {
		return errors.New("command must not be empty")
	}
	*dst = &f
	return nil
}

func setOnSave(dst *[]OnSaveAction, raw json.RawMessage) error {
	if isNull(raw) {
		return errors.New("null is not a valid value")
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return errors.New("must be a list of objects")
	}
	actions := make([]OnSaveAction, 0, len(items))
	for i, item := range items {
		a := OnSaveAction{TimeoutMS: DefaultOnSaveTimeoutMS, Enabled: true}
		if err := decodeStrict(item, &a); err != nil {
			return fmt.Errorf("entry %d: %w", i, err)
		}
		if strings.TrimSpace(a.Command) == "" {
			return fmt.Errorf("entry %d: command must not be empty", i)
		}
		actions = append(actions, a)
	}
	*dst = actions
	return nil
}

func setIndent(dst **IndentRules, raw json.RawMessage) error {
	if isNull(raw) {
		*dst = nil
		return nil
	}
	// Fresh writes null for a pattern that is not set; the pointer values turn that
	// into the empty string.
	var in map[string]*string
	if err := json.Unmarshal(raw, &in); err != nil {
		return errors.New("must be a JSON object")
	}
	var r IndentRules
	fields := map[string]*string{
		"increase_indent_pattern":  &r.IncreaseIndentPattern,
		"decrease_indent_pattern":  &r.DecreaseIndentPattern,
		"indent_next_line_pattern": &r.IndentNextLinePattern,
		"dedent_next_line_pattern": &r.DedentNextLinePattern,
		"self_close_pattern":       &r.SelfClosePattern,
	}
	for _, k := range slices.Sorted(mapKeys(in)) {
		field, ok := fields[k]
		if !ok {
			return fmt.Errorf("unknown field %q", k)
		}
		if in[k] != nil {
			*field = *in[k]
		}
	}
	*dst = &r
	return nil
}

func mapKeys[V any](m map[string]V) func(yield func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// setLanguages applies the languages object entries. See setSection.
func (c *Config) setLanguages(entries map[string]json.RawMessage) (applied []string, failed []sectionFailure) {
	for _, name := range sortedKeys(entries) {
		id := LanguageID(name)
		prefix := "languages." + id
		if id == "" {
			failed = append(failed, sectionFailure{"languages." + name, errors.New("language name must not be empty")})
			continue
		}
		fields, err := decodeObject(entries[name])
		if err != nil {
			failed = append(failed, sectionFailure{prefix, err})
			continue
		}
		cur := c.Languages[id]
		for _, field := range sortedKeys(fields) {
			apply, ok := languageFields[field]
			if !ok {
				failed = append(failed, sectionFailure{prefix + "." + field, errors.New("unknown setting")})
				continue
			}
			// apply works on a copy of the entry, so a field that fails leaves it as it was.
			next := cur
			if err := apply(&next, fields[field]); err != nil {
				failed = append(failed, sectionFailure{prefix + "." + field, err})
				continue
			}
			cur = next
			applied = append(applied, prefix+"."+field)
		}
		if c.Languages == nil {
			c.Languages = make(map[string]LanguageSettings)
		}
		c.Languages[id] = cur
	}
	return applied, failed
}

// overlay returns base with the fields that o sets replacing the ones in base.
func (base LanguageSettings) overlay(o LanguageSettings) LanguageSettings {
	if o.Extensions != nil {
		base.Extensions = o.Extensions
	}
	if o.Filenames != nil {
		base.Filenames = o.Filenames
	}
	if o.Grammar != "" {
		base.Grammar = o.Grammar
	}
	overlayPtr(&base.CommentPrefix, o.CommentPrefix)
	overlayPtr(&base.AutoIndent, o.AutoIndent)
	overlayPtr(&base.AutoClose, o.AutoClose)
	overlayPtr(&base.AutoSurround, o.AutoSurround)
	overlayPtr(&base.TextmateGrammar, o.TextmateGrammar)
	overlayPtr(&base.ShowWhitespaceTabs, o.ShowWhitespaceTabs)
	overlayPtr(&base.LineWrap, o.LineWrap)
	overlayPtr(&base.WrapColumn, o.WrapColumn)
	overlayPtr(&base.PageView, o.PageView)
	overlayPtr(&base.PageWidth, o.PageWidth)
	overlayPtr(&base.UseTabs, o.UseTabs)
	overlayPtr(&base.TabSize, o.TabSize)
	overlayPtr(&base.Formatter, o.Formatter)
	overlayPtr(&base.FormatOnSave, o.FormatOnSave)
	if o.OnSave != nil {
		base.OnSave = o.OnSave
	}
	overlayPtr(&base.WordCharacters, o.WordCharacters)
	overlayPtr(&base.Indent, o.Indent)
	overlayPtr(&base.IndentationGuide, o.IndentationGuide)
	return base
}

func overlayPtr[T any](dst **T, src *T) {
	if src != nil {
		*dst = src
	}
}

// WhitespaceSettings says which whitespace marks show in one document, once
// ShowWhitespace is on. The tab flags are already merged with the language's
// show_whitespace_tabs.
type WhitespaceSettings struct {
	Show           bool // editor.whitespace_show
	SpacesLeading  bool
	SpacesInner    bool
	SpacesTrailing bool
	TabsLeading    bool
	TabsInner      bool
	TabsTrailing   bool
	Newlines       bool
	CarriageReturn bool
	InSelection    bool
}

// EditorSettings is the effective editing and display settings of one document: the
// global editor.* options with the languages.<id> override applied. Get it from
// Config.Editor or Config.EditorFor.
type EditorSettings struct {
	// Language is the language id the settings were resolved for: "" for no name, or
	// "text" for plain text. It is the id even when no entry exists for it.
	Language string

	TabSize      int  // columns per tab stop
	UseTabs      bool // Tab inserts a tab character
	AutoIndent   bool
	AutoClose    bool // global editor.auto_close, switched off by the language when it is on
	AutoSurround bool // same rule as AutoClose
	VirtualSpace string

	LineWrap   bool
	WrapColumn int // 0: the viewport width
	PageView   bool
	PageWidth  int // 0: the viewport width

	Whitespace WhitespaceSettings

	// IndentationGuide is false when guides are suppressed for this document whatever
	// IndentationGuideMode says. Plain text defaults to false; the language's
	// indentation_guide overrides that. Draw guides when IndentationGuideMode is not
	// "none" and this is true.
	IndentationGuide     bool
	IndentationGuideMode string // editor.indentation_guide, global

	CommentPrefix   string // "" when the language sets none
	WordCharacters  string // extra word characters, "" for the standard set
	Formatter       *Formatter
	FormatOnSave    bool
	OnSave          []OnSaveAction
	Indent          *IndentRules
	TextmateGrammar string
}

// Editor returns the settings for a language name, for instance the name that gvim's
// highlighter gives. An empty name or "text" gets default_language's entry when that
// is set. See EditorFor.
func (c Config) Editor(language string) EditorSettings {
	return c.EditorFor(language, "")
}

// EditorFor returns the effective settings for a document with the given language
// name and file path. Either may be empty.
//
// The language is the name when a languages entry (the user's or a built-in one)
// exists for it. Otherwise the path finds it: a file name match first, then an
// extension match, in the user's entries and then the built-in ones. A document
// that has no entry gets the global options. When the language is "" or "text",
// default_language's entry supplies the overrides.
//
// The merge follows Fresh's BufferConfig::resolve: tab_size, use_tabs, line_wrap,
// wrap_column, page_view, page_width and auto_indent are replaced by the entry's
// value when it sets one. auto_close and auto_surround can only be switched off by a
// language (when the global option is off, the language cannot turn it on).
func (c Config) EditorFor(language, path string) EditorSettings {
	id := c.resolveLanguage(language, path)
	entry, found := c.entry(id)
	if !found && (id == "" || id == textLanguage) && c.DefaultLanguage != "" {
		entry, _ = c.entry(LanguageID(c.DefaultLanguage))
	}

	s := EditorSettings{
		Language:     id,
		TabSize:      c.TabSize,
		UseTabs:      !c.InsertSpaces,
		AutoIndent:   c.AutoIndent,
		AutoClose:    c.AutoClose,
		AutoSurround: c.AutoSurround,
		VirtualSpace: c.VirtualSpace,
		LineWrap:     c.LineWrap,
		WrapColumn:   c.WrapColumn,
		PageWidth:    c.PageWidth,
		Whitespace: WhitespaceSettings{
			Show:           c.ShowWhitespace,
			SpacesLeading:  c.WhitespaceSpacesLeading,
			SpacesInner:    c.WhitespaceSpacesInner,
			SpacesTrailing: c.WhitespaceSpacesTrailing,
			TabsLeading:    c.WhitespaceTabsLeading,
			TabsInner:      c.WhitespaceTabsInner,
			TabsTrailing:   c.WhitespaceTabsTrailing,
			Newlines:       c.WhitespaceNewlines,
			CarriageReturn: c.WhitespaceCarriageReturns,
			InSelection:    c.WhitespaceInSelection,
		},
		IndentationGuide:     id != textLanguage,
		IndentationGuideMode: c.IndentationGuide,
	}
	if v := entry.IndentationGuide; v != nil {
		s.IndentationGuide = *v
	}
	if v := entry.TabSize; v != nil {
		s.TabSize = *v
	}
	if v := entry.UseTabs; v != nil {
		s.UseTabs = *v
	}
	if v := entry.LineWrap; v != nil {
		s.LineWrap = *v
	}
	if v := entry.WrapColumn; v != nil {
		s.WrapColumn = *v
	}
	if v := entry.PageView; v != nil {
		s.PageView = *v
	}
	if v := entry.PageWidth; v != nil {
		s.PageWidth = *v
	}
	if v := entry.AutoIndent; v != nil {
		s.AutoIndent = *v
	}
	if v := entry.AutoClose; v != nil && s.AutoClose {
		s.AutoClose = *v
	}
	if v := entry.AutoSurround; v != nil && s.AutoSurround {
		s.AutoSurround = *v
	}
	if v := entry.ShowWhitespaceTabs; v != nil && !*v {
		s.Whitespace.TabsLeading, s.Whitespace.TabsInner, s.Whitespace.TabsTrailing = false, false, false
	}
	if v := entry.CommentPrefix; v != nil {
		s.CommentPrefix = *v
	} else {
		s.CommentPrefix = defaultCommentPrefixes[id]
	}
	if v := entry.WordCharacters; v != nil {
		s.WordCharacters = *v
	}
	if v := entry.TextmateGrammar; v != nil {
		s.TextmateGrammar = *v
	}
	if v := entry.FormatOnSave; v != nil {
		s.FormatOnSave = *v
	}
	s.Formatter = entry.Formatter
	s.OnSave = slices.Clone(entry.OnSave)
	s.Indent = entry.Indent
	return s
}

// entry returns the languages entry of id: the built-in one with the user's merged
// over it. found is false when neither exists.
func (c Config) entry(id string) (LanguageSettings, bool) {
	b, inBuiltin := builtinLanguages[id]
	u, inUser := c.Languages[id]
	return b.overlay(u), inBuiltin || inUser
}

// resolveLanguage returns the id of the entry for a document. See EditorFor. A file
// name match comes first, as in Fresh, so that a justfile, which chroma lexes as a
// Makefile, gets the justfile entry. Then the language name, if an entry exists for it,
// then a file extension match; else the name as is.
func (c Config) resolveLanguage(language, path string) string {
	if id := c.matchFilename(path); id != "" {
		return id
	}
	id := LanguageID(language)
	if id != "" && id != textLanguage {
		if _, ok := c.entry(id); ok {
			return id
		}
	}
	if match := c.matchExtension(path); match != "" {
		return match
	}
	return id
}

// pathTables are the places a path is matched against, in order: the user's entries,
// the built-in entries, and Fresh's file tables. Within a table ids are tried in sorted
// order.
func (c Config) pathTables() [3]map[string]LanguageSettings {
	return [3]map[string]LanguageSettings{c.Languages, builtinLanguages, fileLanguages}
}

// matchFilename returns the id of the entry whose file names match path, or "".
func (c Config) matchFilename(path string) string {
	if path == "" {
		return ""
	}
	base := filepath.Base(path)
	for _, g := range c.pathTables() {
		for _, id := range slices.Sorted(mapKeys(g)) {
			if matchesName(g[id].Filenames, base) {
				return id
			}
		}
	}
	return ""
}

// matchExtension returns the id of the entry whose extensions match path, or "".
func (c Config) matchExtension(path string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(filepath.Base(path)), "."))
	if ext == "" {
		return ""
	}
	for _, g := range c.pathTables() {
		for _, id := range slices.Sorted(mapKeys(g)) {
			if matchesExt(g[id].Extensions, ext) {
				return id
			}
		}
	}
	return ""
}

// matchesName reports whether base is one of names. A name with a "*" is a glob.
func matchesName(names []string, base string) bool {
	for _, n := range names {
		if n == base {
			return true
		}
		if strings.Contains(n, "*") {
			if ok, _ := filepath.Match(n, base); ok {
				return true
			}
		}
	}
	return false
}

func matchesExt(exts []string, ext string) bool {
	return slices.ContainsFunc(exts, func(e string) bool { return strings.EqualFold(strings.TrimPrefix(e, "."), ext) })
}
