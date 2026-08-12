//go:build unix

package coreclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/backend"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
)

func TestUnixBackendRoutesTypedSessionActions(t *testing.T) {
	socketPath := shortSocketPath(t)
	identity := testIdentity(socketPath, testInstanceID, AdminProtocolVersion)
	updatedAt := time.Unix(1_700_001_000, 0).UTC()
	createdAt := time.Unix(1_700_000_000, 0).UTC()
	sessionID := "ses_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	grantToken := "abcdefghijklmnopqrstuvwxyzABCDEFGH123456789"
	credential := "0123456789abcdefghijklmnopqrstuvwxyzABCDEFG"

	var endModes []generated.EndAgentSessionCommandMode
	serveUnixHTTP(t, socketPath, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/v1/readiness" {
			writeJSON(t, writer, generated.Readiness{Ready: true, Identity: identity})
			return
		}

		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/v1/sessions":
			var command generated.CreateAgentSessionCommand
			decodeJSON(t, request, &command)
			if command.InstanceId != testInstanceID || command.ExpectedSnapshotGeneration != 9 || command.Label != "Core action" || command.Workspace != "/workspace/action" || command.ClientProfileId != "profile_codex" || command.RouteId == nil || *command.RouteId != "route_main" {
				t.Errorf("create command = %#v", command)
			}
			writeJSONStatus(t, writer, http.StatusCreated, actionSession(sessionID, createdAt, updatedAt, generated.AgentSessionLifecycleCreated))

		case request.Method == http.MethodPost && request.URL.Path == "/v1/sessions/"+sessionID+"/launch":
			var command generated.SessionLifecycleCommand
			decodeJSON(t, request, &command)
			assertLifecycleCommand(t, command, updatedAt, 5*time.Second)
			writeJSON(t, writer, actionSession(sessionID, createdAt, updatedAt, generated.AgentSessionLifecycleRunning))

		case request.Method == http.MethodPost && request.URL.Path == "/v1/sessions/"+sessionID+"/resume":
			var command generated.SessionLifecycleCommand
			decodeJSON(t, request, &command)
			assertLifecycleCommand(t, command, updatedAt, 6*time.Second)
			writeJSON(t, writer, actionSession(sessionID, createdAt, updatedAt, generated.AgentSessionLifecycleRunning))

		case request.Method == http.MethodDelete && request.URL.Path == "/v1/sessions/"+sessionID:
			var command generated.EndAgentSessionCommand
			decodeJSON(t, request, &command)
			if command.InstanceId != testInstanceID || !command.ExpectedUpdatedAt.Equal(updatedAt) || command.TimeoutMs != 7000 {
				t.Errorf("end command = %#v", command)
			}
			endModes = append(endModes, command.Mode)
			writer.WriteHeader(http.StatusNoContent)

		case request.Method == http.MethodPost && request.URL.Path == "/v1/sessions/"+sessionID+"/credentials":
			var command generated.SessionCredentialCommand
			decodeJSON(t, request, &command)
			assertCredentialCommand(t, command, updatedAt, 3)
			writeJSONStatus(t, writer, http.StatusCreated, generated.OneTimeSessionCredential{SessionId: sessionID, Generation: 4, Credential: credential})

		case request.Method == http.MethodPost && request.URL.Path == "/v1/sessions/"+sessionID+"/credentials/revoke":
			var command generated.SessionCredentialCommand
			decodeJSON(t, request, &command)
			assertCredentialCommand(t, command, updatedAt, 4)
			writer.WriteHeader(http.StatusNoContent)

		case request.Method == http.MethodPost && request.URL.Path == "/v1/sessions/"+sessionID+"/affinity/reset":
			var command generated.SessionVersionCommand
			decodeJSON(t, request, &command)
			assertVersionCommand(t, command, updatedAt)
			writer.WriteHeader(http.StatusNoContent)

		case request.Method == http.MethodGet && request.URL.Path == "/v1/sessions/"+sessionID+"/preview":
			assertReadQuery(t, request, updatedAt)
			writeJSON(t, writer, generated.SessionPreview{SessionId: sessionID, Lines: []string{"line one", "line two"}, ObservedAt: updatedAt})

		case request.Method == http.MethodGet && request.URL.Path == "/v1/sessions/"+sessionID+"/hook-health":
			assertReadQuery(t, request, updatedAt)
			writeJSON(t, writer, generated.NativeHookHealth{SessionId: sessionID, Provider: "codex", State: "active", ObservedAt: updatedAt})

		case request.Method == http.MethodPost && request.URL.Path == "/v1/sessions/"+sessionID+"/attach":
			var command generated.AuthorizeAttachmentCommand
			decodeJSON(t, request, &command)
			assertVersionCommand(t, generated.SessionVersionCommand(command), updatedAt)
			writeJSONStatus(t, writer, http.StatusCreated, generated.AttachmentGrant{InstanceId: testInstanceID, SessionId: sessionID, ExpectedUpdatedAt: updatedAt, Token: grantToken})

		default:
			t.Errorf("unexpected request: %s %s", request.Method, request.URL.String())
			http.NotFound(writer, request)
		}
	}))

	client, err := NewUnixBackend(context.Background(), Options{SocketPath: socketPath, ExpectedInstanceID: testInstanceID, ExpectedProtocolVersion: AdminProtocolVersion})
	if err != nil {
		t.Fatalf("NewUnixBackend: %v", err)
	}
	t.Cleanup(client.Close)

	created, err := client.CreateSession(context.Background(), backend.CreateSessionRequest{
		ExpectedSnapshotGeneration: 9,
		Label:                      "Core action",
		Workspace:                  "/workspace/action",
		ClientProfileID:            "profile_codex",
		RouteID:                    "route_main",
	})
	if err != nil || created.ID != sessionID || created.Status != backend.StatusCreated {
		t.Fatalf("CreateSession = %#v, %v", created, err)
	}

	ref := backend.SessionRef{ID: sessionID, ExpectedUpdatedAt: updatedAt, CredentialGeneration: 3}
	launched, err := client.LaunchSession(context.Background(), ref, 5*time.Second)
	if err != nil || launched.Status != backend.StatusRunning {
		t.Fatalf("LaunchSession = %#v, %v", launched, err)
	}
	resumed, err := client.ResumeSession(context.Background(), ref, 6*time.Second)
	if err != nil || resumed.Status != backend.StatusRunning {
		t.Fatalf("ResumeSession = %#v, %v", resumed, err)
	}
	if err := client.EndSession(context.Background(), backend.EndSessionRequest{Session: ref, Mode: backend.EndGraceful, Timeout: 7 * time.Second}); err != nil {
		t.Fatalf("graceful EndSession: %v", err)
	}
	if err := client.EndSession(context.Background(), backend.EndSessionRequest{Session: ref, Mode: backend.EndForce, Timeout: 7 * time.Second}); err != nil {
		t.Fatalf("force EndSession: %v", err)
	}
	if len(endModes) != 2 || endModes[0] != generated.EndAgentSessionCommandModeGraceful || endModes[1] != generated.EndAgentSessionCommandModeForce {
		t.Fatalf("end modes = %#v", endModes)
	}

	var credentialSink bytes.Buffer
	nextGeneration, err := client.RotateCredential(context.Background(), ref, &credentialSink)
	if err != nil || nextGeneration != 4 || credentialSink.String() != credential {
		t.Fatalf("RotateCredential = generation %d, secret %q, err %v", nextGeneration, credentialSink.String(), err)
	}
	ref.CredentialGeneration = nextGeneration
	if err := client.RevokeCredential(context.Background(), ref); err != nil {
		t.Fatalf("RevokeCredential: %v", err)
	}
	if err := client.ResetAffinity(context.Background(), ref); err != nil {
		t.Fatalf("ResetAffinity: %v", err)
	}

	preview, err := client.Preview(context.Background(), ref)
	if err != nil || len(preview.Lines) != 2 || preview.Lines[1] != "line two" || preview.SessionID != sessionID {
		t.Fatalf("Preview = %#v, %v", preview, err)
	}
	hookHealth, err := client.HookHealth(context.Background(), ref)
	if err != nil || hookHealth.SessionID != sessionID || hookHealth.Provider != "codex" || hookHealth.State != "active" {
		t.Fatalf("HookHealth = %#v, %v", hookHealth, err)
	}

	var grantSink bytes.Buffer
	attachCommand, err := client.PrepareAttach(context.Background(), ref, &grantSink)
	if err != nil {
		t.Fatalf("PrepareAttach: %v", err)
	}
	if grantSink.String() != grantToken {
		t.Fatalf("attach grant sink = %q", grantSink.String())
	}
	if attachCommand.Executable != "agent-harbor-core" || attachCommand.InstanceID != testInstanceID || attachCommand.AdminSocket != socketPath || attachCommand.SessionID != sessionID || !attachCommand.ExpectedUpdatedAt.Equal(updatedAt) {
		t.Fatalf("attach command = %#v", attachCommand)
	}
}

func TestActionResponseStatusAcceptsTypedNil(t *testing.T) {
	var response *generated.CreateAgentSessionResponse
	if status := responseStatus(response); status != 0 {
		t.Fatalf("responseStatus(typed nil) = %d", status)
	}
}

func actionSession(id string, createdAt, updatedAt time.Time, lifecycle generated.AgentSessionLifecycle) generated.AgentSession {
	return generated.AgentSession{
		Id: id, Label: "Core action", Workspace: "/workspace/action", RouteId: "route_main", ClientProfileId: "profile_codex",
		Lifecycle: lifecycle, CreatedAt: createdAt, UpdatedAt: updatedAt, SessionCredential: generated.SessionCredentialView{Generation: 3},
	}
}

func decodeJSON(t *testing.T, request *http.Request, destination any) {
	t.Helper()
	defer request.Body.Close()
	if err := json.NewDecoder(request.Body).Decode(destination); err != nil {
		t.Errorf("decode request: %v", err)
	}
}

func TestUnixBackendPreviewPreservesProblemDetail(t *testing.T) {
	socketPath := shortSocketPath(t)
	identity := testIdentity(socketPath, testInstanceID, AdminProtocolVersion)
	updatedAt := time.Unix(1_700_001_000, 0).UTC()
	sessionID := "ses_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	detail := "The owned session pane could not be inspected."

	serveUnixHTTP(t, socketPath, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/readiness":
			writeJSON(t, writer, generated.Readiness{Ready: true, Identity: identity})
		case "/v1/sessions/" + sessionID + "/preview":
			assertReadQuery(t, request, updatedAt)
			writer.Header().Set("Content-Type", "application/problem+json")
			writer.WriteHeader(http.StatusServiceUnavailable)
			if err := json.NewEncoder(writer).Encode(generated.Phase04Problem{
				Code:      generated.PreviewUnavailable,
				Detail:    &detail,
				RequestId: "req_preview",
				Status:    http.StatusServiceUnavailable,
				Title:     "Preview unavailable",
				Type:      "urn:agent-harbor:problem:preview_unavailable",
			}); err != nil {
				t.Errorf("encode preview problem: %v", err)
			}
		default:
			t.Errorf("unexpected request: %s %s", request.Method, request.URL.String())
			http.NotFound(writer, request)
		}
	}))

	client, err := NewUnixBackend(context.Background(), Options{SocketPath: socketPath, ExpectedInstanceID: testInstanceID, ExpectedProtocolVersion: AdminProtocolVersion})
	if err != nil {
		t.Fatalf("NewUnixBackend: %v", err)
	}
	t.Cleanup(client.Close)

	_, err = client.Preview(context.Background(), backend.SessionRef{ID: sessionID, ExpectedUpdatedAt: updatedAt})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("Preview error = %v, want APIError", err)
	}
	if apiErr.StatusCode != http.StatusServiceUnavailable || apiErr.Code != string(generated.PreviewUnavailable) || apiErr.Detail != detail {
		t.Fatalf("Preview APIError = %#v", apiErr)
	}
	if !strings.Contains(err.Error(), detail) {
		t.Fatalf("Preview error hid problem detail: %q", err)
	}
}

func writeJSONStatus(t *testing.T, writer http.ResponseWriter, status int, value any) {
	t.Helper()
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		t.Errorf("encode response: %v", err)
	}
}

func assertLifecycleCommand(t *testing.T, command generated.SessionLifecycleCommand, updatedAt time.Time, timeout time.Duration) {
	t.Helper()
	if command.InstanceId != testInstanceID || !command.ExpectedUpdatedAt.Equal(updatedAt) || command.TimeoutMs != timeout.Milliseconds() {
		t.Errorf("lifecycle command = %#v", command)
	}
}

func assertCredentialCommand(t *testing.T, command generated.SessionCredentialCommand, updatedAt time.Time, generation int) {
	t.Helper()
	if command.InstanceId != testInstanceID || !command.ExpectedUpdatedAt.Equal(updatedAt) || command.ExpectedCredentialGeneration != generation {
		t.Errorf("credential command = %#v", command)
	}
}

func assertVersionCommand(t *testing.T, command generated.SessionVersionCommand, updatedAt time.Time) {
	t.Helper()
	if command.InstanceId != testInstanceID || !command.ExpectedUpdatedAt.Equal(updatedAt) {
		t.Errorf("version command = %#v", command)
	}
}

func assertReadQuery(t *testing.T, request *http.Request, updatedAt time.Time) {
	t.Helper()
	if request.URL.Query().Get("instance_id") != testInstanceID || request.URL.Query().Get("expected_updated_at") != updatedAt.Format(time.RFC3339) {
		t.Errorf("read query = %q", request.URL.RawQuery)
	}
}
