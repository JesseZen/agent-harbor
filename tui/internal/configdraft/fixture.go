package configdraft

import (
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
)

type fixtureBuilder struct {
	generation  int64
	revision    string
	instanceID  generated.InstanceID
	routes      []generated.RouteConfig
	credentials []generated.MutableCredentialView
}

type FixtureOption func(*fixtureBuilder)

func WithGeneration(g int64) FixtureOption {
	return func(b *fixtureBuilder) { b.generation = g }
}

func WithRevision(r string) FixtureOption {
	return func(b *fixtureBuilder) { b.revision = r }
}

func WithInstanceID(id string) FixtureOption {
	return func(b *fixtureBuilder) { b.instanceID = generated.InstanceID(id) }
}

func WithRoute(r generated.RouteConfig) FixtureOption {
	return func(b *fixtureBuilder) { b.routes = append(b.routes, r) }
}

func WithCredential(c generated.MutableCredentialView) FixtureOption {
	return func(b *fixtureBuilder) { b.credentials = append(b.credentials, c) }
}

func WithManagedCredential(id, name, provider string, generation int) FixtureOption {
	return func(b *fixtureBuilder) {
		var binding generated.CredentialSecretBinding
		_ = binding.FromCredentialSecretBinding0(generated.CredentialSecretBinding0{
			Mode:       generated.CredentialSecretBindingManaged,
			Configured: generated.CredentialSecretBindingConfiguredTrue,
		})
		gen := generation
		b.credentials = append(b.credentials, generated.MutableCredentialView{
			Id:            generated.ConfigID(id),
			Name:          name,
			Provider:      generated.MutableCredentialViewProvider(provider),
			Generation:    &gen,
			SecretBinding: binding,
		})
	}
}

func WithExternalCredential(id, name, provider string, ref generated.ExternalSecretRef) FixtureOption {
	return func(b *fixtureBuilder) {
		var binding generated.CredentialSecretBinding
		_ = binding.FromCredentialSecretBinding1(generated.CredentialSecretBinding1{
			Mode: generated.CredentialSecretBindingExternalRef,
			Ref:  ref,
		})
		b.credentials = append(b.credentials, generated.MutableCredentialView{
			Id:            generated.ConfigID(id),
			Name:          name,
			Provider:      generated.MutableCredentialViewProvider(provider),
			SecretBinding: binding,
		})
	}
}

func FixtureSnapshot(opts ...FixtureOption) generated.MutableConfigSnapshot {
	b := &fixtureBuilder{
		generation: 1,
		revision:   "rev-1",
		instanceID: "inst-1",
	}
	for _, opt := range opts {
		opt(b)
	}
	return generated.MutableConfigSnapshot{
		ActiveGeneration: b.generation,
		ConfigRevision:   b.revision,
		InstanceId:       b.instanceID,
		CompiledSummary:  generated.CompiledSummary{},
		MutableConfig: generated.MutableConfigView{
			Instance: generated.MutableInstanceConfig{
				LogLevel: generated.MutableInstanceConfigLogLevelSimple,
			},
			BackendSets:             []generated.BackendSetConfig{},
			ClientProfiles:          []generated.MutableClientProfile{},
			CompatibilityTransforms: []generated.CompatibilityTransformConfig{},
			ContentPolicies:         []generated.ContentPolicyConfig{},
			Credentials:             b.credentials,
			Endpoints:               []generated.EndpointConfig{},
			ModelPolicies:           []generated.ModelPolicyConfig{},
			ModelProjections:        []generated.ModelProjectionConfig{},
			QuotaGroups:             []generated.QuotaGroupConfig{},
			Routes:                  b.routes,
			Targets:                 []generated.TargetConfig{},
		},
	}
}
