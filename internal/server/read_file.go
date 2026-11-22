package server

import (
	"context"
	"fmt"
	"path"
	"unicode/utf8"

	"github.com/cedricziel/truenas-mcp/internal/truenas"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	fileByteLimit        = 64 * 1024
	defaultFileByteLimit = 1024
)

// FileContent is a UTF-8 page with byte offsets for continuing the read.
type FileContent struct {
	Path       string `json:"path"`
	Content    string `json:"content"`
	Offset     int64  `json:"offset"`
	BytesRead  int    `json:"bytes_read"`
	NextOffset *int64 `json:"next_offset,omitempty"`
	EOF        bool   `json:"eof"`
}

func readFile(ctx context.Context, client *truenas.Client, in DispatchInput) (*mcp.CallToolResult, DispatchOutput, error) {
	offset := int64(0)
	if in.Offset != nil {
		offset = *in.Offset
	}
	limit := in.Limit
	if limit == 0 {
		limit = defaultFileByteLimit
	}
	if offset < 0 || limit < utf8.UTFMax || limit > fileByteLimit {
		return fileReadError("offset must be nonnegative and limit must be between 4 and 65536 bytes (default 1024)")
	}

	// Look ahead far enough to distinguish a split UTF-8 rune from invalid text.
	got, err := client.DownloadRange(ctx, "filesystem.get", []any{in.Path}, path.Base(in.Path), offset, int64(limit+utf8.UTFMax-1))
	if err != nil {
		return nil, DispatchOutput{}, err
	}
	if offset > 0 && len(got.Data) > 0 && !utf8.RuneStart(got.Data[0]) {
		return fileReadError("offset falls inside a UTF-8 character; use the previous page's next_offset")
	}
	data, ok := textPage(got.Data, limit, got.Truncated)
	if !ok {
		return fileReadError(fmt.Sprintf("%s looks binary; only text files can be read", in.Path))
	}
	more := got.Truncated || len(data) < len(got.Data)
	page := FileContent{Path: in.Path, Content: string(data), Offset: offset, BytesRead: len(data), EOF: !more}
	if more {
		next := offset + int64(len(data))
		page.NextOffset = &next
	}
	return nil, DispatchOutput{Op: in.Op, Truncated: more, Result: page}, nil
}

func fileReadError(message string) (*mcp.CallToolResult, DispatchOutput, error) {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: message}}}, DispatchOutput{}, nil
}

func textPage(data []byte, limit int, truncated bool) ([]byte, bool) {
	end := 0
	for pos := 0; pos < len(data); {
		if data[pos] == 0 {
			return nil, false
		}
		r, size := utf8.DecodeRune(data[pos:])
		if r == utf8.RuneError && size == 1 {
			if truncated && !utf8.FullRune(data[pos:]) {
				break
			}
			return nil, false
		}
		pos += size
		if pos <= limit {
			end = pos
		}
	}
	return data[:end], true
}
