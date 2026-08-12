package coreclient

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/backend"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
)

func (client *Client) CreateSession(ctx context.Context, request backend.CreateSessionRequest) (backend.Session, error) {
	var routeID *generated.ConfigID
	if request.RouteID != "" {
		value := generated.ConfigID(request.RouteID)
		routeID = &value
	}
	response, err := client.api.CreateAgentSessionWithResponse(ctx, generated.CreateAgentSessionCommand{
		InstanceId:                 client.expectedInstanceID,
		ExpectedSnapshotGeneration: request.ExpectedSnapshotGeneration,
		Label:                      request.Label,
		Workspace:                  request.Workspace,
		ClientProfileId:            request.ClientProfileID,
		RouteId:                    routeID,
	})
	if err != nil {
		return backend.Session{}, err
	}
	if response == nil || response.StatusCode() != http.StatusCreated || response.JSON201 == nil {
		return backend.Session{}, actionResponseError("createAgentSession", responseStatus(response))
	}
	return projectActionSession(*response.JSON201), nil
}

func (client *Client) LaunchSession(ctx context.Context, ref backend.SessionRef, timeout time.Duration) (backend.Session, error) {
	response, err := client.api.LaunchAgentSessionWithResponse(ctx, ref.ID, generated.SessionLifecycleCommand{
		InstanceId:        client.expectedInstanceID,
		ExpectedUpdatedAt: ref.ExpectedUpdatedAt,
		TimeoutMs:         timeout.Milliseconds(),
	})
	if err != nil {
		return backend.Session{}, err
	}
	if response == nil || response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		return backend.Session{}, actionResponseError("launchAgentSession", responseStatus(response))
	}
	if response.JSON200.Id != ref.ID {
		return backend.Session{}, fmt.Errorf("%w: launch session response", ErrInstanceMismatch)
	}
	return projectActionSession(*response.JSON200), nil
}

func (client *Client) ResumeSession(ctx context.Context, ref backend.SessionRef, timeout time.Duration) (backend.Session, error) {
	response, err := client.api.ResumeAgentSessionWithResponse(ctx, ref.ID, generated.SessionLifecycleCommand{
		InstanceId:        client.expectedInstanceID,
		ExpectedUpdatedAt: ref.ExpectedUpdatedAt,
		TimeoutMs:         timeout.Milliseconds(),
	})
	if err != nil {
		return backend.Session{}, err
	}
	if response == nil || response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		return backend.Session{}, actionResponseError("resumeAgentSession", responseStatus(response))
	}
	if response.JSON200.Id != ref.ID {
		return backend.Session{}, fmt.Errorf("%w: resume session response", ErrInstanceMismatch)
	}
	return projectActionSession(*response.JSON200), nil
}

func (client *Client) EndSession(ctx context.Context, request backend.EndSessionRequest) error {
	mode := generated.EndAgentSessionCommandMode(request.Mode)
	response, err := client.api.DeleteAgentSessionWithResponse(ctx, request.Session.ID, generated.EndAgentSessionCommand{
		InstanceId:        client.expectedInstanceID,
		ExpectedUpdatedAt: request.Session.ExpectedUpdatedAt,
		Mode:              mode,
		TimeoutMs:         request.Timeout.Milliseconds(),
	})
	if err != nil {
		return err
	}
	if response == nil || response.StatusCode() != http.StatusNoContent {
		return actionResponseError("deleteAgentSession", responseStatus(response))
	}
	return nil
}

func (client *Client) RotateCredential(ctx context.Context, ref backend.SessionRef, destination io.Writer) (int, error) {
	if destination == nil {
		return 0, fmt.Errorf("credential destination is required")
	}
	response, err := client.api.RotateSessionCredentialWithResponse(ctx, ref.ID, generated.SessionCredentialCommand{
		InstanceId:                   client.expectedInstanceID,
		ExpectedUpdatedAt:            ref.ExpectedUpdatedAt,
		ExpectedCredentialGeneration: ref.CredentialGeneration,
	})
	if err != nil {
		return 0, err
	}
	if response == nil || response.StatusCode() != http.StatusCreated || response.JSON201 == nil {
		return 0, actionResponseError("rotateSessionCredential", responseStatus(response))
	}
	credential := response.JSON201
	if credential.SessionId != ref.ID || credential.Generation != ref.CredentialGeneration+1 {
		return 0, fmt.Errorf("%w: credential response", ErrInstanceMismatch)
	}
	if err := writeOneTimeSecret(destination, credential.Credential, "session credential"); err != nil {
		return 0, err
	}
	return credential.Generation, nil
}

func (client *Client) RevokeCredential(ctx context.Context, ref backend.SessionRef) error {
	response, err := client.api.RevokeSessionCredentialWithResponse(ctx, ref.ID, generated.SessionCredentialCommand{
		InstanceId:                   client.expectedInstanceID,
		ExpectedUpdatedAt:            ref.ExpectedUpdatedAt,
		ExpectedCredentialGeneration: ref.CredentialGeneration,
	})
	if err != nil {
		return err
	}
	if response == nil || response.StatusCode() != http.StatusNoContent {
		return actionResponseError("revokeSessionCredential", responseStatus(response))
	}
	return nil
}

func (client *Client) ResetAffinity(ctx context.Context, ref backend.SessionRef) error {
	response, err := client.api.ResetSessionAffinityWithResponse(ctx, ref.ID, generated.SessionVersionCommand{
		InstanceId:        client.expectedInstanceID,
		ExpectedUpdatedAt: ref.ExpectedUpdatedAt,
	})
	if err != nil {
		return err
	}
	if response == nil || response.StatusCode() != http.StatusNoContent {
		return actionResponseError("resetSessionAffinity", responseStatus(response))
	}
	return nil
}

func (client *Client) Preview(ctx context.Context, ref backend.SessionRef) (backend.Preview, error) {
	response, err := client.api.GetAgentSessionPreviewWithResponse(ctx, ref.ID, &generated.GetAgentSessionPreviewParams{
		InstanceId:        client.expectedInstanceID,
		ExpectedUpdatedAt: ref.ExpectedUpdatedAt,
	})
	if err != nil {
		return backend.Preview{}, err
	}
	if response == nil || response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		return backend.Preview{}, phase04ResponseError(
			"getAgentSessionPreview",
			responseStatus(response),
			valueOrNil(response, func(value *generated.GetAgentSessionPreviewResponse) *generated.Phase04Problem {
				return value.ApplicationproblemJSON404
			}),
			valueOrNil(response, func(value *generated.GetAgentSessionPreviewResponse) *generated.Phase04Problem {
				return value.ApplicationproblemJSON409
			}),
			valueOrNil(response, func(value *generated.GetAgentSessionPreviewResponse) *generated.Phase04Problem {
				return value.ApplicationproblemJSON500
			}),
			valueOrNil(response, func(value *generated.GetAgentSessionPreviewResponse) *generated.Phase04Problem {
				return value.ApplicationproblemJSON503
			}),
			valueOrNil(response, func(value *generated.GetAgentSessionPreviewResponse) *generated.Phase04Problem {
				return value.ApplicationproblemJSONDefault
			}),
		)
	}
	preview := response.JSON200
	if preview.SessionId != ref.ID {
		return backend.Preview{}, fmt.Errorf("%w: preview response", ErrInstanceMismatch)
	}
	return backend.Preview{SessionID: preview.SessionId, Lines: preview.Lines, ObservedAt: preview.ObservedAt, Truncated: preview.Truncated}, nil
}

func (client *Client) HookHealth(ctx context.Context, ref backend.SessionRef) (backend.HookHealth, error) {
	response, err := client.api.GetNativeHookHealthWithResponse(ctx, ref.ID, &generated.GetNativeHookHealthParams{
		InstanceId:        client.expectedInstanceID,
		ExpectedUpdatedAt: ref.ExpectedUpdatedAt,
	})
	if err != nil {
		return backend.HookHealth{}, err
	}
	if response == nil || response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		return backend.HookHealth{}, actionResponseError("getNativeHookHealth", responseStatus(response))
	}
	health := response.JSON200
	if health.SessionId != ref.ID {
		return backend.HookHealth{}, fmt.Errorf("%w: hook health response", ErrInstanceMismatch)
	}
	return backend.HookHealth{SessionID: health.SessionId, Provider: string(health.Provider), State: string(health.State), ObservedAt: health.ObservedAt}, nil
}

func (client *Client) PrepareAttach(ctx context.Context, ref backend.SessionRef, destination io.Writer) (backend.AttachCommand, error) {
	if destination == nil {
		return backend.AttachCommand{}, fmt.Errorf("attach grant destination is required")
	}
	response, err := client.api.AuthorizeAgentSessionAttachWithResponse(ctx, ref.ID, generated.AuthorizeAttachmentCommand{
		InstanceId:        client.expectedInstanceID,
		ExpectedUpdatedAt: ref.ExpectedUpdatedAt,
	})
	if err != nil {
		return backend.AttachCommand{}, err
	}
	if response == nil || response.StatusCode() != http.StatusCreated || response.JSON201 == nil {
		return backend.AttachCommand{}, actionResponseError("authorizeAgentSessionAttach", responseStatus(response))
	}
	grant := response.JSON201
	if grant.InstanceId != client.expectedInstanceID || grant.SessionId != ref.ID || !grant.ExpectedUpdatedAt.Equal(ref.ExpectedUpdatedAt) {
		return backend.AttachCommand{}, fmt.Errorf("%w: attach grant response", ErrInstanceMismatch)
	}
	if err := writeOneTimeSecret(destination, grant.Token, "attach grant"); err != nil {
		return backend.AttachCommand{}, err
	}
	return backend.AttachCommand{
		Executable:        client.attachExecutable,
		InstanceID:        client.expectedInstanceID,
		AdminSocket:       client.socketPath,
		SessionID:         ref.ID,
		ExpectedUpdatedAt: ref.ExpectedUpdatedAt,
	}, nil
}

func writeOneTimeSecret(destination io.Writer, value, label string) error {
	if len(value) != 43 || strings.IndexFunc(value, func(character rune) bool {
		return !(character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-' || character == '_')
	}) >= 0 {
		return fmt.Errorf("%w: invalid %s shape", ErrProtocolIncompatible, label)
	}
	written, err := io.WriteString(destination, value)
	if err != nil {
		return err
	}
	if written != len(value) {
		return io.ErrShortWrite
	}
	return nil
}

func projectActionSession(session generated.AgentSession) backend.Session {
	projected := backend.Session{
		ID:                   session.Id,
		Title:                session.Label,
		ProjectPath:          session.Workspace,
		GroupPath:            session.RouteId,
		Tool:                 session.ClientProfileId,
		Status:               projectStatus(session.Lifecycle),
		CreatedAt:            session.CreatedAt,
		LastAccessedAt:       session.UpdatedAt,
		UpdatedAt:            session.UpdatedAt,
		ClientProfileID:      session.ClientProfileId,
		RouteID:              session.RouteId,
		CredentialGeneration: session.SessionCredential.Generation,
		NativeActivity:       backend.NativeActivity(session.NativeActivity),
		ActivitySource:       backend.ActivitySource(session.ActivitySource),
		HookHealth:           backend.HookHealthState(session.HookHealth),
		HookHealthObservedAt: session.HookHealthObservedAt,
	}
	if session.NativeActivityObservedAt != nil {
		projected.ActivityObservedAt = *session.NativeActivityObservedAt
	}
	if session.NativeProvider != nil {
		projected.NativeProvider = backend.NativeProvider(*session.NativeProvider)
	}
	if session.NativeSessionId != nil {
		projected.NativeSessionID = *session.NativeSessionId
	}
	return projected
}

func actionResponseError(operation string, status int) error {
	if status == 0 || status == http.StatusOK || status == http.StatusCreated || status == http.StatusNoContent {
		return fmt.Errorf("%w: %s returned no declared response body", ErrProtocolIncompatible, operation)
	}
	return &APIError{Operation: operation, StatusCode: status}
}

type statusResponse interface {
	StatusCode() int
}

func responseStatus(response statusResponse) int {
	if response == nil || reflect.ValueOf(response).IsNil() {
		return 0
	}
	return response.StatusCode()
}
