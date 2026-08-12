package targets

// TargetRuntimeStatus is the narrow HEALTH/ELIGIBLE projection for Targets rows.
// Values come from the host-injected runtime TargetStatus snapshot, never draft.
type TargetRuntimeStatus struct {
	Health   string
	Eligible bool
}

// TargetStatusProvider supplies runtime HEALTH/ELIGIBLE for Targets table rows.
// TICKET-011 must wire a snapshot-backed implementation; nil blanks those cells.
type TargetStatusProvider interface {
	Lookup(id string) (TargetRuntimeStatus, bool)
}

// StaticTargetStatusProvider is a map-backed provider for tests and simple hosts.
type StaticTargetStatusProvider map[string]TargetRuntimeStatus

func (p StaticTargetStatusProvider) Lookup(id string) (TargetRuntimeStatus, bool) {
	if p == nil {
		return TargetRuntimeStatus{}, false
	}
	v, ok := p[id]
	return v, ok
}
