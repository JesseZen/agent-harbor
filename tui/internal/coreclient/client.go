package coreclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/asheshgoplani/agent-deck/internal/backend"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
)

const AdminProtocolVersion = "1"

var (
	ErrInvalidSocket        = errors.New("invalid Agent Harbor Admin socket")
	ErrWrongSocketOwner     = errors.New("Agent Harbor Admin socket is not owned by the current user")
	ErrInstanceMismatch     = errors.New("Agent Harbor instance identity mismatch")
	ErrSocketMismatch       = errors.New("Agent Harbor Admin socket identity mismatch")
	ErrProtocolIncompatible = errors.New("Agent Harbor Admin protocol is incompatible")
	ErrSnapshotMismatch     = errors.New("Agent Harbor snapshot identity mismatch")
	ErrNotReady             = errors.New("Agent Harbor Core is not ready")
)

type Options struct {
	SocketPath              string
	ExpectedInstanceID      string
	ExpectedProtocolVersion string
	AttachExecutable        string
}

type Client struct {
	api                     *generated.ClientWithResponses
	closeIdleConnections    func()
	socketPath              string
	expectedInstanceID      string
	expectedProtocolVersion string
	attachExecutable        string
}

type APIError struct {
	Operation  string
	StatusCode int
	Code       string
	Detail     string
}

func (err *APIError) Error() string {
	message := ""
	if err.Code != "" {
		message = fmt.Sprintf("Agent Harbor Admin %s failed with HTTP %d (%s)", err.Operation, err.StatusCode, err.Code)
	} else {
		message = fmt.Sprintf("Agent Harbor Admin %s failed with HTTP %d", err.Operation, err.StatusCode)
	}
	if err.Detail != "" {
		return message + ": " + err.Detail
	}
	return message
}

func NewUnixBackend(ctx context.Context, options Options) (*Client, error) {
	if options.ExpectedInstanceID == "" {
		return nil, fmt.Errorf("%w: expected instance ID is required", ErrInstanceMismatch)
	}
	if options.ExpectedProtocolVersion == "" {
		return nil, fmt.Errorf("%w: expected protocol version is required", ErrProtocolIncompatible)
	}

	httpClient, closeIdleConnections, socketPath, err := newUnixHTTPClient(options.SocketPath)
	if err != nil {
		return nil, err
	}
	api, err := generated.NewClientWithResponses("http://agent-harbor-unix.invalid", generated.WithHTTPClient(httpClient))
	if err != nil {
		closeIdleConnections()
		return nil, fmt.Errorf("construct generated Admin client: %w", err)
	}

	client := &Client{
		api:                     api,
		closeIdleConnections:    closeIdleConnections,
		socketPath:              socketPath,
		expectedInstanceID:      options.ExpectedInstanceID,
		expectedProtocolVersion: options.ExpectedProtocolVersion,
		attachExecutable:        options.AttachExecutable,
	}
	if client.attachExecutable == "" {
		client.attachExecutable = "agent-harbor-core"
	}
	if _, err := client.getReadiness(ctx); err != nil {
		client.Close()
		return nil, err
	}
	return client, nil
}

func (client *Client) Close() {
	if client != nil && client.closeIdleConnections != nil {
		client.closeIdleConnections()
	}
}

func (client *Client) LoadSessions(ctx context.Context) (backend.Snapshot, error) {
	readiness, err := client.getReadiness(ctx)
	if err != nil {
		return backend.Snapshot{}, err
	}

	configResponse, err := client.api.GetConfigWithResponse(ctx)
	if err != nil {
		return backend.Snapshot{}, err
	}
	configStatus := 0
	if configResponse != nil {
		configStatus = configResponse.StatusCode()
	}
	if configResponse == nil || configStatus != http.StatusOK || configResponse.JSON200 == nil {
		return backend.Snapshot{}, responseError("getConfig", configStatus, problemCodeString(
			valueOrNil(configResponse, func(value *generated.GetConfigResponse) *generated.Problem {
				return value.ApplicationproblemJSONDefault
			}),
		))
	}
	config := configResponse.JSON200

	statusResponse, err := client.api.GetRuntimeStatusWithResponse(ctx)
	if err != nil {
		return backend.Snapshot{}, err
	}
	runtimeStatus := 0
	if statusResponse != nil {
		runtimeStatus = statusResponse.StatusCode()
	}
	if statusResponse == nil || runtimeStatus != http.StatusOK || statusResponse.JSON200 == nil {
		return backend.Snapshot{}, responseError("getRuntimeStatus", runtimeStatus, problemCode(
			valueOrNil(statusResponse, func(value *generated.GetRuntimeStatusResponse) *generated.Phase04Problem {
				return value.ApplicationproblemJSON500
			}),
			valueOrNil(statusResponse, func(value *generated.GetRuntimeStatusResponse) *generated.Phase04Problem {
				return value.ApplicationproblemJSONDefault
			}),
		))
	}
	status := statusResponse.JSON200
	if err := client.validateIdentity(status.Identity); err != nil {
		return backend.Snapshot{}, fmt.Errorf("runtime status: %w", err)
	}
	if config.ActiveGeneration != readiness.Snapshot.Generation || status.Snapshot.Generation != readiness.Snapshot.Generation {
		return backend.Snapshot{}, fmt.Errorf("%w: readiness=%d config=%d status=%d", ErrSnapshotMismatch, readiness.Snapshot.Generation, config.ActiveGeneration, status.Snapshot.Generation)
	}
	projected := projectSessions(config.MutableConfig, status.Sessions)
	projected.Identity = backend.Identity{
		InstanceID:  readiness.Identity.InstanceId,
		Binary:      string(readiness.Identity.Binary),
		Version:     readiness.Identity.Version,
		AdminSocket: readiness.Identity.AdminSocket,
	}
	projected.Generation = readiness.Snapshot.Generation
	projected.ConfigRevision = config.ConfigRevision
	projected.Ready = readiness.Ready
	projected.DroppedEvents = status.Observations.DroppedEvents
	projected.LastEventID = status.Observations.LastEventId
	routeNames := make(map[string]string, len(config.MutableConfig.Routes))
	for _, route := range config.MutableConfig.Routes {
		routeNames[string(route.Id)] = route.Name
	}
	projected.ClientProfiles = make([]backend.ClientProfile, 0, len(config.MutableConfig.ClientProfiles))
	managedProfileNames := map[string]string{}
	if config.MutableConfig.ManagedObjects != nil {
		for _, object := range *config.MutableConfig.ManagedObjects {
			if object.Kind != generated.ManagedObjectKindTrafficRule {
				continue
			}
			for _, member := range object.Members {
				if member.Kind == generated.ManagedResourceRefKindClientProfile {
					managedProfileNames[string(member.Id)] = object.Name
				}
			}
		}
	}
	for _, profile := range config.MutableConfig.ClientProfiles {
		managedName, managed := managedProfileNames[string(profile.Id)]
		if !managed {
			continue
		}
		defaultRouteID := string(profile.DefaultRouteId)
		name := profile.Name
		if managed {
			launcher := string(profile.Launcher)
			if launcher != "" {
				launcher = strings.ToUpper(launcher[:1]) + launcher[1:]
			}
			name = launcher + " · " + managedName
		}
		projected.ClientProfiles = append(projected.ClientProfiles, backend.ClientProfile{
			ID: string(profile.Id), Name: name, Launcher: string(profile.Launcher),
			DefaultRouteID: defaultRouteID, DefaultRouteName: routeNames[defaultRouteID],
		})
	}
	routeStatus := make(map[string]generated.RouteStatus, len(status.Routes))
	for _, route := range status.Routes {
		routeStatus[route.Id] = route
	}
	projected.Routes = make([]backend.Route, 0, len(config.MutableConfig.Routes))
	for _, route := range config.MutableConfig.Routes {
		view := backend.Route{ID: string(route.Id), Name: route.Name}
		if current, ok := routeStatus[string(route.Id)]; ok {
			view.BackendSetID = current.BackendSetId
			view.Policy = string(current.Policy)
			view.EligibleTargetIDs = append([]string(nil), current.EligibleTargetIds...)
			view.RecentDecisionCount = len(current.RecentDecisions)
		}
		projected.Routes = append(projected.Routes, view)
	}
	projected.Targets = make([]backend.Target, 0, len(status.Targets))
	for _, target := range status.Targets {
		projected.Targets = append(projected.Targets, backend.Target{
			ID: target.Id, Name: target.Name, Adapter: string(target.Adapter), Health: string(target.Health),
			BaseEligible: target.BaseEligible, Suspended: target.Suspended, QuotaGroupID: target.QuotaGroupId,
			CredentialAccess: string(target.CredentialAccess), CredentialGeneration: target.CredentialGeneration, TargetGeneration: target.TargetGeneration,
		})
	}
	quotaNames := make(map[string]string, len(config.MutableConfig.QuotaGroups))
	for _, quota := range config.MutableConfig.QuotaGroups {
		quotaNames[string(quota.Id)] = quota.Name
	}
	projected.QuotaGroups = make([]backend.QuotaGroup, 0, len(status.QuotaGroups))
	for _, quota := range status.QuotaGroups {
		projected.QuotaGroups = append(projected.QuotaGroups, backend.QuotaGroup{
			ID: quota.Id, Name: quotaNames[quota.Id], ActiveConcurrency: quota.ActiveConcurrency,
			MaxConcurrency: quota.MaxConcurrency, RPM: quota.Rpm, ForegroundDepth: quota.ForegroundDepth,
			BackgroundDepth: quota.BackgroundDepth, NextPermitAt: quota.NextPermitAt,
		})
	}
	projected.Observations = make([]backend.Observation, 0, len(status.RecentObservations))
	for _, observation := range status.RecentObservations {
		projected.Observations = append(projected.Observations, backend.Observation{
			ID: observation.Id, Type: string(observation.Type), OccurredAt: observation.OccurredAt,
			SnapshotGeneration: observation.SnapshotGeneration, SessionID: optionalString(observation.SessionId),
			RouteID: optionalString(observation.RouteId), TargetID: optionalString(observation.TargetId),
			QuotaGroupID: optionalString(observation.QuotaGroupId), DecisionReason: string(observation.DecisionReason),
			PolicyDecision: optionalString(observation.PolicyDecision), SemanticOutcome: optionalString(observation.SemanticOutcome),
		})
	}
	return projected, nil
}

func optionalString[T ~string](value *T) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

func (client *Client) getReadiness(ctx context.Context) (*generated.Readiness, error) {
	response, err := client.api.GetReadinessWithResponse(ctx)
	if err != nil {
		return nil, err
	}
	readinessStatus := 0
	if response != nil {
		readinessStatus = response.StatusCode()
	}
	if response == nil || readinessStatus != http.StatusOK || response.JSON200 == nil {
		return nil, responseError("getReadiness", readinessStatus, problemCode(
			valueOrNil(response, func(value *generated.GetReadinessResponse) *generated.Phase04Problem {
				return value.ApplicationproblemJSON500
			}),
			valueOrNil(response, func(value *generated.GetReadinessResponse) *generated.Phase04Problem {
				return value.ApplicationproblemJSONDefault
			}),
		))
	}
	if !response.JSON200.Ready {
		return nil, ErrNotReady
	}
	if err := client.validateIdentity(response.JSON200.Identity); err != nil {
		return nil, fmt.Errorf("readiness: %w", err)
	}
	return response.JSON200, nil
}

func (client *Client) validateIdentity(identity generated.InstanceIdentity) error {
	if identity.InstanceId != client.expectedInstanceID {
		return ErrInstanceMismatch
	}
	if identity.Binary != generated.AgentHarborCore ||
		strconv.Itoa(int(identity.AdminProtocolVersion)) != client.expectedProtocolVersion {
		return ErrProtocolIncompatible
	}
	if filepath.Clean(identity.AdminSocket) != client.socketPath {
		return ErrSocketMismatch
	}
	return nil
}

func projectSessions(config generated.MutableConfigView, sessions []generated.AgentSession) backend.Snapshot {
	profiles := make(map[string]string, len(config.ClientProfiles))
	for _, profile := range config.ClientProfiles {
		profiles[string(profile.Id)] = string(profile.Launcher)
	}
	routes := make(map[string]string, len(config.Routes))
	for _, route := range config.Routes {
		routes[string(route.Id)] = route.Name
	}

	projected := make([]backend.Session, 0, len(sessions))
	for _, session := range sessions {
		tool := profiles[string(session.ClientProfileId)]
		if tool == "" {
			tool = string(session.ClientProfileId)
		}
		routeID := string(session.RouteId)
		groupPath := routeID
		if routeName := routes[routeID]; routeName != "" {
			groupPath = routeName + "/" + routeID
		}
		projected = append(projected, backend.Session{
			ID:                   string(session.Id),
			Title:                session.Label,
			ProjectPath:          string(session.Workspace),
			GroupPath:            groupPath,
			Tool:                 tool,
			Status:               projectStatus(session.Lifecycle),
			CreatedAt:            session.CreatedAt,
			LastAccessedAt:       session.UpdatedAt,
			UpdatedAt:            session.UpdatedAt,
			ClientProfileID:      string(session.ClientProfileId),
			RouteID:              string(session.RouteId),
			CredentialGeneration: session.SessionCredential.Generation,
			NativeActivity:       backend.NativeActivity(session.NativeActivity),
			ActivitySource:       backend.ActivitySource(session.ActivitySource),
			HookHealth:           backend.HookHealthState(session.HookHealth),
			HookHealthObservedAt: session.HookHealthObservedAt,
		})
		if session.NativeActivityObservedAt != nil {
			projected[len(projected)-1].ActivityObservedAt = *session.NativeActivityObservedAt
		}
		if session.NativeProvider != nil {
			projected[len(projected)-1].NativeProvider = backend.NativeProvider(*session.NativeProvider)
		}
		if session.NativeSessionId != nil {
			projected[len(projected)-1].NativeSessionID = *session.NativeSessionId
		}
	}
	return backend.Snapshot{Sessions: projected}
}

func projectStatus(status generated.AgentSessionLifecycle) backend.Status {
	switch status {
	case generated.AgentSessionLifecycleCreated:
		return backend.StatusCreated
	case generated.AgentSessionLifecycleLaunching:
		return backend.StatusLaunching
	case generated.AgentSessionLifecycleRunning:
		return backend.StatusRunning
	case generated.AgentSessionLifecycleEnding:
		return backend.StatusEnding
	case generated.AgentSessionLifecycleIdle:
		return backend.StatusIdle
	case generated.AgentSessionLifecycleFailed:
		return backend.StatusFailed
	case generated.AgentSessionLifecycleEnded:
		return backend.StatusEnded
	default:
		return backend.StatusFailed
	}
}

func responseError(operation string, status int, code string) error {
	if status == http.StatusOK || status == 0 {
		return fmt.Errorf("%w: %s returned no declared JSON success body", ErrProtocolIncompatible, operation)
	}
	return &APIError{Operation: operation, StatusCode: status, Code: code}
}

func phase04ResponseError(operation string, status int, problems ...*generated.Phase04Problem) error {
	err := responseError(operation, status, problemCode(problems...))
	apiErr, ok := err.(*APIError)
	if !ok {
		return err
	}
	for _, problem := range problems {
		if problem != nil && problem.Detail != nil {
			apiErr.Detail = *problem.Detail
			break
		}
	}
	return apiErr
}

func problemCode(problems ...*generated.Phase04Problem) string {
	for _, problem := range problems {
		if problem != nil {
			return string(problem.Code)
		}
	}
	return ""
}

func problemCodeString(problems ...*generated.Problem) string {
	for _, problem := range problems {
		if problem != nil {
			return problem.Code
		}
	}
	return ""
}

func valueOrNil[T any, P any](value *T, selectValue func(*T) *P) *P {
	if value == nil {
		return nil
	}
	return selectValue(value)
}
