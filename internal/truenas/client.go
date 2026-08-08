// Package truenas is a JSON-RPC 2.0 client for the TrueNAS middleware's
// WebSocket API.
//
// It is written rather than adapted from the official TrueNAS MCP server,
// whose equivalent layer is GPL-3.0. The surface here is also different by
// necessity: that client is built around a single process-wide API key, while
// this server opens a connection per session under the caller's own credential.
package truenas

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
	// Method is set on server-initiated notifications, which carry no ID.
	Method string `json:"method,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Options configures a client.
type Options struct {
	// URL is the middleware endpoint, e.g. wss://nas.local/api/current.
	URL string

	// InsecureSkipVerify accepts an unverified certificate. Deliberately
	// independent of the URL scheme: a self-signed certificate must not push
	// an operator onto plaintext, where TrueNAS revokes the key it observes.
	InsecureSkipVerify bool

	// DialTimeout bounds connection establishment.
	DialTimeout time.Duration
}

// Client is a connection to one TrueNAS middleware instance, authenticated as
// one user. It is safe for concurrent use.
type Client struct {
	conn *websocket.Conn

	writeMu sync.Mutex

	mu      sync.Mutex
	nextID  uint64
	pending map[uint64]chan rpcResponse
	closed  bool
	// readErr records why the read loop stopped, so callers whose request was
	// in flight can be told the connection was interrupted.
	readErr error
}

// Dial connects to the middleware and starts serving responses. It does not
// authenticate; call Login next.
func Dial(ctx context.Context, opts Options) (*Client, error) {
	timeout := opts.DialTimeout
	if timeout == 0 {
		timeout = 15 * time.Second
	}

	dialer := &websocket.Dialer{
		HandshakeTimeout: timeout,
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: opts.InsecureSkipVerify}, //nolint:gosec // opt-in, see Options
	}

	conn, _, err := dialer.DialContext(ctx, opts.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrUnreachable, opts.URL, err)
	}

	c := &Client{conn: conn, pending: map[uint64]chan rpcResponse{}}
	go c.readLoop()
	return c, nil
}

// readLoop dispatches responses to whichever caller is waiting on their ID.
// Matching by ID rather than by arrival order is what makes concurrent calls
// on one connection safe.
func (c *Client) readLoop() {
	for {
		var resp rpcResponse
		if err := c.conn.ReadJSON(&resp); err != nil {
			c.failPending(err)
			return
		}
		// Server-initiated notifications carry no ID and no waiter.
		if resp.ID == 0 && resp.Method != "" {
			continue
		}

		c.mu.Lock()
		ch, ok := c.pending[resp.ID]
		delete(c.pending, resp.ID)
		c.mu.Unlock()

		if ok {
			ch <- resp
		}
	}
}

// failPending releases every waiting caller when the connection dies. They are
// told the request was interrupted rather than that it failed, because whether
// the target applied it is genuinely unknown.
func (c *Client) failPending(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.readErr = err
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
}

// Call invokes a middleware method and returns its raw result.
func (c *Client) Call(ctx context.Context, method string, params ...any) (json.RawMessage, error) {
	if params == nil {
		params = []any{}
	}
	encoded, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("%s: encoding parameters: %w", method, err)
	}

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, ErrClosed
	}
	c.nextID++
	id := c.nextID
	ch := make(chan rpcResponse, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	req := rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: encoded}

	c.writeMu.Lock()
	err = c.conn.WriteJSON(req)
	c.writeMu.Unlock()
	if err != nil {
		c.forget(id)
		return nil, fmt.Errorf("%s: %w: %w", method, ErrInterrupted, err)
	}

	select {
	case <-ctx.Done():
		c.forget(id)
		return nil, fmt.Errorf("%s: %w", method, ctx.Err())

	case resp, ok := <-ch:
		if !ok {
			// The read loop closed the channel: the connection died while this
			// request was outstanding.
			return nil, fmt.Errorf(
				"%s: %w: the operation may have been applied on the target", method, ErrInterrupted)
		}
		if resp.Error != nil {
			return nil, classify(method, resp.Error)
		}
		return resp.Result, nil
	}
}

func (c *Client) forget(id uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.pending, id)
}

// Login authenticates the connection with a user-linked API key. TrueNAS keys
// are bound to a user account, so the key alone identifies the caller and no
// username is sent.
func (c *Client) Login(ctx context.Context, apiKey string) error {
	raw, err := c.Call(ctx, "auth.login_with_api_key", apiKey)
	if err != nil {
		// Never let the key reach an error string: Call embeds the method name
		// only, but the target's message could echo the argument.
		return scrub(err, apiKey)
	}

	var ok bool
	if err := json.Unmarshal(raw, &ok); err != nil {
		return fmt.Errorf("%w: unexpected login response", ErrUnauthenticated)
	}
	if !ok {
		return fmt.Errorf("%w: the target rejected the API key", ErrUnauthenticated)
	}
	return nil
}

// classify maps a target-reported error onto the sentinels callers match on.
// Rate limiting arrives as a generic auth error distinguished only by its
// message, so it is detected before the code-based mapping in CallError.
func classify(method string, e *rpcError) error {
	callErr := &CallError{Method: method, Code: e.Code, Message: e.Message, Data: e.Data}

	if strings.Contains(strings.ToLower(e.Message), "rate limit") {
		return fmt.Errorf("%w: %w", ErrRateLimited, callErr)
	}
	return callErr
}

// scrub keeps a secret out of an error chain even if the target echoed it.
func scrub(err error, secret string) error {
	if secret == "" || !strings.Contains(err.Error(), secret) {
		return err
	}
	return fmt.Errorf("%s", strings.ReplaceAll(err.Error(), secret, "[redacted]"))
}

// Close releases the connection. It is safe to call more than once.
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()

	return c.conn.Close()
}
