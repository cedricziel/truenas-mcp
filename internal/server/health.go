package server

import (
	"encoding/json"
	"net/http"
	"sync"
)

// Health is the readiness signal the apps platform polls to decide whether
// the app is up. It reports unhealthy until the server is actually serving,
// so a container that starts but cannot work is not reported as running.
type Health struct {
	version string

	mu      sync.RWMutex
	serving bool
	reason  string
}

// NewHealth returns a Health that reports unhealthy until SetServing(true).
func NewHealth(version string) *Health {
	return &Health{version: version, reason: "starting up"}
}

// SetServing records whether the transport is accepting requests.
func (h *Health) SetServing(serving bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.serving = serving
	if serving {
		h.reason = ""
	} else if h.reason == "" {
		h.reason = "not serving"
	}
}

// SetUnhealthy marks the server unhealthy with a reason an operator can act on.
func (h *Health) SetUnhealthy(reason string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.serving = false
	h.reason = reason
}

func (h *Health) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	h.mu.RLock()
	serving, reason := h.serving, h.reason
	h.mu.RUnlock()

	body := map[string]string{"version": h.version}
	status := http.StatusOK
	if serving {
		body["status"] = "healthy"
	} else {
		body["status"] = "unhealthy"
		body["reason"] = reason
		status = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
