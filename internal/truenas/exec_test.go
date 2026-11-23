package truenas

import (
	"context"
	"encoding/json"
	"github.com/gorilla/websocket"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestContainerExecProtocol(t *testing.T) {
	for _, scenario := range []string{"complete", "denied", "overflow", "disconnect", "cancel", "real-shell"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFakeMiddleware(t)
			f.handle("auth.generate_token", func(params json.RawMessage) (any, *rpcError) {
				if string(params) != `[60,{},true]` {
					t.Errorf("token params: %s", params)
				}
				return "private-token", nil
			})
			f.downloads = map[string]http.HandlerFunc{"/websocket/shell": func(w http.ResponseWriter, r *http.Request) {
				conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer conn.Close()
				var greeting struct {
					Token   string            `json:"token"`
					Options map[string]string `json:"options"`
				}
				if conn.ReadJSON(&greeting) != nil {
					return
				}
				if greeting.Token != "private-token" || greeting.Options["app_name"] != "app" || greeting.Options["container_id"] != "container" || greeting.Options["command"] != "/bin/sh" {
					t.Errorf("bad greeting")
				}
				if scenario == "denied" {
					_ = conn.WriteJSON(map[string]string{"msg": "failed"})
					return
				}
				_ = conn.WriteJSON(map[string]string{"msg": "connected"})
				_, script, err := conn.ReadMessage()
				if err != nil {
					return
				}
				parts := regexp.MustCompile(`'([a-f0-9]{16})''([a-f0-9]{16})'`).FindStringSubmatch(string(script))
				if len(parts) != 3 {
					t.Errorf("missing framing")
					return
				}
				marker := parts[1] + parts[2]
				// Echo the input exactly as a PTY may do, then fragment real framing.
				_ = conn.WriteMessage(websocket.BinaryMessage, script)
				if scenario != "real-shell" {
					_ = conn.WriteMessage(websocket.BinaryMessage, []byte(marker[:10]))
					_ = conn.WriteMessage(websocket.BinaryMessage, []byte(marker[10:]+"START"))
				}
				switch scenario {
				case "real-shell":
					out, runErr := exec.Command("/bin/sh", "-c", string(script)).CombinedOutput()
					if runErr != nil {
						t.Errorf("shell: %v", runErr)
					}
					_ = conn.WriteMessage(websocket.BinaryMessage, out)
				case "complete":
					_ = conn.WriteMessage(websocket.BinaryMessage, []byte("hello\r\n"+marker+"END:"))
					_ = conn.WriteMessage(websocket.BinaryMessage, []byte("7\r\n"))
				case "overflow":
					_ = conn.WriteMessage(websocket.BinaryMessage, []byte(strings.Repeat("x", 10000)))
				case "disconnect":
					_ = conn.WriteMessage(websocket.BinaryMessage, []byte("partial"))
				case "cancel":
					_, _, _ = conn.ReadMessage()
				}
			}}
			client, err := Dial(context.Background(), Options{URL: f.URL()})
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			timeout := time.Second
			if scenario == "cancel" {
				timeout = 20 * time.Millisecond
			}
			got, err := client.ContainerExec(context.Background(), "app", "container", "printf hello; exit 7", timeout, 100)
			if scenario == "denied" {
				if err == nil || strings.Contains(err.Error(), "private-token") {
					t.Fatalf("error: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Output) > 100 {
				t.Fatalf("unbounded output: %d", len(got.Output))
			}
			if scenario == "real-shell" {
				if got.ExitCode == nil || *got.ExitCode != 7 || !strings.Contains(got.Output, "hello") {
					t.Fatalf("%+v", got)
				}
				return
			}
			if scenario == "complete" {
				if got.ExitCode == nil || *got.ExitCode != 7 || got.Output != "hello\r\n" || got.Incomplete {
					t.Fatalf("%+v", got)
				}
			} else if !got.Incomplete || got.ExitCode != nil {
				t.Fatalf("%+v", got)
			}
			if scenario == "overflow" && !got.Truncated {
				t.Fatal("missing truncation")
			}
		})
	}
}

func TestContainerExecRejectsInvalidBeforeNetwork(t *testing.T) {
	c := &Client{}
	for _, args := range []struct{ app, container, command string }{{"", "container", "id"}, {"app", "", "id"}, {"app", "container", ""}, {"app", "container", "a\x00b"}} {
		if _, err := c.ContainerExec(context.Background(), args.app, args.container, args.command, time.Second, 100); err == nil {
			t.Fatal("accepted invalid input")
		}
	}
}
