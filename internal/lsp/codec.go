package lsp

import (
	"slices"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
)

// lspCodec is the payload codec for every connection in this package. The
// default JSON codec cannot decode a result whose type is a union interface,
// such as DefinitionResult, so payloads go through protocol.Marshal and
// protocol.Unmarshal, which dispatch union values. protocol.NewClient and
// protocol.NewServer install the same codec; they are not used here because
// they also wrap handlers in AsyncHandler, which would change the order in which
// publishDiagnostics is delivered.
type lspCodec struct{}

// Marshal encodes an LSP payload.
func (lspCodec) Marshal(v any) ([]byte, error) {
	return protocol.Marshal(v)
}

// Unmarshal decodes an LSP payload. A *jsonrpc2.RawMessage destination receives
// a copy of the bytes verbatim, as the library's codec does.
func (lspCodec) Unmarshal(data []byte, v any) error {
	if p, ok := v.(*jsonrpc2.RawMessage); ok {
		*p = slices.Clone(data)
		return nil
	}
	return protocol.Unmarshal(data, v)
}
