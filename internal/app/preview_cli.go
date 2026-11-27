package app

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/color"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/internal/configview"
	"github.com/tbxark/gvim/internal/tui/style"
	"github.com/tbxark/gvim/syntax"
	"github.com/tbxark/gvim/theme"
)

// PreviewOptions selects the source range and output format for PrintPreview.
type PreviewOptions struct {
	Skip, Lines, Width, TabSize int
	LineNumbers                 bool
	Color, ConfigPath, Theme    string
}

const (
	previewLineBytes    = 32 << 10
	previewLexLineBytes = 4 << 10
	previewContextLines = 64
)

// PrintPreview prints a bounded, read-only view of a local UTF-8 file.
func PrintPreview(ctx context.Context, out io.Writer, path string, opts PreviewOptions) error {
	if opts.Skip < 0 || opts.Lines < 0 || opts.Width < 0 {
		return errors.New("--skip, --lines and --width must be non-negative")
	}
	if opts.TabSize < 1 || opts.TabSize > 32 {
		return errors.New("--tab-size must be between 1 and 32")
	}
	var profile colorprofile.Profile
	switch opts.Color {
	case "auto":
		if os.Getenv("NO_COLOR") == "" {
			profile = colorprofile.Detect(out, os.Environ())
		}
	case "always":
		profile = colorprofile.TrueColor
	case "never":
	default:
		return errors.New("--color must be auto, always, or never")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	f, _, err := openRegularFile(path)
	if err != nil {
		return fmt.Errorf("preview %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	r := bufio.NewReaderSize(f, previewLineBytes+2) // room for CRLF
	w := bufio.NewWriter(out)
	p := previewPrinter{w: w, width: opts.Width, tabs: strings.Repeat(" ", opts.TabSize)}
	if opts.LineNumbers {
		p.digits = max(3, len(strconv.FormatUint(uint64(opts.Skip)+uint64(opts.Lines), 10)))
	}
	var highlighter *syntax.Highlighter
	if profile >= colorprofile.ANSI {
		th, err := loadPreviewTheme(opts)
		if err != nil {
			return err
		}
		p.setStyles(th, profile)
		head, err := r.Peek(16 << 10)
		if err != nil && err != io.EOF {
			return err
		}
		lexer := syntax.Detect(path, bytes.TrimPrefix(head, []byte("\ufeff")))
		if !syntax.IsPlain(lexer.Config().Name) {
			highlighter = syntax.NewHighlighter(lexer)
		}
	}
	if err := printPreviewLines(ctx, r, &p, highlighter, opts); err != nil {
		return err
	}
	return w.Flush()
}

func loadPreviewTheme(opts PreviewOptions) (theme.Theme, error) {
	if err := configview.CheckFile(opts.ConfigPath); err != nil {
		return theme.Theme{}, err
	}
	ref := opts.Theme
	if ref == "" {
		cfg, _, err := config.Load(config.Options{UserFile: opts.ConfigPath})
		if err != nil {
			return theme.Theme{}, err
		}
		ref = effectiveThemeRef("", cfg)
	}
	reg := theme.NewRegistry(userThemeDir())
	if dir, err := themeURLCacheDir(); err == nil {
		reg.SetURLCacheDir(dir)
	}
	th, err := reg.Load(ref)
	if err != nil && opts.Theme == "" && theme.IsURLForm(ref) {
		return theme.Load(config.DefaultTheme)
	}
	return th, err
}

func printPreviewLines(ctx context.Context, r *bufio.Reader, p *previewPrinter, highlighter *syntax.Highlighter, opts PreviewOptions) error {
	contextStart, batchSize := opts.Skip, 1
	if highlighter != nil {
		contextStart = max(0, opts.Skip-previewContextLines)
		batchSize = previewContextLines
	}
	lines := make([]string, 0, previewContextLines+batchSize)
	first, visible := contextStart, opts.Skip
	for index := 0; ; index++ {
		offset := index - opts.Skip
		more := opts.Lines == 0 || offset < opts.Lines
		if more {
			needNext := opts.Lines == 0 || offset < opts.Lines-1
			line, err := readPreviewLine(ctx, r, index >= contextStart, needNext)
			switch err {
			case io.EOF:
				more = false
			case nil:
				if index >= contextStart {
					if index == 0 {
						line = strings.TrimPrefix(line, "\ufeff")
					}
					lines = append(lines, line)
				}
			default:
				return err
			}
		}
		if visible < first+len(lines) && (!more || first+len(lines)-visible >= batchSize) {
			var window syntax.Window
			if highlighter != nil {
				var lexed [2 * previewContextLines]string
				for i, line := range lines {
					if len(line) <= previewLexLineBytes {
						lexed[i] = line
					}
				}
				window = highlighter.LinesWindow(0, first, lexed[:len(lines)])
			}
			for ; visible < first+len(lines); visible++ {
				if err := p.line(lines[visible-first], window.Line(visible), visible+1); err != nil {
					return err
				}
			}
			keep := 0
			if highlighter != nil {
				keep = min(len(lines), previewContextLines)
			}
			copy(lines, lines[len(lines)-keep:])
			clear(lines[keep:])
			lines = lines[:keep]
			first = visible - keep
		}
		if !more {
			return nil
		}
	}
}

// readPreviewLine keeps only a long line's display prefix and drains the rest
// when another line is needed. A final line without a newline still counts.
func readPreviewLine(ctx context.Context, r *bufio.Reader, keep, needNext bool) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	raw, err := r.ReadSlice('\n')
	if err == io.EOF && len(raw) == 0 {
		return "", io.EOF
	}
	var line string
	if keep {
		raw = bytes.TrimSuffix(bytes.TrimSuffix(raw, []byte("\n")), []byte("\r"))
		if len(raw) > previewLineBytes {
			end := previewLineBytes
			for end > 0 && !utf8.RuneStart(raw[end]) {
				end--
			}
			line = string(raw[:end]) + "…"
		} else {
			line = string(raw)
		}
	}
	if err == bufio.ErrBufferFull && !needNext {
		err = nil
	}
	for err == bufio.ErrBufferFull {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		_, err = r.ReadSlice('\n')
	}
	if err != nil && err != io.EOF {
		return "", err
	}
	line = strings.ToValidUTF8(line, "\ufffd")
	if strings.IndexFunc(line, unsafePreviewRune) >= 0 {
		line = strings.Map(func(r rune) rune {
			if unsafePreviewRune(r) {
				return -1
			}
			return r
		}, line)
	}
	return line, nil
}

func unsafePreviewRune(r rune) bool { return r != '\t' && unicode.IsControl(r) }

type previewPrinter struct {
	w             *bufio.Writer
	width, digits int
	tabs          string
	styles        [syntax.KindCount]ansi.Style
	gutter        ansi.Style
}

func (p *previewPrinter) setStyles(th theme.Theme, profile colorprofile.Profile) {
	fg := func(c color.Color) ansi.Style {
		if c == nil {
			return nil
		}
		return ansi.NewStyle().ForegroundColor(profile.Convert(c))
	}
	base := style.Color(th, "editor", "fg")
	p.gutter = fg(style.Color(th, "editor", "line_number_fg"))
	for kind := range syntax.KindCount {
		st := fg(style.SyntaxFG(th, kind, base))
		attrs := kind.Attrs()
		if attrs&syntax.AttrBold != 0 {
			st = st.Bold()
		}
		if attrs&syntax.AttrItalic != 0 {
			st = st.Italic(true)
		}
		if attrs&syntax.AttrUnderline != 0 {
			st = st.Underline(true)
		}
		if attrs&syntax.AttrStrike != 0 {
			st = st.Strikethrough(true)
		}
		p.styles[kind] = st
	}
}

func (p *previewPrinter) line(line string, spans []syntax.Span, number int) error {
	width := p.width
	var row strings.Builder
	if p.digits > 0 {
		label := strconv.Itoa(number)
		digits := max(len(label), p.digits)
		if width > 0 {
			digits = min(digits, width-3) // two spaces and one text cell
		}
		if digits >= len(label) {
			row.WriteString(p.gutter.Styled(strings.Repeat(" ", digits-len(label)) + label + "  "))
			width -= digits + 2
		}
	}
	var tail string
	if p.width > 0 {
		line, tail = clipPreviewLine(line, width, p.tabs)
	}
	offset := 0
	for _, span := range spans {
		if span.Start >= len(line) {
			break
		}
		end := min(span.End, len(line))
		row.WriteString(strings.ReplaceAll(line[offset:span.Start], "\t", p.tabs))
		row.WriteString(p.styles[span.Kind].Styled(strings.ReplaceAll(line[span.Start:end], "\t", p.tabs)))
		offset = end
	}
	row.WriteString(strings.ReplaceAll(line[offset:], "\t", p.tabs))
	row.WriteString(tail)
	row.WriteByte('\n')
	_, err := p.w.WriteString(row.String())
	return err
}

// Clip before adding SGR escapes so token boundaries cannot split a grapheme
// during width measurement. A tab can contribute some of its display spaces.
func clipPreviewLine(line string, width int, tabs string) (prefix, tail string) {
	remaining, state := line, -1
	for len(remaining) > 0 {
		cluster, next, cells, nextState := uniseg.FirstGraphemeClusterInString(remaining, state)
		if cluster == "\t" {
			cells = len(tabs)
			if cells > width {
				return line[:len(line)-len(remaining)], tabs[:width]
			}
		}
		if cells > width {
			return line[:len(line)-len(remaining)], ""
		}
		width -= cells
		remaining, state = next, nextState
	}
	return line, ""
}
