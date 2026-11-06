package lsp

import (
	"testing"

	"go.lsp.dev/protocol"
)

// withActive returns sig with the parameter at index n marked as the active one.
func withActive(sig protocol.SignatureInformation, n uint32) protocol.SignatureInformation {
	sig.ActiveParameter = protocol.NewNullable[uint32](n)
	return sig
}

func TestSignatureHelp(t *testing.T) {
	printf := protocol.SignatureInformation{
		Label: "Printf(format string, a ...any)",
		Parameters: []protocol.ParameterInformation{
			{Label: protocol.String("format string")},
			{Label: protocol.String("a ...any")},
		},
	}
	tests := []struct {
		name   string
		answer *protocol.SignatureHelp
		fail   bool
		want   string
	}{
		{
			name:   "label and the string label of the active parameter",
			answer: &protocol.SignatureHelp{Signatures: []protocol.SignatureInformation{withActive(printf, 0)}, ActiveSignature: new(uint32(0))},
			want:   "Printf(format string, a ...any)\nParameter: format string",
		},
		{
			name: "offsets into the label name the active parameter",
			answer: &protocol.SignatureHelp{Signatures: []protocol.SignatureInformation{{
				Label:           "f(a int, b int)",
				Parameters:      []protocol.ParameterInformation{{Label: protocol.ParameterInformationLabelTuple{2, 7}}},
				ActiveParameter: protocol.NewNullable[uint32](0),
			}}},
			want: "f(a int, b int)\nParameter: a int",
		},
		{
			name: "the active parameter of the signature wins over the help's",
			answer: &protocol.SignatureHelp{
				Signatures: []protocol.SignatureInformation{{
					Label:           printf.Label,
					Parameters:      printf.Parameters,
					ActiveParameter: protocol.NewNullable[uint32](1),
				}},
				ActiveParameter: protocol.NewNullable[uint32](0),
			},
			want: "Printf(format string, a ...any)\nParameter: a ...any",
		},
		{
			name: "the active signature is the one the server marks",
			answer: &protocol.SignatureHelp{
				Signatures:      []protocol.SignatureInformation{{Label: "first()"}, {Label: "second()"}},
				ActiveSignature: new(uint32(1)),
			},
			want: "second()",
		},
		{
			name: "markup documentation follows the label after a blank line",
			answer: &protocol.SignatureHelp{Signatures: []protocol.SignatureInformation{{
				Label:         "Abs(x int)",
				Documentation: &protocol.MarkupContent{Kind: protocol.MarkupKindMarkdown, Value: "Abs returns |x|."},
			}}},
			want: "Abs(x int)\n\nAbs returns |x|.",
		},
		{name: "no signature", answer: &protocol.SignatureHelp{}, want: ""},
		{name: "null reply", answer: nil, want: ""},
		{name: "server error", fail: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, Config{}, nil)
			ctx := newTestContext(t)
			f.srv.canned.signature = tt.answer
			f.srv.canned.fail = tt.fail

			got, err := f.client.SignatureHelp(ctx, reqDocURI, reqPos)
			wantDocPosition(t, nextRequest[*protocol.SignatureHelpParams](t, f.srv, "signature help params").TextDocumentPositionParams)
			if tt.fail {
				wantRefused(t, err)
				return
			}
			if err != nil {
				t.Fatalf("SignatureHelp: %v", err)
			}
			if got != tt.want {
				t.Fatalf("SignatureHelp = %q, want %q", got, tt.want)
			}
		})
	}
}
