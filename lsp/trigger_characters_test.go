package lsp

import (
	"testing"

	"go.lsp.dev/protocol"
)

// TestSummarizeCompletionTriggerCharacters checks the static initialize result's
// completion trigger characters reach the capability snapshot the root reads.
func TestSummarizeCompletionTriggerCharacters(t *testing.T) {
	t.Parallel()
	chars := []string{".", "::", "->"}
	caps := summarizeCapabilities(protocol.ServerCapabilities{
		CompletionProvider: &protocol.CompletionOptions{TriggerCharacters: chars},
	})
	if !caps.Completion {
		t.Fatal("completion capability missing")
	}
	if len(caps.CompletionTriggerCharacters) != 3 ||
		caps.CompletionTriggerCharacters[0] != "." ||
		caps.CompletionTriggerCharacters[1] != "::" ||
		caps.CompletionTriggerCharacters[2] != "->" {
		t.Fatalf("trigger characters = %v, want %v", caps.CompletionTriggerCharacters, chars)
	}
	// A provider without trigger characters has none.
	plain := summarizeCapabilities(protocol.ServerCapabilities{
		CompletionProvider: &protocol.CompletionOptions{},
	})
	if len(plain.CompletionTriggerCharacters) != 0 {
		t.Fatalf("plain trigger characters = %v, want none", plain.CompletionTriggerCharacters)
	}
}

// TestRegistrationCompletionTriggerCharacters checks dynamic registrations:
// triggerCharacters replace the set, and a registration without the option keeps
// the static set (Fresh's apply_dynamic_registration only clears on unregister).
func TestRegistrationCompletionTriggerCharacters(t *testing.T) {
	t.Parallel()
	caps := Capabilities{Completion: true, CompletionTriggerCharacters: []string{"."}}
	applyRegistration(&caps, "textDocument/completion", protocol.LSPAny([]byte(`{"triggerCharacters":["::"],"resolveProvider":true}`)))
	if len(caps.CompletionTriggerCharacters) != 1 || caps.CompletionTriggerCharacters[0] != "::" {
		t.Fatalf("dynamic trigger characters = %v, want [::]", caps.CompletionTriggerCharacters)
	}
	if !caps.CompletionResolve {
		t.Fatal("dynamic resolveProvider did not apply")
	}
	applyRegistration(&caps, "textDocument/completion", protocol.LSPAny([]byte(`{"resolveProvider":true}`)))
	if len(caps.CompletionTriggerCharacters) != 1 || caps.CompletionTriggerCharacters[0] != "::" {
		t.Fatalf("a registration without triggerCharacters changed them: %v", caps.CompletionTriggerCharacters)
	}
}
