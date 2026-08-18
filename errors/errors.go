// Package errors defines the error taxonomy shared by every frontend of the
// kit: one coded error drives the CLI exit code, the HTTP status code, and
// the MCP JSON-RPC error code alike, so transport implementations never need
// to interpret command-specific error strings.
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

// CodedError wraps a cause with a Kind and an optional message.
type CodedError struct {
	Kind    Kind
	Message string
	cause   error
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
