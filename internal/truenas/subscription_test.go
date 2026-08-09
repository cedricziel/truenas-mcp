package truenas

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestSubscribeRoutesEventsBySource(t *testing.T) {
	f := newFakeMiddleware(t)
	f.handle("core.subscribe", func(json.RawMessage) (any, *rpcError) { return true, nil })
	c := dial(t, f)

	first, err := c.Subscribe(context.Background(), "source.first")
	if err != nil {
		t.Fatalf("subscribe first: %v", err)
	}
	second, err := c.Subscribe(context.Background(), "source.second")
	if err != nil {
		t.Fatalf("subscribe second: %v", err)
	}
	if err := f.emit("source.first", map[string]string{"value": "first"}); err != nil {
		t.Fatalf("emit first: %v", err)
	}
	if err := f.emit("source.second", map[string]string{"value": "second"}); err != nil {
		t.Fatalf("emit second: %v", err)
	}
	if err := f.emit("source.unknown", map[string]string{"value": "unknown"}); err != nil {
		t.Fatalf("emit unknown: %v", err)
	}

	if got := receiveEvent(t, first.Events); string(got) != `{"value":"first"}` {
		t.Fatalf("first subscription received %s", got)
	}
	if got := receiveEvent(t, second.Events); string(got) != `{"value":"second"}` {
		t.Fatalf("second subscription received %s", got)
	}
	assertNoEvent(t, first.Events)
	assertNoEvent(t, second.Events)
}

func TestSubscribeCancellationUnsubscribesImmediately(t *testing.T) {
	f := newFakeMiddleware(t)
	f.handle("core.subscribe", func(json.RawMessage) (any, *rpcError) { return true, nil })
	f.handle("core.unsubscribe", func(json.RawMessage) (any, *rpcError) { return true, nil })
	c := dial(t, f)

	ctx, cancel := context.WithCancel(context.Background())
	subscription, err := c.Subscribe(ctx, "source.logs")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	cancel()

	select {
	case _, ok := <-subscription.Events:
		if ok {
			t.Fatal("cancelled subscription must close its event channel")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled subscription did not return immediately")
	}
	waitFor(t, func() bool { return f.requestCount("core.unsubscribe") == 1 })

	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.subscriptions) != 0 {
		t.Fatalf("registered subscriptions = %d, want 0", len(c.subscriptions))
	}
}

func TestSubscribeBoundReachedUnsubscribes(t *testing.T) {
	f := newFakeMiddleware(t)
	f.handle("core.subscribe", func(json.RawMessage) (any, *rpcError) { return true, nil })
	f.handle("core.unsubscribe", func(json.RawMessage) (any, *rpcError) { return true, nil })
	c := dial(t, f)

	ctx, cancel := context.WithCancel(context.Background())
	subscription, err := c.Subscribe(ctx, "source.logs")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err := f.emit("source.logs", map[string]string{"value": "first"}); err != nil {
		t.Fatalf("emit: %v", err)
	}
	_ = receiveEvent(t, subscription.Events)
	cancel() // The collector cancels this context when its requested bound is met.

	waitFor(t, func() bool { return f.requestCount("core.unsubscribe") == 1 })
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.subscriptions) != 0 {
		t.Fatalf("registered subscriptions = %d, want 0", len(c.subscriptions))
	}
}

func TestSubscribeOverflowDoesNotBlockCalls(t *testing.T) {
	f := newFakeMiddleware(t)
	f.handle("core.subscribe", func(json.RawMessage) (any, *rpcError) { return true, nil })
	f.handle("core.unsubscribe", func(json.RawMessage) (any, *rpcError) { return true, nil })
	f.handle("system.info", func(json.RawMessage) (any, *rpcError) { return "available", nil })
	c := dial(t, f)

	subscription, err := c.Subscribe(context.Background(), "source.logs")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	for range 65 {
		if err := f.emit("source.logs", map[string]string{"value": "log"}); err != nil {
			t.Fatalf("emit: %v", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := c.Call(ctx, "system.info"); err != nil {
		t.Fatalf("unrelated call after overflow: %v", err)
	}
	waitFor(t, func() bool { return subscription.Err() != nil })
	waitFor(t, func() bool { return f.requestCount("core.unsubscribe") == 1 })

	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.subscriptions) != 0 {
		t.Fatalf("registered subscriptions = %d, want 0", len(c.subscriptions))
	}
}

func TestSubscribeReplacementWaitsForOldUnsubscribe(t *testing.T) {
	f := newFakeMiddleware(t)
	f.handle("core.subscribe", func(json.RawMessage) (any, *rpcError) { return true, nil })
	unsubscribeStarted := make(chan struct{})
	releaseUnsubscribe := make(chan struct{})
	f.handle("core.unsubscribe", func(json.RawMessage) (any, *rpcError) {
		close(unsubscribeStarted)
		<-releaseUnsubscribe
		return true, nil
	})
	c := dial(t, f)

	ctx, cancel := context.WithCancel(context.Background())
	_, err := c.Subscribe(ctx, "source.logs")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	cancel()
	select {
	case <-unsubscribeStarted:
	case <-time.After(time.Second):
		t.Fatal("old unsubscribe was not requested")
	}

	replacement := make(chan error, 1)
	go func() {
		_, err := c.Subscribe(context.Background(), "source.logs")
		replacement <- err
	}()
	select {
	case err := <-replacement:
		t.Fatalf("replacement returned before old unsubscribe completed: %v", err)
	default:
	}

	close(releaseUnsubscribe)
	select {
	case err := <-replacement:
		if err != nil {
			t.Fatalf("replacement subscribe: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("replacement did not subscribe after old unsubscribe completed")
	}
	if got := f.requestCount("core.unsubscribe"); got != 1 {
		t.Fatalf("unsubscribe requests = %d, want 1", got)
	}
}

func TestSubscribeTargetRefusalIsReturned(t *testing.T) {
	f := newFakeMiddleware(t)
	f.handle("core.subscribe", func(json.RawMessage) (any, *rpcError) {
		return nil, &rpcError{Code: -32001, Message: "app is stopped"}
	})
	c := dial(t, f)

	_, err := c.Subscribe(context.Background(), "source.logs")
	var callErr *CallError
	if !errors.As(err, &callErr) {
		t.Fatalf("want target CallError, got %T: %v", err, err)
	}
}

func TestSubscriptionInterruptedByConnectionDrop(t *testing.T) {
	f := newFakeMiddleware(t)
	f.handle("core.subscribe", func(json.RawMessage) (any, *rpcError) { return true, nil })
	c := dial(t, f)

	subscription, err := c.Subscribe(context.Background(), "source.logs")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	f.setDropAfter(2)
	_, _ = c.Call(context.Background(), "trigger.drop")

	select {
	case _, ok := <-subscription.Events:
		if ok {
			t.Fatal("interrupted subscription must close its event channel")
		}
	case <-time.After(time.Second):
		t.Fatal("connection drop left subscription blocked")
	}
	if !errors.Is(subscription.Err(), ErrInterrupted) {
		t.Fatalf("want ErrInterrupted, got %v", subscription.Err())
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.subscriptions) != 0 {
		t.Fatalf("interrupted subscriptions = %d, want 0", len(c.subscriptions))
	}
}

func receiveEvent(t *testing.T, events <-chan json.RawMessage) json.RawMessage {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
		return nil
	}
}

func assertNoEvent(t *testing.T, events <-chan json.RawMessage) {
	t.Helper()
	select {
	case event := <-events:
		t.Fatalf("unexpected event: %s", event)
	case <-time.After(20 * time.Millisecond):
	}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition was not met")
}
