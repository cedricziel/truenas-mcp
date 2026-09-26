package truenas

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// serveDownload makes the fake answer core.download the way the middleware
// does, with a job id and a one-time URL, and serve body on that URL.
func serveDownload(t *testing.T, f *fakeMiddleware, status int, body string) *json.RawMessage {
	t.Helper()

	var seen json.RawMessage
	f.handle("core.download", func(p json.RawMessage) (any, *rpcError) {
		seen = p
		return []any{7, "/_download/7?auth_token=one-time"}, nil
	})
	f.handleDownload("/_download/7", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("auth_token") != "one-time" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
	finishJob(f, "SUCCESS", "")
	return &seen
}

func finishJob(f *fakeMiddleware, state, jobErr string) {
	f.handle("core.get_jobs", func(json.RawMessage) (any, *rpcError) {
		return []any{map[string]any{"id": 7, "method": "filesystem.get", "state": state, "error": jobErr}}, nil
	})
}

// Verified live: a job that fails before writing anything can still be served
// as 200 with an empty body, so only the job's state tells the two apart.
func TestDownloadReportsAJobThatFailedBehindAnEmptySuccess(t *testing.T) {
	f := newFakeMiddleware(t)
	serveDownload(t, f, http.StatusOK, "")
	finishJob(f, "FAILED", "[EFAULT] /etc is not a file")

	c := dial(t, f)
	_, err := c.Download(context.Background(), "filesystem.get", []any{"/etc"}, "etc", 1024)
	if err == nil || !strings.Contains(err.Error(), "/etc is not a file") {
		t.Fatalf("want the job's error, got %v", err)
	}
}

func TestDownloadReturnsTheJobsOutput(t *testing.T) {
	f := newFakeMiddleware(t)
	seen := serveDownload(t, f, http.StatusOK, "hive\n")

	c := dial(t, f)
	got, err := c.Download(context.Background(), "filesystem.get", []any{"/etc/hostname"}, "hostname", 1024)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if string(got.Data) != "hive\n" || got.Truncated {
		t.Errorf("got %q truncated=%v, want the whole file", got.Data, got.Truncated)
	}
	if want := `["filesystem.get",["/etc/hostname"],"hostname",false]`; string(*seen) != want {
		t.Errorf("core.download params = %s, want %s", *seen, want)
	}
}

// A file larger than the caller can use is cut off, and says so, rather
// than read in full.
func TestDownloadStopsAtTheLimit(t *testing.T) {
	f := newFakeMiddleware(t)
	serveDownload(t, f, http.StatusOK, "0123456789")

	c := dial(t, f)
	got, err := c.Download(context.Background(), "filesystem.get", []any{"/var/log/big"}, "big", 4)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if string(got.Data) != "0123" || !got.Truncated {
		t.Errorf("got %q truncated=%v, want the first 4 bytes marked truncated", got.Data, got.Truncated)
	}
}

// The middleware reports a job that failed before producing output as 422
// with the job's error as the body.
func TestDownloadReportsAFailedJob(t *testing.T) {
	f := newFakeMiddleware(t)
	serveDownload(t, f, http.StatusUnprocessableEntity, "[EFAULT] /mnt/tank is not a file")

	c := dial(t, f)
	_, err := c.Download(context.Background(), "filesystem.get", []any{"/mnt/tank"}, "tank", 1024)
	if err == nil {
		t.Fatal("a failed job must be an error")
	}
	if !strings.Contains(err.Error(), "/mnt/tank is not a file") {
		t.Errorf("error should carry the job's reason, got: %v", err)
	}
}

func TestDownloadSurfacesARefusedCall(t *testing.T) {
	f := newFakeMiddleware(t)
	f.handle("core.download", func(json.RawMessage) (any, *rpcError) {
		return nil, callError("EACCES", "Not authorized")
	})

	c := dial(t, f)
	_, err := c.Download(context.Background(), "filesystem.get", []any{"/etc/shadow"}, "shadow", 1024)
	if err == nil || !strings.Contains(err.Error(), "Not authorized") {
		t.Fatalf("want the target's refusal, got %v", err)
	}
}
