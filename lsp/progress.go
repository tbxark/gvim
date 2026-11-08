package lsp

import (
	"encoding/json"
	"strconv"

	"go.lsp.dev/protocol"
)

// maxProgress is the number of $/progress entries one client keeps. A server that
// reports more keeps its newest; the editor shows a bounded list.
const maxProgress = 8

// Progress is one server-side progress entry from $/progress: the title the server
// began it with and its latest message and percentage. A finished entry is removed,
// so a listed entry means the work is still going on. The editor shows this as
// text; there is no animation.
type Progress struct {
	Token      string
	Title      string
	Message    string
	Percentage int // -1 when the server gave none
}

// trackProgress folds one $/progress notification into the entry list. begin adds
// an entry (the oldest is dropped past the cap), report updates it, end removes it.
// A report for an unknown token is ignored: the create request would have made the
// entry first.
func (c *Client) trackProgress(params *protocol.ProgressParams) {
	if params == nil {
		return
	}
	var body struct {
		Kind       string  `json:"kind"`
		Title      string  `json:"title"`
		Message    *string `json:"message,omitempty"`
		Percentage *int    `json:"percentage,omitempty"`
	}
	if err := json.Unmarshal(params.Value, &body); err != nil {
		return
	}
	token := progressTokenKey(params.Token)
	if token == "" {
		return
	}

	c.progMu.Lock()
	defer c.progMu.Unlock()
	if c.prog == nil {
		c.prog = make(map[string]progressEntry)
	}
	switch body.Kind {
	case "begin":
		entry := progressEntry{title: body.Title, percentage: -1}
		if body.Message != nil {
			entry.message = *body.Message
		}
		if body.Percentage != nil {
			entry.percentage = *body.Percentage
		}
		if _, exists := c.prog[token]; !exists {
			c.progOrder = append(c.progOrder, token)
		}
		c.prog[token] = entry
		for len(c.progOrder) > maxProgress {
			delete(c.prog, c.progOrder[0])
			c.progOrder = c.progOrder[1:]
		}
	case "report":
		entry, ok := c.prog[token]
		if !ok {
			return
		}
		if body.Message != nil {
			entry.message = *body.Message
		}
		if body.Percentage != nil {
			entry.percentage = *body.Percentage
		}
		c.prog[token] = entry
	case "end":
		delete(c.prog, token)
		c.progOrder = removeString(c.progOrder, token)
	}
}

// progressEntry is one stored progress entry.
type progressEntry struct {
	title      string
	message    string
	percentage int
}

// Progress returns the server's unfinished progress entries, in the order they
// began. The list is a copy.
func (c *Client) Progress() []Progress {
	c.progMu.Lock()
	defer c.progMu.Unlock()
	out := make([]Progress, 0, len(c.progOrder))
	for _, token := range c.progOrder {
		entry, ok := c.prog[token]
		if !ok {
			continue
		}
		out = append(out, Progress{
			Token: token, Title: entry.title, Message: entry.message, Percentage: entry.percentage,
		})
	}
	return out
}

// progressTokenKey renders a protocol progress token as a map key. A token is a
// number or a string.
func progressTokenKey(token protocol.ProgressToken) string {
	switch v := token.(type) {
	case protocol.String:
		return string(v)
	case protocol.Integer:
		return strconv.FormatInt(int64(v), 10)
	}
	return ""
}

// removeString returns s without the first occurrence of v.
func removeString(s []string, v string) []string {
	for i, x := range s {
		if x == v {
			return append(s[:i], s[i+1:]...)
		}
	}
	return s
}
