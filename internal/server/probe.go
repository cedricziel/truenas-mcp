package server

import (
	"fmt"
	"net"
	"net/http"
	"time"
)

// ProbeURL turns a bind address into the URL the health probe should call.
// A bind address commonly omits the host (":8080") or binds every interface,
// neither of which is dialable, so the probe always targets loopback.
func ProbeURL(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "http://127.0.0.1" + listen + "/healthz"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/healthz"
}

// Probe reports whether the server at url is healthy. It exists because the
// published image is distroless and carries no shell or HTTP client for a
// container health check to use.
func Probe(url string) error {
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("health probe could not reach the server: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health probe reported %s", resp.Status)
	}
	return nil
}
