package truenas

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func dial(t *testing.T, f *fakeMiddleware) *Client {
	t.Helper()
	c, err := Dial(context.Background(), Options{URL: f.URL()})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestDialUnreachableTarget(t *testing.T) {
	_, err := Dial(context.Background(), Options{URL: "ws://127.0.0.1:1/api/current"})
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("want ErrUnreachable, got %v", err)
	}
}

func TestLoginWithValidKey(t *testing.T) {
	f := newFakeMiddleware(t)
	c := dial(t, f)

	if err := c.Login(context.Background(), validAPIKey); err != nil {
		t.Fatalf("login with a valid key should succeed: %v", err)
	}
}

func TestLoginWithRejectedKey(t *testing.T) {
	f := newFakeMiddleware(t)
	c := dial(t, f)

	err := c.Login(context.Background(), "1-wrongkey")
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("want ErrUnauthenticated, got %v", err)
	}
	// The key must never appear in an error that may be logged or returned.
	if strings.Contains(err.Error(), "1-wrongkey") {
		t.Fatalf("error must not disclose the key: %v", err)
	}
}

// TrueNAS API keys are user-linked, so the key alone identifies the caller
// and the login takes no username.
func TestLoginSendsOnlyTheKey(t *testing.T) {
	f := newFakeMiddleware(t)
	var got []any
	f.handle("auth.login_with_api_key", func(params json.RawMessage) (any, *rpcError) {
		_ = json.Unmarshal(params, &got)
		return true, nil
	})

	c := dial(t, f)
	if err := c.Login(context.Background(), validAPIKey); err != nil {
		t.Fatalf("login: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("login must send exactly one parameter, sent %d: %v", len(got), got)
	}
}

func TestLoginRateLimited(t *testing.T) {
	f := newFakeMiddleware(t)
	f.handle("auth.login_with_api_key", func(json.RawMessage) (any, *rpcError) {
		return nil, &rpcError{Code: -32000, Message: "Rate limit exceeded, try again later"}
	})

	c := dial(t, f)
	err := c.Login(context.Background(), validAPIKey)
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("want ErrRateLimited, got %v", err)
	}
}

func TestCallReturnsResult(t *testing.T) {
	f := newFakeMiddleware(t)
	f.handle("system.version", func(json.RawMessage) (any, *rpcError) {
		return "TrueNAS-26.0.0", nil
	})

	c := dial(t, f)
	raw, err := c.Call(context.Background(), "system.version")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	var got string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got != "TrueNAS-26.0.0" {
		t.Fatalf("got %q", got)
	}
}

// A failure the target reported must stay distinguishable from a failure of
// this server's own gating.
func TestCallSurfacesTargetError(t *testing.T) {
	f := newFakeMiddleware(t)
	f.handle("pool.query", func(json.RawMessage) (any, *rpcError) {
		return nil, &rpcError{Code: -32001, Message: "Not authorized"}
	})

	c := dial(t, f)
	_, err := c.Call(context.Background(), "pool.query")

	var callErr *CallError
	if !errors.As(err, &callErr) {
		t.Fatalf("want a *CallError, got %T: %v", err, err)
	}
	if callErr.Method != "pool.query" {
		t.Errorf("CallError.Method = %q, want pool.query", callErr.Method)
	}
	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("an access-denied code must map to ErrUnauthorized, got %v", err)
	}
}

func TestCallPassesParameters(t *testing.T) {
	f := newFakeMiddleware(t)
	var seen json.RawMessage
	f.handle("app.query", func(p json.RawMessage) (any, *rpcError) {
		seen = p
		return []any{}, nil
	})

	c := dial(t, f)
	filters := []any{[]any{"name", "=", "signaldb"}}
	if _, err := c.Call(context.Background(), "app.query", filters); err != nil {
		t.Fatalf("call: %v", err)
	}
	if !strings.Contains(string(seen), "signaldb") {
		t.Fatalf("parameters were not forwarded, got %s", seen)
	}
}

// Concurrent calls must not cross responses: each caller gets its own reply.
func TestConcurrentCallsAreMatchedByID(t *testing.T) {
	f := newFakeMiddleware(t)
	f.handle("echo", func(p json.RawMessage) (any, *rpcError) {
		var args []string
		_ = json.Unmarshal(p, &args)
		return args[0], nil
	})

	c := dial(t, f)

	const n = 20
	errs := make(chan error, n)
	for i := range n {
		go func() {
			want := string(rune('a' + i%26))
			raw, err := c.Call(context.Background(), "echo", want)
			if err != nil {
				errs <- err
				return
			}
			var got string
			_ = json.Unmarshal(raw, &got)
			if got != want {
				errs <- errors.New("response crossed: got " + got + " want " + want)
				return
			}
			errs <- nil
		}()
	}
	for range n {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
}

// When the connection drops mid-request the caller must be told the operation's
// fate is unknown, rather than being given a plain failure.
func TestInFlightRequestInterrupted(t *testing.T) {
	f := newFakeMiddleware(t)
	f.handle("slow.method", func(json.RawMessage) (any, *rpcError) { return nil, nil })
	f.setDropAfter(1)

	c := dial(t, f)
	_, err := c.Call(context.Background(), "slow.method")
	if !errors.Is(err, ErrInterrupted) {
		t.Fatalf("want ErrInterrupted, got %v", err)
	}
	if !strings.Contains(err.Error(), "may have been applied") {
		t.Errorf("error should state the operation's fate is unknown, got: %v", err)
	}
}

func TestCallAfterCloseFails(t *testing.T) {
	f := newFakeMiddleware(t)
	c := dial(t, f)
	_ = c.Close()

	if _, err := c.Call(context.Background(), "system.version"); !errors.Is(err, ErrClosed) {
		t.Fatalf("want ErrClosed, got %v", err)
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	f := newFakeMiddleware(t)
	c := dial(t, f)
	if err := c.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second close should be a no-op, got: %v", err)
	}
}

func TestContextCancellationAbortsCall(t *testing.T) {
	f := newFakeMiddleware(t)
	c := dial(t, f)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := c.Call(ctx, "system.version"); err == nil {
		t.Fatal("a cancelled context must abort the call")
	}
}
