package tui

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi/kitty"

	"github.com/tbxark/gvim/document"
	"github.com/tbxark/gvim/internal/filewatch"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/split"
	"github.com/tbxark/gvim/theme"
)

// TestPlaceholdersReachTheTerminal is the end-to-end check of the render path: the
// cells a pane paints become the frame string (grid.Lines), the frame is parsed the
// way bubbletea's renderer parses it (ultraviolet's StyledString), and the
// placeholder text and the id's color survive to the terminal output. This is what
// makes the viewer work without a post-render hook.
func TestPlaceholdersReachTheTerminal(t *testing.T) {
	g := grid.New(12, 4)
	paintImage(g, imageRegion{x: 0, y: 0, w: 12, h: 4},
		document.ImageInfo{Format: "png", Width: 120, Height: 80}, 42, 1, 10, 20, grid.Style{})

	frame := g.String()
	if !strings.Contains(frame, string(kitty.Placeholder)) {
		t.Fatalf("the frame string lost the placeholder: %q", frame)
	}

	// The host pipeline: bubbletea parses the view with ultraviolet into a screen
	// buffer, which is where the placeholder must survive as a cell's content.
	buf := uv.NewScreenBuffer(12, 4)
	uv.NewStyledString(frame).Draw(buf, buf.Bounds())

	found, colorKept := false, false
	for y := range 4 {
		for x := range 12 {
			c := buf.CellAt(x, y)
			if c == nil || !strings.Contains(c.Content, string(kitty.Placeholder)) {
				continue
			}
			found = true
			if c.Style.Fg != nil {
				colorKept = true
			}
		}
	}
	if !found {
		t.Fatal("the placeholder did not survive the host render pipeline")
	}
	if !colorKept {
		t.Error("the id color did not survive with the placeholder")
	}
}

// TestImageProbeReplyMarksSupport checks the capability answer: a reply with the
// probe id turns the viewer on, and an unrelated reply does not.
func TestImageProbeReplyMarksSupport(t *testing.T) {
	m := &Model{}
	cmd, ok := m.imageMessage(uv.KittyGraphicsEvent{Options: kitty.Options{ID: imageProbeID}})
	if !ok || cmd != nil {
		t.Fatalf("probe reply handled = %v, cmd = %v", ok, cmd)
	}
	if !m.img.kitty || !m.img.probed {
		t.Fatalf("state = %+v, want kitty and probed", m.img)
	}
	if !m.img.imageEnabled() {
		t.Error("imageEnabled = false after a supporting reply")
	}
}

// TestImageProbeTimeoutMarksNoSupport checks that a silent terminal is recorded as
// no support, so the summary text is drawn.
func TestImageProbeTimeoutMarksNoSupport(t *testing.T) {
	m := &Model{}
	m.imageMessage(imageProbeTimeoutMsg{})
	if !m.img.probed || m.img.kitty {
		t.Fatalf("state = %+v, want probed and not kitty", m.img)
	}
	if m.img.imageEnabled() {
		t.Error("imageEnabled = true after a timeout")
	}
}

// TestImageCellSizeFromTerminal checks the cell-size reply, which the fit uses.
func TestImageCellSizeFromTerminal(t *testing.T) {
	m := &Model{}
	m.imageMessage(uv.CellSizeEvent{Width: 9, Height: 18})
	if w, h := m.img.cellSize(); w != 9 || h != 18 {
		t.Errorf("cellSize = %dx%d, want 9x18", w, h)
	}
	// Without a reply the default is used.
	empty := &Model{}
	if w, h := empty.img.cellSize(); w != imageCellSize.w || h != imageCellSize.h {
		t.Errorf("default cellSize = %dx%d", w, h)
	}
}

// TestImageSendRemovesPending checks that the upload message clears the in-flight
// mark and returns the upload, and that the picture becomes drawable only when the
// terminal's Raw message has been delivered.
func TestImageSendRemovesPending(t *testing.T) {
	m := &Model{}
	m.img.sent = map[string]sentImage{"/a.png": {id: 7, size: 1, mtime: 1}}
	m.img.pending = map[int]bool{7: true}
	cmd, ok := m.imageMessage(imageSendMsg{path: "/a.png", id: 7, data: tinyPNG()})
	if !ok || cmd == nil {
		t.Fatal("imageSendMsg was not handled with a command")
	}
	if m.img.pending[7] {
		t.Error("the id stayed pending after the send")
	}
	if _, uploaded := m.img.imageUploaded("/a.png", 1, 1); uploaded {
		t.Error("the picture was marked uploaded before the Raw message was delivered")
	}
	if next, _ := m.imageMessage(imageUploadedMsg{path: "/a.png", id: 7}); next != nil {
		t.Error("imageUploadedMsg started a command")
	}
	if _, uploaded := m.img.imageUploaded("/a.png", 1, 1); !uploaded {
		t.Error("the picture was not marked uploaded after the Raw message")
	}
}

// TestImageUploadRawPrecedesTheUploadedMark checks the command order: the Raw
// output runs before the message that makes the pane draw placeholders.
func TestImageUploadRawPrecedesTheUploadedMark(t *testing.T) {
	m := &Model{}
	m.img.sent = map[string]sentImage{"/a.png": {id: 7, size: 1, mtime: 1}}
	m.img.pending = map[int]bool{7: true}
	cmd, ok := m.imageMessage(imageSendMsg{path: "/a.png", id: 7, data: tinyPNG()})
	if !ok || cmd == nil {
		t.Fatal("imageSendMsg was not handled with a command")
	}
	msgs := collect(cmd)
	if len(msgs) != 2 {
		t.Fatalf("upload produced %d messages, want Raw then imageUploadedMsg", len(msgs))
	}
	if _, ok := msgs[0].(tea.RawMsg); !ok {
		t.Fatalf("first message = %T, want tea.RawMsg", msgs[0])
	}
	if _, ok := msgs[1].(imageUploadedMsg); !ok {
		t.Fatalf("second message = %T, want imageUploadedMsg", msgs[1])
	}
}

// TestImageSendFailureClearsPendingAndStaysFailed checks that a read failure is
// not retried on every frame: the id is no longer pending, the entry is marked
// failed, and a later send for the same stat is not queued again.
func TestImageSendFailureClearsPendingAndStaysFailed(t *testing.T) {
	m := &Model{}
	m.img.sent = map[string]sentImage{"/a.png": {id: 7, size: 1, mtime: 1}}
	m.img.pending = map[int]bool{7: true}
	cmd, ok := m.imageMessage(imageSendMsg{path: "/a.png", id: 7, err: errors.New("permission denied")})
	if !ok || cmd != nil {
		t.Fatalf("handled = %v, cmd = %v, want a handled message with no upload", ok, cmd)
	}
	if m.img.pending[7] {
		t.Error("the id stayed pending after a failed read")
	}
	got := m.img.sent["/a.png"]
	if got.state != imageFailed {
		t.Error("the picture was not marked failed")
	}
	if _, send := m.img.imageID("/a.png", 1, 1); send {
		t.Error("the same failed file was queued again")
	}
	if _, upload := m.img.imageUploaded("/a.png", 1, 1); upload {
		t.Error("a failed read was marked uploaded")
	}
}

// TestImageSendCmdRejectsAnOversizedFile checks the byte limit before the read:
// a file over the cap is reported as failed and is not loaded into memory.
func TestImageSendCmdRejectsAnOversizedFile(t *testing.T) {
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
	msg, ok := imageSendCmd(path, 3)().(imageSendMsg)
	if !ok {
		t.Fatal("imageSendCmd did not return imageSendMsg")
	}
	if !errors.Is(msg.err, errImageTooLarge) || msg.data != nil {
		t.Fatalf("message = %+v, want the size error and no bytes", msg)
	}
}

// TestImageSendKeepsSummaryForUndecodablePicture checks that a picture that cannot
// be decoded leaves the entry un-uploaded, so the summary stays and nothing is sent.
func TestImageSendKeepsSummaryForUndecodablePicture(t *testing.T) {
	m := &Model{}
	m.img.sent = map[string]sentImage{"/a.png": {id: 7, size: 1, mtime: 1}}
	m.img.pending = map[int]bool{7: true}
	cmd, ok := m.imageMessage(imageSendMsg{path: "/a.png", id: 7, data: []byte("not an image")})
	if !ok || cmd != nil {
		t.Fatalf("handled = %v, cmd = %v, want a handled message with no upload", ok, cmd)
	}
	if _, uploaded := m.img.imageUploaded("/a.png", 1, 1); uploaded {
		t.Error("an undecodable picture was marked uploaded")
	}
}

// TestProbeSequenceIsAnswered checks the query: it must not be quiet (a quiet
// query is not answered even by a terminal that supports the protocol, so every
// probe would time out), it must carry the probe id, and it must name a 1x1 RGB
// image with the protocol example's payload.
func TestProbeSequenceIsAnswered(t *testing.T) {
	seq := probeSequence()
	for _, want := range []string{"a=q", "i=" + strconv.Itoa(imageProbeID), "f=24", "s=1", "v=1"} {
		if !strings.Contains(seq, want) {
			t.Errorf("probe sequence %q lacks %q", seq, want)
		}
	}
	if strings.Contains(seq, "q=1") || strings.Contains(seq, "q=2") {
		t.Errorf("probe sequence %q is quiet: a quiet query is never answered", seq)
	}
	if imageProbeID == 0 {
		t.Error("the probe id must not be 0: a reply whose id failed to parse reads as 0")
	}
}

// tinyPNG returns a 1x1 PNG, the smallest picture the upload accepts.
func tinyPNG() []byte {
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1)))
	return buf.Bytes()
}

// TestImageQueueSkipsDisabledProtocol checks that no read is queued while the
// terminal cannot draw images.
func TestImageQueueSkipsDisabledProtocol(t *testing.T) {
	m := &Model{}
	m.img = imageProtocolOf("off")
	if cmd := m.imageQueue(); cmd != nil {
		t.Error("imageQueue queued a read with the protocol off")
	}
}

// TestImageQueueStartsNothingForAHostedWorkspace checks that a daemon-hosted
// workspace never probes the terminal and never reads a picture.
func TestImageQueueStartsNothingForAHostedWorkspace(t *testing.T) {
	m := &Model{}
	m.img = imageProtocolOf("auto")
	m.SetHostedWorkspace(true)
	if cmd := m.imageQueue(); cmd != nil {
		t.Error("imageQueue started work for a hosted workspace")
	}
	if m.img.arming || len(m.img.sent) != 0 {
		t.Error("a hosted workspace probed or allocated an image")
	}
}

// TestImageProbeReplyQueuesTheUpload checks that a supporting terminal gets the
// picture in the same update as its reply, without waiting for another event.
func TestImageProbeReplyQueuesTheUpload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pic.png")
	if err := os.WriteFile(path, tinyPNG(), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := &imageTestDoc{fakeDoc: fakeDoc{lines: []string{"pic.png"}}, path: path}
	ed := newSplitImageEditor(doc)
	m := New(Deps{Editor: ed, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	m.img = imageProtocolOf("auto")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	cmd, ok := m.imageMessage(uv.KittyGraphicsEvent{Options: kitty.Options{ID: imageProbeID}})
	if !ok || cmd == nil {
		t.Fatalf("probe reply handled = %v, cmd = %v, want an upload command", ok, cmd)
	}
	msgs := collect(cmd)
	var sent *imageSendMsg
	for i := range msgs {
		if msg, ok := msgs[i].(imageSendMsg); ok {
			sent = &msg
		}
	}
	if sent == nil {
		t.Fatalf("the probe reply produced %T messages, want an imageSendMsg", msgs)
	}
	if sent.path != path || sent.err != nil || len(sent.data) == 0 {
		t.Fatalf("send = %+v, want the picture bytes of %s", *sent, path)
	}
}

// TestImageCloseReleasesAndReopenUploads checks the close path: the last tab of
// the image releases its placement and resource, drops the local entries, and a
// reopen reads and sends the picture again under a fresh id.
func TestImageCloseReleasesAndReopenUploads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pic.png")
	if err := os.WriteFile(path, tinyPNG(), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := &imageTestDoc{fakeDoc: fakeDoc{lines: []string{"pic.png"}}, path: path}
	ed := newSplitImageEditor(doc)
	m := New(Deps{Editor: ed, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	m.img = imageProtocolOf("kitty")
	_, openCmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// The window-size update queued the read; feed its upload so the entry has the
	// picture's own stat and is marked uploaded. The collect helper does not flatten
	// a sequence, so deliver the two messages in the order the program would.
	msgs := collect(openCmd)
	if len(msgs) != 1 {
		t.Fatalf("the open produced %d messages, want one send", len(msgs))
	}
	_, cmd := m.Update(msgs[0])
	feedMessages(m, cmd)
	current := m.img.sent[path]
	if _, uploaded := m.img.imageUploaded(path, current.size, current.mtime); !uploaded {
		t.Fatal("the opened picture did not reach the terminal")
	}
	ed.docs = nil
	ed.tabs = nil
	cmd = m.imageReleaseClosed()
	if cmd == nil {
		t.Fatal("closing the image did not release its terminal resource")
	}
	msgs = collect(cmd)
	if len(msgs) != 1 {
		t.Fatalf("release produced %d messages, want one raw output", len(msgs))
	}
	raw, ok := msgs[0].(tea.RawMsg)
	if !ok {
		t.Fatalf("release produced %T, want tea.RawMsg", msgs[0])
	}
	if got := fmt.Sprint(raw.Msg); !strings.Contains(got, imageDeleteSequence(current.id)) {
		t.Fatalf("release output = %q, want the delete sequence of image %d", got, current.id)
	}
	if len(m.img.sent) != 0 || len(m.img.pending) != 0 {
		t.Fatalf("after close: sent=%v pending=%v, want both empty", m.img.sent, m.img.pending)
	}

	ed.docs = []Document{doc}
	ed.tabs = []TabInfo{{Name: "pic.png", Path: path, Active: true}}
	m.img.kitty, m.img.probed = true, true
	cmd = m.imageQueue()
	if cmd == nil {
		t.Fatal("reopening the image did not queue another read")
	}
	msgs = collect(cmd)
	if len(msgs) != 1 {
		t.Fatalf("reopen produced %d messages, want one send", len(msgs))
	}
	send, ok := msgs[0].(imageSendMsg)
	if !ok {
		t.Fatalf("reopen produced %T, want imageSendMsg", msgs[0])
	}
	if send.id == current.id {
		t.Errorf("reopen reused image id %d, want a fresh id", send.id)
	}
	if send.path != path || send.err != nil || len(send.data) == 0 {
		t.Fatalf("reopen send = %+v, want the picture bytes", send)
	}
}

// TestImageProtocolOffReleasesAndOnUploadsAgain checks the live setting: off
// deletes the terminal resources and clears the local entries, and kitty reads and
// uploads the open picture again.
func TestImageProtocolOffReleasesAndOnUploadsAgain(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pic.png")
	if err := os.WriteFile(path, tinyPNG(), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := &imageTestDoc{fakeDoc: fakeDoc{lines: []string{"pic.png"}}, path: path}
	m := New(Deps{Editor: newSplitImageEditor(doc), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	m.img = imageProtocolOf("kitty")
	_, openCmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	msgs := collect(openCmd)
	if len(msgs) != 1 {
		t.Fatalf("the open produced %d messages, want one send", len(msgs))
	}
	_, cmd := m.Update(msgs[0])
	feedMessages(m, cmd)
	current := m.img.sent[path]
	if _, uploaded := m.img.imageUploaded(path, current.size, current.mtime); !uploaded {
		t.Fatal("the opened picture did not reach the terminal")
	}

	cmd = m.applyImageProtocol(imageProtocolOff)
	if cmd == nil {
		t.Fatal("turning the protocol off did not delete the terminal resource")
	}
	msgs = collect(cmd)
	if len(msgs) != 1 {
		t.Fatalf("release produced %d messages, want one raw output", len(msgs))
	}
	raw, ok := msgs[0].(tea.RawMsg)
	if !ok {
		t.Fatalf("release produced %T, want tea.RawMsg", msgs[0])
	}
	if got := fmt.Sprint(raw.Msg); !strings.Contains(got, imageDeleteSequence(current.id)) {
		t.Fatalf("release output = %q, want the delete sequence of image %d", got, current.id)
	}
	if len(m.img.sent) != 0 || len(m.img.pending) != 0 {
		t.Fatalf("after off: sent=%v pending=%v, want both empty", m.img.sent, m.img.pending)
	}

	cmd = m.applyImageProtocol(imageProtocolKitty)
	if cmd == nil {
		t.Fatal("turning the protocol back on did not queue the picture")
	}
	msgs = collect(cmd)
	if len(msgs) != 1 {
		t.Fatalf("turn-on produced %d messages, want one send", len(msgs))
	}
	send, ok := msgs[0].(imageSendMsg)
	if !ok {
		t.Fatalf("turn-on produced %T, want imageSendMsg", msgs[0])
	}
	if send.path != path || send.err != nil || len(send.data) == 0 {
		t.Fatalf("turn-on send = %+v, want the picture bytes", send)
	}
}

// TestImageProtocolSwitchRetriesAFailedRead checks that a read which failed while
// auto found no support is retried when the protocol becomes usable (kitty). The
// entry's stat is unchanged, so only clearing the failed entry makes imageID queue
// it again.
func TestImageProtocolSwitchRetriesAFailedRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pic.png")
	if err := os.WriteFile(path, tinyPNG(), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := &imageTestDoc{fakeDoc: fakeDoc{lines: []string{"pic.png"}}, path: path}
	m := New(Deps{Editor: newSplitImageEditor(doc), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// Auto's probe timed out: no support. A failed file entry can only exist after
	// such a switch in real use, so model it directly with the file's stat.
	m.img.probed = true
	size, mtime := m.imageStatOf(doc)
	id, _ := m.img.imageID(path, size, mtime)
	m.img.markFailed(path, id)

	cmd := m.applyImageProtocol(imageProtocolKitty)
	if cmd == nil {
		t.Fatal("switching to kitty did not retry the failed picture")
	}
	msgs := collect(cmd)
	if len(msgs) != 1 {
		t.Fatalf("retry produced %d messages, want one send", len(msgs))
	}
	send, ok := msgs[0].(imageSendMsg)
	if !ok {
		t.Fatalf("retry produced %T, want imageSendMsg", msgs[0])
	}
	if send.path != path || send.err != nil || len(send.data) == 0 {
		t.Fatalf("retry send = %+v, want the picture bytes", send)
	}
}

// TestImageQueueUploadsEachPathOnce checks the two-pane case: when both panes show
// the same image, one update queues one read, not one per pane.
func TestImageQueueUploadsEachPathOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pic.png")
	if err := os.WriteFile(path, tinyPNG(), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := &imageTestDoc{fakeDoc: fakeDoc{lines: []string{"pic.png"}}, path: path}
	m := New(Deps{Editor: newSplitImageEditor(doc), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	m.img = imageProtocolOf("kitty")
	_, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	msgs := collect(cmd)
	if len(msgs) != 1 {
		t.Fatalf("the open produced %d messages, want one send", len(msgs))
	}

	// Split the window so both panes show the same tab, then queue again. The
	// picture is already in flight (or uploaded), so no second read is started.
	runAction(t, m, "split_vertical")
	if cmd := m.imageQueue(); cmd != nil {
		msgs = collect(cmd)
		if len(msgs) != 0 {
			t.Fatalf("two panes produced %d messages, want no second read", len(msgs))
		}
	}
}

// TestImageQueueUploadsAnImageInANonFocusedPane checks that the viewer reads every
// image a split shows, not only the active tab's: a background pane can show an
// image while the focused pane shows text.
func TestImageQueueUploadsAnImageInANonFocusedPane(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pic.png")
	if err := os.WriteFile(path, tinyPNG(), 0o644); err != nil {
		t.Fatal(err)
	}
	text := &fakeDoc{lines: []string{"notes"}}
	image := &imageTestDoc{fakeDoc: fakeDoc{lines: []string{"pic.png"}}, path: path}
	ed := newSplitImageEditor(text, image)
	m := New(Deps{Editor: ed, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	m.img = imageProtocolOf("kitty")
	// The left pane shows the image tab while the right (focused) pane and the
	// editor's active tab show text.
	root := &split.Node{
		Dir: split.SideBySide,
		A:   &split.Node{Tab: 1},
		B:   &split.Node{Tab: 0},
	}
	m.splits.Root, m.splits.Focus = root, root.B
	_, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	for _, msg := range collect(cmd) {
		send, ok := msg.(imageSendMsg)
		if !ok {
			continue
		}
		if send.path != path || send.err != nil || len(send.data) == 0 {
			t.Fatalf("send = %+v, want the background image's bytes from %s", send, path)
		}
		return
	}
	t.Fatal("the image in the non-focused pane was not read")
}

// TestImageWatcherAndAutoRevertSkipAnImage checks the image tab's place in the reload
// paths: its folder is not added to the watcher, and a watcher report for its file
// starts no revert.
func TestImageWatcherAndAutoRevertSkipAnImage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pic.png")
	if err := os.WriteFile(path, tinyPNG(), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := &imageTestDoc{fakeDoc: fakeDoc{lines: []string{"pic.png"}}, path: path}
	ed := newSplitImageEditor(doc)
	dirs := &fakeWatchDirs{}
	m := New(Deps{
		Editor: ed,
		Keys:   testKeys(),
		Theme:  theme.Theme{},
		Clock:  testClock,
		Root:   RootPorts{Watch: make(chan filewatch.Changed), WatchDirs: dirs},
	})
	_, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	collect(cmd)
	if len(dirs.added) != 0 {
		t.Fatalf("the image's folder was watched: %q", dirs.added)
	}
	if files := m.openTabFiles(); len(files) != 1 || !files[0].image {
		t.Fatalf("openTabFiles = %+v, want the one image tab", files)
	}
	if cmd := m.autoRevertChanged([]string{path}); cmd != nil {
		t.Fatalf("auto-revert of an image started %T, want no command", cmd)
	}
}
