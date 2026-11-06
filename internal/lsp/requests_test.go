package lsp

import (
	"context"
	"errors"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

const reqDocURI = "file:///work/proj/main.go"

var reqPos = protocol.Position{Line: 7, Character: 12}

// rng builds a range from start and end line and character pairs.
func rng(sl, sc, el, ec uint32) protocol.Range {
	return protocol.Range{
		Start: protocol.Position{Line: sl, Character: sc},
		End:   protocol.Position{Line: el, Character: ec},
	}
}

// nextRequest waits for the next request the fake server recorded and returns
// its params, failing the test when they have another type.
func nextRequest[P any](t *testing.T, s *fakeServer, what string) P {
	t.Helper()
	got := recv(t, s.requests, what)
	p, ok := got.(P)
	if !ok {
		t.Fatalf("%s: server recorded %T", what, got)
	}
	return p
}

// wantDocPosition checks the document and position the server received.
func wantDocPosition(t *testing.T, got protocol.TextDocumentPositionParams) {
	t.Helper()
	if string(got.TextDocument.URI) != reqDocURI || got.Position != reqPos {
		t.Fatalf("server got %q at %+v, want %q at %+v", got.TextDocument.URI, got.Position, reqDocURI, reqPos)
	}
}

// wantRefused checks that err carries the fake server's refusal.
func wantRefused(t *testing.T, err error) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), errRefused.Error()) {
		t.Fatalf("error = %v, want one carrying %q", err, errRefused)
	}
}

func TestDefinition(t *testing.T) {
	single := protocol.Location{URI: "file:///work/proj/lib.go", Range: rng(3, 4, 3, 9)}
	second := protocol.Location{URI: "file:///work/proj/util.go", Range: rng(10, 0, 10, 5)}
	link := protocol.DefinitionLink{
		TargetURI:            "file:///work/proj/link.go",
		TargetRange:          rng(1, 0, 20, 0),
		TargetSelectionRange: rng(2, 5, 2, 8),
	}
	tests := []struct {
		name   string
		answer protocol.DefinitionResult
		fail   bool
		want   []protocol.Location
	}{
		{name: "single location", answer: &single, want: []protocol.Location{single}},
		{name: "location list", answer: protocol.LocationSlice{single, second}, want: []protocol.Location{single, second}},
		{
			name:   "link keeps its selection range",
			answer: protocol.DefinitionLinkSlice{link},
			want:   []protocol.Location{{URI: link.TargetURI, Range: link.TargetSelectionRange}},
		},
		{name: "null result", want: nil},
		{name: "server error", fail: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, Config{}, nil)
			ctx := newTestContext(t)
			f.srv.canned.definition = tt.answer
			f.srv.canned.fail = tt.fail

			got, err := f.client.Definition(ctx, reqDocURI, reqPos)
			wantDocPosition(t, nextRequest[*protocol.DefinitionParams](t, f.srv, "definition params").TextDocumentPositionParams)
			if tt.fail {
				wantRefused(t, err)
				return
			}
			if err != nil {
				t.Fatalf("Definition: %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("Definition = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestImplementation(t *testing.T) {
	impl := protocol.Location{URI: "file:///work/proj/impl.go", Range: rng(12, 5, 12, 11)}
	tests := []struct {
		name   string
		answer protocol.DefinitionResult
		fail   bool
		want   []protocol.Location
	}{
		{name: "single location", answer: &impl, want: []protocol.Location{impl}},
		{name: "location list", answer: protocol.LocationSlice{impl}, want: []protocol.Location{impl}},
		{name: "null result", want: nil},
		{name: "server error", fail: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, Config{}, nil)
			ctx := newTestContext(t)
			f.srv.canned.implementation = tt.answer
			f.srv.canned.fail = tt.fail

			got, err := f.client.Implementation(ctx, reqDocURI, reqPos)
			wantDocPosition(t, nextRequest[*protocol.ImplementationParams](t, f.srv, "implementation params").TextDocumentPositionParams)
			if tt.fail {
				wantRefused(t, err)
				return
			}
			if err != nil {
				t.Fatalf("Implementation: %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("Implementation = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestReferences(t *testing.T) {
	refs := []protocol.Location{
		{URI: reqDocURI, Range: rng(7, 12, 7, 15)},
		{URI: "file:///work/proj/use.go", Range: rng(40, 2, 40, 5)},
	}
	tests := []struct {
		name               string
		includeDeclaration bool
		answer             []protocol.Location
		fail               bool
	}{
		{name: "with declaration", includeDeclaration: true, answer: refs},
		{name: "without declaration", includeDeclaration: false, answer: refs[1:]},
		{name: "server error", includeDeclaration: true, fail: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, Config{}, nil)
			ctx := newTestContext(t)
			f.srv.canned.references = tt.answer
			f.srv.canned.fail = tt.fail

			got, err := f.client.References(ctx, reqDocURI, reqPos, tt.includeDeclaration)
			p := nextRequest[*protocol.ReferenceParams](t, f.srv, "reference params")
			wantDocPosition(t, p.TextDocumentPositionParams)
			if p.Context.IncludeDeclaration != tt.includeDeclaration {
				t.Fatalf("server got includeDeclaration = %v, want %v", p.Context.IncludeDeclaration, tt.includeDeclaration)
			}
			if tt.fail {
				wantRefused(t, err)
				return
			}
			if err != nil {
				t.Fatalf("References: %v", err)
			}
			if !slices.Equal(got, tt.answer) {
				t.Fatalf("References = %+v, want %+v", got, tt.answer)
			}
		})
	}
}

func TestHover(t *testing.T) {
	tests := []struct {
		name   string
		answer *protocol.Hover
		fail   bool
		want   string
	}{
		{
			name: "markdown code fence becomes plain lines",
			answer: &protocol.Hover{Contents: &protocol.MarkupContent{
				Kind:  protocol.MarkupKindMarkdown,
				Value: "```go\nfunc Foo()\n```\n\nFoo does x.",
			}},
			want: "func Foo()\n\nFoo does x.",
		},
		{
			name: "plaintext is returned as written",
			answer: &protocol.Hover{Contents: &protocol.MarkupContent{
				Kind:  protocol.MarkupKindPlainText,
				Value: "```not a fence\nline",
			}},
			want: "```not a fence\nline",
		},
		{
			name:   "plain string",
			answer: &protocol.Hover{Contents: protocol.String("unused variable")},
			want:   "unused variable",
		},
		{
			name: "marked string list joins with blank lines",
			answer: &protocol.Hover{Contents: protocol.MarkedStringSlice{
				protocol.String("first"),
				&protocol.MarkedStringWithLanguage{Language: "go", Value: "func X()"}, //nolint:staticcheck // deprecated type, still a valid hover reply
			}},
			want: "first\n\nfunc X()",
		},
		{name: "no hover", answer: nil, want: ""},
		{name: "server error", fail: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, Config{}, nil)
			ctx := newTestContext(t)
			f.srv.canned.hover = tt.answer
			f.srv.canned.fail = tt.fail

			got, err := f.client.Hover(ctx, reqDocURI, reqPos)
			wantDocPosition(t, nextRequest[*protocol.HoverParams](t, f.srv, "hover params").TextDocumentPositionParams)
			if tt.fail {
				wantRefused(t, err)
				return
			}
			if err != nil {
				t.Fatalf("Hover: %v", err)
			}
			if got != tt.want {
				t.Fatalf("Hover = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCompletion(t *testing.T) {
	textEdit := &protocol.TextEdit{Range: rng(7, 10, 7, 12), NewText: "fmt.Println"}
	insertRange := rng(7, 10, 7, 12)
	replaceRange := rng(7, 10, 7, 15)
	tests := []struct {
		name   string
		answer protocol.CompletionResult
		fail   bool
		want   []CompletionItem
	}{
		{
			name:   "label is the insert text by default",
			answer: protocol.CompletionItemSlice{{Label: "Println", Detail: protocol.NewOptional("func(a ...any)")}},
			want:   []CompletionItem{{Label: "Println", Detail: "func(a ...any)", InsertText: "Println"}},
		},
		{
			name:   "insertText overrides the label",
			answer: protocol.CompletionItemSlice{{Label: "Println", InsertText: protocol.NewOptional("PrintLn")}},
			want:   []CompletionItem{{Label: "Println", InsertText: "PrintLn"}},
		},
		{
			name: "textEdit overrides insertText and keeps its range",
			answer: protocol.CompletionItemSlice{{
				Label:      "fmt",
				InsertText: protocol.NewOptional("ignored"),
				TextEdit:   textEdit,
			}},
			want: []CompletionItem{{Label: "fmt", InsertText: "fmt.Println", EditRange: &textEdit.Range}},
		},
		{
			name: "insertReplaceEdit keeps both ranges",
			answer: protocol.CompletionItemSlice{{
				Label: "fmt",
				TextEdit: &protocol.InsertReplaceEdit{
					NewText: "fmt.Println",
					Insert:  insertRange,
					Replace: replaceRange,
				},
			}},
			want: []CompletionItem{{
				Label:        "fmt",
				InsertText:   "fmt.Println",
				EditRange:    &insertRange,
				ReplaceRange: &replaceRange,
			}},
		},
		{
			name: "completion list items",
			answer: &protocol.CompletionList{IsIncomplete: true, Items: []protocol.CompletionItem{
				{Label: "a"},
				{Label: "b", Detail: protocol.NewOptional("int")},
			}},
			want: []CompletionItem{{Label: "a", InsertText: "a"}, {Label: "b", Detail: "int", InsertText: "b"}},
		},
		{name: "server error", fail: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, Config{}, nil)
			ctx := newTestContext(t)
			f.srv.canned.completion = tt.answer
			f.srv.canned.fail = tt.fail

			got, err := f.client.Completion(ctx, reqDocURI, reqPos)
			wantDocPosition(t, nextRequest[*protocol.CompletionParams](t, f.srv, "completion params").TextDocumentPositionParams)
			if tt.fail {
				wantRefused(t, err)
				return
			}
			if err != nil {
				t.Fatalf("Completion: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Completion = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestRename(t *testing.T) {
	const (
		mainURI = reqDocURI
		libURI  = "file:///work/proj/lib.go"
	)
	mainEdit := protocol.TextEdit{Range: rng(7, 12, 7, 15), NewText: "Total"}
	libEdit := protocol.TextEdit{Range: rng(2, 5, 2, 8), NewText: "Total"}
	libEdit2 := protocol.TextEdit{Range: rng(9, 1, 9, 4), NewText: "Total"}
	annotated := protocol.AnnotatedTextEdit{TextEdit: libEdit2, AnnotationID: "rename"}
	tests := []struct {
		name    string
		edit    *protocol.WorkspaceEdit
		fail    bool
		wantErr bool
		want    map[string][]protocol.TextEdit
	}{
		{
			name: "changes map",
			edit: &protocol.WorkspaceEdit{Changes: map[uri.URI][]protocol.TextEdit{
				mainURI: {mainEdit},
				libURI:  {libEdit, libEdit2},
			}},
			want: map[string][]protocol.TextEdit{
				mainURI: {mainEdit},
				libURI:  {libEdit, libEdit2},
			},
		},
		{
			name: "document changes with plain and annotated edits",
			edit: &protocol.WorkspaceEdit{DocumentChanges: []protocol.DocumentChange{
				&protocol.TextDocumentEdit{
					TextDocument: protocol.OptionalVersionedTextDocumentIdentifier{
						TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: libURI},
					},
					Edits: []protocol.TextDocumentEditElement{&libEdit, &annotated},
				},
			}},
			want: map[string][]protocol.TextEdit{libURI: {libEdit, libEdit2}},
		},
		{
			name: "document changes take precedence over changes",
			edit: &protocol.WorkspaceEdit{
				Changes: map[uri.URI][]protocol.TextEdit{mainURI: {mainEdit}},
				DocumentChanges: []protocol.DocumentChange{
					&protocol.TextDocumentEdit{
						TextDocument: protocol.OptionalVersionedTextDocumentIdentifier{
							TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: mainURI},
						},
						Edits: []protocol.TextDocumentEditElement{&libEdit},
					},
				},
			},
			want: map[string][]protocol.TextEdit{mainURI: {libEdit}},
		},
		{
			name: "file creation cannot be flattened",
			edit: &protocol.WorkspaceEdit{DocumentChanges: []protocol.DocumentChange{
				&protocol.CreateFile{Kind: "create", URI: "file:///work/proj/new.go"},
			}},
			wantErr: true,
		},
		{
			name: "snippet edit is refused",
			edit: &protocol.WorkspaceEdit{DocumentChanges: []protocol.DocumentChange{
				&protocol.TextDocumentEdit{
					TextDocument: protocol.OptionalVersionedTextDocumentIdentifier{
						TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: mainURI},
					},
					Edits: []protocol.TextDocumentEditElement{&protocol.SnippetTextEdit{
						Range:   mainEdit.Range,
						Snippet: protocol.StringValue{Kind: "snippet", Value: "fmt.Println($0)"},
					}},
				},
			}},
			wantErr: true,
		},
		{name: "null result", edit: nil, want: nil},
		{name: "server error", fail: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, Config{}, nil)
			ctx := newTestContext(t)
			f.srv.canned.rename = tt.edit
			f.srv.canned.fail = tt.fail

			got, err := f.client.Rename(ctx, reqDocURI, reqPos, "Total")
			p := nextRequest[*protocol.RenameParams](t, f.srv, "rename params")
			wantDocPosition(t, p.TextDocumentPositionParams)
			if p.NewName != "Total" {
				t.Fatalf("server got newName %q, want %q", p.NewName, "Total")
			}
			if tt.fail {
				wantRefused(t, err)
				return
			}
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Rename = %+v, want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Rename: %v", err)
			}
			if !maps.EqualFunc(got, tt.want, func(a, b []protocol.TextEdit) bool { return slices.Equal(a, b) }) {
				t.Fatalf("Rename = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestCodeActions(t *testing.T) {
	quickfix := protocol.CodeActionKindQuickFix
	reqRange := rng(7, 0, 7, 20)
	tests := []struct {
		name   string
		answer []protocol.CommandOrCodeAction
		fail   bool
		want   []CodeAction
	}{
		{
			name: "code action and bare command",
			answer: []protocol.CommandOrCodeAction{
				&protocol.CodeAction{Title: "Add import", Kind: &quickfix},
				&protocol.Command{Title: "Run tests", Command: "go.test"},
			},
			want: []CodeAction{{Title: "Add import", Kind: "quickfix"}, {Title: "Run tests"}},
		},
		{
			name:   "action without kind",
			answer: []protocol.CommandOrCodeAction{&protocol.CodeAction{Title: "Organize imports"}},
			want:   []CodeAction{{Title: "Organize imports"}},
		},
		{name: "null result", want: nil},
		{name: "server error", fail: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, Config{}, nil)
			ctx := newTestContext(t)
			f.srv.canned.codeActions = tt.answer
			f.srv.canned.fail = tt.fail

			got, err := f.client.CodeActions(ctx, reqDocURI, reqRange)
			p := nextRequest[*protocol.CodeActionParams](t, f.srv, "code action params")
			if string(p.TextDocument.URI) != reqDocURI || p.Range != reqRange {
				t.Fatalf("server got %q range %+v, want %q range %+v", p.TextDocument.URI, p.Range, reqDocURI, reqRange)
			}
			if p.Context.Diagnostics == nil {
				t.Fatal("server got context diagnostics as null, want an empty list")
			}
			if tt.fail {
				wantRefused(t, err)
				return
			}
			if err != nil {
				t.Fatalf("CodeActions: %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("CodeActions = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestRequestsReturnWhenContextEnds parks each request in the fake server, then
// cancels the caller's context. Every request must return promptly with the
// context's error.
func TestRequestsReturnWhenContextEnds(t *testing.T) {
	calls := []struct {
		name string
		run  func(ctx context.Context, c *Client) error
	}{
		{"definition", func(ctx context.Context, c *Client) error {
			_, err := c.Definition(ctx, reqDocURI, reqPos)
			return err
		}},
		{"implementation", func(ctx context.Context, c *Client) error {
			_, err := c.Implementation(ctx, reqDocURI, reqPos)
			return err
		}},
		{"references", func(ctx context.Context, c *Client) error {
			_, err := c.References(ctx, reqDocURI, reqPos, true)
			return err
		}},
		{"hover", func(ctx context.Context, c *Client) error {
			_, err := c.Hover(ctx, reqDocURI, reqPos)
			return err
		}},
		{"completion", func(ctx context.Context, c *Client) error {
			_, err := c.Completion(ctx, reqDocURI, reqPos)
			return err
		}},
		{"rename", func(ctx context.Context, c *Client) error {
			_, err := c.Rename(ctx, reqDocURI, reqPos, "Total")
			return err
		}},
		{"code actions", func(ctx context.Context, c *Client) error {
			_, err := c.CodeActions(ctx, reqDocURI, rng(7, 0, 7, 20))
			return err
		}},
	}
	for _, tt := range calls {
		t.Run(tt.name, func(t *testing.T) {
			stall := make(chan struct{})
			f := newFixture(t, Config{}, stall)
			t.Cleanup(func() { close(stall) })

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			errCh := make(chan error, 1)
			go func() { errCh <- tt.run(ctx, f.client) }()

			recv(t, f.srv.stallEntered, "server entered the request")
			cancel()

			select {
			case err := <-errCh:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("error = %v, want context.Canceled", err)
				}
			case <-time.After(testTimeout):
				t.Fatal("request did not return after its context was canceled")
			}
		})
	}
}
