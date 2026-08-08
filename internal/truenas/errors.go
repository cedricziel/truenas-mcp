package truenas

import (
	"errors"
	"fmt"
)

// The connection layer distinguishes these because a caller acts differently
// on each: an unreachable host is an operations problem, a rejected credential
// is the user's to fix, an authorization failure means the key is real but too
// weak, and a rate limit means waiting rather than retrying.
var (
	// ErrUnreachable means the target could not be contacted at all.
	ErrUnreachable = errors.New("target unreachable")

	// ErrUnauthenticated means the credential was rejected.
	ErrUnauthenticated = errors.New("authentication failed")

	// ErrExpired means the credential was recognised but has expired.
	ErrExpired = errors.New("credential expired")

	// ErrOTPRequired means the credential cannot complete login unattended.
	ErrOTPRequired = errors.New("one-time password required")

	// ErrRateLimited means the target refused the attempt for rate limiting.
	// Retrying immediately prolongs it.
	ErrRateLimited = errors.New("authentication rate limit reached")

	// ErrUnauthorized means the credential is valid but lacks the privilege
	// the operation requires.
	ErrUnauthorized = errors.New("not authorized for this operation")

	// ErrInterrupted means the connection dropped with a request in flight,
	// so whether the operation was applied on the target is unknown.
	ErrInterrupted = errors.New("connection interrupted with a request in flight")

	// ErrClosed means the client has been closed.
	ErrClosed = errors.New("client closed")
)

// CallError is a failure the target itself reported, as distinct from a
// failure of the server's own gating or transport. Keeping the distinction
// visible matters: a caller should be able to tell "TrueNAS said no" from
// "this server would not ask".
type CallError struct {
	Method  string
	Code    int
	Message string
	Data    any
}

func (e *CallError) Error() string {
	return fmt.Sprintf("%s: target reported error %d: %s", e.Method, e.Code, e.Message)
}

// Unwrap maps the target's error codes onto the sentinels above so callers can
// use errors.Is rather than matching on numbers.
func (e *CallError) Unwrap() error {
	switch e.Code {
	case codeNotAuthenticated:
		return ErrUnauthenticated
	case codeAccessDenied:
		return ErrUnauthorized
	}
	return nil
}

// JSON-RPC error codes the middleware uses beyond the standard set.
const (
	codeNotAuthenticated = -32000
	codeAccessDenied     = -32001
)
