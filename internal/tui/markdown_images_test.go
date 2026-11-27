package tui

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi/kitty"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/theme"
)

type markdownImageTestDoc struct{ realDoc }

func (d markdownImageTestDoc) FilePath() string { return d.d.Path }
func (markdownImageTestDoc) LocalImage() bool   { return true }

type markdownImageTestEditor struct{ realEditor }

func (e markdownImageTestEditor) Active() Document {
	return markdownImageTestDoc{realDoc{e.ed.Active()}}
}

func TestMarkdownImageRendersInPageAndDual(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "notes.md")
	source := "# Docs\n\n![Diagram](<a b.png>)\n\n```md\n![Literal](<a b.png>)\n```\n"
	if err := os.WriteFile(file, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 96, 64))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{R: 255, A: 255}), image.Point{}, draw.Src)
	f, err := os.Create(filepath.Join(dir, "a b.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	ed := editor.New(editor.Options{})
	if err := ed.OpenFile(file); err != nil {
		t.Fatal(err)
	}
	th, err := theme.Load("dark")
	if err != nil {
		t.Fatal(err)
	}
	m := New(Deps{Editor: markdownImageTestEditor{realEditor{ed}}, Keys: testKeys(), Theme: th, Clock: testClock})
	t.Cleanup(m.clearMarkdownImages)
	runAction(t, m, "markdown_mode_rendered")
	_, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 22})
	drainReviewCmds(m, cmd)
	if !paneHas(m, 0, "▀") {
		t.Fatalf("rendered Markdown is missing the raster: %q", paneRows(m, 0))
	}
	for _, loaded := range m.markdownImages {
		if len(loaded.byLine) != 1 || loaded.byLine[2] == nil {
			t.Fatalf("image references included a fenced sample or missed the image: %+v", loaded.byLine)
		}
	}
	runAction(t, m, "markdown_mode_dual")
	_, cmd = m.Update(tea.WindowSizeMsg{Width: 100, Height: 22})
	drainReviewCmds(m, cmd)
	if paneHas(m, 0, "▀") || !paneHas(m, 1, "▀") || !paneHas(m, 0, "![Diagram]") {
		t.Fatalf("dual view did not keep source and image in their respective panes: %q / %q", paneRows(m, 0), paneRows(m, 1))
	}
	m.img.off = true
	if paneHas(m, 1, "▀") || !paneHas(m, 1, "Diagram") {
		t.Fatalf("disabling image rendering did not restore the alt text: %q", paneRows(m, 1))
	}
}

func TestMarkdownImageNegotiatesKittyAndPlacesClippedRows(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "notes.md")
	path := filepath.Join(dir, "a.png")
	if err := os.WriteFile(file, []byte("![first](a.png)\n![second](a.png)\nending\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 320, 192))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	ed := editor.New(editor.Options{})
	if err := ed.OpenFile(file); err != nil {
		t.Fatal(err)
	}
	m := New(Deps{Editor: markdownImageTestEditor{realEditor{ed}}, Keys: testKeys(), Clock: testClock})
	t.Cleanup(m.clearMarkdownImages)
	runAction(t, m, "markdown_mode_rendered")
	_, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	var probeQueued bool
	for _, msg := range collect(cmd) {
		if loaded, ok := msg.(markdownImagesLoaded); ok {
			_, next := m.Update(loaded)
			for _, msg := range collect(next) {
				_, probeQueued = msg.(imageProbeArmMsg)
			}
		}
	}
	if !probeQueued {
		t.Fatal("displaying a Markdown image did not schedule the Kitty capability query")
	}
	_, cmd = m.Update(uv.KittyGraphicsEvent{Options: kitty.Options{ID: imageProbeID}})
	uploads := collect(cmd)
	if len(uploads) != 1 {
		t.Fatalf("same file is referenced twice, got %d uploads, want one", len(uploads))
	}
	send, ok := uploads[0].(imageSendMsg)
	if !ok || send.err != nil || send.path != path {
		t.Fatalf("unexpected image upload: %T", uploads[0])
	}
	_, cmd = m.Update(send)
	feedMessages(m, cmd)
	placeFirst := imageVirtualPlacement(send.id, 1<<23, 20, 6)
	placeSecond := imageVirtualPlacement(send.id, (1<<23)+1, 20, 6)
	got := m.View().Content
	if !strings.Contains(got, placeFirst) || !strings.Contains(got, placeSecond) {
		t.Fatal("both occurrences must have independent native placements")
	}
	buf := uv.NewScreenBuffer(100, 24)
	uv.NewStyledString(got).Draw(buf, buf.Bounds())
	var survives bool
	for y := range 24 {
		for x := range 100 {
			if cell := buf.CellAt(x, y); cell != nil && strings.Contains(cell.Content, placeFirst+placeholderCell(0, 0)) {
				survives = true
			}
		}
	}
	if !survives {
		t.Fatal("virtual placement or its placeholder was discarded by the terminal renderer")
	}
	sp := m.layout().FocusedPane()
	pane := m.paneGeometry(sp, m.ed.Active())
	pane.Top, pane.TopRow = 0, 3
	clipped := pane.Render(sp.W, sp.H).Lines()[0]
	if !strings.Contains(clipped, placeFirst+placeholderCell(2, 0)) {
		t.Fatalf("clipped image top row lacks its virtual placement and row 2 coordinates: %q", clipped)
	}
	if got := pane.PosAt(sp.W, sp.H, 0, 4); got.Line != 1 {
		t.Fatalf("click after the clipped image mapped to line %d, want line 1", got.Line)
	}
	// Return to source mode. Its last Markdown reference no longer owns the resource.
	runAction(t, m, "markdown_mode_source")
	_, cmd = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	var deleted bool
	for _, msg := range collect(cmd) {
		if raw, ok := msg.(tea.RawMsg); ok {
			seq, _ := raw.Msg.(string)
			deleted = deleted || strings.Contains(seq, imageDeleteSequence(send.id))
		}
	}
	if !deleted {
		t.Fatal("switching to source view did not release the image")
	}
}
