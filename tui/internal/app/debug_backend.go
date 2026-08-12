package app

import (
	"context"

	"github.com/asheshgoplani/agent-deck/internal/backend"
	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
)

const debugUIInstanceID = "ins_debug_ui"

// DebugBackend is an in-process empty Core stand-in for UI work without a
// running agent-harbor-core. Tabs and chrome render; mutations are unsupported.
type DebugBackend struct {
	backend.UnimplementedBackend
}

// NewDebugBackend returns a ready-to-use empty debug UI backend.
func NewDebugBackend() *DebugBackend {
	return &DebugBackend{}
}

func (*DebugBackend) LoadSessions(context.Context) (backend.Snapshot, error) {
	return backend.Snapshot{
		Identity: backend.Identity{
			InstanceID: debugUIInstanceID,
			Binary:     "debug-ui",
			Version:    "debug",
		},
		Generation:     1,
		ConfigRevision: "debug",
		Ready:          true,
	}, nil
}

func (*DebugBackend) LoadConfig(context.Context) (generated.MutableConfigSnapshot, error) {
	return configdraft.FixtureSnapshot(configdraft.WithInstanceID(debugUIInstanceID)), nil
}

func (*DebugBackend) LoadConfigMutationStatus(context.Context) (generated.ConfigMutationStatus, error) {
	return generated.ConfigMutationStatusAvailable, nil
}

func (*DebugBackend) LoadAgentSessions(context.Context) ([]generated.AgentSession, error) {
	return nil, nil
}

func (*DebugBackend) ValidateConfig(context.Context, generated.ValidateConfigCommand) (generated.ValidationResult, error) {
	return generated.ValidationResult{Valid: true}, nil
}

func (*DebugBackend) PatchConfig(context.Context, generated.PatchConfigCommand) (generated.SnapshotIdentity, error) {
	return generated.SnapshotIdentity{}, backend.ErrUnsupported
}

func (*DebugBackend) Preview(context.Context, backend.SessionRef) (backend.Preview, error) {
	return backend.Preview{Lines: []string{"debug-ui: no session preview"}}, nil
}
