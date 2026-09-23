// Package broker is the local service that sits between AI agents and the
// user's databases. It has two doors with two different tokens:
//
//   - the agent door (`dbhelm agent ...`): reads run immediately through
//     internal/guard; anything that changes data becomes a request that only
//     DBHelm can approve;
//   - the operator door (the VS Code extension): the only place a request is
//     approved or refused, and the only place Autopilot is switched on.
//
// An agent can never approve its own request because it does not hold the
// operator token, and the broker fails closed: with no operator attached, a
// write request is refused rather than queued forever or run unattended.
package broker

import (
	"encoding/json"
	"fmt"
	"time"
)

// ProtocolVersion is bumped whenever a method or envelope changes in a way
// an older client cannot understand; the extension checks it at startup.
const ProtocolVersion = 1

// Role identifies which door a request came through.
type Role string

const (
	RoleAgent    Role = "agent"
	RoleOperator Role = "operator"
)

// Stable, machine-readable error codes (see skill/dbhelm/references/protocol.md).
const (
	CodeUnauthorized     = "UNAUTHORIZED"
	CodeBadRequest       = "BAD_REQUEST"
	CodeUnknownMethod    = "UNKNOWN_METHOD"
	CodeNotFound         = "NOT_FOUND"
	CodeAccessOff        = "ACCESS_OFF"
	CodeReadOnly         = "READ_ONLY_VIOLATION"
	CodePendingApproval  = "PENDING_APPROVAL"
	CodeDenied           = "DENIED"
	CodeTimeout          = "TIMEOUT"
	CodeNoOperator       = "NO_OPERATOR"
	CodeFailed           = "FAILED"
	CodeUnsupported      = "UNSUPPORTED"
	CodeConnectionFailed = "CONNECTION_FAILED"
)

// Envelope is the one response shape every method returns, and what
// `dbhelm agent ... --json` prints.
type Envelope struct {
	OK    bool       `json:"ok"`
	Data  any        `json:"data,omitempty"`
	Error *ErrorBody `json:"error,omitempty"`
	Meta  *Meta      `json:"meta,omitempty"`
}

// ErrorBody describes a failure: a stable Code, a scrubbed Message, and a
// Hint on what to do next.
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

func (e *ErrorBody) Error() string { return e.Code + ": " + e.Message }

// Meta carries per-call facts that are not part of the data itself.
type Meta struct {
	Truncated bool   `json:"truncated,omitempty"`
	Rows      int    `json:"rows,omitempty"`
	MS        int64  `json:"ms,omitempty"`
	RequestID string `json:"requestId,omitempty"`
}

// Request is the body of POST /rpc.
type Request struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Info is what a running broker publishes so clients can find it.
type Info struct {
	Port          int    `json:"port"`
	PID           int    `json:"pid"`
	AgentToken    string `json:"agentToken"`
	OperatorToken string `json:"operatorToken,omitempty"` // only ever printed to the spawner's pipe, never written to disk
	Headless      bool   `json:"headless"`
	StartedAt     string `json:"startedAt"`
	Version       string `json:"version,omitempty"`
}

func errBody(code, hint, format string, args ...any) *ErrorBody {
	return &ErrorBody{Code: code, Message: fmt.Sprintf(format, args...), Hint: hint}
}

func now() time.Time { return time.Now().UTC() }
