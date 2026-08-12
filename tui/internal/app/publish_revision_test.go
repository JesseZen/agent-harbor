package app

import (
	"errors"
	"regexp"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/backend"
	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
)

func TestNewConfigRevisionIsUniqueAndAPISafe(t *testing.T) {
	first, err := newConfigRevision()
	if err != nil {
		t.Fatalf("first revision: %v", err)
	}
	second, err := newConfigRevision()
	if err != nil {
		t.Fatalf("second revision: %v", err)
	}
	if first == second {
		t.Fatalf("duplicate revision %q", first)
	}
	valid := regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,119}$`)
	for _, revision := range []string{first, second} {
		if !valid.MatchString(revision) {
			t.Fatalf("revision is not API-safe: %q", revision)
		}
	}
}

func TestManagedTargetProbeRefsOnlyIncludesOwnedRuntimeTargets(t *testing.T) {
	snapshot := configdraft.FixtureSnapshot()
	objects := []generated.ManagedObject{{
		Id: "upstream-main", Name: "Main", Kind: generated.ManagedObjectKindUpstream,
		Members: []generated.ManagedResourceRef{{Id: "target-managed", Kind: generated.ManagedResourceRefKindTarget}},
	}}
	snapshot.MutableConfig.ManagedObjects = &objects
	runtime := backend.Snapshot{Targets: []backend.Target{
		{ID: "target-unmanaged", TargetGeneration: 2},
		{ID: "target-managed", TargetGeneration: 7},
	}}

	refs := managedTargetProbeRefs(snapshot, runtime)
	if len(refs) != 1 || refs[0].id != "target-managed" || refs[0].generation != 7 {
		t.Fatalf("managed probe refs=%#v", refs)
	}
}

func TestProbeQueueFailureKeepsSuccessfulSaveVisible(t *testing.T) {
	model := loadedRootModel(t, &appBackend{})
	updated, command := model.Update(targetProbeQueuedMsg{targetIDs: []string{"target-managed"}, err: errors.New("connection refused")})
	model = updated.(*Model)
	if command != nil || model.status != "Saved, but connection test failed: connection refused" {
		t.Fatalf("status=%q command=%v", model.status, command)
	}
}

func TestLauncherIdentityFailureUsesActionableStatus(t *testing.T) {
	model := loadedRootModel(t, &appBackend{})
	model.applyPublishResult(publishResultMsg{err: &coreclient.APIError{
		Operation: "validateConfig", StatusCode: 422, Code: "launcher_identity",
		Detail: "mutable_config.client_profiles[0].launcher: Field violates the closed Admin contract.",
	}})
	if model.status != "Save blocked: CLI installation is not trusted · run agent-harbor doctor" {
		t.Fatalf("status=%q", model.status)
	}
}
