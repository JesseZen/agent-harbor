package page

import (
	"fmt"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/upstream/k9s/resourceview"
)

func rowFromSession(session generated.AgentSession, now time.Time) resourceview.Row {
	return resourceview.Row{
		ID: string(session.Id),
		Cells: []string{
			session.Label,
			string(session.Lifecycle),
			activityCell(session),
			string(session.ActivitySource),
			providerCell(session),
			string(session.Workspace),
			formatAge(now.Sub(session.CreatedAt)),
		},
	}
}

// activityCell renders Core native_activity unless detecting applies.
// Detecting is only launching without a native session id — never lifecycle→working.
func activityCell(session generated.AgentSession) string {
	if session.Lifecycle == generated.AgentSessionLifecycleLaunching &&
		(session.NativeSessionId == nil || *session.NativeSessionId == "") {
		return "detecting"
	}
	return string(session.NativeActivity)
}

func providerCell(session generated.AgentSession) string {
	if session.NativeProvider == nil {
		return ""
	}
	return string(*session.NativeProvider)
}

func nativeID(session generated.AgentSession) string {
	if session.NativeSessionId == nil {
		return ""
	}
	return *session.NativeSessionId
}

func formatAge(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
