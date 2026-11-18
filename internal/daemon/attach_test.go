package daemon

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
)

func TestRunAttachSessionEmitsVTSequencesBuffersNoticesAndRestores(t *testing.T) {
	client, server := net.Pipe()
	defer func() { _ = server.Close() }()

	inR, inW := io.Pipe()
	defer func() { _ = inW.Close() }()

	var out, errOut bytes.Buffer
	var order []string
	restore := func() {
		order = append(order, "restore:"+strconv.Itoa(out.Len()))
	}

	go func() {
		fw := NewFrameWriter(server)
		_ = fw.WriteFrame(TypeOutput, []byte("FRAME"))
		_ = fw.WriteNotice("first line\nsecond line\x1b[31m")
		_ = fw.WriteFrame(TypeDetached, nil)
	}()

	fw := NewFrameWriter(client)
	err := runAttachSession(context.Background(), client, fw, inR, &out, &errOut, true, restore)
	if err != nil {
		t.Fatalf("runAttachSession: %v", err)
	}

	gotOut := out.String()
	if !strings.HasPrefix(gotOut, attachEnterSeq) {
		t.Fatalf("stdout missing enter sequence prefix: %q", gotOut)
	}
	if !strings.Contains(gotOut, "FRAME") {
		t.Fatalf("stdout missing output frame: %q", gotOut)
	}
	if !strings.HasSuffix(gotOut, attachExitSeq) {
		t.Fatalf("stdout missing teardown sequence suffix: %q", gotOut)
	}
	for _, want := range []string{"?1000l", "?1002l", "?1003l", "?1006l", "?2004l", "?1004l", "<u", "?25h", "?1049l"} {
		if !strings.Contains(attachExitSeq, want) {
			t.Fatalf("attachExitSeq missing %q", want)
		}
	}
	if len(order) != 1 || order[0] != "restore:"+strconv.Itoa(len(gotOut)) {
		t.Fatalf("restore ran at %v, want after all %d output bytes", order, len(gotOut))
	}
	if gotErr := errOut.String(); gotErr != "gvim: first line second line[31m\n" {
		t.Fatalf("stderr notices = %q", gotErr)
	}
}

func TestRunAttachSessionPreservesReadFrameErrors(t *testing.T) {
	client, server := net.Pipe()
	defer func() { _ = server.Close() }()
	inR, inW := io.Pipe()
	defer func() { _ = inW.Close() }()

	go func() {
		// Truncated frame (5-byte header claiming 10 bytes, then EOF) -> io.ErrUnexpectedEOF.
		_, _ = server.Write([]byte{0, 0, 0, 10, TypeOutput, 'a', 'b'})
		_ = server.Close()
	}()

	var out, errOut bytes.Buffer
	err := runAttachSession(context.Background(), client, NewFrameWriter(client), inR, &out, &errOut, false, nil)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("err = %v, want io.ErrUnexpectedEOF", err)
	}
}
