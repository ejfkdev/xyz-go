// Package errors defines the error taxonomy shared by every frontend of the
// kit: one coded error drives the CLI exit code, the HTTP status code, and
// the MCP JSON-RPC error code alike, so transport implementations never need
// to interpret command-specific error strings.
//
// Two layers of identity ride on one error:
//
//   - Kind  — the coarse taxonomy (invalid_input, not_found, …). It is what
//     frontends translate into an HTTP status / CLI exit code / JSON-RPC
//     code. Stable and small; frontends switch on it.
//   - Code  — an optional free-form business identifier ("USER_NOT_FOUND",
//     "QUOTA_EXCEEDED"). It never affects transport mapping; it is carried
//     verbatim to the caller so clients can branch on domain semantics
//     without parsing messages.
//
// On top of those, an error may carry Detail (structured key/value context)
// and an explicit Status override (HTTP only). The simplest possible handler
// error — errors.New("boom") or fmt.Errorf(...) — needs none of this: it is
// classified KindInternal and rendered with its message. Richness is opt-in
// and composes:
//
//	return errors.New("boom")                                  // 最简
//	return errs.NotFound("user %s", id)                        // 带 Kind
//	return errs.NotFound("user %s", id).                       // 带业务标识符
//	       WithCode("USER_NOT_FOUND").WithDetail("user_id", id)
//	return errs.Wrap(errs.KindUnavailable, err).WithCode("DB_DOWN")
package errors

import (
	"errors"
	"fmt"
)

// Kind classifies an error. Frontends translate a Kind into their own error
// representation via HTTPStatus, ExitCode and JSONRPCCode.
type Kind string

const (
	// KindInvalidInput means the caller provided malformed or invalid
	// arguments (missing required fields, failed validation, bad enums).
	KindInvalidInput Kind = "invalid_input"
	// KindUnauthorized means the caller must authenticate before retrying.
	KindUnauthorized Kind = "unauthorized"
	// KindForbidden means the caller authenticated but lacks permission.
	KindForbidden Kind = "forbidden"
	// KindNotFound means the target of the operation does not exist.
	KindNotFound Kind = "not_found"
	// KindConflict means the operation collides with existing state.
	KindConflict Kind = "conflict"
	// KindCanceled means the operation was canceled by the caller.
	KindCanceled Kind = "canceled"
	// KindUnavailable means a dependency is temporarily down.
	KindUnavailable Kind = "unavailable"
	// KindInternal is the fallback for unclassified failures.
	KindInternal Kind = "internal"
	// KindNone is what Classify returns for a nil error.
	KindNone Kind = ""
)

// CodedError wraps a cause with a Kind, an optional message, an optional
// business Code, structured Detail and an optional HTTP Status override.
// Its methods are chainable and return the same pointer, so a handler can
// build a rich error in one expression.
type CodedError struct {
	Kind    Kind
	Message string
	// Code is a free-form business identifier (e.g. "USER_NOT_FOUND").
	// It is carried verbatim to callers and never affects transport mapping.
	Code string
	// Detail is structured context attached to the error. It is rendered
	// into the HTTP error body and the MCP result _meta as-is.
	Detail map[string]any
	// Status overrides the HTTP status derived from Kind (0 = derive).
	// It only affects the HTTP frontend.
	Status int

	cause error
}

func (e *CodedError) Error() string {
	switch {
	case e.Message != "" && e.cause != nil:
		return fmt.Sprintf("%s: %s", e.Message, e.cause)
	case e.Message != "":
		return e.Message
	case e.cause != nil:
		return e.cause.Error()
	default:
		return string(e.Kind)
	}
}

// Unwrap exposes the cause for errors.Is / errors.As.
func (e *CodedError) Unwrap() error { return e.cause }

// WithCode attaches a free-form business identifier. Chainable.
func (e *CodedError) WithCode(code string) *CodedError {
	e.Code = code
	return e
}

// WithDetail attaches one structured detail entry. Chainable; a nil Detail
// map is created on first use.
func (e *CodedError) WithDetail(key string, value any) *CodedError {
	if e.Detail == nil {
		e.Detail = map[string]any{}
	}
	e.Detail[key] = value
	return e
}

// WithDetails merges a batch of structured detail entries. Chainable.
func (e *CodedError) WithDetails(m map[string]any) *CodedError {
	if len(m) == 0 {
		return e
	}
	if e.Detail == nil {
		e.Detail = make(map[string]any, len(m))
	}
	for k, v := range m {
		e.Detail[k] = v
	}
	return e
}

// WithStatus overrides the HTTP status derived from Kind (HTTP only).
// Chainable.
func (e *CodedError) WithStatus(status int) *CodedError {
	e.Status = status
	return e
}

// WithCause attaches (or replaces) the underlying cause. Chainable.
func (e *CodedError) WithCause(err error) *CodedError {
	e.cause = err
	return e
}

// Err builds a chainable *CodedError with a formatted message and no cause.
// It is the constructor meant for one-expression rich errors:
//
//	return errs.Err(errs.KindConflict, "%s exists", name).WithCode("DUPLICATE")
func Err(kind Kind, format string, a ...any) *CodedError {
	return &CodedError{Kind: kind, Message: fmt.Sprintf(format, a...)}
}

// Per-Kind shortcuts. Each returns a chainable *CodedError so the common
// "kind + message + optional code/detail" reads in one line.
func InvalidInput(format string, a ...any) *CodedError { return Err(KindInvalidInput, format, a...) }
func Unauthorized(format string, a ...any) *CodedError { return Err(KindUnauthorized, format, a...) }
func Forbidden(format string, a ...any) *CodedError    { return Err(KindForbidden, format, a...) }
func NotFound(format string, a ...any) *CodedError     { return Err(KindNotFound, format, a...) }
func Conflict(format string, a ...any) *CodedError     { return Err(KindConflict, format, a...) }
func Canceled(format string, a ...any) *CodedError     { return Err(KindCanceled, format, a...) }
func Unavailable(format string, a ...any) *CodedError  { return Err(KindUnavailable, format, a...) }
func Internal(format string, a ...any) *CodedError     { return Err(KindInternal, format, a...) }

// New builds a coded error with no cause.
func New(kind Kind, msg string) error {
	return &CodedError{Kind: kind, Message: msg}
}

// Errorf builds a coded error with a formatted message.
func Errorf(kind Kind, format string, a ...any) error {
	return &CodedError{Kind: kind, Message: fmt.Sprintf(format, a...)}
}

// Wrap builds a coded error whose message is the cause's.
func Wrap(kind Kind, cause error) error {
	return &CodedError{Kind: kind, cause: cause}
}

// WrapMsg builds a coded error with both a message and a cause.
func WrapMsg(kind Kind, cause error, msg string) error {
	return &CodedError{Kind: kind, Message: msg, cause: cause}
}

// Cause unwraps coded errors and returns the innermost known cause.
func Cause(err error) error {
	for err != nil {
		var ce *CodedError
		if !errors.As(err, &ce) {
			return err
		}
		err = ce.cause
	}
	return nil
}

// From extracts the first *CodedError in the chain, or nil when the error
// carries no coded layer (a plain errors.New / fmt.Errorf). Frontends use it
// to read Code/Detail/Status without re-walking the chain.
func From(err error) *CodedError {
	if err == nil {
		return nil
	}
	var ce *CodedError
	if errors.As(err, &ce) {
		return ce
	}
	return nil
}

// Code returns the business identifier of the first coded layer, or "".
func Code(err error) string {
	if ce := From(err); ce != nil {
		return ce.Code
	}
	return ""
}

// Detail returns the structured detail of the first coded layer (nil if none).
func Detail(err error) map[string]any {
	if ce := From(err); ce != nil {
		return ce.Detail
	}
	return nil
}

// Classify walks the error chain and returns the first coded Kind.
// Unclassified non-nil errors fall back to KindInternal; a nil error is
// KindNone.
func Classify(err error) Kind {
	if err == nil {
		return KindNone
	}
	var ce *CodedError
	if errors.As(err, &ce) {
		return ce.Kind
	}
	return KindInternal
}

// HTTPStatus maps a Kind to its natural HTTP status code.
func HTTPStatus(k Kind) int {
	switch k {
	case KindInvalidInput:
		return 400
	case KindUnauthorized:
		return 401
	case KindForbidden:
		return 403
	case KindNotFound:
		return 404
	case KindConflict:
		return 409
	case KindUnavailable:
		return 503
	case KindCanceled:
		return 499 // non-standard but commonly understood; frontends may override
	default:
		return 500
	}
}

// StatusFor returns the HTTP status for an error: the explicit Status
// override when set, otherwise the status derived from its Kind. A non-coded
// error classifies as KindInternal (500).
func StatusFor(err error) int {
	if ce := From(err); ce != nil && ce.Status > 0 {
		return ce.Status
	}
	return HTTPStatus(Classify(err))
}

// ExitCode maps a Kind to a CLI process exit code.
func ExitCode(k Kind) int {
	if k == KindInvalidInput {
		return 2 // mirrors conventional flag-parse failures
	}
	return 1
}

// JSONRPCCode maps a Kind to a JSON-RPC 2.0 error code. Standard codes are
// negative; application-defined server errors live in the -32000..-32099
// range both JSON-RPC and MCP reserve for this purpose.
func JSONRPCCode(k Kind) int {
	switch k {
	case KindInvalidInput:
		return -32602 // Invalid params
	case KindNotFound:
		return -32001
	case KindConflict:
		return -32009
	case KindUnauthorized:
		return -32010
	case KindForbidden:
		return -32011
	case KindCanceled:
		return -32012
	default:
		return -32603 // Internal error
	}
}

// RPCCodeFor returns the JSON-RPC error code for an error (Kind-derived).
func RPCCodeFor(err error) int { return JSONRPCCode(Classify(err)) }

// Body is the enriched machine-readable form of an error, shared by every
// frontend so HTTP bodies, CLI machine-mode stderr and MCP _meta agree on
// one shape. The flat Error string keeps the pre-v0.4.2 HTTP contract
// ({"error":"<message>"}) intact for existing clients; Kind/Code/Detail are
// additive.
type Body struct {
	Error  string         `json:"error"`
	Kind   Kind           `json:"kind,omitempty"`
	Code   string         `json:"code,omitempty"`
	Detail map[string]any `json:"detail,omitempty"`
}

// ErrorBody projects any error into the shared Body. A nil error yields the
// zero Body. A plain (non-coded) error yields just its message under
// KindInternal.
func ErrorBody(err error) Body {
	if err == nil {
		return Body{}
	}
	// Mirror the frontends' long-standing causeMessage semantics: when a
	// cause exists, its message is the human-facing text (the coded layer's
	// own message, if any, is still reachable via Error()). Rich context
	// rides on Kind/Code/Detail rather than the message string.
	msg := err.Error()
	if cause := Cause(err); cause != nil {
		msg = cause.Error()
	}
	b := Body{Error: msg, Kind: Classify(err)}
	if ce := From(err); ce != nil {
		b.Code = ce.Code
		b.Detail = ce.Detail
	}
	return b
}
