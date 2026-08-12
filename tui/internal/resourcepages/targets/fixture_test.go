package targets

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/secretinput"
)

func plantedToken() []byte {
	parts := [][]byte{
		{0x70, 0x6c, 0x61, 0x6e, 0x74, 0x65, 0x64},
		{'-'},
		{0x63, 0x72, 0x65, 0x64},
		{'-'},
		{0x74, 0x6f, 0x6b, 0x65, 0x6e},
		{'-'},
		{0x5a, 0x5a, 0x59, 0x59, 0x58, 0x58, 0x57, 0x57},
	}
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

type fakeStageHTTP struct {
	createFn func(ctx context.Context, contentType string, body []byte) (*generated.CreateProviderSecretStageResponse, error)
	deleteFn func(ctx context.Context, stageID generated.SecretStageID) (*generated.DeleteProviderSecretStageResponse, error)

	createCalls int
	deleteCalls int
	deletedIDs  []string
	lastBody    []byte
}

func (f *fakeStageHTTP) CreateProviderSecretStageWithBodyWithResponse(
	ctx context.Context,
	contentType string,
	body io.Reader,
	_ ...generated.RequestEditorFn,
) (*generated.CreateProviderSecretStageResponse, error) {
	f.createCalls++
	raw, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	f.lastBody = append([]byte(nil), raw...)
	if f.createFn == nil {
		return nil, errors.New("createFn not set")
	}
	return f.createFn(ctx, contentType, raw)
}

func (f *fakeStageHTTP) DeleteProviderSecretStageWithResponse(
	ctx context.Context,
	stageID generated.SecretStageID,
	_ ...generated.RequestEditorFn,
) (*generated.DeleteProviderSecretStageResponse, error) {
	f.deleteCalls++
	f.deletedIDs = append(f.deletedIDs, string(stageID))
	if f.deleteFn == nil {
		return &generated.DeleteProviderSecretStageResponse{
			HTTPResponse: &http.Response{StatusCode: 204},
		}, nil
	}
	return f.deleteFn(ctx, stageID)
}

func okCreate(stageID string) func(context.Context, string, []byte) (*generated.CreateProviderSecretStageResponse, error) {
	return func(context.Context, string, []byte) (*generated.CreateProviderSecretStageResponse, error) {
		return &generated.CreateProviderSecretStageResponse{
			HTTPResponse: &http.Response{StatusCode: 201},
			JSON201: &generated.ProviderSecretStage{
				StageId:   generated.SecretStageID(stageID),
				ExpiresAt: time.Now().UTC().Add(time.Hour),
			},
		}, nil
	}
}

func seedDraft(t *testing.T) *configdraft.Draft {
	t.Helper()
	ref := generated.ExternalSecretRef{
		Env:        &generated.EnvSecretLocator{Name: "OPENAI_API_KEY"},
		Exportable: false,
	}
	snap := configdraft.FixtureSnapshot(
		configdraft.WithManagedCredential("cred-1", "Primary", "openai", 3),
		configdraft.WithExternalCredential("cred-ext", "External", "anthropic", ref),
	)
	draft := configdraft.Load(snap)
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Targets = []generated.MutableTargetCommand{{
			Id:           "tgt-1",
			Name:         "target-1",
			Adapter:      generated.MutableTargetCommandAdapterOpenai,
			Bridge:       generated.MutableTargetCommandBridgeOpenaiChat,
			Capabilities: []generated.MutableTargetCommandCapabilities{generated.MutableTargetCommandCapabilitiesChat},
			CredentialId: "cred-1",
			EndpointId:   "ep-1",
			QuotaGroupId: "quota-1",
			HealthPolicy: generated.HealthPolicyConfig{
				FailureThreshold:         2,
				InitialBackoffMs:         100,
				JitterPercent:            10,
				MaxBackoffMs:             1000,
				ProbeTimeoutMs:           1000,
				RecoverySuccessThreshold: 1,
				StableProbeIntervalMs:    5000,
			},
			ThrottlePolicy: generated.ThrottlePolicyConfig{
				DefaultCoolingMs: 1000,
				MaxCoolingMs:     5000,
			},
		}}
		cmd.Endpoints = []generated.EndpointConfig{{
			Id:                      "ep-1",
			Name:                    "endpoint-1",
			BaseUrl:                 "https://api.example.com",
			Http2Enabled:            true,
			AllowPrivateNetwork:     false,
			IdleConnectionTimeoutMs: 30000,
			MaxIdleConnections:      10,
		}}
		cmd.QuotaGroups = []generated.QuotaGroupConfig{{
			Id:                 "quota-1",
			Name:               "interactive",
			MaxConcurrency:     8,
			Rpm:                240,
			ForegroundCapacity: 4,
			ForegroundWeight:   2,
			BackgroundCapacity: 2,
			BackgroundWeight:   1,
			QueueTimeoutMs:     5000,
		}}
	})
	return draft
}

func newTestPage(t *testing.T, client secretinput.StageHTTPClient) (*Page, *configdraft.Draft, *fakeStageHTTP) {
	t.Helper()
	draft := seedDraft(t)
	fake, ok := client.(*fakeStageHTTP)
	if !ok {
		fake = &fakeStageHTTP{createFn: okCreate("stage-default")}
		client = fake
	}
	page := New(Deps{
		Draft:       draft,
		StageClient: client,
		TargetStatus: StaticTargetStatusProvider{
			"tgt-1": {Health: string(generated.Healthy), Eligible: true},
		},
		Scope: "all",
	})
	page.SetSize(120, 30)
	page.SetKind(KindCredentials)
	_ = page.Init()
	return page, draft, fake
}
