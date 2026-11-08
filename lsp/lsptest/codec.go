package lsptest

import (
	"slices"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
)

// codec is the payload codec for every connection in this test support package.
// It is the same codec package lsp uses: payloads go through protocol.Marshal
// and protocol.Unmarshal, which dispatch union values, and a
// *jsonrpc2.RawMessage destination receives a copy of the bytes verbatim.
//
// It is duplicated here so that this package imports no production package of
// this module: the package's tests are white-box tests of package lsp and must
// be able to import it.
type codec struct{}

// Marshal encodes an LSP payload.
func (codec) Marshal(v any) ([]byte, error) {
	return protocol.Marshal(v)
}

// Unmarshal decodes an LSP payload.
func (codec) Unmarshal(data []byte, v any) error {
	if p, ok := v.(*jsonrpc2.RawMessage); ok {
		*p = slices.Clone(data)
		return nil
	}
	return protocol.Unmarshal(data, v)
}
