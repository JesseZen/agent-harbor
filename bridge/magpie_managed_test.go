package main

import (
	"errors"
	"net"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestManagedLayoutKeepsEverythingInsideHarborState(t *testing.T) {
	layout := managedLayoutFor("/state/magpie")
	if layout.Executable != "/state/magpie/bin/magpie" || layout.Marker != "/state/magpie/bin/.portable" || layout.DataDir != "/state/magpie/bin/data" {
		t.Fatalf("layout: %+v", layout)
	}
	if layout.Home != "/state/magpie/home" || layout.WebKey != "/state/magpie/web.key" || layout.Record != "/state/magpie/instance.json" {
		t.Fatalf("layout: %+v", layout)
	}
}

func TestManagedPortPairPicksTheNextFreeNeighbors(t *testing.T) {
	gateway, web, err := pickPortPair(3435)
	if err != nil {
		t.Fatal(err)
	}
	if gateway != 3435 && web != gateway+1 {
		t.Fatalf("pair %d/%d is not consecutive", gateway, web)
	}
	blocked := freeConsecutivePair(t)
	listenOn(t, blocked)
	gateway, web, err = pickPortPair(blocked)
	if err != nil {
		t.Fatal(err)
	}
	if gateway == blocked || web == blocked+1 {
		t.Fatalf("took a held port: %d/%d", gateway, web)
	}
	if web != gateway+1 {
		t.Fatalf("pair %d/%d is not consecutive", gateway, web)
	}
}

func TestManagedRecordIsPrivateAndReplaceable(t *testing.T) {
	layout := managedLayoutFor(tempRoot(t))
	record := managedRecord{Address: "http://127.0.0.1:3435", ConfigDir: layout.DataDir, Executable: layout.Executable, Version: "0.1.3528", PID: 4242, StartedAt: time.Now().UTC().Truncate(time.Second)}
	if err := writeManagedRecord(layout, record); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(layout.Record)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("record permissions: %v %v", info.Mode().Perm(), err)
	}
	got, err := readManagedRecord(layout)
	if err != nil || got.PID != 4242 || got.Address != record.Address || !got.StartedAt.Equal(record.StartedAt) {
		t.Fatalf("record: %+v %v", got, err)
	}
}

func TestManagedWebKeyIsPrivateAndRandom(t *testing.T) {
	layout := managedLayoutFor(tempRoot(t))
	key, err := ensureWebKey(layout)
	if err != nil {
		t.Fatal(err)
	}
	if len(key) < 16 || strings.ContainsAny(key, " \t\n\"'") {
		t.Fatalf("key: %q", key)
	}
	info, err := os.Stat(layout.WebKey)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("key permissions: %v %v", info.Mode().Perm(), err)
	}
	again, err := ensureWebKey(layout)
	if err != nil || again != key {
		t.Fatalf("key was replaced: %q %v", again, err)
	}
	other := managedLayoutFor(tempRoot(t))
	different, err := ensureWebKey(other)
	if err != nil || different == key {
		t.Fatalf("two instances share a key: %q %v", different, err)
	}
}

func TestManagedEnvironmentIsolatesHomeCodexAndPath(t *testing.T) {
	layout := managedLayoutFor(tempRoot(t))
	env := managedProcessEnvironment(layout, "http://127.0.0.1:3435", "test-web-key", time.Now().UTC())
	values := environmentValues(env)
	if values["HOME"] != layout.Home || values["CODEX_HOME"] != layout.Home {
		t.Fatalf("home: %v", values)
	}
	if values["MAGPIE_ADDR"] != "127.0.0.1:3435" {
		t.Fatalf("address: %q", values["MAGPIE_ADDR"])
	}
	if values["MAGPIE_WEB_KEY"] != "test-web-key" || values["MAGPIE_NO_STATS"] != "1" {
		t.Fatalf("web: %v", values)
	}
	for _, key := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		if !strings.HasPrefix(values[key], layout.Home) {
			t.Fatalf("%s outside the managed home: %q", key, values[key])
		}
	}
	if values["PATH"] == "" {
		t.Fatal("PATH was dropped; Magpie cannot find agents")
	}
}

func TestManagedSettingsDisableStatsAndSelfUpdate(t *testing.T) {
	layout := managedLayoutFor(tempRoot(t))
	if err := os.MkdirAll(filepath.Dir(layout.Executable), 0700); err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(filepath.Dir(layout.Executable), "capture")
	stub := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$CAPTURE\"\nprintf '%s\\n' \"${MAGPIE_ADDR-unset}\" >> \"$CAPTURE\"\n"
	if err := os.WriteFile(layout.Executable, []byte(stub), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CAPTURE", capture)
	if err := disableManagedExtras(layout, "127.0.0.1:3435"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"settings set noStats on", "settings set noAutoUpdate on", "settings set noAutoDownload on"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("missing %q in %s", want, data)
		}
	}
	if !strings.Contains(string(data), "127.0.0.1:3435") {
		t.Fatalf("settings ran without the managed address: %s", data)
	}
	if _, err := os.Stat(layout.Marker); err != nil || !isRegularFile(layout.Marker) {
		t.Fatalf("portable marker: %v", err)
	}
}

func TestManagedUpgradeRefusesWhileRunning(t *testing.T) {
	root := tempRoot(t)
	layout := managedLayoutFor(root)
	bundled := filepath.Join(root, "bundled", "magpie")
	if err := os.MkdirAll(filepath.Dir(bundled), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundled, []byte("new build"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(layout.Executable), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout.Executable, []byte("old build"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := installBundledMagpie(layout, bundled, 4242); err == nil {
		t.Fatal("replaced a running executable")
	}
	if data, _ := os.ReadFile(layout.Executable); string(data) != "old build" {
		t.Fatalf("running executable changed: %q", data)
	}
	if changed, err := installBundledMagpie(layout, bundled, 0); err != nil || !changed {
		t.Fatalf("stopped upgrade: %v %v", changed, err)
	}
	if data, _ := os.ReadFile(layout.Executable); string(data) != "new build" {
		t.Fatalf("executable not replaced: %q", data)
	}
	if changed, err := installBundledMagpie(layout, bundled, 0); err != nil || changed {
		t.Fatalf("unchanged copy reported as replaced: %v %v", changed, err)
	}
}

func TestManagedStopDecisionRefusesAnythingItDidNotStart(t *testing.T) {
	layout := managedLayoutFor("/state/magpie")
	startedAt := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)
	record := managedRecord{PID: 4242, Executable: layout.Executable, Address: "http://127.0.0.1:3435", StartedAt: startedAt}
	ours := processFacts{PID: 4242, Executable: layout.Executable, Variables: map[string]string{processStartedAtVariable: startedAt.Format(time.RFC3339Nano)}}
	// A different process that reused the PID, or the same executable started
	// outside Harbor, carries no matching started time.
	reused := processFacts{PID: 4242, Executable: layout.Executable, Variables: map[string]string{}}
	facts := ours
	cases := []struct {
		name   string
		record *managedRecord
		probe  gatewayProbe
		facts  processFacts
		action string
	}{
		{"no record", nil, gatewayProbe{State: gatewayVerified, PID: 4242}, facts, managedStopNotRecorded},
		{"stopped record", &managedRecord{PID: 0, Executable: layout.Executable}, gatewayProbe{State: gatewayOffline}, processFacts{}, managedStopNotRunning},
		{"already stopped", &record, gatewayProbe{State: gatewayOffline}, facts, managedStopNotRunning},
		{"another process", &record, gatewayProbe{State: gatewayVerified, PID: 5555}, processFacts{PID: 5555, Executable: layout.Executable}, managedStopForeign},
		{"another executable", &record, gatewayProbe{State: gatewayVerified, PID: 4242}, processFacts{PID: 4242, Executable: "/opt/homebrew/bin/magpie"}, managedStopForeign},
		{"a reused pid", &record, gatewayProbe{State: gatewayVerified, PID: 4242}, reused, managedStopForeign},
		{"our process", &record, gatewayProbe{State: gatewayVerified, PID: 4242}, facts, managedStopOwn},
	}
	for _, c := range cases {
		if got := managedStopAction(c.record, c.probe, c.facts); got != c.action {
			t.Errorf("%s: %q", c.name, got)
		}
	}
}

func TestManagedPortInspectionNamesTheHolder(t *testing.T) {
	address, err := freeGatewayAddress()
	if err != nil {
		t.Fatal(err)
	}
	if holder := inspectPort(address); holder.State != portFree {
		t.Fatalf("free port: %+v", holder)
	}
	magpie := httptestMagpie(t, 4242)
	if holder := inspectPort(magpie); holder.State != portMagpie || holder.Probe.PID != 4242 {
		t.Fatalf("magpie port: %+v", holder)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if holder := inspectPort("http://" + listener.Addr().String()); holder.State != portOtherProgram {
		t.Fatalf("other program: %+v", holder)
	}
}

func TestManagedChildAddressMatchesTheRecordedProbe(t *testing.T) {
	// The readiness check requires the probe to name the child, not merely
	// that something answers.
	if !managedReady(4242, gatewayProbe{State: gatewayVerified, PID: 4242}) {
		t.Fatal("did not accept its own child")
	}
	if managedReady(4242, gatewayProbe{State: gatewayVerified, PID: 5555}) {
		t.Fatal("accepted another process")
	}
	if managedReady(4242, gatewayProbe{State: gatewayUnknown, PID: 4242, Reason: reasonNoServingProcess}) {
		t.Fatal("accepted a probe without identity")
	}
	if managedReady(4242, gatewayProbe{State: gatewayOffline}) {
		t.Fatal("accepted offline")
	}
}

func environmentValues(env []string) map[string]string {
	values := map[string]string{}
	for _, pair := range env {
		if key, value, ok := strings.Cut(pair, "="); ok {
			values[key] = value
		}
	}
	return values
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func freeConsecutivePair(t *testing.T) int {
	t.Helper()
	for port := 3481; port < 4000; port++ {
		first, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
		if err != nil {
			continue
		}
		second, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port+1))
		first.Close()
		if err != nil {
			continue
		}
		second.Close()
		return port
	}
	t.Fatal("no free consecutive port pair")
	return 0
}

func listenOn(t *testing.T, port int) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		t.Fatalf("port %d unavailable: %v", port, err)
	}
	t.Cleanup(func() { listener.Close() })
	return listener
}

func httptestMagpie(t *testing.T, pid int) string {
	t.Helper()
	server := httptest.NewServer(magpieSelfReport(pid, "serve"))
	t.Cleanup(server.Close)
	return server.URL
}

// A child that exits before it is ready must produce a readable error, not a
// deadlock from waiting on it a second time.
func TestStartReportsAChildThatExitsBeforeReady(t *testing.T) {
	root := tempRoot(t)
	t.Setenv("AGENT_HARBOR_BRIDGE_HOME", filepath.Join(root, "state"))
	t.Setenv("HOME", filepath.Join(root, "home"))
	bundled := filepath.Join(root, "bundled-magpie")
	if err := os.WriteFile(bundled, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_HARBOR_MAGPIE_BIN", bundled)
	done := make(chan error, 1)
	go func() { done <- startManagedMagpie(managedStartOptions{}) }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "during startup") {
			t.Fatalf("start reported %v", err)
		}
	case <-time.After(25 * time.Second):
		t.Fatal("start did not return; the exited child was waited on twice")
	}
}

// The authenticated page target must not appear in a process argument list.
func TestWebPageOpensThroughAPrivateRedirectFile(t *testing.T) {
	layout := managedLayoutFor(filepath.Join(tempRoot(t), "state #1 ?x"))
	if err := os.MkdirAll(layout.Root, 0700); err != nil {
		t.Fatal(err)
	}
	target, err := webRedirectFile(layout, "http://127.0.0.1:3436/?k=SECRETKEY")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(target, "SECRETKEY") {
		t.Fatalf("the launch argument carries the key: %q", target)
	}
	parsed, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "file" || parsed.Fragment != "" || parsed.RawQuery != "" {
		t.Fatalf("the target is not a clean file URL: %q", target)
	}
	if parsed.Path != filepath.Join(layout.Root, "web.html") {
		t.Fatalf("reserved characters changed the path: %q", parsed.Path)
	}
	path := parsed.Path
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("redirect file mode: %o", info.Mode().Perm())
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "k=SECRETKEY") {
		t.Fatalf("redirect file does not carry the target: %s", content)
	}
}

// A silent gateway whose process is still alive must keep its record, not be
// treated as stopped, and start must refuse rather than spawn a second child.
func TestSilentGatewayKeepsOwnership(t *testing.T) {
	root := tempRoot(t)
	startedAt := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)
	child := startFactsHelper(t, root, map[string]string{processStartedAtVariable: startedAt.Format(time.RFC3339Nano)})
	record := &managedRecord{PID: child.pid(), Executable: mustExecutable(t), Address: "127.0.0.1:1", StartedAt: startedAt}
	// Nothing answers at the recorded address, but the process is alive.
	action := managedStopAction(record, gatewayProbe{Address: record.Address, State: gatewayOffline}, processFacts{})
	if action != managedStopOwn {
		t.Fatalf("a live but silent gateway is %q, not Harbor's to stop", action)
	}
	// A pid that no longer exists is the only offline proof of exit.
	action = managedStopAction(&managedRecord{PID: 1 << 30, Executable: "/x/magpie", StartedAt: startedAt},
		gatewayProbe{State: gatewayOffline}, processFacts{})
	if action != managedStopNotRunning {
		t.Fatalf("a dead pid is %q", action)
	}
}

// A native child reports its actual PID and environment through the same probe
// as a gateway. Persistence failure must kill it after readiness is established.
func TestFailedRecordWriteStopsTheChild(t *testing.T) {
	root := tempRoot(t)
	t.Setenv("AGENT_HARBOR_BRIDGE_HOME", filepath.Join(root, "state"))
	t.Setenv("HOME", filepath.Join(root, "home"))
	pidFile := filepath.Join(root, "child.pid")
	pairStart := freeConsecutivePair(t)
	bundled := filepath.Join(root, "bundled-magpie")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundled, data, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_HARBOR_MAGPIE_BIN", bundled)
	t.Setenv("AH_MANAGED_READY_FIXTURE", pidFile)
	writeManagedRecordFile = func(string, []byte) error { return errors.New("disk is full") }
	defer func() { writeManagedRecordFile = writeStateFile }()

	err = startManagedMagpie(managedStartOptions{Port: pairStart, WebPort: pairStart + 1})
	if err == nil {
		t.Fatal("start reported success without writing its record")
	}
	if !strings.Contains(err.Error(), "record could not be written") {
		t.Fatalf("start failed for another reason: %v", err)
	}
	pid := readRecordedChildPID(t, pidFile)
	if pid <= 1 {
		t.Fatal("the child never started, so persistence was not the failure")
	}
	if probeGateway("http://127.0.0.1:"+strconv.Itoa(pairStart)).State == gatewayVerified {
		t.Fatal("a gateway was left answering without an ownership record")
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("child %d was left running without an ownership record", pid)
}

func readRecordedChildPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
				return pid
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return 0
}

// A pid that no longer holds the recorded process must not be signalled, and
// evidence that cannot be read must not clear the record.
func TestStopDoesNotSignalARecycledPid(t *testing.T) {
	root := tempRoot(t)
	startedAt := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)
	child := startFactsHelper(t, root, map[string]string{processStartedAtVariable: startedAt.Format(time.RFC3339Nano)})
	matches := managedRecord{PID: child.pid(), Executable: mustExecutable(t), StartedAt: startedAt}
	if got := recordedProcessEvidence(matches); got != processEvidenceMatches {
		t.Fatalf("the recorded child was %v", got)
	}
	recycled := matches
	recycled.StartedAt = startedAt.Add(time.Hour)
	if got := recordedProcessEvidence(recycled); got != processEvidenceGone {
		t.Fatalf("a recycled pid was %v", got)
	}
	otherExecutable := matches
	otherExecutable.Executable = filepath.Join(root, "magpie")
	if got := recordedProcessEvidence(otherExecutable); got != processEvidenceGone {
		t.Fatalf("a different executable was %v", got)
	}
	// A live process owned by another user cannot be read, which is
	// uncertainty about ownership, not proof that the child exited.
	unreadable := matches
	recordedProcessFacts = func(int) (processFacts, error) {
		return processFacts{}, errProcessDifferentUser
	}
	defer func() { recordedProcessFacts = readProcessFacts }()
	if got := recordedProcessEvidence(unreadable); got != processEvidenceUnreadable {
		t.Fatalf("unreadable evidence was %v", got)
	}
	dead := managedRecord{PID: 1 << 30, Executable: matches.Executable, StartedAt: startedAt}
	if got := recordedProcessEvidence(dead); got != processEvidenceGone {
		t.Fatalf("a missing process was %v", got)
	}
}

// Two concurrent starts must leave at most one managed child and one record.
func TestConcurrentStartsLeaveOneManagedChild(t *testing.T) {
	root := tempRoot(t)
	t.Setenv("AGENT_HARBOR_BRIDGE_HOME", filepath.Join(root, "state"))
	t.Setenv("HOME", filepath.Join(root, "home"))
	layout, err := managedInstanceState()
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := lockManagedLifecycle(layout)
	if err != nil {
		t.Fatal(err)
	}
	// While one lifecycle operation holds the lock, a second cannot start a
	// child or write a record, so a record cannot be overwritten.
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = lockManagedLifecycle(layout)
	}()
	select {
	case <-done:
		t.Fatal("a second lifecycle operation ran while the lock was held")
	case <-time.After(300 * time.Millisecond):
	}
	unlock()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the second lifecycle operation did not proceed after the lock was released")
	}
}
