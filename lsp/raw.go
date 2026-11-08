package lsp

import (
	"context"
	"fmt"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
)

// Request sends the request method with params, which is any value that marshals to the
// JSON params of the method, and returns the raw JSON of the result. A null result is the
// bytes "null". It is how a caller reaches a method that has no typed wrapper here. The
// error is the server's error response, a transport error, or ctx.Err().
//
// It goes through protocol.Call, which sends a $/cancelRequest for this request's id when
// ctx is canceled while the request is outstanding (over a detached context, so the
// cancellation still reaches a server that is not answering). Every request of this client
// therefore cancels when its caller gives up: a timeout or a stop/restart cancels the work
// instead of leaving it running on the server.
func (c *Client) Request(ctx context.Context, method string, params any) ([]byte, error) {
	var raw jsonrpc2.RawMessage
	if err := protocol.Call(ctx, c.conn, method, params, &raw); err != nil {
		return nil, fmt.Errorf("lsp: %s: %w", method, err)
	}
	return raw, nil
}
