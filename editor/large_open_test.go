package editor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestLargeThresholdBoundary(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name      string
		mutate    func(*Options)
		Size      int
		wantLarge bool
	}{
		{Name: "size equal to the threshold", Size: 1024, wantLarge: true},
		{Name: "size above the threshold", Size: 1025, wantLarge: true},
		{Name: "size below the threshold", Size: 1023, wantLarge: false},
		{Name: "mode off", mutate: func(o *Options) { o.LargeFileMode = false }, Size: 4096, wantLarge: false},
		{Name: "threshold zero is off", mutate: func(o *Options) { o.LargeFileThresholdBytes = 0 }, Size: 4096, wantLarge: false},
		{Name: "threshold negative is off", mutate: func(o *Options) { o.LargeFileThresholdBytes = -1 }, Size: 4096, wantLarge: false},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			opts := largeOptions()
			if tt.mutate != nil {
				tt.mutate(&opts)
			}
			content := strings.Repeat("x", tt.Size-1) + "\n"
			if tt.wantLarge {
				content = strings.Repeat("abc def\n", tt.Size/8) + strings.Repeat("x", tt.Size%8)
			}
			_, d := openLarge(t, opts, content)
			if d.Large() != tt.wantLarge {
				t.Fatalf("Large() = %v, want %v", d.Large(), tt.wantLarge)
			}
			if d.Buffer.Large() != tt.wantLarge {
				t.Fatalf("Buffer.Large() = %v, want %v", d.Buffer.Large(), tt.wantLarge)
			}
			if !tt.wantLarge && !d.Index().Complete {
				t.Fatal("a document in memory has its whole index")
			}
		})
	}
}

func TestLargeOpenReadsLazily(t *testing.T) {
	t.Parallel()
	content := lineText(10000)
	_, d, src := openMem(t, largeOptions(), content)
	if !d.Large() {
		t.Fatal("not large")
	}
	if got := src.BytesRead(); got > largeSampleSize+64+32 {
		t.Fatalf("open read %d bytes of %d, want only the sample and the first chunks", got, len(content))
	}
	if d.Index().Complete {
		t.Fatal("the index is complete after open, want it to build in the background")
	}
	if got := d.Buffer.Line(0); got != "line 0000" {
		t.Fatalf("Line(0) = %q", got)
	}
	if d.Saved != "" {
		t.Fatalf("a large document keeps no saved text, has %d bytes", len(d.Saved))
	}
	if st := d.Buffer.Stats(); st.OverCap != 0 || st.MaterializedBytes != 0 {
		t.Fatalf("open materialized text: %+v", st)
	}
}

func TestLargeEncodingAndLineEnding(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name     string
		content  string
		wantCode string
		wantEnc  string
		wantEOL  string
		wantLine string
	}{
		{Name: "ascii lf", content: lineText(120), wantCode: "utf-8", wantEnc: "ASCII", wantEOL: "LF", wantLine: "line 0000"},
		{
			Name:     "utf-8 with multibyte text",
			content:  strings.Repeat("café olé\n", 100),
			wantCode: "utf-8", wantEnc: "UTF-8", wantEOL: "LF", wantLine: "café olé",
		},
		{
			Name:     "utf-8 bom stays the codec and is not text",
			content:  "\ufeff" + lineText(120),
			wantCode: "utf-8-bom", wantEnc: "UTF-8 BOM", wantEOL: "LF", wantLine: "line 0000",
		},
		{
			Name:     "crlf majority",
			content:  strings.Repeat("crlf line\r\n", 100) + "bare\n",
			wantCode: "utf-8", wantEnc: "ASCII", wantEOL: "CRLF", wantLine: "crlf line",
		},
		{
			Name:     "a tie is lf",
			content:  strings.Repeat("a\r\nb\n", 300),
			wantCode: "utf-8", wantEnc: "ASCII", wantEOL: "LF", wantLine: "a",
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			_, d := openLarge(t, largeOptions(), tt.content)
			if !d.Large() {
				t.Fatal("not large")
			}
			if d.Codec != tt.wantCode {
				t.Errorf("codec = %q, want %q", d.Codec, tt.wantCode)
			}
			if d.Encoding() != tt.wantEnc {
				t.Errorf("Encoding() = %q, want %q", d.Encoding(), tt.wantEnc)
			}
			if d.LineEnding() != tt.wantEOL {
				t.Errorf("LineEnding() = %q, want %q", d.LineEnding(), tt.wantEOL)
			}
			if got := d.Buffer.Line(0); got != tt.wantLine {
				t.Errorf("Line(0) = %q, want %q", got, tt.wantLine)
			}
			if d.Ends != nil {
				t.Error("a large document records no per-break endings; its lines keep their own")
			}
		})
	}
}

func TestLargeSampleCutInsideARuneIsUTF8(t *testing.T) {
	t.Parallel()
	// The 64 KiB sample ends in the middle of the two-byte e-acute.
	content := strings.Repeat("x", largeSampleSize-1) + "é\n" + strings.Repeat("y\n", 200)
	opts := largeOptions()
	opts.largeGeom.MaxLine = 0
	opts.largeGeom.Chunk = 0
	_, d := openLarge(t, opts, content)
	if !d.Large() || d.Binary() {
		t.Fatalf("large = %v, binary = %v", d.Large(), d.Binary())
	}
	if d.Encoding() != "UTF-8" {
		t.Fatalf("Encoding() = %q: the label comes from the sample", d.Encoding())
	}
}

func TestLargeNonUTF8NeedsFullLoad(t *testing.T) {
	t.Parallel()
	latin1 := strings.Repeat("caf\xe9 cr\xe8me br\xfbl\xe9e\n", 100)
	tests := []struct {
		Name    string
		content string
	}{
		{Name: "latin-1 text", content: latin1},
		{Name: "invalid bytes at the end of the sample", content: strings.Repeat("abc\n", 16383) + "\xff\xff\xff\xff" + strings.Repeat("def\n", 200)},
		{Name: "utf-16 le with a bom", content: "\xff\xfe" + strings.Repeat("a\x00b\x00\n\x00", 400)},
		{Name: "utf-16 be with a bom", content: "\xfe\xff" + strings.Repeat("\x00a\x00b\x00\n", 400)},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := New(largeOptions())
			path := writeLargeFile(t, "enc.txt", tt.content)
			_, err := e.LoadFunc(path)()
			var need ErrLargeNeedsFullLoad
			if !errors.As(err, &need) {
				t.Fatalf("LoadFunc error = %v, want ErrLargeNeedsFullLoad", err)
			}
			if need.Path != path || need.Size != int64(len(tt.content)) || need.Codec == "" || need.Codec == "utf-8" {
				t.Fatalf("error = %+v", need)
			}
			if want := "(" + formatSize(int64(len(tt.content))) + ") requires full load"; !strings.HasSuffix(need.Error(), want) {
				t.Fatalf("message = %q, want it to end in %q", need.Error(), want)
			}
			if err := e.OpenFile(path); !errors.As(err, &need) {
				t.Fatalf("OpenFile error = %v", err)
			}
			if !e.Empty() && e.Active().Path == path {
				t.Fatal("the file was opened")
			}
		})
	}
}

func TestLargeNonUTF8OverTheLimitIsRefusedAtOnce(t *testing.T) {
	t.Parallel()
	opts := largeOptions()
	opts.fullLoadMax = 2048
	e := New(opts)
	path := writeLargeFile(t, "latin.txt", strings.Repeat("caf\xe9\n", 600))
	_, err := e.LoadFunc(path)()
	var need ErrLargeNeedsFullLoad
	if !errors.Is(err, ErrTooLargeToTranscode) || errors.As(err, &need) {
		t.Fatalf("LoadFunc over the limit = %v, want ErrTooLargeToTranscode and no question", err)
	}
	small := writeLargeFile(t, "small.txt", strings.Repeat("caf\xe9\n", 300))
	if _, err := e.LoadFunc(small)(); !errors.As(err, &need) {
		t.Fatalf("LoadFunc under the limit = %v, want ErrLargeNeedsFullLoad", err)
	}
}

func TestLoadFullFuncLoadsWhole(t *testing.T) {
	t.Parallel()
	content := strings.Repeat("caf\xe9\n", 400)
	e := New(largeOptions())
	path := writeLargeFile(t, "latin.txt", content)
	d, err := e.LoadFullFunc(path)()
	if err != nil {
		t.Fatalf("LoadFullFunc: %v", err)
	}
	if d.Large() || d.Buffer.Large() {
		t.Fatal("a full load is a document in memory")
	}
	if got := d.Buffer.Line(0); got != "café" {
		t.Fatalf("Line(0) = %q", got)
	}
	if d.Codec == "" || d.Codec == "utf-8" {
		t.Fatalf("codec = %q", d.Codec)
	}
	if d.Index().Complete != true {
		t.Fatal("index of a document in memory is complete")
	}
}

func TestLoadFullFuncRefusesOverTheLimit(t *testing.T) {
	t.Parallel()
	opts := largeOptions()
	opts.fullLoadMax = 2048
	e := New(opts)
	path := writeLargeFile(t, "latin.txt", strings.Repeat("caf\xe9\n", 600))
	if _, err := e.LoadFullFunc(path)(); !errors.Is(err, ErrTooLargeToTranscode) {
		t.Fatalf("LoadFullFunc over the limit = %v, want ErrTooLargeToTranscode", err)
	}
	small := writeLargeFile(t, "small.txt", strings.Repeat("caf\xe9\n", 300))
	if _, err := e.LoadFullFunc(small)(); err != nil {
		t.Fatalf("LoadFullFunc under the limit = %v", err)
	}
	if _, err := e.LoadFullFunc("")(); !errors.Is(err, ErrEmptyPath) {
		t.Fatalf("LoadFullFunc of no path = %v", err)
	}
}

func TestLargeBinarySampleOpensAsSummary(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name    string
		content string
	}{
		{Name: "nul in utf-8 text", content: strings.Repeat("text line\n", 200) + "a\x00b\n" + strings.Repeat("x\n", 50)},
		{Name: "control bytes", content: strings.Repeat("abc\x01\x02\n", 300)},
		{Name: "nul in utf-8 with a bom", content: "\ufeff" + strings.Repeat("ab\x00\n", 400)},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e, d := openLarge(t, largeOptions(), tt.content)
			if d.Large() || !d.Binary() || !d.ReadOnly() {
				t.Fatalf("large = %v, binary = %v, read-only = %v", d.Large(), d.Binary(), d.ReadOnly())
			}
			if d.Encoding() != "Binary" {
				t.Errorf("Encoding() = %q", d.Encoding())
			}
			if e.Status() != "Opened big.txt (binary file, read-only)" {
				t.Errorf("status = %q", e.Status())
			}
			if r := e.Do("insert_char", map[string]any{"text": "x"}); !errors.Is(r.Err, ErrReadOnly) {
				t.Errorf("typing into a binary file = %v, want ErrReadOnly", r.Err)
			}
			if _, err := e.PlanSave(); !errors.Is(err, ErrReadOnly) {
				t.Errorf("PlanSave = %v, want ErrReadOnly", err)
			}
		})
	}
}

func TestLargeNulBeyondTheSampleIsNotBinary(t *testing.T) {
	t.Parallel()
	content := strings.Repeat("x", largeSampleSize+100) + "\x00\n"
	opts := largeOptions()
	opts.largeGeom.Chunk = 0
	opts.largeGeom.MaxLine = 0
	_, d := openLarge(t, opts, content)
	if d.Binary() {
		t.Fatal("only the sample decides")
	}
}

func TestLargeOpenStatus(t *testing.T) {
	t.Parallel()
	t.Run("index loading", func(t *testing.T) {
		t.Parallel()
		e, _ := openLarge(t, largeOptions(), lineText(300))
		want := "LARGE: folds, wrap, LSP, auto-save off. " +
			"Large file (2.9 KB): line index loading in the background. " +
			"Syntax is limited to the visible lines; change marks are off."
		if e.Status() != want {
			t.Fatalf("status = %q\nwant     %q", e.Status(), want)
		}
	})
	t.Run("the limits come first and fit the status row", func(t *testing.T) {
		t.Parallel()
		// The status row of a 120 column terminal shows about 40 cells of the message.
		e, _ := openLarge(t, largeOptions(), lineText(300))
		if !strings.HasPrefix(e.Status(), largeLimitsHint) || utf8.RuneCountInString(largeLimitsHint) > 40 {
			t.Fatalf("status = %q: the hint must start it and be at most 40 cells", e.Status())
		}
		for _, off := range []string{"folds", "wrap", "LSP", "auto-save"} {
			if !strings.Contains(largeLimitsHint, off) {
				t.Errorf("the hint does not name %q", off)
			}
		}
	})
	t.Run("index complete at open", func(t *testing.T) {
		t.Parallel()
		opts := largeOptions()
		opts.largeGeom.InitialBytes = 1 << 20
		e, _ := openLarge(t, opts, lineText(150))
		if want := largeLimitsHint + " Large file (1.5 KB): 151 lines. "; !strings.HasPrefix(e.Status(), want) {
			t.Fatalf("status = %q, want prefix %q", e.Status(), want)
		}
	})
	t.Run("leftover temp file", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "big.txt")
		if err := os.WriteFile(path, []byte(lineText(150)), 0o600); err != nil {
			t.Fatal(err)
		}
		temp := filepath.Join(dir, ".big.txt.gvim-123")
		other := filepath.Join(dir, ".other.txt.gvim-9")
		for _, p := range []string{temp, other} {
			if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		e := New(largeOptions())
		if err := e.OpenFile(path); err != nil {
			t.Fatal(err)
		}
		if want := "Leftover temp file " + temp + " from an interrupted save."; !strings.Contains(e.Status(), want) {
			t.Fatalf("status = %q, want it to contain %q", e.Status(), want)
		}
		if strings.Contains(e.Status(), other) {
			t.Fatalf("status = %q names the temp file of another file", e.Status())
		}
	})
}

func TestLargeDocumentHasNoSavedText(t *testing.T) {
	t.Parallel()
	e, d := openLarge(t, largeOptions(), lineText(120))
	if got := d.SavedLines(); got != nil {
		t.Fatalf("SavedLines() = %d lines, want nil", len(got))
	}
	run(t, e, "move_line_end")
	typeText(t, e, "edited")
	if !d.Buffer.Modified() {
		t.Fatal("not modified")
	}
	if got := d.LinesChangedSinceSave(); got != nil {
		t.Fatalf("LinesChangedSinceSave() = %v, want nil", got)
	}
	// A document in memory keeps both.
	small := docWith(Options{}, "a\nb")
	if got := small.Active().SavedLines(); len(got) != 2 {
		t.Fatalf("SavedLines() = %v", got)
	}
}

func TestLoadFuncOpensLargeDocumentOffTheUpdateLoop(t *testing.T) {
	t.Parallel()
	e := New(largeOptions())
	path := writeLargeFile(t, "big.txt", lineText(150))
	d, err := e.LoadFunc(path)()
	if err != nil {
		t.Fatalf("LoadFunc: %v", err)
	}
	if !d.Large() {
		t.Fatal("not large")
	}
	e.OpenLoaded(d)
	if e.Active() != d || !strings.HasPrefix(e.Status(), largeLimitsHint+" Large file (") {
		t.Fatalf("active = %v, status = %q", e.Active() == d, e.Status())
	}
}

func TestOpenPreviewDoesNotLoadALargeFile(t *testing.T) {
	t.Parallel()
	e := New(largeOptions())
	big := writeLargeFile(t, "big.txt", lineText(150))
	small := writeLargeFile(t, "small.txt", "tiny\n")
	if !e.isLargePath(big) || e.isLargePath(small) || e.isLargePath(filepath.Join(t.TempDir(), "missing")) {
		t.Fatal("isLargePath misjudges the files")
	}
	if err := e.OpenPreview(big); err != nil {
		t.Fatalf("OpenPreview: %v", err)
	}
	if e.docs[0].Path == big || len(e.docs) != 1 {
		t.Fatalf("the large file was previewed: %d tabs", len(e.docs))
	}
	if e.Status() != "Large file: open it to load" {
		t.Fatalf("status = %q", e.Status())
	}
	if err := e.OpenPreview(small); err != nil {
		t.Fatal(err)
	}
	if e.Active().Path != small || !e.Active().Preview() {
		t.Fatal("a small file still opens as a preview")
	}
	// A permanent open still works.
	if err := e.OpenFile(big); err != nil || !e.Active().Large() {
		t.Fatalf("OpenFile: %v", err)
	}
}

func TestLargeSaveIsRefusedUntilTheIndexIsBuilt(t *testing.T) {
	t.Parallel()
	opts := largeOptions()
	opts.AutoSaveInterval = 1
	e, d := openLarge(t, opts, lineText(150))
	path := d.Path
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	run(t, e, "move_line_end")
	typeText(t, e, "!")
	if err := e.Save(); !errors.Is(err, ErrIndexing) {
		t.Fatalf("Save = %v, want ErrIndexing", err)
	}
	if jobs := e.PlanAutoSave(time.Now()); len(jobs) != 0 {
		t.Fatalf("auto-save planned %d jobs for a large document", len(jobs))
	}
	if _, err := e.PlanRevert(true); !errors.Is(err, ErrLargeFile) {
		t.Fatalf("PlanRevert = %v, want ErrLargeFile", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("the file changed")
	}
	if !d.Buffer.Modified() {
		t.Fatal("the edit is gone")
	}
}

func TestCheckSourceHook(t *testing.T) {
	t.Parallel()
	t.Run("unchanged", func(t *testing.T) {
		t.Parallel()
		_, d, _ := openMem(t, largeOptions(), lineText(150))
		if err := d.CheckSource(); err != nil || d.ReadOnly() {
			t.Fatalf("CheckSource = %v, read-only = %v", err, d.ReadOnly())
		}
	})
	t.Run("truncated", func(t *testing.T) {
		t.Parallel()
		_, d, src := openMem(t, largeOptions(), lineText(150))
		src.Truncate(100)
		if err := d.CheckSource(); err == nil {
			t.Fatal("CheckSource found no change")
		}
		if !d.ReadOnly() || d.Status != "File changed on disk: reload or Save As after reloading" {
			t.Fatalf("read-only = %v, status = %q", d.ReadOnly(), d.Status)
		}
	})
	t.Run("grown", func(t *testing.T) {
		t.Parallel()
		e, d, src := openMem(t, largeOptions(), lineText(150))
		src.Grow([]byte("appended\n"))
		if err := d.CheckSource(); err == nil {
			t.Fatal("CheckSource found no change")
		}
		if d.ReadOnly() {
			t.Fatal("a file that only grew stays editable")
		}
		if want := "File grew on disk: Save As to another path or reload"; e.Status() != want {
			t.Fatalf("status = %q", e.Status())
		}
	})
	t.Run("not large", func(t *testing.T) {
		t.Parallel()
		if err := docWith(Options{}, "a").Active().CheckSource(); err != nil {
			t.Fatal(err)
		}
	})
}
