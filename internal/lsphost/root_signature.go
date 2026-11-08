package lsphost

import (
	"context"

	"go.lsp.dev/protocol"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/lsp"
)

// SignatureHelp returns the signature help text at at, as the root's optional
// signature help asks for it. See lsp.Client.SignatureHelp for the text.
func (p *lspPort) SignatureHelp(path string, at buffer.Pos) (string, error) {
	var text string
	err := p.call(path, at, func(ctx context.Context, c *lsp.Client, u string, pos protocol.Position) error {
		var err error
		text, err = c.SignatureHelp(ctx, u, pos)
		return err
	})
	return text, err
}
