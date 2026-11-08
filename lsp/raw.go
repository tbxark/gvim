package lsp

import (
	"context"
	"fmt"

	"go.lsp.dev/jsonrpc2"
)

// Request sends the request method with params, which is any value that marshals to the
// JSON params of the method, and returns the raw JSON of the result. A null result is the
// bytes "null". It is how a caller reaches a method that has no typed wrapper here. The
// error is the server's error response, a transport error, or ctx.Err().
func (c *Client) Request(ctx context.Context, method string, params any) ([]byte, error) {
	var raw jsonrpc2.RawMessage
	if _, err := c.conn.Call(ctx, method, params, &raw); err != nil {
		return nil, fmt.Errorf("lsp: %s: %w", method, err)
	}
	return raw, nil
}
