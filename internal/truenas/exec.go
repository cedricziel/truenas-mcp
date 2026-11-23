package truenas

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// ExecResult contains combined terminal output, not separate stdout/stderr.
// A nil exit code means completion was not observed. Disconnecting a terminal
// cannot guarantee that processes spawned by the command have stopped.
type ExecResult struct {
	App        string `json:"app"`
	Container  string `json:"container"`
	Output     string `json:"output"`
	ExitCode   *int   `json:"exit_code,omitempty"`
	Incomplete bool   `json:"incomplete"`
	Truncated  bool   `json:"truncated"`
	Reason     string `json:"reason,omitempty"`
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

// ContainerExec opens a dedicated terminal under this client's credential.
func (c *Client) ContainerExec(ctx context.Context, app, container, command string, timeout time.Duration, limit int) (ExecResult, error) {
	result := ExecResult{App: app, Container: container}
	if strings.TrimSpace(app) == "" || strings.TrimSpace(container) == "" || strings.TrimSpace(command) == "" || strings.ContainsRune(command, 0) || len(shellQuote(command)) > 3500 || timeout <= 0 || timeout > 120*time.Second || limit < 1 || limit > 65536 {
		return result, fmt.Errorf("app, container and command are required; shell-quoted command must be <=3500 bytes without NUL; timeout 1–120 seconds; output limit 1–65536 bytes")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	raw, err := c.Call(ctx, "auth.generate_token", 60, map[string]any{}, true)
	if err != nil {
		return result, err
	}
	var token string
	if json.Unmarshal(raw, &token) != nil || token == "" {
		return result, fmt.Errorf("could not obtain shell token")
	}
	endpoint := *c.httpBase
	if endpoint.Scheme == "https" {
		endpoint.Scheme = "wss"
	} else {
		endpoint.Scheme = "ws"
	}
	endpoint.Path = "/websocket/shell"
	var tlsConfig *tls.Config
	if transport, ok := c.http.Transport.(*http.Transport); ok {
		tlsConfig = transport.TLSClientConfig
	}
	dialer := websocket.Dialer{TLSClientConfig: tlsConfig, HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.DialContext(ctx, endpoint.String(), nil)
	if err != nil {
		return result, fmt.Errorf("connecting to container shell failed")
	}
	defer func() { _ = conn.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	conn.SetReadLimit(128 * 1024)
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetWriteDeadline(deadline)
	}
	if err = conn.WriteJSON(map[string]any{"token": token, "options": map[string]any{"app_name": app, "container_id": container, "command": "/bin/sh"}}); err != nil {
		return result, fmt.Errorf("sending shell authentication failed")
	}
	_, data, err := conn.ReadMessage()
	if err != nil {
		return result, fmt.Errorf("shell authentication interrupted")
	}
	var greeting struct {
		Msg string `json:"msg"`
	}
	if json.Unmarshal(data, &greeting) != nil || greeting.Msg != "connected" {
		return result, fmt.Errorf("container shell rejected: check Web Shell Access, app permissions and container membership")
	}
	nonce := make([]byte, 16)
	if _, err = rand.Read(nonce); err != nil {
		return result, err
	}
	marker := hex.EncodeToString(nonce)
	start := marker + "START"
	end := marker + "END:"
	// Split marker literals so echoed terminal input cannot match the framing.
	prefix := shellQuote(marker[:16]) + shellQuote(marker[16:])
	script := "stty -echo; printf '%sSTART' " + prefix + "; /bin/sh -c " + shellQuote(command) + "; __mcp_status=$?; printf '%sEND:%s\\n' " + prefix + " \"$__mcp_status\"; exit\n"
	if err = conn.WriteMessage(websocket.BinaryMessage, []byte(script)); err != nil {
		return result, fmt.Errorf("sending container command failed")
	}
	var output strings.Builder
	started := false
	for {
		_, reader, readErr := conn.NextReader()
		if readErr != nil {
			result.Incomplete = true
			result.Reason = "shell disconnected before completion; remote processes may still be running"
			if ctx.Err() != nil {
				result.Reason = "deadline or cancellation reached; remote processes may still be running"
			}
			result.Output = boundedExecOutput(output.String(), limit, &result)
			return result, nil
		}
		// Stream frames so a large output frame does not require unbounded memory.
		buf := make([]byte, 4096)
		for {
			n, e := reader.Read(buf)
			output.Write(buf[:n])
			text := output.String()
			if !started {
				if i := strings.Index(text, start); i >= 0 {
					text = text[i+len(start):]
					output.Reset()
					output.WriteString(text)
					started = true
				} else if output.Len() > len(start) {
					tail := text[len(text)-len(start):]
					output.Reset()
					output.WriteString(tail)
				}
			}
			if started {
				text = output.String()
				if i := strings.Index(text, end); i >= 0 {
					if j := strings.IndexByte(text[i+len(end):], '\n'); j >= 0 {
						code, parseErr := strconv.Atoi(strings.TrimSpace(text[i+len(end) : i+len(end)+j]))
						if parseErr == nil && code >= 0 && code <= 255 {
							if i > limit {
								result.Output = strings.ToValidUTF8(text[:limit], "?")
								result.Truncated = true
							} else {
								result.Output = strings.ToValidUTF8(text[:i], "?")
							}
							result.ExitCode = &code
							return result, nil
						}
					}
				}
				if output.Len() > limit+len(end)+16 {
					result.Output = strings.ToValidUTF8(text[:limit], "?")
					result.Truncated = true
					result.Incomplete = true
					result.Reason = "output limit reached; remote processes may still be running"
					return result, nil
				}
			}
			if e == io.EOF {
				break
			}
			if e != nil {
				result.Output = boundedExecOutput(output.String(), limit, &result)
				result.Incomplete = true
				result.Reason = "shell output interrupted"
				return result, nil
			}
		}
	}
}

func boundedExecOutput(s string, limit int, result *ExecResult) string {
	if len(s) > limit {
		s = s[:limit]
		result.Truncated = true
	}
	return strings.ToValidUTF8(s, "?")
}
