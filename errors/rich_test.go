package errors

import (
	"errors"
	"testing"
)

// 富化错误上下文：Code（业务标识符）/ Detail（结构化详情）/ Status（HTTP 覆盖）
// 与链式构造器、提取器、共享 ErrorBody。

func TestRichErrorContext(t *testing.T) {
	err := NotFound("user %s", "42").
		WithCode("USER_NOT_FOUND").
		WithDetail("user_id", 42).
		WithStatus(410)
	if got := Code(err); got != "USER_NOT_FOUND" {
		t.Fatalf("Code = %q", got)
	}
	if got := Detail(err)["user_id"]; got != 42 {
		t.Fatalf("Detail[user_id] = %v", got)
	}
	if got := StatusFor(err); got != 410 {
		t.Fatalf("StatusFor (override) = %d, want 410", got)
	}
	if got := Classify(err); got != KindNotFound {
		t.Fatalf("Classify = %q", got)
	}
	body := ErrorBody(err)
	if body.Error != "user 42" || body.Kind != KindNotFound || body.Code != "USER_NOT_FOUND" {
		t.Fatalf("ErrorBody = %+v", body)
	}
	if body.Detail["user_id"] != 42 {
		t.Fatalf("ErrorBody.Detail = %v", body.Detail)
	}
}

func TestStatusForDerivesWithoutOverride(t *testing.T) {
	if got := StatusFor(New(KindNotFound, "x")); got != 404 {
		t.Fatalf("StatusFor(not_found) = %d, want 404", got)
	}
	if got := StatusFor(errors.New("plain")); got != 500 {
		t.Fatalf("StatusFor(plain) = %d, want 500", got)
	}
}

func TestErrorBodyPlainAndWrapped(t *testing.T) {
	// 纯标准库错误：internal + 消息，无 code/detail。
	b := ErrorBody(errors.New("boom"))
	if b.Error != "boom" || b.Kind != KindInternal || b.Code != "" || b.Detail != nil {
		t.Fatalf("plain ErrorBody = %+v", b)
	}
	// Wrap：消息取最内层 cause（与前端历史 causeMessage 行为逐字一致）。
	w := Wrap(KindUnavailable, errors.New("db down"))
	bw := ErrorBody(w)
	if bw.Error != "db down" || bw.Kind != KindUnavailable {
		t.Fatalf("wrapped ErrorBody = %+v", bw)
	}
	// nil 错误 → 零 Body。
	if got := ErrorBody(nil); got.Error != "" || got.Kind != KindNone {
		t.Fatalf("nil ErrorBody = %+v", got)
	}
}

func TestFromAndChainBuilders(t *testing.T) {
	e := Err(KindConflict, "dup").WithCode("DUP").WithDetails(map[string]any{"a": 1, "b": 2})
	if From(e) != e {
		t.Fatalf("From should return the coded layer")
	}
	if e.Detail["a"] != 1 || e.Detail["b"] != 2 {
		t.Fatalf("WithDetails = %v", e.Detail)
	}
	cause := errors.New("root")
	e2 := Err(KindInternal, "wrap").WithCause(cause)
	if Cause(e2) != cause {
		t.Fatalf("WithCause/Cause mismatch")
	}
	if From(nil) != nil || Code(nil) != "" || Detail(nil) != nil {
		t.Fatalf("nil extraction should be empty")
	}
}

func TestKindShortcuts(t *testing.T) {
	cases := []struct {
		err  *CodedError
		kind Kind
	}{
		{InvalidInput("x"), KindInvalidInput},
		{Unauthorized("x"), KindUnauthorized},
		{Forbidden("x"), KindForbidden},
		{NotFound("x"), KindNotFound},
		{Conflict("x"), KindConflict},
		{Canceled("x"), KindCanceled},
		{Unavailable("x"), KindUnavailable},
		{Internal("x"), KindInternal},
	}
	for _, c := range cases {
		if c.err.Kind != c.kind {
			t.Fatalf("shortcut kind = %q, want %q", c.err.Kind, c.kind)
		}
	}
}

func TestRPCCodeFor(t *testing.T) {
	if got := RPCCodeFor(New(KindNotFound, "x")); got != -32001 {
		t.Fatalf("RPCCodeFor(not_found) = %d", got)
	}
	if got := RPCCodeFor(errors.New("plain")); got != -32603 {
		t.Fatalf("RPCCodeFor(plain) = %d, want internal -32603", got)
	}
}
