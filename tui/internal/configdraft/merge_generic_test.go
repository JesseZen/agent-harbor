package configdraft

import (
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMerge_GenericCollectionFieldLevelCoalesce(t *testing.T) {
	baseSet := generated.BackendSetConfig{
		Id: "bs1", Name: "Base", Candidates: []generated.BackendCandidate{{TargetId: "t1"}},
	}
	snap := FixtureSnapshot()
	snap.MutableConfig.BackendSets = []generated.BackendSetConfig{baseSet}
	d := Load(snap)
	d.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.BackendSets[0].Name = "Local"
	})

	current := snap
	current.ActiveGeneration = 2
	current.MutableConfig.BackendSets = []generated.BackendSetConfig{{
		Id: "bs1", Name: "Base", Candidates: []generated.BackendCandidate{{TargetId: "t2"}},
	}}
	d.BeginConflict(current)
	conflicts := d.Reapply()
	assert.Empty(t, conflicts)
	require.Len(t, d.LocalCommand().BackendSets, 1)
	assert.Equal(t, "Local", d.LocalCommand().BackendSets[0].Name)
	assert.Equal(t, generated.ConfigID("t2"), d.LocalCommand().BackendSets[0].Candidates[0].TargetId)
}

func TestMerge_GenericCollectionConflictKeepsDraftValue(t *testing.T) {
	baseSet := generated.BackendSetConfig{
		Id: "bs1", Name: "Base", Candidates: []generated.BackendCandidate{{TargetId: "t1"}},
	}
	snap := FixtureSnapshot()
	snap.MutableConfig.BackendSets = []generated.BackendSetConfig{baseSet}
	d := Load(snap)
	d.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.BackendSets[0].Name = "Local"
	})

	current := snap
	current.ActiveGeneration = 3
	current.MutableConfig.BackendSets = []generated.BackendSetConfig{{
		Id: "bs1", Name: "Remote", Candidates: []generated.BackendCandidate{{TargetId: "t1"}},
	}}
	d.BeginConflict(current)
	conflicts := d.Reapply()
	require.NotEmpty(t, conflicts)
	require.Len(t, d.LocalCommand().BackendSets, 1)
	assert.Equal(t, "Local", d.LocalCommand().BackendSets[0].Name, "conflict must not zero merged draft")
}
