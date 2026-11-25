package tui

import (
	"errors"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi/kitty"

	"github.com/tbxark/gvim/internal/tui/split"
	"github.com/tbxark/gvim/theme"
)

// TestMosaicEnabledTruthTable checks the half-block fallback's precondition: the
// probe has an answer and it was "no support" — not forced, not off, not hosted.
func TestMosaicEnabledTruthTable(t *testing.T) {
	tests := []struct {
		name                              string
		probed, kitty, forced, off, local bool
		want                              bool
	}{
		{"answered no support", true, false, false, false, false, true},
		{"not probed yet", false, false, false, false, false, false},
		{"kitty answered", true, true, false, false, false, false},
		{"kitty forced", true, true, true, false, false, false},
		{"forced without answer", false, false, true, false, false, false},
		{"protocol off", true, false, false, true, false, false},
		{"hosted workspace", true, false, false, false, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := imageState{probed: tt.probed, kitty: tt.kitty, forced: tt.forced, off: tt.off, localOnly: tt.local}
			if got := s.mosaicEnabled(); got != tt.want {
				t.Errorf("mosaicEnabled = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestMosaicCellsRendersHalfBlocks checks the render+convert step on a synthetic
// picture: each cell is the upper half block whose foreground is the picture's
// top half-pixel and whose background is the bottom one.
func TestMosaicCellsRendersHalfBlocks(t *testing.T) {
	red := color.RGBA{255, 0, 0, 255}
	green := color.RGBA{0, 200, 0, 255}
	blue := color.RGBA{0, 0, 255, 255}
	yellow := color.RGBA{255, 255, 0, 255}
	// 4x2 pixels, so a 2x1 cell render samples them without interpolation.
	src := image.NewRGBA(image.Rect(0, 0, 4, 2))
	for x := range 4 {
		src.SetRGBA(x, 0, red)
		src.SetRGBA(x, 1, blue)
	}
	src.SetRGBA(2, 0, green)
	src.SetRGBA(3, 0, green)
	src.SetRGBA(2, 1, yellow)
	src.SetRGBA(3, 1, yellow)

	cells := mosaicCells(src, 2, 1)
	if len(cells) != 2 {
		t.Fatalf("cells = %d, want 2", len(cells))
	}
	rgb := func(c color.Color) (r, g, b uint32) {
		r, g, b, _ = c.RGBA()
		return r >> 8, g >> 8, b >> 8
	}
	for i, want := range [2][2]color.RGBA{{red, blue}, {green, yellow}} {
		c := cells[i]
		if c.Text != "▀" {
			t.Fatalf("cell %d text = %q, want the upper half block", i, c.Text)
		}
		if r, g, b := rgb(c.Style.FG); r != uint32(want[0].R) || g != uint32(want[0].G) || b != uint32(want[0].B) {
			t.Errorf("cell %d fg = (%d,%d,%d), want the top pixel %v", i, r, g, b, want[0])
		}
		if r, g, b := rgb(c.Style.BG); r != uint32(want[1].R) || g != uint32(want[1].G) || b != uint32(want[1].B) {
			t.Errorf("cell %d bg = (%d,%d,%d), want the bottom pixel %v", i, r, g, b, want[1])
		}
	}
}

// TestMosaicFallbackPaintsHalfBlocks drives the update path: the probe answer is
// "no support", the queue decodes the picture, and the pane then paints it as
// half-block cells the same size as the pane — not Kitty placeholders.
func TestMosaicFallbackPaintsHalfBlocks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pic.png")
	if err := os.WriteFile(path, tinyPNG(), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := &imageTestDoc{lines: []string{"pic.png"}, path: path}
	m := New(Deps{Editor: newSplitImageEditor(doc), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	m.img = imageProtocolOf("auto")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// The probe times out: no support. The same update queues the decode.
	_, cmd := m.Update(imageProbeTimeoutMsg{})
	if !m.img.probed || m.img.kitty || !m.img.mosaicEnabled() {
		t.Fatalf("state = %+v, want probed, not kitty, mosaic enabled", m.img)
	}
	msgs := collect(cmd)
	if len(msgs) != 1 {
		t.Fatalf("no-support update produced %d messages, want one load", len(msgs))
	}
	loaded, ok := msgs[0].(mosaicLoadedMsg)
	if !ok {
		t.Fatalf("no-support update produced %T, want mosaicLoadedMsg", msgs[0])
	}
	if loaded.err != nil || loaded.img == nil {
		t.Fatalf("load = %+v, want a decoded picture", loaded)
	}
	// Before the decode lands the pane is the text summary.
	if _, ok := m.imagePane(split.Pane{W: 40, H: 10}, doc, 1); ok {
		t.Fatal("imagePane = true before the decode, want the summary")
	}
	m.Update(loaded)

	pg, ok := m.imagePane(split.Pane{W: 40, H: 10}, doc, 1)
	if !ok {
		t.Fatal("imagePane = false after the decode, want the half-block picture")
	}
	if pg.Width() != 40 || pg.Height() != 10 {
		t.Fatalf("pane grid = %dx%d, want the pane's 40x10", pg.Width(), pg.Height())
	}
	half, placeholders := 0, 0
	for y := range pg.Height() {
		for x := range pg.Width() {
			c := pg.At(x, y)
			if c.Text == "▀" {
				half++
				if c.Style.FG == nil && c.Style.BG == nil {
					t.Errorf("half-block cell (%d,%d) has no colors", x, y)
				}
			}
			if strings.Contains(c.Text, string(kitty.Placeholder)) {
				placeholders++
			}
		}
	}
	if half == 0 {
		t.Fatal("no half-block cells painted")
	}
	if placeholders != 0 {
		t.Errorf("%d kitty placeholder cells in the mosaic pane", placeholders)
	}
}

// TestMosaicFallbackStaysSummary covers the negative cases of imagePane for the
// fallback: off, kitty, before the probe answer and a failed decode all leave
// the text pane.
func TestMosaicFallbackStaysSummary(t *testing.T) {
	doc := &imageTestDoc{path: "/pic.png"}
	pane := split.Pane{W: 20, H: 5}
	m := &Model{}
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))

	m.img = imageProtocolOf("off")
	if _, ok := m.imagePane(pane, doc, 1); ok {
		t.Error("imagePane = true with the protocol off")
	}
	m.img = imageProtocolOf("kitty")
	if _, ok := m.imagePane(pane, doc, 1); ok {
		t.Error("imagePane = true with kitty before the upload")
	}
	m.img = imageProtocolOf("auto")
	if _, ok := m.imagePane(pane, doc, 1); ok {
		t.Error("imagePane = true before the probe answer")
	}
	// Probed, no support, a failed decode: the summary stays.
	m.img.probed = true
	m.img.mosaic = map[string]*mosaicEntry{"/pic.png": {size: 0, mtime: 0, failed: true}}
	if _, ok := m.imagePane(pane, doc, 1); ok {
		t.Error("imagePane = true for a failed decode")
	}
	// A stale entry (the file changed since the decode) is not drawn either.
	m.img.mosaic["/pic.png"] = &mosaicEntry{size: 5, mtime: 9, img: img}
	if _, ok := m.imagePane(pane, doc, 1); ok {
		t.Error("imagePane = true for a stale decode")
	}
}

// TestMosaicFailedLoadIsNotReissued checks that a read which failed is not tried
// again: the failed entry keeps its stat, so a second queue call starts nothing.
func TestMosaicFailedLoadIsNotReissued(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.png")
	doc := &imageTestDoc{lines: []string{"missing.png"}, path: path}
	m := New(Deps{Editor: newSplitImageEditor(doc), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	m.img = imageProtocolOf("auto")
	m.img.probed = true // answered "no support"

	// The open's imageQueue issues the decode; the second call must not.
	_, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if cmd == nil {
		t.Fatal("the open did not queue the decode")
	}
	msgs := collect(cmd)
	if len(msgs) != 1 {
		t.Fatalf("first imageQueue produced %d messages, want one load", len(msgs))
	}
	loaded, ok := msgs[0].(mosaicLoadedMsg)
	if !ok || loaded.err == nil {
		t.Fatalf("load = %+v, want a read failure for a missing file", msgs[0])
	}
	m.Update(loaded)
	if e := m.img.mosaic[path]; e == nil || !e.failed || e.pending {
		t.Fatalf("entry = %+v, want failed and not pending", e)
	}
	if cmd := m.imageQueue(); cmd != nil {
		if msgs := collect(cmd); len(msgs) != 0 {
			t.Fatalf("second imageQueue produced %d messages, want no reload", len(msgs))
		}
	}
}

// TestMosaicLoadCmdRejectsAnOversizedFile checks the byte limit before the read:
// a file over the cap is reported as failed and its pixels are never decoded.
func TestMosaicLoadCmdRejectsAnOversizedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(imageMaxBytes + 1); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	msg, ok := mosaicLoadCmd(path, 0, 0)().(mosaicLoadedMsg)
	if !ok {
		t.Fatal("mosaicLoadCmd did not return mosaicLoadedMsg")
	}
	if !errors.Is(msg.err, errImageTooLarge) || msg.img != nil {
		t.Fatalf("message = %+v, want the size error and no image", msg)
	}
}

// TestMosaicCacheEvictsTheOldest checks the cache bound: past four pictures the
// earliest-loaded entry is dropped.
func TestMosaicCacheEvictsTheOldest(t *testing.T) {
	var s imageState
	for i, p := range []string{"/a", "/b", "/c", "/d", "/e"} {
		s.mosaicPut(p, &mosaicEntry{size: int64(i)})
	}
	if len(s.mosaic) != mosaicCacheMax {
		t.Fatalf("cache holds %d entries, want %d", len(s.mosaic), mosaicCacheMax)
	}
	if _, ok := s.mosaic["/a"]; ok {
		t.Error("the oldest entry was not evicted")
	}
	if _, ok := s.mosaic["/e"]; !ok {
		t.Error("the newest entry was evicted")
	}
}
