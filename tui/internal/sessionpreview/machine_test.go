package sessionpreview_test

import (
	"testing"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/sessionpreview"
)

type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) advance(d time.Duration) { c.now = c.now.Add(d) }

func newMachine(t *testing.T, start time.Time) (*sessionpreview.Machine, *fakeClock) {
	t.Helper()
	clock := &fakeClock{now: start}
	return sessionpreview.New(clock), clock
}

func TestSelectClearsResultWhileLoading(t *testing.T) {
	m, clock := newMachine(t, time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC))
	k1 := sessionpreview.CacheKey{SessionID: "s1", UpdatedAt: clock.Now()}
	_ = m.Select(k1)
	m.Apply(sessionpreview.OutcomeSuccess, sessionpreview.Result{Lines: []string{"old"}}, nil)
	if len(m.Result().Lines) != 1 {
		t.Fatalf("setup result=%#v", m.Result())
	}

	k2 := sessionpreview.CacheKey{SessionID: "s2", UpdatedAt: clock.Now()}
	_ = m.Select(k2)
	if !m.Loading() {
		t.Fatal("expected loading after selection change")
	}
	if got := m.Result(); len(got.Lines) != 0 || got.Truncated || got.Empty {
		t.Fatalf("Select must clear prior result while loading; got %#v", got)
	}
}

func TestCacheKeyUsesDurableUpdatedAt(t *testing.T) {
	m, clock := newMachine(t, time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC))
	key := sessionpreview.CacheKey{SessionID: "s1", UpdatedAt: clock.Now()}
	cmd := m.Select(key)
	if cmd.Key != key {
		t.Fatalf("Select fetch key = %#v, want %#v", cmd.Key, key)
	}
	if !m.Loading() || m.ViewState() != sessionpreview.StateLoading {
		t.Fatalf("state=%q loading=%v", m.ViewState(), m.Loading())
	}

	// Pure activity change does not alter durable UpdatedAt → same cache key.
	sameDurable := sessionpreview.CacheKey{SessionID: "s1", UpdatedAt: key.UpdatedAt}
	m.Apply(sessionpreview.OutcomeSuccess, sessionpreview.Result{Lines: []string{"a"}}, nil)
	if m.ViewState() != sessionpreview.StateReady {
		t.Fatalf("state=%q", m.ViewState())
	}
	// Re-select same durable key within Ready TTL must not issue a new fetch.
	cmd2 := m.Select(sameDurable)
	if cmd2.ShouldFetch {
		t.Fatal("same durable key within Ready TTL must not refetch")
	}
	if m.ViewState() != sessionpreview.StateReady {
		t.Fatalf("state=%q after reselect", m.ViewState())
	}

	// Different durable UpdatedAt is a different cache key.
	newer := sessionpreview.CacheKey{SessionID: "s1", UpdatedAt: key.UpdatedAt.Add(time.Second)}
	cmd3 := m.Select(newer)
	if !cmd3.ShouldFetch || cmd3.Key != newer {
		t.Fatalf("durable change must fetch: %#v", cmd3)
	}
	if !m.Loading() {
		t.Fatal("expected loading after durable key change")
	}
}

func TestSuccessReadyTTL(t *testing.T) {
	m, clock := newMachine(t, time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC))
	key := sessionpreview.CacheKey{SessionID: "s1", UpdatedAt: clock.Now()}
	_ = m.Select(key)
	m.Apply(sessionpreview.OutcomeSuccess, sessionpreview.Result{Lines: []string{"line"}}, nil)
	if m.ViewState() != sessionpreview.StateReady || m.Loading() {
		t.Fatalf("state=%q loading=%v", m.ViewState(), m.Loading())
	}
	if m.Message() != "" {
		t.Fatalf("message=%q", m.Message())
	}

	clock.advance(1999 * time.Millisecond)
	delay, shouldFetch := m.Tick()
	if shouldFetch {
		t.Fatalf("should not refetch before Ready TTL; delay=%s", delay)
	}

	clock.advance(2 * time.Millisecond)
	delay, shouldFetch = m.Tick()
	if !shouldFetch {
		t.Fatalf("Ready TTL expiry must refetch; delay=%s", delay)
	}
	if !m.Loading() {
		t.Fatal("TTL refetch enters loading")
	}
}

func TestEmptyTTL(t *testing.T) {
	m, clock := newMachine(t, time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC))
	key := sessionpreview.CacheKey{SessionID: "s1", UpdatedAt: clock.Now()}
	_ = m.Select(key)
	m.Apply(sessionpreview.OutcomeEmpty, sessionpreview.Result{Empty: true}, nil)
	if m.ViewState() != sessionpreview.StateEmpty || m.Loading() {
		t.Fatalf("state=%q loading=%v", m.ViewState(), m.Loading())
	}

	clock.advance(999 * time.Millisecond)
	_, shouldFetch := m.Tick()
	if shouldFetch {
		t.Fatal("empty TTL not expired yet")
	}
	clock.advance(2 * time.Millisecond)
	_, shouldFetch = m.Tick()
	if !shouldFetch || !m.Loading() {
		t.Fatal("empty TTL expiry must refetch and load")
	}
}

func TestStaleRetriesOnceThenManualError(t *testing.T) {
	m, clock := newMachine(t, time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC))
	key := sessionpreview.CacheKey{SessionID: "s1", UpdatedAt: clock.Now()}
	_ = m.Select(key)

	refreshed := sessionpreview.CacheKey{SessionID: "s1", UpdatedAt: key.UpdatedAt.Add(time.Minute)}
	m.Apply(sessionpreview.OutcomeStale, sessionpreview.Result{}, &refreshed)
	if !m.Loading() {
		t.Fatal("first stale must stay loading for retry")
	}
	delay, shouldFetch := m.Tick()
	if !shouldFetch || delay != 0 {
		t.Fatalf("first stale must immediate-retry; delay=%s shouldFetch=%v", delay, shouldFetch)
	}
	if got := m.SelectedKey(); got != refreshed {
		t.Fatalf("selected key after stale reload = %#v", got)
	}

	// Second stale → exact manual error; exit loading.
	m.Apply(sessionpreview.OutcomeStale, sessionpreview.Result{}, &refreshed)
	if m.Loading() {
		t.Fatal("second stale must exit loading")
	}
	if m.ViewState() != sessionpreview.StateError {
		t.Fatalf("state=%q", m.ViewState())
	}
	if m.Message() != "Session changed again; press r" {
		t.Fatalf("message=%q", m.Message())
	}
}

func TestUnavailableBackoff2_4_8_10(t *testing.T) {
	m, clock := newMachine(t, time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC))
	key := sessionpreview.CacheKey{SessionID: "s1", UpdatedAt: clock.Now()}
	_ = m.Select(key)
	m.Apply(sessionpreview.OutcomeUnavailable, sessionpreview.Result{}, nil)
	if m.ViewState() != sessionpreview.StateUnavailable || m.Loading() {
		t.Fatalf("state=%q loading=%v", m.ViewState(), m.Loading())
	}

	wantDelays := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second, 10 * time.Second, 10 * time.Second}
	for i, want := range wantDelays {
		delay, shouldFetch := m.Tick()
		if shouldFetch {
			t.Fatalf("step %d: should not fetch before backoff; delay=%s", i, delay)
		}
		if delay != want {
			t.Fatalf("step %d: backoff delay=%s want %s", i, delay, want)
		}
		clock.advance(want)
		delay, shouldFetch = m.Tick()
		if !shouldFetch || !m.Loading() {
			t.Fatalf("step %d: expected fetch after backoff; delay=%s loading=%v", i, delay, m.Loading())
		}
		m.Apply(sessionpreview.OutcomeUnavailable, sessionpreview.Result{}, nil)
		if m.ViewState() != sessionpreview.StateUnavailable || m.Loading() {
			t.Fatalf("step %d: state=%q loading=%v", i, m.ViewState(), m.Loading())
		}
	}
}

func TestTimeoutExitsLoading(t *testing.T) {
	m, clock := newMachine(t, time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC))
	_ = m.Select(sessionpreview.CacheKey{SessionID: "s1", UpdatedAt: clock.Now()})
	m.Apply(sessionpreview.OutcomeTimeout, sessionpreview.Result{}, nil)
	if m.Loading() {
		t.Fatal("timeout must exit loading")
	}
	if m.ViewState() != sessionpreview.StateError {
		t.Fatalf("state=%q", m.ViewState())
	}
}

func TestCancelExitsLoading(t *testing.T) {
	m, clock := newMachine(t, time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC))
	_ = m.Select(sessionpreview.CacheKey{SessionID: "s1", UpdatedAt: clock.Now()})
	m.Apply(sessionpreview.OutcomeCanceled, sessionpreview.Result{}, nil)
	if m.Loading() {
		t.Fatal("cancel must exit loading")
	}
	if m.ViewState() != sessionpreview.StateError {
		t.Fatalf("state=%q", m.ViewState())
	}
}

func TestSelectionChangeResetsBackoffAndLoading(t *testing.T) {
	m, clock := newMachine(t, time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC))
	k1 := sessionpreview.CacheKey{SessionID: "s1", UpdatedAt: clock.Now()}
	_ = m.Select(k1)
	m.Apply(sessionpreview.OutcomeUnavailable, sessionpreview.Result{}, nil)
	delay, _ := m.Tick()
	if delay != 2*time.Second {
		t.Fatalf("initial backoff=%s", delay)
	}

	k2 := sessionpreview.CacheKey{SessionID: "s2", UpdatedAt: clock.Now()}
	cmd := m.Select(k2)
	if !cmd.ShouldFetch || cmd.Key != k2 {
		t.Fatalf("selection change must fetch: %#v", cmd)
	}
	if !m.Loading() || m.ViewState() != sessionpreview.StateLoading {
		t.Fatalf("state=%q loading=%v", m.ViewState(), m.Loading())
	}
	// Backoff reset: after unavailable again, first delay is 2s.
	m.Apply(sessionpreview.OutcomeUnavailable, sessionpreview.Result{}, nil)
	delay, _ = m.Tick()
	if delay != 2*time.Second {
		t.Fatalf("backoff after selection change=%s want 2s", delay)
	}
}

func TestManualRRetriesAndResetsBackoff(t *testing.T) {
	m, clock := newMachine(t, time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC))
	key := sessionpreview.CacheKey{SessionID: "s1", UpdatedAt: clock.Now()}
	_ = m.Select(key)
	m.Apply(sessionpreview.OutcomeUnavailable, sessionpreview.Result{}, nil)
	clock.advance(2 * time.Second)
	_, _ = m.Tick()
	m.Apply(sessionpreview.OutcomeUnavailable, sessionpreview.Result{}, nil)
	delay, _ := m.Tick()
	if delay != 4*time.Second {
		t.Fatalf("pre-manual backoff=%s", delay)
	}

	cmd := m.ManualRetry()
	if !cmd.ShouldFetch || cmd.Key != key {
		t.Fatalf("manual r must fetch: %#v", cmd)
	}
	if !m.Loading() {
		t.Fatal("manual r enters loading")
	}
	m.Apply(sessionpreview.OutcomeUnavailable, sessionpreview.Result{}, nil)
	delay, _ = m.Tick()
	if delay != 2*time.Second {
		t.Fatalf("manual r must reset backoff to 2s; got %s", delay)
	}

	// Other errors only retry on r.
	m.Apply(sessionpreview.OutcomeError, sessionpreview.Result{}, nil)
	if m.Loading() || m.ViewState() != sessionpreview.StateError {
		t.Fatalf("state=%q loading=%v", m.ViewState(), m.Loading())
	}
	_, shouldFetch := m.Tick()
	if shouldFetch {
		t.Fatal("generic error must not auto-retry")
	}
	cmd = m.ManualRetry()
	if !cmd.ShouldFetch || !m.Loading() {
		t.Fatal("manual r retries generic error")
	}
}

func TestInvalidateTriggersLoadingFetch(t *testing.T) {
	m, clock := newMachine(t, time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC))
	key := sessionpreview.CacheKey{SessionID: "s1", UpdatedAt: clock.Now()}
	_ = m.Select(key)
	m.Apply(sessionpreview.OutcomeSuccess, sessionpreview.Result{Lines: []string{"ok"}}, nil)

	m.Invalidate("other")
	if m.Loading() {
		t.Fatal("invalidate other session must not affect selection")
	}
	m.Invalidate("s1")
	if !m.Loading() || m.ViewState() != sessionpreview.StateLoading {
		t.Fatalf("SSE invalidate selected must load; state=%q", m.ViewState())
	}
	_, shouldFetch := m.Tick()
	if !shouldFetch {
		t.Fatal("invalidate must queue a fetch")
	}
}

func TestSuccessResetsUnavailableBackoff(t *testing.T) {
	m, clock := newMachine(t, time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC))
	key := sessionpreview.CacheKey{SessionID: "s1", UpdatedAt: clock.Now()}
	_ = m.Select(key)
	m.Apply(sessionpreview.OutcomeUnavailable, sessionpreview.Result{}, nil)
	clock.advance(2 * time.Second)
	_, _ = m.Tick()
	m.Apply(sessionpreview.OutcomeSuccess, sessionpreview.Result{Lines: []string{"ok"}}, nil)
	if m.ViewState() != sessionpreview.StateReady {
		t.Fatalf("state=%q", m.ViewState())
	}
	// Force unavailable again — backoff starts at 2s.
	_ = m.Select(key) // may hit TTL cache; advance past ready TTL
	clock.advance(3 * time.Second)
	_, should := m.Tick()
	if !should {
		_ = m.ManualRetry()
	}
	m.Apply(sessionpreview.OutcomeUnavailable, sessionpreview.Result{}, nil)
	delay, _ := m.Tick()
	if delay != 2*time.Second {
		t.Fatalf("success must reset backoff; delay=%s", delay)
	}
}
