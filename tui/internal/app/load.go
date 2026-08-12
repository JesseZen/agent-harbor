package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/backend"
	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/resourcepages/observations"
	"github.com/asheshgoplani/agent-deck/internal/resourcepages/overview"
	"github.com/asheshgoplani/agent-deck/internal/resourcepages/profiles"
	"github.com/asheshgoplani/agent-deck/internal/resourcepages/quotas"
	"github.com/asheshgoplani/agent-deck/internal/resourcepages/routes"
	"github.com/asheshgoplani/agent-deck/internal/resourcepages/targets"
	"github.com/asheshgoplani/agent-deck/internal/secretinput"
	sessionpage "github.com/asheshgoplani/agent-deck/internal/session/page"
	tea "github.com/charmbracelet/bubbletea"
)

type configSource interface {
	LoadConfig(context.Context) (generated.MutableConfigSnapshot, error)
	ValidateConfig(context.Context, generated.ValidateConfigCommand) (generated.ValidationResult, error)
	PatchConfig(context.Context, generated.PatchConfigCommand) (generated.SnapshotIdentity, error)
}

type configMutationStatusSource interface {
	LoadConfigMutationStatus(context.Context) (generated.ConfigMutationStatus, error)
}

type targetModelSource interface {
	DiscoverTargetModels(context.Context, string) ([]string, error)
}

type targetProbeSource interface {
	ProbeTarget(context.Context, string, int, time.Duration) error
}

type agentSessionSource interface {
	LoadAgentSessions(context.Context) ([]generated.AgentSession, error)
}

type stageSource interface {
	secretinput.StageHTTPClient
}

type loadResultMsg struct {
	config       generated.MutableConfigSnapshot
	configErr    error
	mutation     generated.ConfigMutationStatus
	mutationErr  error
	runtime      backend.Snapshot
	runtimeErr   error
	sessions     []generated.AgentSession
	sessionsErr  error
	forceReplace bool
}

func (model *Model) loadAll(forceReplace bool) tea.Cmd {
	return func() tea.Msg {
		return model.fetchAll(model.ctx, forceReplace)
	}
}

func (model *Model) fetchAll(ctx context.Context, forceReplace bool) loadResultMsg {
	result := loadResultMsg{forceReplace: forceReplace}
	if model.configSource == nil {
		result.configErr = backend.ErrUnsupported
	} else {
		result.config, result.configErr = model.configSource.LoadConfig(ctx)
	}
	if model.statusSource == nil {
		result.mutationErr = backend.ErrUnsupported
	} else {
		result.mutation, result.mutationErr = model.statusSource.LoadConfigMutationStatus(ctx)
	}
	if model.source == nil {
		result.runtimeErr = backend.ErrUnsupported
	} else {
		result.runtime, result.runtimeErr = model.source.LoadSessions(ctx)
	}
	if model.sessionSource == nil {
		result.sessionsErr = backend.ErrUnsupported
	} else {
		result.sessions, result.sessionsErr = model.sessionSource.LoadAgentSessions(ctx)
	}
	return result
}

func (model *Model) applyLoadResult(result loadResultMsg) {
	model.runtime = cloneRuntimeSnapshot(result.runtime)
	model.lastLoadError = errors.Join(result.configErr, result.runtimeErr, result.sessionsErr, result.mutationErr)

	replacedDraft := false
	switch {
	case result.configErr == nil && (model.draft == nil || result.forceReplace || !model.draft.IsDirty()):
		model.draft = configdraft.Load(result.config)
		replacedDraft = true
	case result.configErr == nil:
		model.draft.SetDisconnected(false)
		model.draft.BeginConflict(result.config)
		_ = model.draft.Reapply()
	case model.draft == nil:
		model.draft = configdraft.Load(generated.MutableConfigSnapshot{
			MutableConfig: emptyMutableConfigView(),
		})
		model.draft.SetDisconnected(true)
		replacedDraft = true
	default:
		model.draft.SetDisconnected(true)
	}

	if result.configErr == nil {
		model.draft.SetDisconnected(false)
	}
	if result.mutationErr == nil {
		model.draft.SetMutationStatus(result.mutation)
	}

	if replacedDraft || model.overview == nil {
		model.composePages(result.sessions, result.sessionsErr)
	} else {
		model.replaceSessionsPage(result.sessions, result.sessionsErr)
		model.refreshPages(result.runtimeErr != nil)
	}

	if result.mutationErr == nil && result.mutation == generated.ConfigMutationStatusSecretCleanupPending {
		model.targets.ApplyCleanupPending()
	}
	if result.configErr != nil {
		model.status = "Configuration disconnected"
	} else if result.runtimeErr != nil {
		model.status = "Runtime snapshot disconnected"
	} else if result.sessionsErr != nil {
		model.status = "Sessions disconnected"
	} else {
		model.status = ""
	}
	if replacedDraft && result.configErr == nil && len(result.config.MutableConfig.ClientProfiles) == 0 && len(result.config.MutableConfig.Endpoints) == 0 {
		model.active = tabTargets
		model.targets.BeginCreate()
		model.pageFocus = true
	}
	model.resizePages()
}

func (model *Model) composePages(initialSessions []generated.AgentSession, sessionsErr error) {
	model.overview = overview.NewPage(model.draft)
	model.profiles = profiles.NewPage(model.draft, profiles.Options{Referrers: model.profileReferrers})
	var discover routes.ModelDiscoverer
	if source, ok := model.source.(targetModelSource); ok {
		discover = source.DiscoverTargetModels
	}
	model.routes = routes.New(model.draft, routes.Options{
		Status: routeStatuses(model.runtime.Routes), DiscoverModels: discover,
		TrafficRuleReferrers: model.trafficRuleReferrers,
	})
	model.targets = targets.New(targets.Deps{
		Draft:        model.draft,
		StageClient:  model.stageSource,
		TargetStatus: targetStatuses(model.runtime.Targets),
		Scope:        "all",
	})
	model.quotas = quotas.NewPage(model.draft)
	model.observations = observations.NewPage()
	model.replaceSessionsPage(initialSessions, sessionsErr)
	model.refreshPages(false)
}

func (model *Model) replaceSessionsPage(initial []generated.AgentSession, initialErr error) {
	loader := &initialSessionLoader{
		initial: append([]generated.AgentSession(nil), initial...),
		err:     initialErr,
		source:  model.sessionSource,
	}
	deps := sessionpage.Deps{
		Load: loader.Load,
		// The Agent Deck home is the user-facing Sessions page and owns runtime
		// preview requests. Keep this legacy page as a passive compatibility view
		// so it cannot issue a second preview request for the same selection.
		Backend: nil,
		ReloadSession: func(ctx context.Context, id string) (generated.AgentSession, error) {
			if model.sessionSource == nil {
				return generated.AgentSession{}, backend.ErrUnsupported
			}
			sessions, err := model.sessionSource.LoadAgentSessions(ctx)
			if err != nil {
				return generated.AgentSession{}, err
			}
			for _, session := range sessions {
				if string(session.Id) == id {
					return session, nil
				}
			}
			return generated.AgentSession{}, fmt.Errorf("session %s not found", id)
		},
	}
	model.sessions = sessionpage.New(deps)
	_ = model.sessions.Refresh(model.ctx)
}

func (model *Model) refreshPages(runtimeDisconnected bool) {
	model.overview.Refresh()
	model.profiles.Refresh()
	model.routes.Refresh()
	model.targets.Refresh()
	model.quotas.SetRuntime(model.runtime.QuotaGroups)
	model.quotas.Sync()
	model.observations.SetObservations(model.runtime.Observations)
	model.observations.SetDisconnected(runtimeDisconnected)
}

func (model *Model) profileReferrers(profileID string) []profiles.Referrer {
	var refs []profiles.Referrer
	for _, session := range model.runtime.Sessions {
		if session.ClientProfileID == profileID {
			refs = append(refs, profiles.Referrer{
				Path: fmt.Sprintf("sessions[%s].client_profile_id", session.ID),
			})
		}
	}
	return refs
}

func (model *Model) trafficRuleReferrers(profileID string) []string {
	var refs []string
	for _, session := range model.runtime.Sessions {
		if session.ClientProfileID == profileID {
			label := session.Title
			if strings.TrimSpace(label) == "" {
				label = session.ID
			}
			refs = append(refs, fmt.Sprintf("Session %q is using this traffic rule", label))
		}
	}
	return refs
}

func targetStatuses(runtime []backend.Target) targets.StaticTargetStatusProvider {
	statuses := make(targets.StaticTargetStatusProvider, len(runtime))
	for _, target := range runtime {
		statuses[target.ID] = targets.TargetRuntimeStatus{
			Health:   target.Health,
			Eligible: target.BaseEligible && !target.Suspended,
		}
	}
	return statuses
}

func routeStatuses(runtime []backend.Route) routes.StaticRouteStatusProvider {
	statuses := make(routes.StaticRouteStatusProvider, len(runtime))
	for _, route := range runtime {
		statuses[route.ID] = routes.RouteRuntimeStatus{
			EligibleTargetIDs: append([]string(nil), route.EligibleTargetIDs...),
		}
	}
	return statuses
}

func emptyMutableConfigView() generated.MutableConfigView {
	return generated.MutableConfigView{
		BackendSets:             []generated.BackendSetConfig{},
		ClientProfiles:          []generated.MutableClientProfile{},
		CompatibilityTransforms: []generated.CompatibilityTransformConfig{},
		ContentPolicies:         []generated.ContentPolicyConfig{},
		Credentials:             []generated.MutableCredentialView{},
		Endpoints:               []generated.EndpointConfig{},
		ModelPolicies:           []generated.ModelPolicyConfig{},
		ModelProjections:        []generated.ModelProjectionConfig{},
		QuotaGroups:             []generated.QuotaGroupConfig{},
		Routes:                  []generated.RouteConfig{},
		Targets:                 []generated.TargetConfig{},
	}
}

type initialSessionLoader struct {
	initial []generated.AgentSession
	err     error
	source  agentSessionSource
	used    bool
}

func (loader *initialSessionLoader) Load(ctx context.Context) ([]generated.AgentSession, error) {
	if !loader.used {
		loader.used = true
		return append([]generated.AgentSession(nil), loader.initial...), loader.err
	}
	if loader.source == nil {
		return nil, backend.ErrUnsupported
	}
	return loader.source.LoadAgentSessions(ctx)
}

func cloneRuntimeSnapshot(source backend.Snapshot) backend.Snapshot {
	clone := source
	clone.ClientProfiles = append([]backend.ClientProfile(nil), source.ClientProfiles...)
	clone.Routes = append([]backend.Route(nil), source.Routes...)
	for index := range clone.Routes {
		clone.Routes[index].EligibleTargetIDs = append([]string(nil), source.Routes[index].EligibleTargetIDs...)
	}
	clone.Targets = append([]backend.Target(nil), source.Targets...)
	clone.QuotaGroups = append([]backend.QuotaGroup(nil), source.QuotaGroups...)
	clone.Observations = append([]backend.Observation(nil), source.Observations...)
	clone.Sessions = append([]backend.Session(nil), source.Sessions...)
	return clone
}

type invalidationWatchStartedMsg struct {
	events <-chan backend.Invalidation
	err    error
}

type invalidationMsg struct {
	event  backend.Invalidation
	events <-chan backend.Invalidation
}

type invalidationStreamClosedMsg struct{}
type invalidationWatchRetryMsg struct{}

func (model *Model) startInvalidationWatch() tea.Cmd {
	cursor := model.eventCursor
	if cursor == "" {
		cursor = model.runtime.LastEventID
	}
	return func() tea.Msg {
		source, ok := model.source.(interface {
			WatchInvalidations(context.Context, string) (<-chan backend.Invalidation, error)
		})
		if !ok {
			return invalidationWatchStartedMsg{err: backend.ErrUnsupported}
		}
		events, err := source.WatchInvalidations(model.ctx, cursor)
		return invalidationWatchStartedMsg{events: events, err: err}
	}
}

func listenForInvalidation(events <-chan backend.Invalidation) tea.Cmd {
	return func() tea.Msg {
		event, open := <-events
		if !open {
			return invalidationStreamClosedMsg{}
		}
		return invalidationMsg{event: event, events: events}
	}
}

func retryInvalidationWatch() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return invalidationWatchRetryMsg{} })
}
