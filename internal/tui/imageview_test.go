package tui

import (
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"

	"github.com/tbxark/gvim/document"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/split"
)

// TestImageProtocolOf checks the three settings, including the empty value that
// reads as auto.
func TestImageProtocolOf(t *testing.T) {
	tests := []struct {
		value         string
		off, forced   bool
		kitty, probed bool
		enabled       bool
		name          string
	}{
		{"", false, false, false, false, false, "empty"},
		{"auto", false, false, false, false, false, "auto"},
		{"kitty", false, true, true, true, true, "kitty"},
		{"off", true, false, false, true, false, "off"},
		{"bogus", false, false, false, false, false, "unknown reads as auto"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := imageProtocolOf(tt.value)
			if s.off != tt.off || s.forced != tt.forced || s.kitty != tt.kitty || s.probed != tt.probed {
				t.Fatalf("state = %+v", s)
			}
			if got := s.imageEnabled(); got != tt.enabled {
				t.Errorf("imageEnabled = %v, want %v", got, tt.enabled)
			}
		})
	}
}

// TestAsPNGConvertsWebp checks the format conversion: the protocol only carries
// PNG, so a WEBP is decoded and re-encoded. A PNG passes through unchanged.
func TestAsPNGConvertsWebp(t *testing.T) {
	// A 1x1 lossless WEBP, the fixture the standard library cannot encode.
	webp, err := base64.StdEncoding.DecodeString("UklGRhwAAABXRUJQVlA4TA8AAAAvAAAAAAcQ/Y/+ByKi/wEA")
	if err != nil {
		t.Fatalf("decode the webp fixture: %v", err)
	}
	out, ok := asPNG(webp)
	if !ok {
		t.Fatal("asPNG(webp) = false, want a converted PNG")
	}
	if len(out) < 8 || string(out[1:4]) != "PNG" {
		t.Fatalf("asPNG(webp) is not a PNG: %q", out[:min(len(out), 8)])
	}
	// A PNG goes through untouched.
	orig := tinyPNG()
	got, ok := asPNG(orig)
	if !ok || len(got) != len(orig) || got[0] != orig[0] {
		t.Error("asPNG(png) did not pass the bytes through")
	}
	// Something that is not an image is refused.
	if _, ok := asPNG([]byte("not an image")); ok {
		t.Error("asPNG accepted a non-image")
	}
	// A file that merely contains "PNG" in the middle of its header is not a
	// PNG and must go through the decoder, which rejects it.
	if _, ok := asPNG([]byte("?PNG\r\n\x1a\nnot really")); ok {
		t.Error("asPNG accepted a file whose signature is not the PNG signature")
	}
}

// TestImageUploadSequenceRejectsUndecodablePicture checks that an undecodable
// picture yields no sequence, so the caller leaves the summary text up.
func TestImageUploadSequenceRejectsUndecodablePicture(t *testing.T) {
	if seq := imageUploadSequence([]byte("not an image"), 1); seq != "" {
		t.Errorf("imageUploadSequence = %q, want no sequence", seq)
	}
	if seq := imageUploadSequence(tinyPNG(), 1); seq == "" {
		t.Error("imageUploadSequence(png) = empty, want an upload")
	}
}

// TestImageUploadSequenceRejectsTooManyPixels checks the pixel cap before the
// picture is turned into an upload. The header is enough; the pixels are never
// decoded.
func TestImageUploadSequenceRejectsTooManyPixels(t *testing.T) {
	data := fakePNGSize(t, imageMaxPixels+1, 1)
	if seq := imageUploadSequence(data, 1); seq != "" {
		t.Errorf("imageUploadSequence over the pixel cap = %q, want no sequence", seq)
	}
}

// fakePNGSize changes the IHDR dimensions of a 1x1 PNG and fixes its CRC. It
// tests the header cap without allocating the pixels of a huge picture.
func fakePNGSize(t *testing.T, w, h int) []byte {
	t.Helper()
	data := append([]byte(nil), tinyPNG()...)
	if len(data) < 33 {
		t.Fatal("the PNG fixture is too short")
	}
	binary.BigEndian.PutUint32(data[16:20], uint32(w))
	binary.BigEndian.PutUint32(data[20:24], uint32(h))
	crc := crc32.ChecksumIEEE(data[12:29])
	binary.BigEndian.PutUint32(data[29:33], crc)
	return data
}

// TestImageIDReusedWhileFileUnchanged checks that a picture is given one id and
// sent once, and takes a new id when the file changes.
func TestImageIDReusedWhileFileUnchanged(t *testing.T) {
	var s imageState
	id1, send1 := s.imageID("/a.png", 100, 1)
	if !send1 {
		t.Fatal("first imageID send = false, want true")
	}
	// Before the queued send lands: the file is already tracked, so a repaint
	// does not allocate a second id for it. The in-flight read is guarded by
	// pending in imageQueue, not by imageID.
	id2, send2 := s.imageID("/a.png", 100, 1)
	if send2 || id2 != id1 {
		t.Fatalf("second imageID = (%d, %v), want (%d, false)", id2, send2, id1)
	}
	id3, send3 := s.imageID("/a.png", 200, 2)
	if !send3 || id3 == id1 {
		t.Fatalf("changed file imageID = (%d, %v), want a fresh id and true", id3, send3)
	}
	if id1 == 0 || id3 == 0 {
		t.Error("an id must not be zero: the placeholder color cannot be 0")
	}
}

// TestImageUploadedIsRecordedSeparately checks the upload flag: a picture is not
// drawn until its data reached the terminal, and a changed file is not uploaded
// under the old id.
func TestImageUploadedIsRecordedSeparately(t *testing.T) {
	var s imageState
	id, _ := s.imageID("/a.png", 100, 1)
	if _, uploaded := s.imageUploaded("/a.png", 100, 1); uploaded {
		t.Fatal("uploaded = true before the send")
	}
	s.markUploaded("/a.png", id)
	if got, uploaded := s.imageUploaded("/a.png", 100, 1); !uploaded || got != id {
		t.Fatalf("uploaded = %v id = %d, want true and %d", uploaded, got, id)
	}
	// A file that changed since is not uploaded: its entry has new size and mtime.
	if _, uploaded := s.imageUploaded("/a.png", 200, 2); uploaded {
		t.Error("uploaded = true for a changed file")
	}
}

// TestImageFitKeepsAspectRatio checks that a picture is scaled into the area
// without stretching, and centered.
func TestImageFitKeepsAspectRatio(t *testing.T) {
	area := imageRegion{x: 0, y: 0, w: 40, h: 20}
	// A picture wider than the area: its width is capped and the rows follow.
	got := imageFit(area, 1600, 400, 10, 20)
	if got.w != 40 {
		t.Errorf("width = %d, want capped at 40", got.w)
	}
	if got.h != 5 {
		t.Errorf("height = %d, want the aspect ratio kept (5)", got.h)
	}
	// A picture smaller than the area is not enlarged.
	small := imageFit(area, 80, 40, 10, 20)
	if small.w != 8 || small.h != 2 {
		t.Errorf("small fit = %dx%d, want 8x2", small.w, small.h)
	}
	if small.x != (40-8)/2 || small.y != (20-2)/2 {
		t.Errorf("small fit origin = (%d, %d), want it centered", small.x, small.y)
	}
}

// TestPaintImageDrawsPlaceholders checks the cells: each covered cell holds the
// placeholder with its row and column diacritics and the id in the foreground,
// and the first cell carries the virtual placement.
func TestPaintImageDrawsPlaceholders(t *testing.T) {
	g := grid.New(10, 6)
	area := imageRegion{x: 0, y: 0, w: 10, h: 6}
	info := document.ImageInfo{Format: "png", Width: 100, Height: 60}
	paintImage(g, area, info, 42, 1, 10, 20, grid.Style{})

	// 100x60 pixels at 10x20 cell pixels is 10x3 cells, centered in 6 rows, so the
	// picture starts at row 1.
	c := g.At(0, 1)
	if !strings.Contains(c.Text, string(kitty.Placeholder)) {
		t.Fatalf("cell (0,1) = %q, want a placeholder", c.Text)
	}
	if !strings.Contains(c.Text, "\x1b_G") {
		t.Errorf("cell (0,1) = %q, want the virtual placement before the placeholder", c.Text)
	}
	if c.Style.FG == nil {
		t.Error("placeholder cell lost the id color")
	}
	if c.Style.Underline {
		t.Error("placeholder cell enabled SGR 4, which draws a line under every row")
	}
	if c.Style.UnderlineColor == nil {
		t.Error("placeholder cell lost the placement id in the underline color")
	}
	if line := g.Lines()[1]; !strings.Contains(line, ";58;") || strings.Contains(line, ";4;") {
		t.Errorf("placeholder SGR = %q, want SGR 58 without a visible underline", line)
	}
	// A cell inside the picture but not the first: the placeholder only.
	c2 := g.At(1, 1)
	if !strings.Contains(c2.Text, string(kitty.Placeholder)) || strings.Contains(c2.Text, "\x1b_G") {
		t.Errorf("cell (1,1) = %q, want just the placeholder", c2.Text)
	}
	// Row 0 is above the centered picture: the body fill, no placeholder.
	out := g.At(0, 0)
	if strings.Contains(out.Text, string(kitty.Placeholder)) {
		t.Errorf("cell (0,0) = %q, want the body fill", out.Text)
	}
}

// TestImagePlacementIsStable checks that a pane's placement id is part of both
// the placement command and the cells that name it: the terminal then replaces
// the previous placement instead of stacking a new one every frame.
func TestImagePlacementIsStable(t *testing.T) {
	seq := imageVirtualPlacement(42, 7, 10, 5)
	if !strings.Contains(seq, "i=42") || !strings.Contains(seq, "p=7") {
		t.Fatalf("placement = %q, want image id 42 and placement id 7", seq)
	}
	if !strings.Contains(seq, "c=10") || !strings.Contains(seq, "r=5") {
		t.Fatalf("placement = %q, want the 10x5 cell size", seq)
	}
}

// TestPlaceholderCellEncodesRowAndColumn checks the diacritics: distinct cells of
// the picture get distinct text, so the terminal does not repeat one cell.
func TestPlaceholderCellEncodesRowAndColumn(t *testing.T) {
	a := placeholderCell(0, 0)
	b := placeholderCell(0, 1)
	c := placeholderCell(1, 0)
	if a == b || a == c || b == c {
		t.Fatalf("placeholder cells collide: %q %q %q", a, b, c)
	}
	for _, s := range []string{a, b, c} {
		if !strings.HasPrefix(s, string(kitty.Placeholder)) {
			t.Errorf("%q does not start with the placeholder rune", s)
		}
	}
}

// TestImageIDColorFitsTrueColor checks the id's foreground color: small ids are
// indexed, and larger ones are a true color that carries the id.
func TestImageIDColorFitsTrueColor(t *testing.T) {
	if got, ok := imageIDColor(42).(ansi.IndexedColor); !ok || got != 42 {
		t.Errorf("id 42 color = %#v, want indexed 42", imageIDColor(42))
	}
	if _, ok := imageIDColor(300).(ansi.IndexedColor); ok {
		t.Error("id 300 must not be an indexed color")
	}
}

// TestImageTransmitIsChunkedAndQuiet checks the upload: quiet, chunked, and the
// chunks carry the continuation keys the protocol requires.
func TestImageTransmitIsChunkedAndQuiet(t *testing.T) {
	data := make([]byte, 9000)
	for i := range data {
		data[i] = byte(i)
	}
	seq := imageTransmit(data, 7)
	if !strings.Contains(seq, "a=t") || !strings.Contains(seq, "f=100") || !strings.Contains(seq, "i=7") {
		t.Fatalf("upload = %q, want the transmit keys", seq)
	}
	if !strings.Contains(seq, "q=2") {
		t.Error("upload is not quiet: the ack would reach the program")
	}
	if strings.Count(seq, "\x1b_G") < 2 {
		t.Error("a 9 KB picture must be chunked")
	}
	if !strings.Contains(seq, "m=1") || !strings.Contains(seq, "m=0") {
		t.Error("chunks must mark more-data and the last chunk")
	}
	// An id the terminal is told must not be zero.
	if !strings.Contains(seq, "i=7") {
		t.Error("the upload must name the image id")
	}
}

// TestTextDocumentIsNotAnImagePane is the guard for the interface shape: the
// document adapter has Image, FilePath and ImageStat for every document, so a text
// document satisfies imagePathDoc too. The Viewer must still treat it as text, or
// every tab would try to read its "picture".
func TestTextDocumentIsNotAnImagePane(t *testing.T) {
	m := &Model{}
	m.img = imageProtocolOf("kitty")
	doc := &textImageShapedDoc{}
	if _, ok := m.imagePane(split.Pane{W: 20, H: 5}, doc, 1); ok {
		t.Error("imagePane = true for a text document, want the text pane")
	}
	if _, ok := m.imagePathOf(doc); ok {
		t.Error("imagePathOf = true for a text document, want false")
	}
}

// textImageShapedDoc is a text document that also has the image methods, the way
// the app's adapter does. Its Image reports false.
type textImageShapedDoc struct{ fakeDoc }

func (*textImageShapedDoc) Image() (document.ImageInfo, bool) { return document.ImageInfo{}, false }
func (*textImageShapedDoc) FilePath() string                  { return "/notes.txt" }
func (*textImageShapedDoc) ImageStat() (size, mtime int64)    { return 10, 1 }

// remoteImageDoc is an image document whose file belongs to another host.
type remoteImageDoc struct{ imageTestDoc }

func (*remoteImageDoc) LocalImage() bool { return false }

// TestRemoteImageKeepsTheSummary checks the local-only gate: a remote image must
// never reach os.ReadFile, even when its adapter exposes a path.
func TestRemoteImageKeepsTheSummary(t *testing.T) {
	m := &Model{}
	m.img = imageProtocolOf("kitty")
	doc := &remoteImageDoc{imageTestDoc{path: "/server/pic.png"}}
	if path, ok := m.imagePathOf(doc); ok || path != "" {
		t.Fatalf("imagePathOf(remote) = %q, %v, want no local path", path, ok)
	}
	if _, ok := m.imagePane(split.Pane{W: 20, H: 5}, doc, 1); ok {
		t.Error("imagePane = true for a remote image, want the text summary")
	}
}

// TestImagePanelFallsBackWhenDisabled checks that a pane showing an image is not
// drawn as an image when the terminal cannot, or before the upload landed, so the
// summary text stays.
func TestImagePanelFallsBackWhenDisabled(t *testing.T) {
	m := &Model{}
	m.img = imageProtocolOf("off")
	doc := &imageTestDoc{}
	if _, ok := m.imagePane(split.Pane{W: 20, H: 5}, doc, 1); ok {
		t.Error("imagePane = true with the protocol off, want the text fallback")
	}
	m.img = imageProtocolOf("kitty")
	if _, ok := m.imagePane(split.Pane{W: 20, H: 5}, doc, 1); ok {
		t.Error("imagePane = true before the upload, want the text fallback")
	}
	// With the picture uploaded, the pane draws it.
	id, _ := m.img.imageID("/pic.png", 0, 0)
	m.img.markUploaded("/pic.png", id)
	doc.path = "/pic.png"
	if _, ok := m.imagePane(split.Pane{W: 20, H: 5}, doc, 1); !ok {
		t.Error("imagePane = false after the upload, want the picture")
	}
}

// imageTestDoc is a Document that holds an image, for the pane tests.
type imageTestDoc struct {
	fakeDoc
	path string
}

func (*imageTestDoc) Image() (document.ImageInfo, bool) {
	return document.ImageInfo{Format: "png", Width: 100, Height: 50}, true
}

func (d *imageTestDoc) FilePath() string { return d.path }

func (d *imageTestDoc) ImageStat() (size, mtime int64) { return 0, 0 }

// splitImageEditor is a one-tab editor that also exposes DocAt, so the split
// layout can find an image document. The tab has no path of its own until the
// test sets it, and the image document supplies the path and the stat.
type splitImageEditor struct {
	*fakeEditor
	docs []Document
}

func newSplitImageEditor(docs ...Document) *splitImageEditor {
	ed := &splitImageEditor{fakeEditor: newFakeEditor("x"), docs: docs}
	ed.doc = &fakeDoc{lines: []string{"x"}}
	ed.tabs = make([]TabInfo, len(docs))
	for i := range docs {
		ed.tabs[i] = TabInfo{Name: "tab" + strconv.Itoa(i)}
		if p, ok := docs[i].(interface{ FilePath() string }); ok {
			ed.tabs[i].Path = p.FilePath()
		}
	}
	ed.tabs[0].Active = true
	return ed
}

func (e *splitImageEditor) Active() Document {
	if len(e.docs) == 0 {
		return nil
	}
	return e.docs[0]
}

func (e *splitImageEditor) Tabs() []TabInfo { return e.tabs }

func (e *splitImageEditor) DocAt(i int) Document {
	if i < 0 || i >= len(e.docs) {
		return nil
	}
	return e.docs[i]
}
