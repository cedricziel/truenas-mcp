package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func callRead(t *testing.T, target *fakeTarget, path string) *mcp.CallToolResult {
	t.Helper()
	return callReadPage(t, target, map[string]any{"op": "read", "path": path})
}

func callReadPage(t *testing.T, target *fakeTarget, args map[string]any) *mcp.CallToolResult {
	t.Helper()

	client := discoveryClient(t, discoverySession(t, target), false)
	res, err := client.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "filesystem",
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("call filesystem: %v", err)
	}
	return res
}

func TestReadFileReturnsItsText(t *testing.T) {
	target := newFakeTarget(t)
	target.serveDownload(http.StatusOK, "hive\n")

	res := callRead(t, target, "/etc/hostname")
	if res.IsError {
		t.Fatalf("read failed: %v", res.Content[0].(*mcp.TextContent).Text)
	}
	out := res.StructuredContent.(map[string]any)
	result := out["result"].(map[string]any)
	if result["content"] != "hive\n" || result["path"] != "/etc/hostname" {
		t.Errorf("result = %#v", result)
	}
	if out["truncated"] == true {
		t.Error("a file under the limit must not read as truncated")
	}

	params, _ := target.lastParams("core.download")
	if !strings.Contains(string(params), `"filesystem.get",["/etc/hostname"]`) {
		t.Errorf("core.download params = %s, want filesystem.get on the path", params)
	}
}

func TestReadFileCutsALargeFileAndSaysSo(t *testing.T) {
	target := newFakeTarget(t)
	target.serveDownload(http.StatusOK, strings.Repeat("a", fileByteLimit+100))

	out := callRead(t, target, "/var/log/big.log").StructuredContent.(map[string]any)
	content, _ := out["result"].(map[string]any)["content"].(string)
	if len(content) != 1024 {
		t.Errorf("content is %d bytes, want the first %d", len(content), 1024)
	}
	if out["truncated"] != true {
		t.Error("a cut-off file must say it was cut off")
	}
}

// Binary content mangled into a string reads as text but is not, so the
// read is refused instead.
func TestReadFileRefusesBinary(t *testing.T) {
	target := newFakeTarget(t)
	target.serveDownload(http.StatusOK, "\x7fELF\x02\x01\x01\x00\xff\xfe")

	res := callRead(t, target, "/usr/bin/true")
	if !res.IsError || !strings.Contains(res.Content[0].(*mcp.TextContent).Text, "binary") {
		t.Fatalf("want a binary refusal, got %#v", res.Content)
	}
}

func TestReadFileKeepsTextCutMidCharacter(t *testing.T) {
	target := newFakeTarget(t)
	target.serveDownload(http.StatusOK, strings.Repeat("a", fileByteLimit-1)+"ü")

	res := callReadPage(t, target, map[string]any{"op": "read", "path": "/etc/motd", "limit": 65536})
	if res.IsError {
		t.Fatalf("text cut mid-character must still read: %v", res.Content[0].(*mcp.TextContent).Text)
	}
	page := res.StructuredContent.(map[string]any)["result"].(map[string]any)
	if page["content"] != strings.Repeat("a", 65535) || page["next_offset"] != float64(65535) {
		t.Fatalf("page split a character: %#v", page)
	}
}

func TestReadFileReportsTheTargetsReason(t *testing.T) {
	target := newFakeTarget(t)
	target.serveDownload(http.StatusUnprocessableEntity, "[EFAULT] /mnt/tank is not a file")

	res := callRead(t, target, "/mnt/tank")
	if !res.IsError || !strings.Contains(res.Content[0].(*mcp.TextContent).Text, "is not a file") {
		t.Fatalf("want the target's reason, got %#v", res.Content)
	}
}

func TestReadFileContinuesBeyond64KiB(t *testing.T) {
	target := newFakeTarget(t)
	target.serveDownload(http.StatusOK, strings.Repeat("a", 65536)+"tail")
	first := callReadPage(t, target, map[string]any{"op": "read", "path": "/big", "limit": 65536})
	out := first.StructuredContent.(map[string]any)
	page := out["result"].(map[string]any)
	if page["bytes_read"] != float64(65536) || page["next_offset"] != float64(65536) || page["eof"] != false || out["truncated"] != true {
		t.Fatalf("first page = %#v", out)
	}
	second := callReadPage(t, target, map[string]any{"op": "read", "path": "/big", "offset": page["next_offset"], "limit": 4})
	out = second.StructuredContent.(map[string]any)
	page = out["result"].(map[string]any)
	if page["content"] != "tail" || page["offset"] != float64(65536) || page["bytes_read"] != float64(4) || page["eof"] != true || page["next_offset"] != nil || out["truncated"] == true {
		t.Fatalf("last page = %#v", out)
	}
}

func TestReadFileUTF8PagesPreserveCharacters(t *testing.T) {
	target := newFakeTarget(t)
	target.serveDownload(http.StatusOK, "abcü🙂z")
	first := callReadPage(t, target, map[string]any{"op": "read", "path": "/text", "limit": 4})
	page := first.StructuredContent.(map[string]any)["result"].(map[string]any)
	if page["content"] != "abc" || page["next_offset"] != float64(3) || !utf8.ValidString(page["content"].(string)) {
		t.Fatalf("first page = %#v", page)
	}
	second := callReadPage(t, target, map[string]any{"op": "read", "path": "/text", "offset": 3, "limit": 4})
	page = second.StructuredContent.(map[string]any)["result"].(map[string]any)
	if page["content"] != "ü" || page["next_offset"] != float64(5) {
		t.Fatalf("second page = %#v", page)
	}
	third := callReadPage(t, target, map[string]any{"op": "read", "path": "/text", "offset": 5, "limit": 4})
	page = third.StructuredContent.(map[string]any)["result"].(map[string]any)
	if page["content"] != "🙂" || page["next_offset"] != float64(9) {
		t.Fatalf("third page = %#v", page)
	}
}

func TestReadFileRejectsInvalidPagination(t *testing.T) {
	for _, args := range []map[string]any{
		{"offset": -1}, {"limit": -1}, {"limit": 3}, {"limit": 65537}, {"offset": 4, "limit": 4},
	} {
		t.Run(fmt.Sprint(args), func(t *testing.T) {
			target := newFakeTarget(t)
			target.serveDownload(http.StatusOK, "abcütext")
			args["op"] = "read"
			args["path"] = "/text"
			result := callReadPage(t, target, args)
			if !result.IsError {
				t.Fatalf("wanted pagination error, got %#v", result.StructuredContent)
			}
		})
	}
}

func TestReadFileRejectsInvalidUTF8AtPageBoundary(t *testing.T) {
	target := newFakeTarget(t)
	target.serveDownload(http.StatusOK, "abc\xffmore text")
	result := callReadPage(t, target, map[string]any{"op": "read", "path": "/binary", "limit": 4})
	if !result.IsError {
		t.Fatalf("wanted binary refusal, got %#v", result.StructuredContent)
	}
}

func TestReadFileBeyondEOFReturnsEmptyFinalPage(t *testing.T) {
	target := newFakeTarget(t)
	target.serveDownload(http.StatusOK, "text")
	result := callReadPage(t, target, map[string]any{"op": "read", "path": "/text", "offset": 100})
	page := result.StructuredContent.(map[string]any)["result"].(map[string]any)
	if page["content"] != "" || page["offset"] != float64(100) || page["bytes_read"] != float64(0) || page["eof"] != true || page["next_offset"] != nil {
		t.Fatalf("page = %#v", page)
	}
}
