package configdraft

import (
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
)

func sampleTarget(id, name string, generation int) generated.TargetConfig {
	return generated.TargetConfig{
		Id: id, Name: name, EndpointId: "e1", CredentialId: "c1", QuotaGroupId: "q1",
		Adapter: generated.TargetConfigAdapterOpenai, Bridge: generated.TargetConfigBridgeOpenaiChat,
		Capabilities: []generated.TargetConfigCapabilities{generated.TargetConfigCapabilitiesChat},
		HealthPolicy: generated.HealthPolicyConfig{
			FailureThreshold: 2, InitialBackoffMs: 100, JitterPercent: 10, MaxBackoffMs: 1000,
			ProbeTimeoutMs: 1000, RecoverySuccessThreshold: 1, StableProbeIntervalMs: 5000,
		},
		ThrottlePolicy: generated.ThrottlePolicyConfig{DefaultCoolingMs: 1000, MaxCoolingMs: 5000},
		Generation:     generation,
	}
}

func TestDraft_CommandSuccessAndAccessors(t *testing.T) {
	d := Load(FixtureSnapshot(
		WithManagedCredential("cred-1", "Primary", "openai", 2),
		WithCredential(generated.MutableCredentialView{
			Id: "cred-2", Name: "Extra", Provider: generated.MutableCredentialViewProviderAnthropic,
			Generation: intPtr(1),
			SecretBinding: mustBinding(generated.CredentialSecretBinding0{
				Mode:       generated.CredentialSecretBindingManaged,
				Configured: generated.CredentialSecretBindingConfiguredTrue,
			}),
		}),
	))
	if d.BaseView().Instance.LogLevel == "" {
		t.Fatal("BaseView empty")
	}
	if d.Disconnected() {
		t.Fatal("unexpected disconnected")
	}
	if _, err := d.Command(); err != nil {
		t.Fatalf("Command: %v", err)
	}
	d.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Instance.LogLevel = generated.MutableInstanceConfigLogLevelDetail
	})
	if !d.DomainDirty(DomainInstance) || len(d.DirtyDomains()) == 0 {
		t.Fatal("expected dirty instance domain")
	}
	_ = d.Conflicts()
}

func TestDraft_TargetDirtyAndAcceptCurrent(t *testing.T) {
	snap := FixtureSnapshot()
	snap.MutableConfig.Targets = []generated.TargetConfig{sampleTarget("t1", "T", 3)}
	snap.MutableConfig.Endpoints = []generated.EndpointConfig{{
		Id: "e1", Name: "E", BaseUrl: "https://example.com", Http2Enabled: true,
		AllowPrivateNetwork: false, IdleConnectionTimeoutMs: 1000, MaxIdleConnections: 2,
	}}
	d := Load(snap)
	d.Mutate(func(cmd *generated.MutableConfigCommand) { cmd.Targets[0].Name = "Renamed" })
	if !d.DomainDirty(DomainTargets) {
		t.Fatal("targets should be dirty")
	}
	current := snap
	current.ActiveGeneration = 2
	current.MutableConfig.Instance.LogLevel = generated.MutableInstanceConfigLogLevelDetail
	d.BeginConflict(current)
	_ = d.Reapply()
	d.AcceptCurrent()
	if d.IsDirty() || d.InConflict() {
		t.Fatalf("AcceptCurrent left dirty=%v conflict=%v", d.IsDirty(), d.InConflict())
	}
}

func TestSecret_ActionFromModeRoundTrip(t *testing.T) {
	action, err := actionFromMode(SecretPreserve, "", generated.ExternalSecretRef{})
	if err != nil {
		t.Fatal(err)
	}
	mode, _, _, err := secretModeFromAction(action)
	if err != nil || mode != SecretPreserve {
		t.Fatalf("mode = %q err=%v", mode, err)
	}
	replace, err := actionFromMode(SecretReplace, "stage_1", generated.ExternalSecretRef{})
	if err != nil {
		t.Fatal(err)
	}
	mode, stage, _, err := secretModeFromAction(replace)
	if err != nil || mode != SecretReplace || stage != "stage_1" {
		t.Fatalf("replace mode=%q stage=%q err=%v", mode, stage, err)
	}
	ref := generated.ExternalSecretRef{Exportable: true, Env: &generated.EnvSecretLocator{Name: "TOKEN"}}
	ext, err := actionFromMode(SecretExternalRef, "", ref)
	if err != nil {
		t.Fatal(err)
	}
	mode, _, gotRef, err := secretModeFromAction(ext)
	if err != nil || mode != SecretExternalRef || gotRef.Env == nil || gotRef.Env.Name != "TOKEN" {
		t.Fatalf("external mode=%q ref=%#v err=%v", mode, gotRef, err)
	}
	d := Load(FixtureSnapshot(WithExternalCredential("cred-1", "E", "openai", ref)))
	d.SetCredentialReplace("cred-1", "stage_new")
	d.MarkReplaceRequired("cred-1")
	if d.CanPublish() {
		t.Fatal("replace_required should block publish")
	}
	if _, err := d.Command(); err == nil {
		t.Fatal("expected Command error")
	}
}

func TestMerge_TargetCollectionAndDelete(t *testing.T) {
	snap := FixtureSnapshot()
	snap.MutableConfig.Targets = []generated.TargetConfig{sampleTarget("t1", "T", 1)}
	d := Load(snap)
	d.Mutate(func(cmd *generated.MutableConfigCommand) { cmd.Targets[0].Name = "Local" })
	current := snap
	current.ActiveGeneration = 2
	current.MutableConfig.Targets[0].Name = "Current"
	d.BeginConflict(current)
	if len(d.Reapply()) == 0 {
		t.Fatal("expected target name conflict")
	}

	d2 := Load(snap)
	d2.Mutate(func(cmd *generated.MutableConfigCommand) { cmd.Targets = nil })
	cur2 := snap
	cur2.ActiveGeneration = 2
	cur2.MutableConfig.Targets[0].Name = "Changed"
	d2.BeginConflict(cur2)
	if len(d2.Reapply()) == 0 {
		t.Fatal("expected delete vs modify conflict")
	}
}

func TestHelpers_SetEqualAndWithCredential(t *testing.T) {
	if !setEqual([]string{"b", "a"}, []string{"a", "b"}) {
		t.Fatal("setEqual failed")
	}
	if canonicalSetJSON([]string{"b", "a"}) == "" {
		t.Fatal("canonicalSetJSON empty")
	}
	snap := FixtureSnapshot(WithCredential(generated.MutableCredentialView{
		Id: "cred-x", Name: "X", Provider: generated.MutableCredentialViewProviderOpenai,
		Generation: intPtr(9),
		SecretBinding: mustBinding(generated.CredentialSecretBinding0{
			Mode: generated.CredentialSecretBindingManaged, Configured: generated.CredentialSecretBindingConfiguredTrue,
		}),
	}))
	if len(snap.MutableConfig.Credentials) != 1 || snap.MutableConfig.Credentials[0].Id != "cred-x" {
		t.Fatalf("WithCredential = %#v", snap.MutableConfig.Credentials)
	}

	d := Load(FixtureSnapshot(WithRevision("rev-cover"), WithInstanceID("inst-cover")))
	if d.Revision() != "rev-cover" || d.InstanceID() != "inst-cover" {
		t.Fatalf("metadata revision=%q id=%q", d.Revision(), d.InstanceID())
	}
	local := d.LocalCommand()
	if local.Instance.LogLevel == "" {
		t.Fatal("LocalCommand empty")
	}
	d.SetDisconnected(true)
	if !d.Disconnected() || d.CanPublish() {
		t.Fatal("disconnected should disable publish")
	}
	d.SetDisconnected(false)
	d.SetMutationStatus(generated.ConfigMutationStatusSecretCleanupPending)
	if d.CanPublish() {
		t.Fatal("secret_cleanup_pending should disable publish")
	}
}

func intPtr(v int) *int { return &v }

func mustBinding(v generated.CredentialSecretBinding0) generated.CredentialSecretBinding {
	var binding generated.CredentialSecretBinding
	if err := binding.FromCredentialSecretBinding0(v); err != nil {
		panic(err)
	}
	return binding
}
