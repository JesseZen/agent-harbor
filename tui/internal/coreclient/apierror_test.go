package coreclient

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
)

func TestAPIErrorAndProblemHelpers(t *testing.T) {
	err := &APIError{Operation: "getConfig", StatusCode: 500, Code: "operation_error"}
	if !strings.Contains(err.Error(), "getConfig") || !strings.Contains(err.Error(), "operation_error") {
		t.Fatalf("Error = %q", err.Error())
	}
	plain := &APIError{Operation: "getConfig", StatusCode: 500}
	if !strings.Contains(plain.Error(), "HTTP 500") {
		t.Fatalf("plain Error = %q", plain.Error())
	}

	phase := &generated.Phase04Problem{Code: generated.OperationError}
	if got := problemCode(nil, phase); got != string(generated.OperationError) {
		t.Fatalf("problemCode = %q", got)
	}
	if got := problemCode(nil, nil); got != "" {
		t.Fatalf("empty problemCode = %q", got)
	}

	problem := &generated.Problem{Code: "generation_conflict"}
	if got := problemCodeString(nil, problem); got != "generation_conflict" {
		t.Fatalf("problemCodeString = %q", got)
	}
	if got := problemCodeString(nil); got != "" {
		t.Fatalf("empty problemCodeString = %q", got)
	}

	resp := &generated.GetConfigResponse{ApplicationproblemJSONDefault: problem}
	selected := valueOrNil(resp, func(value *generated.GetConfigResponse) *generated.Problem {
		return value.ApplicationproblemJSONDefault
	})
	if selected == nil || selected.Code != "generation_conflict" {
		t.Fatalf("valueOrNil = %#v", selected)
	}
	if valueOrNil((*generated.GetConfigResponse)(nil), func(value *generated.GetConfigResponse) *generated.Problem {
		return value.ApplicationproblemJSONDefault
	}) != nil {
		t.Fatal("nil valueOrNil should return nil")
	}

	if responseError("getConfig", 200, "") == nil {
		t.Fatal("expected protocol error for empty success body")
	}
	if responseError("getConfig", 0, "") == nil {
		t.Fatal("expected protocol error for zero status")
	}
	apiErr := responseError("getConfig", 409, "generation_conflict")
	if apiErr == nil {
		t.Fatal("expected APIError")
	}
	var typed *APIError
	if !errors.As(apiErr, &typed) || typed.StatusCode != 409 || typed.Code != "generation_conflict" {
		t.Fatalf("APIError = %#v", apiErr)
	}

	if actionResponseError("launch", 0) == nil || actionResponseError("launch", 200) == nil || actionResponseError("launch", 201) == nil || actionResponseError("launch", 204) == nil {
		t.Fatal("expected protocol errors for empty success statuses")
	}
	if actionResponseError("launch", 409) == nil {
		t.Fatal("expected APIError for launch conflict")
	}
	if _, err := NewUnixBackend(context.Background(), Options{}); !errors.Is(err, ErrInstanceMismatch) {
		t.Fatalf("missing instance id = %v", err)
	}
	if _, err := NewUnixBackend(context.Background(), Options{ExpectedInstanceID: "ins"}); !errors.Is(err, ErrProtocolIncompatible) {
		t.Fatalf("missing protocol = %v", err)
	}
}
