package tui

import (
	"context"
	"image"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/editorpane"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/split"
	"github.com/tbxark/gvim/internal/tui/style"
	"github.com/tbxark/gvim/syntax"
)

const (
	markdownImageMaxDocs  = 4
	markdownImageMaxFiles = 8
	markdownImageMaxLines = 20000
	markdownImageMaxText  = 2 << 20
)

type markdownImageDocKey struct {
	path string
	id   uint64
}

type markdownImageState struct {
	version uint64
	cancel  context.CancelFunc
	byLine  map[int]*markdownPicture
	byPath  map[string]*markdownPicture
}

type markdownPicture struct {
	path   string
	pixels image.Image
	size   image.Point
	bytes  int64
	mtime  int64
	rows   mosaicRender
}

type markdownImagesLoaded struct {
	key   markdownImageDocKey
	state *markdownImageState
	lines map[int]*markdownPicture
	paths map[string]*markdownPicture
}

func markdownImageKey(doc Document) (markdownImageDocKey, bool) {
	file := docPath(doc)
	local, ok := doc.(imageLocalDoc)
	if file == "" || !filepath.IsAbs(file) || !ok || !local.LocalImage() {
		return markdownImageDocKey{}, false
	}
	key := markdownImageDocKey{path: file}
	if v, ok := doc.(bufferIdentified); ok {
		key.id = uint64(v.BufferID())
	}
	return key, true
}

func (m *Model) markdownImageQueue() tea.Cmd {
	hasMarkdown := false
	if m.ed != nil && !m.img.off && !m.review.active {
		for _, leaf := range m.splits.Root.Leaves() {
			if doc := m.leafDoc(leaf); doc != nil && m.markdownDoc(doc) {
				hasMarkdown = true
				break
			}
		}
	}
	if !hasMarkdown {
		m.clearMarkdownImages()
		return nil
	}
	if m.markdownImages == nil {
		m.markdownImages = make(map[markdownImageDocKey]*markdownImageState)
		m.markdownImageSlots = make(chan struct{}, 2)
	}
	visible := make(map[markdownImageDocKey]bool)
	var cmds []tea.Cmd
	for _, pane := range m.layout().Panes {
		if len(visible) >= markdownImageMaxDocs {
			break
		}
		doc := m.paneDoc(pane)
		if doc == nil {
			continue
		}
		pv := m.panePageViewWith(pane, doc, docViewOf(doc), m.docLanguageSettings(doc))
		if !pv.On || !pv.Markdown {
			continue
		}
		key, ok := markdownImageKey(doc)
		if !ok || visible[key] {
			continue
		}
		if large, ok := doc.(LargeDoc); ok && large.Large() {
			continue
		}
		version, ok := docVersion(doc)
		if !ok {
			continue
		}
		visible[key] = true
		old := m.markdownImages[key]
		if old != nil && old.version == version {
			continue
		}
		var cached map[string]*markdownPicture
		if old != nil {
			old.cancel()
			cached = old.byPath
		}
		ctx, cancel := context.WithCancel(context.Background())
		state := &markdownImageState{version: version, cancel: cancel, byPath: cached}
		m.markdownImages[key] = state
		n := min(doc.LineCount(), markdownImageMaxLines)
		lines := make([]string, 0, n)
		bytes := 0
		for i := range n {
			line := doc.Line(i)
			bytes += len(line) + 1
			if bytes > markdownImageMaxText {
				break
			}
			lines = append(lines, line)
		}
		slots := m.markdownImageSlots
		cmds = append(cmds, func() tea.Msg {
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-ctx.Done():
				return nil
			}
			byLine, byPath := readMarkdownPictures(ctx, key.path, lines, cached)
			return markdownImagesLoaded{key: key, state: state, lines: byLine, paths: byPath}
		})
	}
	for key, state := range m.markdownImages {
		if !visible[key] {
			state.cancel()
			delete(m.markdownImages, key)
		}
	}
	return tea.Batch(cmds...)
}

func (m *Model) clearMarkdownImages() {
	for _, state := range m.markdownImages {
		state.cancel()
	}
	clear(m.markdownImages)
}

func readMarkdownPictures(ctx context.Context, file string, lines []string, cached map[string]*markdownPicture) (map[int]*markdownPicture, map[string]*markdownPicture) {
	byLine := make(map[int]*markdownPicture)
	byPath := make(map[string]*markdownPicture)
	fences := syntax.MarkdownFences(lines)
	inComment := false
	for i, line := range lines {
		if ctx.Err() != nil {
			break
		}
		if fences[i].Kind != syntax.FenceNone {
			continue
		}
		if inComment {
			inComment = !strings.Contains(line, "-->")
			continue
		}
		if _, comment, ok := strings.Cut(line, "<!--"); ok {
			inComment = !strings.Contains(comment, "-->")
			continue
		}
		dest := syntax.MarkdownBlockImage(line)
		if dest == "" {
			continue
		}
		path := markdownLocalImagePath(file, dest)
		if path == "" {
			continue
		}
		picture, seen := byPath[path]
		if !seen {
			if len(byPath) >= markdownImageMaxFiles {
				continue
			}
			picture, seen = cached[path]
			if !seen {
				picture = readMarkdownPicture(path)
			}
			byPath[path] = picture
		}
		if picture != nil {
			byLine[i] = picture
		}
	}
	return byLine, byPath
}

func markdownLocalImagePath(file, dest string) string {
	u, err := url.Parse(dest)
	if err != nil || u.Scheme != "" || u.Host != "" || strings.HasPrefix(dest, "//") || strings.ContainsAny(u.Path, "\\\x00") {
		return ""
	}
	rel := filepath.FromSlash(u.Path)
	if filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" || !ImagePreviewPath(rel) {
		return ""
	}
	return filepath.Join(filepath.Dir(file), rel)
}

func readMarkdownPicture(path string) *markdownPicture {
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() || st.Size() > ImagePreviewMaxBytes {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, ImagePreviewMaxBytes+1))
	if err != nil {
		return nil
	}
	pixels, size, err := DecodeImagePreview(data)
	if err != nil {
		return nil
	}
	return &markdownPicture{path: path, pixels: pixels, size: size, bytes: st.Size(), mtime: st.ModTime().UnixNano()}
}

func (m *Model) markdownImageExtraRows(pane split.Pane, doc Document, base func(line, width, gutter, measure int) editorpane.ExtraRows) func(line, width, gutter, measure int) editorpane.ExtraRows {
	key, ok := markdownImageKey(doc)
	if m.img.off || !ok {
		return base
	}
	state := m.markdownImages[key]
	version, _ := docVersion(doc)
	if state == nil || state.version != version || len(state.byLine) == 0 {
		return base
	}
	cw, ch := m.img.cellSize()
	native := m.img.imageEnabled()
	slot := 0
	if native && pane.W > 0 {
		slot = max(0, slices.Index(m.layout().Panes, pane))
	}
	bg := style.Cell(m.styles.Editor)
	return func(line, width, gutter, measure int) editorpane.ExtraRows {
		extras := base(line, width, gutter, measure)
		picture := state.byLine[line]
		if picture == nil {
			return extras
		}
		cols := min(96, measure)
		area := imageRegion{x: max(0, (width-1-cols)/2), w: cols, h: 20}
		fit := imageFit(area, picture.size.X, picture.size.Y, cw, ch)
		if fit.w == 0 || fit.h == 0 {
			return extras
		}
		rows := make([]editorpane.ExtraRow, fit.h)
		// Inline placements occupy a separate range from ordinary image panes.
		// Each source occurrence and each pane needs its own placement, even
		// when they all refer to the same upload.
		placement := (1 << 23) + slot*markdownImageMaxLines + line
		id, uploaded := m.img.imageUploaded(picture.path, picture.bytes, picture.mtime)
		useKitty := native && uploaded && placement <= maxImageID
		placementSeq := ""
		st := bg
		if useKitty {
			st = imagePlaceholderStyle(id, placement, bg)
			placementSeq = imageVirtualPlacement(id, placement, fit.w, fit.h)
		}
		for y := range rows {
			rows[y].Draw = func(g *grid.Grid, screenY int) {
				if useKitty {
					for x := range fit.w {
						content := placeholderCell(y, x)
						if x == 0 && (y == 0 || screenY == 0) {
							content = placementSeq + content
						}
						g.Set(fit.x+x, screenY, content, st)
					}
					return
				}
				cells := picture.rows.pictureCells(picture.pixels, fit.w, fit.h)
				for x, cell := range cells[y*fit.w : (y+1)*fit.w] {
					g.Set(fit.x+x, screenY, cell.Text, cell.Style)
				}
			}
		}
		extras.Below = append(extras.Below, rows...)
		return extras
	}
}
