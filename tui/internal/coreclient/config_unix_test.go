//go:build unix

package coreclient

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
)

func TestUnixBackendLoadConfig(t *testing.T) {
	socketPath := shortSocketPath(t)
	snapshot := generated.SnapshotIdentity{
		ConfigRevision: "cfg-load",
		Generation:     11,
		PublishedAt:    time.Unix(1_700_000_000, 0).UTC(),
	}
	identity := testIdentity(socketPath, testInstanceID, AdminProtocolVersion)

	serveUnixHTTP(t, socketPath, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/readiness":
			writeJSON(t, writer, generated.Readiness{Ready: true, Identity: identity, Snapshot: snapshot})
		case "/v1/config":
			writeJSON(t, writer, generated.MutableConfigSnapshot{
				ActiveGeneration: snapshot.Generation,
				ConfigRevision:   snapshot.ConfigRevision,
				InstanceId:       testInstanceID,
				MutableConfig: generated.MutableConfigView{
					Instance: generated.MutableInstanceConfig{LogLevel: generated.MutableInstanceConfigLogLevelSimple},
				},
			})
		default:
			http.NotFound(writer, request)
		}
	}))

	client, err := NewUnixBackend(context.Background(), Options{
		SocketPath:              socketPath,
		ExpectedInstanceID:      testInstanceID,
		ExpectedProtocolVersion: AdminProtocolVersion,
	})
	if err != nil {
		t.Fatalf("NewUnixBackend: %v", err)
	}
	t.Cleanup(client.Close)

	cfg, err := client.LoadConfig(context.Background())
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.ActiveGeneration != snapshot.Generation || cfg.ConfigRevision != snapshot.ConfigRevision {
		t.Fatalf("config = gen %d rev %q, want gen %d rev %q", cfg.ActiveGeneration, cfg.ConfigRevision, snapshot.Generation, snapshot.ConfigRevision)
	}
}

func TestUnixBackendValidateConfig(t *testing.T) {
	socketPath := shortSocketPath(t)
	identity := testIdentity(socketPath, testInstanceID, AdminProtocolVersion)
	serveUnixHTTP(t, socketPath, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/readiness":
			writeJSON(t, writer, generated.Readiness{Ready: true, Identity: identity})
		case "/v1/config/validate":
			writeJSON(t, writer, generated.ValidationResult{Valid: true})
		default:
			http.NotFound(writer, request)
		}
	}))

	client, err := NewUnixBackend(context.Background(), Options{
		SocketPath:              socketPath,
		ExpectedInstanceID:      testInstanceID,
		ExpectedProtocolVersion: AdminProtocolVersion,
	})
	if err != nil {
		t.Fatalf("NewUnixBackend: %v", err)
	}
	t.Cleanup(client.Close)

	result, err := client.ValidateConfig(context.Background(), generated.ValidateConfigCommand{
		InstanceId: testInstanceID,
	})
	if err != nil {
		t.Fatalf("ValidateConfig: %v", err)
	}
	if !result.Valid {
		t.Fatalf("validation = %#v", result)
	}
}

func TestUnixBackendPatchConfig(t *testing.T) {
	socketPath := shortSocketPath(t)
	identity := testIdentity(socketPath, testInstanceID, AdminProtocolVersion)
	expected := generated.SnapshotIdentity{Generation: 12, ConfigRevision: "cfg-patch"}
	serveUnixHTTP(t, socketPath, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/readiness":
			writeJSON(t, writer, generated.Readiness{Ready: true, Identity: identity})
		case "/v1/config":
			if request.Method == http.MethodPatch {
				writeJSON(t, writer, expected)
				return
			}
			http.NotFound(writer, request)
		default:
			http.NotFound(writer, request)
		}
	}))

	client, err := NewUnixBackend(context.Background(), Options{
		SocketPath:              socketPath,
		ExpectedInstanceID:      testInstanceID,
		ExpectedProtocolVersion: AdminProtocolVersion,
	})
	if err != nil {
		t.Fatalf("NewUnixBackend: %v", err)
	}
	t.Cleanup(client.Close)

	result, err := client.PatchConfig(context.Background(), generated.PatchConfigCommand{
		InstanceId: testInstanceID,
	})
	if err != nil {
		t.Fatalf("PatchConfig: %v", err)
	}
	if result.Generation != expected.Generation || result.ConfigRevision != expected.ConfigRevision {
		t.Fatalf("patch result = gen %d rev %q, want gen %d rev %q", result.Generation, result.ConfigRevision, expected.Generation, expected.ConfigRevision)
	}
}

func TestUnixBackendConfigErrorsPreserveProblemIdentity(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		code       string
		path       string
		message    string
		invoke     func(context.Context, *Client) error
		operation  string
		requestURL string
		protocol   bool
	}{
		{
			name:       "load empty success body",
			status:     http.StatusOK,
			operation:  "getConfig",
			requestURL: "/v1/config",
			protocol:   true,
			invoke: func(ctx context.Context, client *Client) error {
				_, err := client.LoadConfig(ctx)
				return err
			},
		},
		{
			name:       "validate rejected",
			status:     http.StatusUnprocessableEntity,
			code:       "null_field",
			path:       "mutable_config.backend_sets",
			message:    "must be an array",
			operation:  "validateConfig",
			requestURL: "/v1/config/validate",
			invoke: func(ctx context.Context, client *Client) error {
				_, err := client.ValidateConfig(ctx, generated.ValidateConfigCommand{InstanceId: testInstanceID})
				return err
			},
		},
		{
			name:       "patch cleanup pending",
			status:     http.StatusServiceUnavailable,
			code:       "secret_cleanup_pending",
			operation:  "patchConfig",
			requestURL: "/v1/config",
			invoke: func(ctx context.Context, client *Client) error {
				_, err := client.PatchConfig(ctx, generated.PatchConfigCommand{InstanceId: testInstanceID})
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			socketPath := shortSocketPath(t)
			identity := testIdentity(socketPath, testInstanceID, AdminProtocolVersion)
			serveUnixHTTP(t, socketPath, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/v1/readiness" {
					writeJSON(t, writer, generated.Readiness{Ready: true, Identity: identity})
					return
				}
				if request.URL.Path != test.requestURL {
					http.NotFound(writer, request)
					return
				}
				if test.code == "" {
					writer.WriteHeader(test.status)
					return
				}
				writer.Header().Set("Content-Type", "application/problem+json")
				problem := generated.Problem{
					Code: test.code, RequestId: "req_config_error", Status: test.status,
					Title: "Config request failed", Type: "about:blank",
				}
				if test.path != "" || test.message != "" {
					violations := []generated.FieldViolation{{Code: test.code, Path: test.path, Message: test.message}}
					problem.Violations = &violations
				}
				writeJSONStatus(t, writer, test.status, problem)
			}))

			client, err := NewUnixBackend(context.Background(), Options{
				SocketPath:              socketPath,
				ExpectedInstanceID:      testInstanceID,
				ExpectedProtocolVersion: AdminProtocolVersion,
			})
			if err != nil {
				t.Fatalf("NewUnixBackend: %v", err)
			}
			t.Cleanup(client.Close)

			err = test.invoke(context.Background(), client)
			if err == nil {
				t.Fatal("expected config request error")
			}
			if test.protocol {
				if !errors.Is(err, ErrProtocolIncompatible) {
					t.Fatalf("error = %v, want ErrProtocolIncompatible", err)
				}
				return
			}
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("error type = %T, want *APIError: %v", err, err)
			}
			if apiErr.Operation != test.operation || apiErr.StatusCode != test.status || apiErr.Code != test.code {
				t.Fatalf("APIError = %#v, want operation=%q status=%d code=%q", apiErr, test.operation, test.status, test.code)
			}
			if test.path != "" && (!strings.Contains(apiErr.Detail, test.path) || !strings.Contains(apiErr.Detail, test.message)) {
				t.Fatalf("APIError detail = %q, want path %q and message %q", apiErr.Detail, test.path, test.message)
			}
		})
	}
}
