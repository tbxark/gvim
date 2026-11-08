//go:build !unix

package terminal

import (
	"context"
	"errors"
	"testing"
)

func TestStartUnsupportedOnThisPlatform(t *testing.T) {
	s, err := Start(context.Background(), Options{Shell: "cmd.exe"})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Start() error = %v, want ErrUnsupported", err)
	}
	if s != nil {
		t.Fatalf("Start() session = %v, want nil", s)
	}
}
