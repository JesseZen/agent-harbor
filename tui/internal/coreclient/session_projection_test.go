package coreclient

import (
	"testing"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/backend"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
)

func TestSessionProjectionPreservesAuthoritativeActivityAndNativeIdentity(t *testing.T) {
	observedAt := time.Unix(1_700_002_000, 0).UTC()
	provider := generated.AgentSessionNativeProviderCodex
	nativeID := "native-session-123"
	source := generated.AgentSession{
		Id:                       "ses_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Label:                    "Projection",
		Workspace:                "/workspace/projection",
		RouteId:                  "route_main",
		ClientProfileId:          "profile_codex",
		Lifecycle:                generated.AgentSessionLifecycleRunning,
		NativeActivity:           generated.AgentSessionNativeActivityWaiting,
		ActivitySource:           generated.SessionActivitySourceHook,
		NativeActivityObservedAt: &observedAt,
		HookHealth:               generated.AgentSessionHookHealthActive,
		HookHealthObservedAt:     observedAt,
		NativeProvider:           &provider,
		NativeSessionId:          &nativeID,
		CreatedAt:                observedAt.Add(-time.Minute),
		UpdatedAt:                observedAt,
		SessionCredential:        generated.SessionCredentialView{Generation: 4},
	}

	listProjection := projectSessions(generated.MutableConfigView{}, []generated.AgentSession{source}).Sessions
	if len(listProjection) != 1 {
		t.Fatalf("list projection count = %d, want 1", len(listProjection))
	}
	assertAuthoritativeSessionProjection(t, listProjection[0], observedAt, nativeID)
	assertAuthoritativeSessionProjection(t, projectActionSession(source), observedAt, nativeID)
}

func assertAuthoritativeSessionProjection(t *testing.T, projected backend.Session, observedAt time.Time, nativeID string) {
	t.Helper()
	if projected.NativeActivity != backend.NativeActivityWaiting ||
		projected.ActivitySource != backend.ActivitySourceHook ||
		!projected.ActivityObservedAt.Equal(observedAt) ||
		projected.HookHealth != backend.HookHealthActive ||
		!projected.HookHealthObservedAt.Equal(observedAt) ||
		projected.NativeProvider != backend.NativeProviderCodex ||
		projected.NativeSessionID != nativeID {
		t.Fatalf("authoritative projection mismatch: %#v", projected)
	}
}
