package sessionpreview

import "time"

type State string

const (
	StateLoading     State = "loading"
	StateReady       State = "ready"
	StateEmpty       State = "empty"
	StateUnavailable State = "unavailable"
	StateError       State = "error"
)

const (
	ReadyTTL                = 2 * time.Second
	EmptyTTL                = 1 * time.Second
	StaleManualErrorMessage = "Session changed again; press r"
	maxUnavailableBackoff   = 10 * time.Second
)

var unavailableBackoffSteps = []time.Duration{
	2 * time.Second,
	4 * time.Second,
	8 * time.Second,
	10 * time.Second,
}

type CacheKey struct {
	SessionID string
	UpdatedAt time.Time
}

func (k CacheKey) Equal(other CacheKey) bool {
	return k.SessionID == other.SessionID && k.UpdatedAt.Equal(other.UpdatedAt)
}

type Clock interface {
	Now() time.Time
}

type Result struct {
	Lines     []string
	Empty     bool
	Truncated bool
}

type OutcomeKind int

const (
	OutcomeSuccess OutcomeKind = iota
	OutcomeEmpty
	OutcomeStale
	OutcomeUnavailable
	OutcomeTimeout
	OutcomeCanceled
	OutcomeError
)

type FetchCmd struct {
	Key         CacheKey
	ShouldFetch bool
}

type Machine struct {
	clock Clock

	state   State
	message string
	loading bool
	result  Result

	selected     CacheKey
	hasSelection bool
	expiresAt    time.Time

	staleRetries         int
	unavailableStep      int
	unavailableDue       time.Time
	unavailableScheduled bool

	pendingFetch bool
}

func New(clock Clock) *Machine {
	if clock == nil {
		clock = realClock{}
	}
	return &Machine{clock: clock, state: StateEmpty}
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (m *Machine) Select(key CacheKey) FetchCmd {
	same := m.hasSelection && m.selected.Equal(key)
	if same && !m.loading && (m.state == StateReady || m.state == StateEmpty) {
		if m.clock.Now().Before(m.expiresAt) {
			return FetchCmd{Key: key, ShouldFetch: false}
		}
	}
	m.resetBackoff()
	m.staleRetries = 0
	m.selected = key
	m.hasSelection = true
	m.enterLoading(true)
	return FetchCmd{Key: key, ShouldFetch: true}
}

func (m *Machine) Invalidate(sessionID string) {
	if !m.hasSelection || m.selected.SessionID != sessionID {
		return
	}
	m.staleRetries = 0
	m.enterLoading(true)
}

func (m *Machine) ManualRetry() FetchCmd {
	if !m.hasSelection {
		return FetchCmd{}
	}
	m.resetBackoff()
	m.staleRetries = 0
	m.enterLoading(true)
	return FetchCmd{Key: m.selected, ShouldFetch: true}
}

func (m *Machine) Apply(outcome OutcomeKind, result Result, refreshedKey *CacheKey) {
	switch outcome {
	case OutcomeSuccess:
		m.result = result
		m.message = ""
		m.loading = false
		m.pendingFetch = false
		m.resetBackoff()
		m.staleRetries = 0
		m.state = StateReady
		m.expiresAt = m.clock.Now().Add(ReadyTTL)
	case OutcomeEmpty:
		m.result = result
		m.message = ""
		m.loading = false
		m.pendingFetch = false
		m.resetBackoff()
		m.staleRetries = 0
		m.state = StateEmpty
		m.expiresAt = m.clock.Now().Add(EmptyTTL)
	case OutcomeStale:
		m.result = Result{}
		if refreshedKey != nil {
			m.selected = *refreshedKey
			m.hasSelection = true
		}
		m.staleRetries++
		if m.staleRetries >= 2 {
			m.loading = false
			m.pendingFetch = false
			m.state = StateError
			m.message = StaleManualErrorMessage
			return
		}
		m.enterLoading(true)
	case OutcomeUnavailable:
		m.result = Result{}
		m.message = ""
		m.loading = false
		m.pendingFetch = false
		m.state = StateUnavailable
		m.scheduleUnavailable()
	case OutcomeTimeout, OutcomeCanceled, OutcomeError:
		m.result = Result{}
		m.loading = false
		m.pendingFetch = false
		m.unavailableScheduled = false
		m.state = StateError
		switch outcome {
		case OutcomeTimeout:
			m.message = "Preview timed out; press r"
		case OutcomeCanceled:
			m.message = "Preview canceled; press r"
		default:
			m.message = "Preview failed; press r"
		}
	}
}

func (m *Machine) Tick() (delay time.Duration, shouldFetch bool) {
	if m.pendingFetch {
		m.pendingFetch = false
		m.loading = true
		m.state = StateLoading
		return 0, true
	}
	if !m.hasSelection || m.loading {
		return 0, false
	}
	now := m.clock.Now()
	switch m.state {
	case StateReady, StateEmpty:
		if !m.expiresAt.IsZero() && !now.Before(m.expiresAt) {
			m.enterLoading(false)
			m.loading = true
			m.state = StateLoading
			return 0, true
		}
	case StateUnavailable:
		if !m.unavailableScheduled {
			return 0, false
		}
		if now.Before(m.unavailableDue) {
			return m.unavailableDue.Sub(now), false
		}
		m.unavailableScheduled = false
		m.enterLoading(false)
		m.loading = true
		m.state = StateLoading
		return 0, true
	}
	return 0, false
}

func (m *Machine) ViewState() State { return m.state }

func (m *Machine) Message() string { return m.message }

func (m *Machine) Loading() bool { return m.loading }

func (m *Machine) SelectedKey() CacheKey { return m.selected }

func (m *Machine) Result() Result { return m.result }

func (m *Machine) enterLoading(queueFetch bool) {
	m.loading = true
	m.state = StateLoading
	m.message = ""
	m.result = Result{}
	m.unavailableScheduled = false
	m.pendingFetch = queueFetch
}

func (m *Machine) resetBackoff() {
	m.unavailableStep = 0
	m.unavailableScheduled = false
	m.unavailableDue = time.Time{}
}

func (m *Machine) scheduleUnavailable() {
	step := m.unavailableStep
	if step >= len(unavailableBackoffSteps) {
		step = len(unavailableBackoffSteps) - 1
	}
	delay := unavailableBackoffSteps[step]
	if delay > maxUnavailableBackoff {
		delay = maxUnavailableBackoff
	}
	m.unavailableDue = m.clock.Now().Add(delay)
	m.unavailableScheduled = true
	if m.unavailableStep < len(unavailableBackoffSteps)-1 {
		m.unavailableStep++
	} else {
		m.unavailableStep = len(unavailableBackoffSteps) - 1
	}
}
