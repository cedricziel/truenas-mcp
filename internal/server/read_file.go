package server

import (
	"context"
	"fmt"
	"path"
	"unicode/utf8"

	"github.com/cedricziel/truenas-mcp/internal/truenas"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const fileByteLimit = 64 * 1024

// FileContent is one file's text as filesystem.read returns it.
type FileContent struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func readFile(ctx context.Context, client *truenas.Client, in DispatchInput) (*mcp.CallToolResult, DispatchOutput, error) {
	got, err := client.Download(ctx, "filesystem.get", []any{in.Path}, path.Base(in.Path), fileByteLimit)
	if err != nil {
		return nil, DispatchOutput{}, err
	}

	if !isText(got.Data, got.Truncated) {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{
				Text: fmt.Sprintf("%s looks binary; only text files can be read", in.Path),
			}},
		}, DispatchOutput{}, nil
	}

	return nil, DispatchOutput{
		Op:        in.Op,
		Truncated: got.Truncated,
		Result:    FileContent{Path: in.Path, Content: string(got.Data)},
	}, nil
}

// isText reports whether data is UTF-8 without NUL bytes. A cut-off file may
// end partway through a character, so up to utf8.UTFMax-1 trailing bytes
// are forgiven when truncated.
func isText(data []byte, truncated bool) bool {
	for _, b := range data {
		if b == 0 {
			return false
		}
	}
	if utf8.Valid(data) {
		return true
	}
	if !truncated {
		return false
	}
	for cut := 1; cut < utf8.UTFMax && cut <= len(data); cut++ {
		if utf8.Valid(data[:len(data)-cut]) {
			return true
		}
	}
	return false
}
