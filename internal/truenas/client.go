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
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const subscriptionCleanupTimeout = 5 * time.Second

var errSubscriptionOverflow = errors.New("subscription event buffer overflow")

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
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
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
	// subscriptions are keyed by the exact event source name, including its
	// argument suffix where present.
	subscriptions map[string]*subscription
	// unsubscribing prevents a replacement subscription from reaching the
	// target until the previous target-side subscription has ended.
	unsubscribing map[string]*subscriptionCleanup
	closed        bool
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

	c := &Client{
		conn:          conn,
		pending:       map[uint64]chan rpcResponse{},
		subscriptions: map[string]*subscription{},
		unsubscribing: map[string]*subscriptionCleanup{},
	}
	go c.readLoop()
	return c, nil
}

// collectionUpdateMethod is the fixed top-level method every event-source
// notification arrives as. The subscribed collection name -- including any
// argument suffix, e.g. `app.container_log_follow:{"app_name":...}` -- is
// never the top-level method; it is nested in params.collection.
const collectionUpdateMethod = "collection_update"

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
		// Server-initiated notifications carry no ID and no waiter. Every
		// event-source notification is wrapped in a fixed "collection_update"
		// envelope; unwrap it to route by the actual collection name rather
		// than by the literal string "collection_update", which every
		// notification shares and which matches no real subscription.
		if resp.ID == 0 && resp.Method == collectionUpdateMethod {
			var envelope struct {
				Collection string          `json:"collection"`
				Fields     json.RawMessage `json:"fields"`
			}
			if json.Unmarshal(resp.Params, &envelope) == nil && envelope.Collection != "" {
				c.dispatchNotification(envelope.Collection, envelope.Fields)
			}
			continue
		}
		if resp.ID == 0 && resp.Method != "" {
			// A server-initiated message under some other method: not a
			// subscription this client knows how to route.
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
	for source, sub := range c.subscriptions {
		sub.close(fmt.Errorf("%w: %v", ErrInterrupted, err))
		delete(c.subscriptions, source)
	}
	for source, cleanup := range c.unsubscribing {
		close(cleanup.done)
		delete(c.unsubscribing, source)
	}
}

// Subscription receives events from one middleware event source. Err reports
// why Events closed, if it closed because the connection was interrupted.
type Subscription struct {
	Events <-chan json.RawMessage
	sub    *subscription
}

// Err returns the terminal error, if any, after Events closes.
func (s *Subscription) Err() error {
	s.sub.mu.Lock()
	defer s.sub.mu.Unlock()
	return s.sub.err
}

type subscription struct {
	mu     sync.Mutex
	events chan json.RawMessage
	done   chan struct{}
	err    error
	closed bool
}

type subscriptionCleanup struct {
	done chan struct{}
}

func (s *subscription) close(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	s.err = err
	close(s.events)
	close(s.done)
}

// Subscribe opens an event-source subscription. Cancel ctx to stop the stream;
// the target is unsubscribed asynchronously so cancellation returns promptly.
func (c *Client) Subscribe(ctx context.Context, source string) (*Subscription, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	sub := &subscription{
		events: make(chan json.RawMessage, 64),
		done:   make(chan struct{}),
	}

	for {
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return nil, ErrClosed
		}
		if c.readErr != nil {
			c.mu.Unlock()
			return nil, fmt.Errorf("%w: %v", ErrInterrupted, c.readErr)
		}
		if _, exists := c.subscriptions[source]; exists {
			c.mu.Unlock()
			return nil, fmt.Errorf("subscription already open for %q", source)
		}
		cleanup := c.unsubscribing[source]
		if cleanup == nil {
			// A leak here degrades every tool in this shared session connection, not
			// just the caller that stopped reading this particular event stream.
			c.subscriptions[source] = sub
			c.mu.Unlock()
			break
		}
		c.mu.Unlock()

		select {
		case <-cleanup.done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if _, err := c.Call(ctx, "core.subscribe", source); err != nil {
		var targetErr *CallError
		// A transport interruption or cancelled context can happen after the
		// target accepted the subscribe request, so end it defensively. A target
		// refusal never created a stream and needs no matching unsubscribe.
		c.endSubscription(source, sub, err, !errors.As(err, &targetErr))
		return nil, err
	}

	public := &Subscription{Events: sub.events, sub: sub}
	go func() {
		select {
		case <-ctx.Done():
			c.endSubscription(source, sub, ctx.Err(), true)
		case <-sub.done:
		}
	}()
	return public, nil
}

func (c *Client) dispatchNotification(source string, params json.RawMessage) {
	c.mu.Lock()
	sub, ok := c.subscriptions[source]
	if !ok {
		c.mu.Unlock()
		return
	}
	select {
	case sub.events <- params:
		c.mu.Unlock()
	case <-sub.done:
		c.mu.Unlock()
	default:
		cleanup := c.unregisterSubscriptionLocked(source, sub, errSubscriptionOverflow, true)
		c.mu.Unlock()
		c.unsubscribeAsync(source, cleanup)
	}
}

func (c *Client) endSubscription(source string, sub *subscription, err error, unsubscribe bool) {
	c.mu.Lock()
	cleanup := c.unregisterSubscriptionLocked(source, sub, err, unsubscribe)
	c.mu.Unlock()

	c.unsubscribeAsync(source, cleanup)
}

func (c *Client) unregisterSubscriptionLocked(source string, sub *subscription, err error, unsubscribe bool) *subscriptionCleanup {
	if c.subscriptions[source] != sub {
		return nil
	}
	delete(c.subscriptions, source)
	sub.close(err)
	if !unsubscribe {
		return nil
	}

	cleanup := &subscriptionCleanup{done: make(chan struct{})}
	c.unsubscribing[source] = cleanup
	return cleanup
}

func (c *Client) unsubscribeAsync(source string, cleanup *subscriptionCleanup) {
	if cleanup == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), subscriptionCleanupTimeout)
		defer cancel()
		if _, err := c.Call(ctx, "core.unsubscribe", source); err != nil {
			// The target may still apply a timed-out unsubscribe. This connection
			// cannot safely open a replacement for the same source in that case.
			_ = c.conn.Close()
		}

		c.mu.Lock()
		if c.unsubscribing[source] == cleanup {
			delete(c.unsubscribing, source)
			close(cleanup.done)
		}
		c.mu.Unlock()
	}()
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

// Alive reports whether the connection can still carry requests. A caller
// holding a long-lived session uses this to decide whether to re-establish
// rather than retrying against a socket the peer has already closed.
func (c *Client) Alive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.closed && c.readErr == nil
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
