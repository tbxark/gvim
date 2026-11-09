package lsphost

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/config"
	"go.lsp.dev/uri"
)

// TestLocationFileExternalProvider checks the scheme split and the app seam: file URIs
// stay direct, slang-synth goes through the opener with the answering server's launch
// context and is marked read-only, and a bad or missing provider is an explicit error
// rather than a guessed path.
func TestLocationFileExternalProvider(t *testing.T) {
	port := NewFor(config.Defaults()).(*lspPort)
	generated := filepath.Join(t.TempDir(), "core.slang")
	if err := os.WriteFile(generated, []byte("float3 x;\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	port.SetExternalLocationOpener(func(_ context.Context, req ExternalLocationRequest) (string, error) {
		if req.URI != "slang-synth://core/core.builtin" {
			t.Errorf("opener URI = %q", req.URI)
		}
		if req.Program != "/opt/slangd" || req.Root != "/work/proj" || len(req.Env) != 1 || req.Env[0] != "PATH=/server/bin" {
			t.Errorf("opener launch context = %+v", req)
		}
		return generated, nil
	})
	target := ExternalLocationRequest{
		URI: "slang-synth://core/core.builtin", Root: "/work/proj", Program: "/opt/slangd", Env: []string{"PATH=/server/bin"},
	}
	file, readOnly, err := port.locationFile(context.Background(), target.URI, target)
	if err != nil || file != generated || !readOnly {
		t.Fatalf("external = %q readOnly=%v err=%v, want generated read-only", file, readOnly, err)
	}
	fileURI := uri.File(generated).String()
	file, readOnly, err = port.locationFile(context.Background(), fileURI, ExternalLocationRequest{})
	if err != nil || file != generated || readOnly {
		t.Fatalf("file URI = %q readOnly=%v err=%v", file, readOnly, err)
	}
	// A relative provider result is refused.
	port.SetExternalLocationOpener(func(context.Context, ExternalLocationRequest) (string, error) {
		return "relative.slang", nil
	})
	if _, _, err := port.locationFile(context.Background(), "slang-synth://core/core", ExternalLocationRequest{}); err == nil {
		t.Fatal("a relative provider path was accepted")
	}
	// Provider error is propagated with the URI.
	port.SetExternalLocationOpener(func(context.Context, ExternalLocationRequest) (string, error) {
		return "", errors.New("gate refused")
	})
	if _, _, err := port.locationFile(context.Background(), "slang-synth://core/core", ExternalLocationRequest{}); err == nil || !strings.Contains(err.Error(), "gate refused") {
		t.Fatalf("provider error = %v", err)
	}
	// No provider is explicit.
	port.SetExternalLocationOpener(nil)
	if _, _, err := port.locationFile(context.Background(), "slang-synth://core/core", ExternalLocationRequest{}); err == nil || !strings.Contains(err.Error(), "no URI provider") {
		t.Fatalf("missing provider error = %v", err)
	}
}
