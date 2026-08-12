package app

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/backend"
)

// snapshotBackend keeps the complete Agent Deck Session workbench on Core's
// public backend contract while the other tabs use their typed page clients.
type snapshotBackend struct {
	source backend.SessionLoader
	mu     sync.RWMutex
	latest backend.Snapshot
}

func (cache *snapshotBackend) LoadSessions(ctx context.Context) (backend.Snapshot, error) {
	snapshot, err := cache.source.LoadSessions(ctx)
	if err == nil {
		cache.mu.Lock()
		cache.latest = cloneSnapshot(snapshot)
		cache.mu.Unlock()
	}
	return snapshot, err
}

func (cache *snapshotBackend) Snapshot() backend.Snapshot {
	cache.mu.RLock()
	defer cache.mu.RUnlock()
	return cloneSnapshot(cache.latest)
}

func (cache *snapshotBackend) runtime() (backend.Backend, error) {
	value, ok := cache.source.(backend.Backend)
	if !ok {
		return nil, backend.ErrUnsupported
	}
	return value, nil
}

func (cache *snapshotBackend) CreateSession(ctx context.Context, request backend.CreateSessionRequest) (backend.Session, error) {
	value, err := cache.runtime()
	if err != nil {
		return backend.Session{}, err
	}
	return value.CreateSession(ctx, request)
}

func (cache *snapshotBackend) LaunchSession(ctx context.Context, ref backend.SessionRef, timeout time.Duration) (backend.Session, error) {
	value, err := cache.runtime()
	if err != nil {
		return backend.Session{}, err
	}
	return value.LaunchSession(ctx, ref, timeout)
}

func (cache *snapshotBackend) ResumeSession(ctx context.Context, ref backend.SessionRef, timeout time.Duration) (backend.Session, error) {
	value, err := cache.runtime()
	if err != nil {
		return backend.Session{}, err
	}
	return value.ResumeSession(ctx, ref, timeout)
}

func (cache *snapshotBackend) EndSession(ctx context.Context, request backend.EndSessionRequest) error {
	value, err := cache.runtime()
	if err != nil {
		return err
	}
	return value.EndSession(ctx, request)
}

func (cache *snapshotBackend) RotateCredential(ctx context.Context, ref backend.SessionRef, destination io.Writer) (int, error) {
	value, err := cache.runtime()
	if err != nil {
		return 0, err
	}
	return value.RotateCredential(ctx, ref, destination)
}

func (cache *snapshotBackend) RevokeCredential(ctx context.Context, ref backend.SessionRef) error {
	value, err := cache.runtime()
	if err != nil {
		return err
	}
	return value.RevokeCredential(ctx, ref)
}

func (cache *snapshotBackend) ResetAffinity(ctx context.Context, ref backend.SessionRef) error {
	value, err := cache.runtime()
	if err != nil {
		return err
	}
	return value.ResetAffinity(ctx, ref)
}

func (cache *snapshotBackend) Preview(ctx context.Context, ref backend.SessionRef) (backend.Preview, error) {
	value, err := cache.runtime()
	if err != nil {
		return backend.Preview{}, err
	}
	return value.Preview(ctx, ref)
}

func (cache *snapshotBackend) HookHealth(ctx context.Context, ref backend.SessionRef) (backend.HookHealth, error) {
	value, err := cache.runtime()
	if err != nil {
		return backend.HookHealth{}, err
	}
	return value.HookHealth(ctx, ref)
}

func (cache *snapshotBackend) PrepareAttach(ctx context.Context, ref backend.SessionRef, destination io.Writer) (backend.AttachCommand, error) {
	value, err := cache.runtime()
	if err != nil {
		return backend.AttachCommand{}, err
	}
	return value.PrepareAttach(ctx, ref, destination)
}

func (cache *snapshotBackend) WatchInvalidations(ctx context.Context, cursor string) (<-chan backend.Invalidation, error) {
	value, err := cache.runtime()
	if err != nil {
		return nil, err
	}
	return value.WatchInvalidations(ctx, cursor)
}

func cloneSnapshot(source backend.Snapshot) backend.Snapshot {
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
