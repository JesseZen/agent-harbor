//go:build unix

package coreclient

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/secretinput"
)

var _ secretinput.StageHTTPClient = (*Client)(nil)

func TestUnixBackendForwardsGeneratedSecretStageOperations(t *testing.T) {
	socketPath := shortSocketPath(t)
	identity := testIdentity(socketPath, testInstanceID, AdminProtocolVersion)
	expiresAt := time.Unix(1_700_000_100, 0).UTC()
	var createdBody []byte
	var deletedStage string

	serveUnixHTTP(t, socketPath, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/readiness":
			writeJSON(t, writer, generated.Readiness{Ready: true, Identity: identity})
		case "/v1/provider-secret-stages":
			if request.Method != http.MethodPost || request.Header.Get("Content-Type") != secretinput.ContentTypeOctetStream {
				http.Error(writer, "unexpected create request", http.StatusBadRequest)
				return
			}
			createdBody, _ = io.ReadAll(request.Body)
			writeJSONStatus(t, writer, http.StatusCreated, generated.ProviderSecretStage{
				StageId:   "stage_0123456789abcdef",
				ExpiresAt: expiresAt,
			})
		case "/v1/provider-secret-stages/stage_0123456789abcdef":
			deletedStage = request.URL.Path
			writer.WriteHeader(http.StatusNoContent)
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

	create, err := client.CreateProviderSecretStageWithBodyWithResponse(
		context.Background(),
		secretinput.ContentTypeOctetStream,
		bytes.NewReader([]byte("stage-only-test-value")),
	)
	if err != nil {
		t.Fatalf("create stage: %v", err)
	}
	if create == nil || create.JSON201 == nil || create.JSON201.StageId != "stage_0123456789abcdef" ||
		!create.JSON201.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("create response = %#v", create)
	}
	if string(createdBody) != "stage-only-test-value" {
		t.Fatalf("create body = %q", createdBody)
	}

	deleted, err := client.DeleteProviderSecretStageWithResponse(
		context.Background(),
		"stage_0123456789abcdef",
	)
	if err != nil {
		t.Fatalf("delete stage: %v", err)
	}
	if deleted == nil || deleted.StatusCode() != http.StatusNoContent {
		t.Fatalf("delete response = %#v", deleted)
	}
	if deletedStage == "" {
		t.Fatal("delete request did not reach generated client transport")
	}
}

func TestUnixBackendLoadsConfigMutationStatus(t *testing.T) {
	socketPath := shortSocketPath(t)
	identity := testIdentity(socketPath, testInstanceID, AdminProtocolVersion)
	serveUnixHTTP(t, socketPath, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/readiness" {
			http.NotFound(writer, request)
			return
		}
		writeJSON(t, writer, generated.Readiness{
			Ready:                true,
			Identity:             identity,
			ConfigMutationStatus: generated.ConfigMutationStatusSecretCleanupPending,
		})
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

	status, err := client.LoadConfigMutationStatus(context.Background())
	if err != nil {
		t.Fatalf("LoadConfigMutationStatus: %v", err)
	}
	if status != generated.ConfigMutationStatusSecretCleanupPending {
		t.Fatalf("mutation status = %q", status)
	}
}
