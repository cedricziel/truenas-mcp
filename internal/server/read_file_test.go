package server

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func callRead(t *testing.T, target *fakeTarget, path string) *mcp.CallToolResult {
	t.Helper()

	client := discoveryClient(t, discoverySession(t, target), false)
	res, err := client.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "filesystem",
		Arguments: map[string]any{"op": "read", "path": path},
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
	if len(content) != fileByteLimit {
		t.Errorf("content is %d bytes, want the first %d", len(content), fileByteLimit)
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

// Cutting a file at the limit can split a multi-byte character; that is
// still text.
func TestReadFileKeepsTextCutMidCharacter(t *testing.T) {
	target := newFakeTarget(t)
	target.serveDownload(http.StatusOK, strings.Repeat("a", fileByteLimit-1)+"ü")

	res := callRead(t, target, "/etc/motd")
	if res.IsError {
		t.Fatalf("text cut mid-character must still read: %v", res.Content[0].(*mcp.TextContent).Text)
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
