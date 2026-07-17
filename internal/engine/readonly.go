package engine

import "errors"

// ErrReadOnly is returned when a write is attempted against a connection
// profile marked read-only (Safe Mode). Callers that perform a mutating
// operation — Execute, Insert/Update/Delete, index or namespace changes —
// must check RequireWritable before dispatching to a Session, so the
// restriction is enforced once, centrally, in Go, rather than only by
// disabling a button in the frontend.
var ErrReadOnly = errors.New("this connection is marked read-only; writes are disabled")

// RequireWritable returns ErrReadOnly if cfg is marked read-only, nil
// otherwise.
func RequireWritable(cfg ConnConfig) error {
	if cfg.ReadOnly {
		return ErrReadOnly
	}
	return nil
}
