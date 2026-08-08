package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/cedricziel/truenas-mcp/internal/truenas"
)

// ErrNoCredential means the caller supplied no TrueNAS API key. The server
// holds none of its own and never falls back to one, so this is fatal to the
// session rather than a condition to work around.
var ErrNoCredential = errors.New(
	"no TrueNAS API key supplied: send it as `Authorization: Bearer <key>` or `X-TrueNAS-API-Key: <key>`")

// APIKeyHeader is accepted for clients that cannot set an Authorization header.
const APIKeyHeader = "X-TrueNAS-API-Key"

// CredentialFromRequest extracts the caller's TrueNAS API key.
//
// Errors never include the header value: a misconfigured client puts its key
// in the wrong place, and an error that echoes it would move the secret into
// logs.
func CredentialFromRequest(r *http.Request) (string, error) {
	if v := strings.TrimSpace(r.Header.Get(APIKeyHeader)); v != "" {
		return v, nil
	}

	auth := r.Header.Get("Authorization")
	if scheme, value, found := strings.Cut(auth, " "); found &&
		strings.EqualFold(strings.TrimSpace(scheme), "bearer") {
		if key := strings.TrimSpace(value); key != "" {
			return key, nil
		}
	}

	return "", ErrNoCredential
}

// Session is one caller's authenticated connection to the middleware.
//
// It is established once and reused for the session's lifetime rather than
// per tool call, because the target rate-limits authentication attempts.
type Session struct {
	client *truenas.Client
}

// Client returns the middleware connection this session's calls run on. Every
// call executes under the caller's own credential, so a session can never
// reach capability its own key does not permit.
func (s *Session) Client() *truenas.Client { return s.client }

// Close releases the session's middleware connection.
func (s *Session) Close() error {
	if s.client == nil {
		return nil
	}
	return s.client.Close()
}

// SessionManager establishes and tracks per-caller middleware connections.
//
// Connections are never shared between callers and never reused across
// credentials: two callers with different keys get two connections, each
// bounded by its own user's privileges.
type SessionManager struct {
	targetURL string
	insecure  bool

	mu       sync.Mutex
	sessions map[string]*Session
}

// NewSessionManager returns a manager for the given middleware endpoint.
func NewSessionManager(targetURL string, insecureSkipVerify bool) *SessionManager {
	return &SessionManager{
		targetURL: targetURL,
		insecure:  insecureSkipVerify,
		sessions:  map[string]*Session{},
	}
}

// Open authenticates a new session with the caller's key.
//
// A failure here is not retried: the target permits a bounded number of
// authentication attempts per minute, and retrying a rejected key would spend
// that budget on an attempt that cannot succeed.
func (m *SessionManager) Open(ctx context.Context, apiKey string) (*Session, error) {
	client, err := truenas.Dial(ctx, truenas.Options{
		URL:                m.targetURL,
		InsecureSkipVerify: m.insecure,
	})
	if err != nil {
		return nil, err
	}

	if err := client.Login(ctx, apiKey); err != nil {
		_ = client.Close()
		return nil, err
	}

	// Refuse a target too old for the versioned JSON-RPC API rather than
	// failing obscurely on the first method that does not exist there.
	if _, err := client.CheckVersion(ctx); err != nil {
		_ = client.Close()
		return nil, err
	}

	return &Session{client: client}, nil
}

// Track stores a session under an opaque id so it can be closed later.
func (m *SessionManager) Track(id string, s *Session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[id] = s
}

// Release closes and forgets a tracked session.
func (m *SessionManager) Release(id string) {
	m.mu.Lock()
	s, ok := m.sessions[id]
	delete(m.sessions, id)
	m.mu.Unlock()

	if ok {
		_ = s.Close()
	}
}

// NewSessionProvider returns a sessionFor that opens the middleware
// connection for apiKey lazily on first use and re-establishes it whenever
// the existing one has gone dead.
//
// The target restarts, networks blip, and middleware connections are
// long-lived. Re-establish rather than handing back a dead socket -- but
// only when the existing one is genuinely gone, since the target
// rate-limits authentication.
//
// id is the identifier the session is tracked under, so sessions can close
// it later via Release or CloseAll. A caller that wants the connection
// opened immediately -- to fail fast on a bad credential, say, rather than on
// the first tool call -- can simply call the returned function once itself;
// every call after that goes through the same lazy-open-then-reconnect path.
// Both the HTTP transport (one provider per caller credential) and the stdio
// transport (one provider for the process's single credential) share this.
func NewSessionProvider(sessions *SessionManager, id, apiKey string) func(context.Context) (*Session, error) {
	var (
		once    sync.Once
		mu      sync.Mutex
		sess    *Session
		sessErr error
	)
	return func(ctx context.Context) (*Session, error) {
		once.Do(func() {
			sess, sessErr = sessions.Open(ctx, apiKey)
			if sessErr == nil {
				sessions.Track(id, sess)
			}
		})
		if sessErr != nil {
			return nil, sessErr
		}

		mu.Lock()
		defer mu.Unlock()
		if sess != nil && !sess.Client().Alive() {
			replacement, err := sessions.Open(ctx, apiKey)
			if err != nil {
				return nil, err
			}
			_ = sess.Close()
			sess = replacement
			sessions.Track(id, sess)
		}
		return sess, nil
	}
}

// CloseAll releases every tracked session.
func (m *SessionManager) CloseAll() {
	m.mu.Lock()
	sessions := m.sessions
	m.sessions = map[string]*Session{}
	m.mu.Unlock()

	for _, s := range sessions {
		_ = s.Close()
	}
}
