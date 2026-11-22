package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cedricziel/truenas-mcp/internal/truenas"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type resourceSubscriptionKey struct {
	client *truenas.Client
	source string
}

type resourceSubscriber struct {
	session *mcp.ServerSession
	uri     string
	listen  *resourceListen
}

type resourceListen struct{ cancel context.CancelCauseFunc }
type resourceListenContextKey struct{}

var errResourceEventStream = errors.New("resource event stream interrupted")

type resourceEventStream struct {
	cancel    context.CancelFunc
	listeners map[resourceSubscriber]int64
}

type resourceSubscriptions struct {
	server   *mcp.Server
	session  sessionFor
	mu       sync.Mutex
	streams  map[resourceSubscriptionKey]*resourceEventStream
	sessions map[*mcp.ServerSession]bool
	listens  map[*mcp.ServerSession]map[string]*resourceListen
}

func newResourceSubscriptions(session sessionFor) *resourceSubscriptions {
	return &resourceSubscriptions{
		session:  session,
		streams:  make(map[resourceSubscriptionKey]*resourceEventStream),
		sessions: make(map[*mcp.ServerSession]bool),
		listens:  make(map[*mcp.ServerSession]map[string]*resourceListen),
	}
}

func (r *resourceSubscriptions) setServer(server *mcp.Server) {
	r.server = server
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
			if method != "subscriptions/listen" {
				return next(ctx, method, request)
			}
			req := request.(*mcp.SubscriptionsListenRequest)
			if req.Params == nil || req.Params.Notifications == nil {
				return next(ctx, method, request)
			}
			ctx, cancel := context.WithCancelCause(ctx)
			defer cancel(nil)
			listen := &resourceListen{cancel: cancel}
			uris := req.Params.Notifications.ResourceSubscriptions
			if err := r.reserveListen(req.Session, listen, uris); err != nil {
				return nil, err
			}
			defer r.releaseListen(req.Session, listen, uris)
			result, err := next(context.WithValue(ctx, resourceListenContextKey{}, listen), method, request)
			if cause := context.Cause(ctx); errors.Is(cause, errResourceEventStream) {
				return nil, cause
			}
			return result, err
		}
	})
}

func (r *resourceSubscriptions) reserveListen(session *mcp.ServerSession, listen *resourceListen, uris []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := make(map[string]bool)
	for _, uri := range uris {
		if seen[uri] || r.listens[session][uri] != nil {
			return fmt.Errorf("resource %q already has an active listen on this session", uri)
		}
		seen[uri] = true
	}
	if len(uris) > 0 {
		if r.listens[session] == nil {
			r.listens[session] = make(map[string]*resourceListen)
		}
		for _, uri := range uris {
			r.listens[session][uri] = listen
		}
	}
	return nil
}

func (r *resourceSubscriptions) releaseListen(session *mcp.ServerSession, listen *resourceListen, uris []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, uri := range uris {
		if r.listens[session][uri] == listen {
			delete(r.listens[session], uri)
		}
	}
	if len(r.listens[session]) == 0 {
		delete(r.listens, session)
	}
}

func resourceEventSource(uri string) (string, int64, error) {
	switch uri {
	case uriAlerts:
		return "alert.list", 0, nil
	case uriPools:
		return "pool.query", 0, nil
	case uriApps:
		return "app.query", 0, nil
	}
	if suffix, ok := strings.CutPrefix(uri, "truenas://job/"); ok {
		id, err := strconv.ParseInt(suffix, 10, 64)
		if err == nil && id > 0 && strconv.FormatInt(id, 10) == suffix {
			return "core.get_jobs", id, nil
		}
	}
	return "", 0, fmt.Errorf("resource %q does not support subscriptions", uri)
}

func (r *resourceSubscriptions) subscribe(ctx context.Context, req *mcp.SubscribeRequest) error {
	listen, _ := ctx.Value(resourceListenContextKey{}).(*resourceListen)
	if listen == nil && req.ProtocolVersion() >= "2026-07-28" {
		return fmt.Errorf("use subscriptions/listen for resource subscriptions in protocol %s", req.ProtocolVersion())
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	source, jobID, err := resourceEventSource(req.Params.URI)
	if err != nil {
		return err
	}
	session, err := r.session(ctx)
	if err != nil {
		return err
	}
	key := resourceSubscriptionKey{client: session.Client(), source: source}
	subscriber := resourceSubscriber{session: req.Session, uri: req.Params.URI, listen: listen}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	stream := r.streams[key]
	if stream == nil {
		// Legacy subscribe requests finish immediately; their request cancellation must not end the stream.
		streamCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
		stopRequest := context.AfterFunc(ctx, cancel)
		setupTimer := time.AfterFunc(15*time.Second, cancel)
		subscription, err := key.client.Subscribe(streamCtx, source)
		setupErr := finishResourceSubscriptionSetup(ctx, streamCtx, cancel, setupTimer, stopRequest)
		if err == nil {
			err = setupErr
		}
		if err != nil {
			cancel()
			return err
		}
		stream = &resourceEventStream{cancel: cancel, listeners: make(map[resourceSubscriber]int64)}
		r.streams[key] = stream
		go r.forward(streamCtx, key, stream, subscription)
	}
	stream.listeners[subscriber] = jobID
	if !r.sessions[req.Session] {
		r.sessions[req.Session] = true
		go func() {
			_ = req.Session.Wait()
			r.releaseSession(req.Session)
		}()
	}
	return nil
}

func finishResourceSubscriptionSetup(ctx, streamCtx context.Context, cancel context.CancelFunc, timer *time.Timer, stopRequest func() bool) error {
	timerStopped := timer.Stop()
	requestStopped := stopRequest()
	err := ctx.Err()
	if err == nil && !timerStopped {
		err = context.DeadlineExceeded
	}
	if err == nil && !requestStopped {
		err = context.Canceled
	}
	if err == nil {
		err = streamCtx.Err()
	}
	if err != nil {
		cancel()
	}
	return err
}

func (r *resourceSubscriptions) unsubscribe(ctx context.Context, req *mcp.UnsubscribeRequest) error {
	listen, _ := ctx.Value(resourceListenContextKey{}).(*resourceListen)
	if listen == nil && req.ProtocolVersion() >= "2026-07-28" {
		return fmt.Errorf("cancel subscriptions/listen to unsubscribe in protocol %s", req.ProtocolVersion())
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.removeLocked(func(subscriber resourceSubscriber) bool {
		return subscriber.session == req.Session && subscriber.uri == req.Params.URI && subscriber.listen == listen
	})
	return nil
}

func (r *resourceSubscriptions) releaseSession(session *mcp.ServerSession) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, session)
	r.removeLocked(func(subscriber resourceSubscriber) bool { return subscriber.session == session })
}

func (r *resourceSubscriptions) removeLocked(remove func(resourceSubscriber) bool) {
	for key, stream := range r.streams {
		for subscriber := range stream.listeners {
			if remove(subscriber) {
				delete(stream.listeners, subscriber)
			}
		}
		if len(stream.listeners) == 0 {
			delete(r.streams, key)
			stream.cancel()
		}
	}
}

func (r *resourceSubscriptions) forward(ctx context.Context, key resourceSubscriptionKey, stream *resourceEventStream, subscription *truenas.Subscription) {
	defer stream.cancel()
	defer func() {
		r.mu.Lock()
		if r.streams[key] == stream {
			delete(r.streams, key)
		}
		r.mu.Unlock()
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-subscription.Events:
			if !ok {
				if ctx.Err() == nil {
					r.failStream(key, stream, subscription.Err())
				}
				return
			}
			r.notify(ctx, key, stream, event)
		}
	}
}

func (r *resourceSubscriptions) failStream(key resourceSubscriptionKey, stream *resourceEventStream, err error) {
	if err == nil {
		err = errors.New("middleware event stream ended unexpectedly")
	}
	cause := fmt.Errorf("%w: %s: %w", errResourceEventStream, key.source, err)
	r.mu.Lock()
	if r.streams[key] != stream {
		r.mu.Unlock()
		return
	}
	delete(r.streams, key)
	listens := make(map[*resourceListen]bool)
	legacy := make(map[*mcp.ServerSession]bool)
	for subscriber := range stream.listeners {
		if subscriber.listen == nil {
			legacy[subscriber.session] = true
		} else {
			listens[subscriber.listen] = true
		}
	}
	r.mu.Unlock()
	for listen := range listens {
		listen.cancel(cause)
	}
	for session := range legacy {
		go func() { _ = session.Close() }()
	}
}

func (r *resourceSubscriptions) notify(ctx context.Context, key resourceSubscriptionKey, stream *resourceEventStream, event json.RawMessage) {
	var job struct {
		ID int64 `json:"id"`
	}
	if key.source == "core.get_jobs" && (json.Unmarshal(event, &job) != nil || job.ID <= 0) {
		return
	}
	r.mu.Lock()
	uris := make(map[string]bool)
	if r.streams[key] == stream {
		for subscriber, id := range stream.listeners {
			if id == 0 || id == job.ID {
				uris[subscriber.uri] = true
			}
		}
	}
	r.mu.Unlock()
	for uri := range uris {
		if ctx.Err() != nil {
			return
		}
		_ = r.server.ResourceUpdated(ctx, &mcp.ResourceUpdatedNotificationParams{URI: uri})
	}
}
