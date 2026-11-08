package lsp

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf16"

	"go.lsp.dev/protocol"
)

// SignatureHelp asks for the signature of the call at pos in docURI, and returns the
// text an editor shows for it: the label of the active signature, then the label of
// the active parameter as "Parameter: ..." when the server marks one, then the
// documentation after a blank line. It returns "" when the server has no signature.
func (c *Client) SignatureHelp(ctx context.Context, docURI string, pos protocol.Position) (string, error) {
	res, err := c.server.SignatureHelp(ctx, &protocol.SignatureHelpParams{
		TextDocumentPositionParams: positionParams(docURI, pos),
	})
	if err != nil {
		return "", fmt.Errorf("lsp: signature help %s: %w", docURI, err)
	}
	if res == nil || len(res.Signatures) == 0 {
		return "", nil
	}
	idx := 0
	if res.ActiveSignature != nil && int(*res.ActiveSignature) < len(res.Signatures) {
		idx = int(*res.ActiveSignature)
	}
	sig := res.Signatures[idx]
	lines := []string{sig.Label}
	if p := activeParameter(res, sig); p != "" {
		lines = append(lines, "Parameter: "+p)
	}
	if doc := tooltipText(sig.Documentation); doc != "" {
		lines = append(lines, "", doc)
	}
	return strings.Join(lines, "\n"), nil
}

// activeParameter returns the label of the active parameter of sig, or "" when no
// parameter is marked or the label cannot be read. The signature's own marker wins
// over the one of the whole reply. A label given as offsets is cut from the signature
// label, with the offsets in UTF-16 units as the protocol counts them.
func activeParameter(res *protocol.SignatureHelp, sig protocol.SignatureInformation) string {
	n, ok := sig.ActiveParameter.Get()
	if !ok {
		n, ok = res.ActiveParameter.Get()
	}
	if !ok || int(n) >= len(sig.Parameters) {
		return ""
	}
	switch v := sig.Parameters[n].Label.(type) {
	case protocol.String:
		return string(v)
	case protocol.ParameterInformationLabelTuple:
		units := utf16.Encode([]rune(sig.Label))
		start, end := int(v[0]), int(v[1])
		if start > end || end > len(units) {
			return ""
		}
		return string(utf16.Decode(units[start:end]))
	}
	return ""
}

// tooltipText returns the text of a documentation value, plain or markup.
func tooltipText(doc protocol.InlayHintTooltip) string {
	switch v := doc.(type) {
	case protocol.String:
		return string(v)
	case *protocol.MarkupContent:
		if v != nil {
			return v.Value
		}
	}
	return ""
}
