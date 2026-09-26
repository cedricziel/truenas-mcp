package truenas

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Download is what a streaming job produced, up to the caller's limit.
type Download struct {
	Data      []byte
	Truncated bool
}

// Download runs a job that streams its output, such as filesystem.get, and
// reads at most max bytes of it.
//
// Such a job only runs with an output pipe attached, and the middleware only
// attaches one through core.download. That call answers with a job id and a
// single-use URL on the same host, valid for a few minutes and only from the
// address that asked for it.
func (c *Client) Download(ctx context.Context, method string, args []any, filename string, max int64) (Download, error) {
	raw, err := c.Call(ctx, "core.download", method, args, filename, false)
	if err != nil {
		return Download{}, err
	}

	var answer []json.RawMessage
	var jobID int64
	var path string
	if err := json.Unmarshal(raw, &answer); err != nil || len(answer) != 2 ||
		json.Unmarshal(answer[0], &jobID) != nil || json.Unmarshal(answer[1], &path) != nil {
		return Download{}, fmt.Errorf("%s: unexpected core.download answer: %s", method, raw)
	}

	ref, err := url.Parse(path)
	if err != nil {
		return Download{}, fmt.Errorf("%s: unusable download URL: %w", method, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.httpBase.ResolveReference(ref).String(), nil)
	if err != nil {
		return Download{}, fmt.Errorf("%s: %w", method, err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		// The URL carries a token; keep it out of the error.
		return Download{}, fmt.Errorf("%s: %w: fetching the download failed", method, ErrUnreachable)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		reason := strings.TrimSpace(string(body))
		if reason == "" {
			reason = resp.Status
		}
		return Download{}, fmt.Errorf("%s: target reported: %s", method, reason)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return Download{}, fmt.Errorf("%s: %w: reading the download: %w", method, ErrInterrupted, err)
	}
	if int64(len(data)) > max {
		return Download{Data: data[:max], Truncated: true}, nil
	}
	if err := c.awaitDownloadJob(ctx, method, jobID); err != nil {
		return Download{}, err
	}
	return Download{Data: data}, nil
}

const (
	downloadJobWait = 5 * time.Second
	downloadJobPoll = 100 * time.Millisecond
)

// awaitDownloadJob reports a failed job behind a complete download. A job
// that fails before writing anything can still be served as 200 with an empty
// body, so the status code alone does not prove it succeeded. A job still
// running after downloadJobWait is given the benefit of the doubt: its output
// has already ended.
func (c *Client) awaitDownloadJob(ctx context.Context, method string, id int64) error {
	ctx, cancel := context.WithTimeout(ctx, downloadJobWait)
	defer cancel()

	for {
		job, err := c.Job(ctx, id)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if job.Failed() {
			return fmt.Errorf("%s: target reported: %s", method, job.Error)
		}
		if job.Done() {
			return nil
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(downloadJobPoll):
		}
	}
}

// httpBaseURL turns the middleware's WebSocket endpoint into the plain HTTP
// origin of the same host.
func httpBaseURL(endpoint string) (*url.URL, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrUnreachable, endpoint, err)
	}
	switch u.Scheme {
	case "wss":
		u.Scheme = "https"
	case "ws":
		u.Scheme = "http"
	}
	return &url.URL{Scheme: u.Scheme, Host: u.Host}, nil
}
