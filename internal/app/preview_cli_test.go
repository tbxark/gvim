package app

import (
	"bufio"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

func TestFinalPreviewLineOnlyReadsPrefix(t *testing.T) {
	prefix := strings.Repeat("x", previewLineBytes+2)
	r := bufio.NewReaderSize(io.MultiReader(strings.NewReader(prefix),
		iotest.ErrReader(errors.New("read past the display prefix"))), len(prefix))
	var out strings.Builder
	p := previewPrinter{w: bufio.NewWriter(&out), tabs: "    "}
	if err := printPreviewLines(t.Context(), r, &p, nil, PreviewOptions{Lines: 1}); err != nil {
		t.Fatal(err)
	}
	if err := p.w.Flush(); err != nil {
		t.Fatal(err)
	}
	if want, got := prefix[:previewLineBytes]+"…\n", out.String(); got != want {
		t.Fatalf("stdout length = %d, want %d and a truncation marker", len(got), len(want))
	}
}
