package main

import (
	"context"
	"errors"
	"strconv"
	"strings"
)

func (h harborService) ProfileCodexConnection(ctx context.Context, req profileClientRequest) (string, error) {
	result, err := h.ProfileCodexAccounts(ctx, codexAccountRequest{Profile: req.Binding, Action: "inspect"}, nil, nil, nil)
	if err != nil {
		return "", err
	}
	switch result.State {
	case "signed-out":
		return "api", nil
	case "selected":
		return "caller", nil
	default:
		return "", errors.New("login is expired, unavailable or unconfirmed; repair it or explicitly select Not signed in")
	}
}

// The Profile's selected login affects new connections only. Recovery consumes
// the persisted form; it never calls ProfileCodexConnection again.
func (h harborService) ProfileMagpieCatalog(ctx context.Context, req profileClientRequest, instanceID string) ([]catalogModel, error) {
	form, err := h.ProfileCodexConnection(ctx, req)
	if err != nil {
		return nil, err
	}
	env, err := h.ResolveProfileClient(req)
	if err != nil {
		return nil, err
	}
	registry, err := loadMagpieRegistry()
	if err != nil {
		return nil, err
	}
	instance, ok := registry.find(instanceID)
	if !ok {
		return nil, errors.New("Magpie instance not found")
	}
	var official func() []catalogModel
	if form == "caller" {
		official = func() []catalogModel { return cachedCodexModelsIn(env.Home) }
	}
	return fetchCatalogWithOfficial(instance.Address, official)
}

// A separate upstream-supported provider uses the gateway credential, never an
// OpenAI key or ChatGPT token. Do not reapply initial model/provider on resume.
func apiOnlyCodexArgs(s session, kind launchKind) []string {
	base := strings.TrimSuffix(strings.TrimRight(s.MagpieURL, "/"), "/v1") + "/v1"
	settings := []string{
		`model_providers.agent_harbor_magpie.name="Magpie"`,
		"model_providers.agent_harbor_magpie.base_url=" + strconv.Quote(base),
		`model_providers.agent_harbor_magpie.wire_api="responses"`,
		`model_providers.agent_harbor_magpie.env_key="AGENT_HARBOR_MAGPIE_KEY"`,
		"model_providers.agent_harbor_magpie.requires_openai_auth=false",
		"model_providers.agent_harbor_magpie.supports_websockets=false",
	}
	if kind == newConversation {
		settings = append(settings, `model_provider="agent_harbor_magpie"`, "model="+strconv.Quote(s.MagpieModel))
	}
	args := []string{}
	for _, setting := range settings {
		args = append(args, "-c", setting)
	}
	return args
}

func catalogForDraft(service applicationService, draft sessionDraft, id string) ([]catalogModel, error) {
	if draft.Profile == nil {
		return service.Catalog(id)
	}
	scoped, ok := service.(interface {
		ProfileMagpieCatalog(context.Context, profileClientRequest, string) ([]catalogModel, error)
	})
	if !ok {
		return nil, ErrProfileNotReady
	}
	return scoped.ProfileMagpieCatalog(context.Background(), profileClientRequest{Binding: *draft.Profile}, id)
}
