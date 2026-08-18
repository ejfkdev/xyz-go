package errors

import (
	"errors"
	"fmt"
	"testing"
)

func TestClassify(t *testing.T) {
	if got := Classify(nil); got != KindNone {
		t.Fatalf("Classify(nil) = %q, want kind none", got)
	}
	if got := Classify(fmt.Errorf("plain")); got != KindInternal {
		t.Fatalf("plain error classified as %q, want internal", got)
	}
	if got := Classify(Wrap(KindNotFound, fmt.Errorf("x"))); got != KindNotFound {
		t.Fatalf("wrapped error classified as %q, want not_found", got)
	}
	var ce *CodedError
	if !errors.As(WrapMsg(KindConflict, fmt.Errorf("inner"), "outer"), &ce) {
		t.Fatal("errors.As should find CodedError")
	}
	if ce.Kind != KindConflict || ce.Message != "outer" {
		t.Fatalf("coded error = %+v", ce)
	}
}

func TestUnwrapChain(t *testing.T) {
	inner := fmt.Errorf("inner")
	err := Wrap(KindNotFound, inner)
	if Cause(err) != inner {
		t.Fatal("Cause should reach the innermost cause")
	}
}

func TestMappings(t *testing.T) {
	if HTTPStatus(KindInvalidInput) != 400 {
		t.Fatal("invalid_input must map to 400")
	}
	if HTTPStatus(KindNotFound) != 404 {
		t.Fatal("not_found must map to 404")
	}
	if HTTPStatus(KindInternal) != 500 {
		t.Fatal("internal must map to 500")
	}
	if ExitCode(KindInvalidInput) != 2 {
		t.Fatal("invalid_input exit code must be 2")
	}
	if ExitCode(KindInternal) != 1 {
		t.Fatal("internal exit code must be 1")
	}
	if JSONRPCCode(KindInvalidInput) != -32602 {
		t.Fatal("invalid_input must map to -32602")
	}
	if JSONRPCCode(KindNotFound) != -32001 {
		t.Fatal("not_found must map to -32001")
	}
}
