package backend

import (
	"context"
	"errors"
	"io"
	"time"
)

var ErrUnsupported = errors.New("backend operation is not supported")

type Status string

const (
	StatusCreated   Status = "created"
	StatusLaunching Status = "launching"
	StatusRunning   Status = "running"
	StatusEnding    Status = "ending"
	StatusIdle      Status = "idle"
	StatusFailed    Status = "failed"
	StatusEnded     Status = "ended"
)

type NativeActivity string

const (
	NativeActivityRunning NativeActivity = "running"
	NativeActivityWaiting NativeActivity = "waiting"
	NativeActivityIdle    NativeActivity = "idle"
	NativeActivityFailed  NativeActivity = "failed"
	NativeActivityUnknown NativeActivity = "unknown"
)

type ActivitySource string

const (
	ActivitySourceHook      ActivitySource = "hook"
	ActivitySourcePane      ActivitySource = "pane"
	ActivitySourceProcess   ActivitySource = "process"
	ActivitySourceLifecycle ActivitySource = "lifecycle"
	ActivitySourceUnknown   ActivitySource = "unknown"
)

type HookHealthState string

const (
	HookHealthActive      HookHealthState = "active"
	HookHealthInvalid     HookHealthState = "invalid"
	HookHealthStale       HookHealthState = "stale"
	HookHealthUnsupported HookHealthState = "unsupported"
)

type NativeProvider string

const (
	NativeProviderClaude NativeProvider = "claude"
	NativeProviderCodex  NativeProvider = "codex"
)

type Session struct {
	ID                   string
	Title                string
	ProjectPath          string
	GroupPath            string
	Tool                 string
	Status               Status
	CreatedAt            time.Time
	LastAccessedAt       time.Time
	UpdatedAt            time.Time
	ClientProfileID      string
	RouteID              string
	CredentialGeneration int
	NativeActivity       NativeActivity
	ActivitySource       ActivitySource
	ActivityObservedAt   time.Time
	HookHealth           HookHealthState
	HookHealthObservedAt time.Time
	NativeProvider       NativeProvider
	NativeSessionID      string
}

type Snapshot struct {
	Identity       Identity
	Generation     int64
	ConfigRevision string
	Ready          bool
	DroppedEvents  int
	LastEventID    string
	ClientProfiles []ClientProfile
	Routes         []Route
	Targets        []Target
	QuotaGroups    []QuotaGroup
	Observations   []Observation
	Sessions       []Session
}

type Identity struct {
	InstanceID  string
	Binary      string
	Version     string
	AdminSocket string
}

type ClientProfile struct {
	ID               string
	Name             string
	Launcher         string
	DefaultRouteID   string
	DefaultRouteName string
}

type Route struct {
	ID                  string
	Name                string
	BackendSetID        string
	Policy              string
	EligibleTargetIDs   []string
	RecentDecisionCount int
}

type Target struct {
	ID                   string
	Name                 string
	Adapter              string
	Health               string
	BaseEligible         bool
	Suspended            bool
	QuotaGroupID         string
	CredentialAccess     string
	CredentialGeneration int
	TargetGeneration     int
}

type QuotaGroup struct {
	ID                string
	Name              string
	ActiveConcurrency int
	MaxConcurrency    int
	RPM               int
	ForegroundDepth   int
	BackgroundDepth   int
	NextPermitAt      time.Time
}

type Observation struct {
	ID                 string
	Type               string
	OccurredAt         time.Time
	SnapshotGeneration int64
	SessionID          string
	RouteID            string
	TargetID           string
	QuotaGroupID       string
	DecisionReason     string
	PolicyDecision     string
	SemanticOutcome    string
}

type Invalidation struct {
	EventID string
	Type    string
	Err     error
}

type SessionRef struct {
	ID                   string
	ExpectedUpdatedAt    time.Time
	CredentialGeneration int
}

type CreateSessionRequest struct {
	ExpectedSnapshotGeneration int64
	Label                      string
	Workspace                  string
	ClientProfileID            string
	RouteID                    string
}

type EndMode string

const (
	EndGraceful EndMode = "graceful"
	EndForce    EndMode = "force"
)

type EndSessionRequest struct {
	Session SessionRef
	Mode    EndMode
	Timeout time.Duration
}

type Preview struct {
	SessionID  string
	Lines      []string
	ObservedAt time.Time
	Truncated  bool
}

type HookHealth struct {
	SessionID  string
	Provider   string
	State      string
	ObservedAt time.Time
}

// AttachCommand contains only non-secret metadata needed to invoke Core's
// public one-shot attach helper. The grant itself is written exclusively to
// the caller-provided descriptor.
type AttachCommand struct {
	Executable        string
	InstanceID        string
	AdminSocket       string
	SessionID         string
	ExpectedUpdatedAt time.Time
}

type SessionLoader interface {
	LoadSessions(context.Context) (Snapshot, error)
}

type Backend interface {
	SessionLoader
	CreateSession(context.Context, CreateSessionRequest) (Session, error)
	LaunchSession(context.Context, SessionRef, time.Duration) (Session, error)
	ResumeSession(context.Context, SessionRef, time.Duration) (Session, error)
	EndSession(context.Context, EndSessionRequest) error
	RotateCredential(context.Context, SessionRef, io.Writer) (int, error)
	RevokeCredential(context.Context, SessionRef) error
	ResetAffinity(context.Context, SessionRef) error
	Preview(context.Context, SessionRef) (Preview, error)
	HookHealth(context.Context, SessionRef) (HookHealth, error)
	PrepareAttach(context.Context, SessionRef, io.Writer) (AttachCommand, error)
	WatchInvalidations(context.Context, string) (<-chan Invalidation, error)
}

type UnimplementedBackend struct{}

func (UnimplementedBackend) LoadSessions(context.Context) (Snapshot, error) {
	return Snapshot{}, ErrUnsupported
}

func (UnimplementedBackend) CreateSession(context.Context, CreateSessionRequest) (Session, error) {
	return Session{}, ErrUnsupported
}

func (UnimplementedBackend) LaunchSession(context.Context, SessionRef, time.Duration) (Session, error) {
	return Session{}, ErrUnsupported
}

func (UnimplementedBackend) ResumeSession(context.Context, SessionRef, time.Duration) (Session, error) {
	return Session{}, ErrUnsupported
}

func (UnimplementedBackend) EndSession(context.Context, EndSessionRequest) error {
	return ErrUnsupported
}

func (UnimplementedBackend) RotateCredential(context.Context, SessionRef, io.Writer) (int, error) {
	return 0, ErrUnsupported
}

func (UnimplementedBackend) RevokeCredential(context.Context, SessionRef) error {
	return ErrUnsupported
}

func (UnimplementedBackend) ResetAffinity(context.Context, SessionRef) error {
	return ErrUnsupported
}

func (UnimplementedBackend) Preview(context.Context, SessionRef) (Preview, error) {
	return Preview{}, ErrUnsupported
}

func (UnimplementedBackend) HookHealth(context.Context, SessionRef) (HookHealth, error) {
	return HookHealth{}, ErrUnsupported
}

func (UnimplementedBackend) PrepareAttach(context.Context, SessionRef, io.Writer) (AttachCommand, error) {
	return AttachCommand{}, ErrUnsupported
}

func (UnimplementedBackend) WatchInvalidations(context.Context, string) (<-chan Invalidation, error) {
	return nil, ErrUnsupported
}
