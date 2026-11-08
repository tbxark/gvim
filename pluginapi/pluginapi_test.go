package pluginapi

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestErrorTextAndCode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		err      error
		wantText string
		wantCode Code
	}{
		{"read only", Errf(CodeReadOnly, "buffer %q is read-only", "a.go"), `read_only: buffer "a.go" is read-only`, CodeReadOnly},
		{"no args", Errf(CodeStale, "version changed"), "stale: version changed", CodeStale},
		{"wrapped", fmt.Errorf("edit: %w", Errf(CodeClosed, "gone")), "edit: closed: gone", CodeClosed},
		{"plain error is io", errors.New("disk full"), "disk full", CodeIO},
		{"direct struct", &Error{Code: CodeLimit, Msg: "too many"}, "limit: too many", CodeLimit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.err.Error(); got != tt.wantText {
				t.Fatalf("Error() = %q, want %q", got, tt.wantText)
			}
			if got := CodeOf(tt.err); got != tt.wantCode {
				t.Fatalf("CodeOf = %q, want %q", got, tt.wantCode)
			}
		})
	}
}

func TestVersionTokenFormat(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		v    Version
		want string
	}{
		{"zero", Version{}, "0.0"},
		{"fresh buffer", Version{Gen: 1}, "1.0"},
		{"edited", Version{Gen: 3, Ver: 42}, "3.42"},
		{"large", Version{Gen: 1<<64 - 1, Ver: 1<<64 - 1}, "18446744073709551615.18446744073709551615"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.v.String(); got != tt.want {
				t.Fatalf("String() = %q, want %q", got, tt.want)
			}
			back, ok := ParseVersion(tt.want)
			if !ok || back != tt.v {
				t.Fatalf("ParseVersion(%q) = %+v, %v; want %+v, true", tt.want, back, ok, tt.v)
			}
		})
	}
}

func TestParseVersionRejectsMalformedTokens(t *testing.T) {
	t.Parallel()
	for _, s := range []string{
		"", "1", ".", "1.", ".1", "1.2.3", "a.b", "1.-2", "+1.2", " 1.2", "1.2 ", "01.2", "1.02",
		"18446744073709551616.1", "1.18446744073709551616", "0x1.2", "1,2", "1_0.2",
	} {
		if v, ok := ParseVersion(s); ok {
			t.Errorf("ParseVersion(%q) = %+v, true; want false", s, v)
		}
	}
}

func TestVersionIsZeroAndEquality(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		v    Version
		zero bool
	}{
		{"zero value", Version{}, true},
		{"gen only", Version{Gen: 1}, false},
		{"ver only", Version{Ver: 1}, false},
	}
	for _, tt := range tests {
		if tt.v.IsZero() != tt.zero {
			t.Errorf("%s: IsZero = %v, want %v", tt.name, tt.v.IsZero(), tt.zero)
		}
	}
	// Equal Versions mean equal text; a different Gen never equals an older one.
	if (Version{Gen: 1, Ver: 5}) == (Version{Gen: 2, Ver: 5}) {
		t.Fatal("tokens of different generations compare equal")
	}
}

// TestUnsupportedRefusesEveryHostMethod calls every method of Host on Unsupported
// with zero arguments. A method that returns an error must return CodeUnsupported;
// no method may panic. The method count guards against the interface shrinking
// without anyone noticing.
func TestUnsupportedRefusesEveryHostMethod(t *testing.T) {
	t.Parallel()
	errorType := reflect.TypeFor[error]()
	hostType := reflect.TypeFor[Host]()
	if hostType.NumMethod() < 80 {
		t.Fatalf("Host has %d methods, want at least 80", hostType.NumMethod())
	}
	var h Host = Unsupported{}
	recv := reflect.ValueOf(h)
	withError := 0
	for i := range hostType.NumMethod() {
		m := hostType.Method(i)
		t.Run(m.Name, func(t *testing.T) {
			args := make([]reflect.Value, m.Type.NumIn())
			for j := range args {
				args[j] = reflect.Zero(m.Type.In(j))
			}
			out := recv.MethodByName(m.Name).Call(args)
			if len(out) == 0 || m.Type.Out(len(out)-1) != errorType {
				return
			}
			withError++
			err, _ := out[len(out)-1].Interface().(error)
			if err == nil {
				t.Fatalf("%s returned a nil error", m.Name)
			}
			if got := CodeOf(err); got != CodeUnsupported {
				t.Fatalf("%s: code = %q (%v), want %q", m.Name, got, err, CodeUnsupported)
			}
		})
	}
	if withError < 40 {
		t.Fatalf("only %d Host methods return an error, want at least 40", withError)
	}
}

func TestUnsupportedZeroValues(t *testing.T) {
	t.Parallel()
	var h Host = Unsupported{}
	if got := h.ListBuffers(); got != nil {
		t.Errorf("ListBuffers = %v, want nil", got)
	}
	if _, ok := h.Buffer(1); ok {
		t.Error("Buffer reported a buffer")
	}
	if h.ActiveBuffer() != 0 || h.FindBuffer("x") != 0 || h.CurrentWindow() != 0 {
		t.Error("ids are not zero")
	}
	if w, hh := h.ScreenSize(); w != 0 || hh != 0 {
		t.Errorf("ScreenSize = %d, %d, want 0, 0", w, hh)
	}
	if res, err := h.FindLines(1, FindLinesQuery{}); err == nil || res.Next != -1 {
		t.Errorf("FindLines = %+v, %v; want Next -1 and an error", res, err)
	}
	if h.Holds("read_only") || h.KeyLabel("save") != "" || h.Actions() != nil {
		t.Error("CommandHost zero values are not zero")
	}
	if _, _, _, ok := h.ThemeColor("x"); ok {
		t.Error("ThemeColor reported a colour")
	}
	if _, ok := h.Config("tab_size"); ok {
		t.Error("Config reported a value")
	}
}

// TestPackageImportsStdlibOnly is import rule R9 as a unit test: the contract imports no
// package of this module and no third-party package.
func TestPackageImportsStdlibOnly(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("glob: %v, %d files", err, len(files))
	}
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		af, err := parser.ParseFile(fset, f, src, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range af.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			first, _, _ := strings.Cut(p, "/")
			if strings.Contains(first, ".") {
				t.Errorf("%s imports %q: only the standard library is allowed", f, p)
			}
		}
	}
}
