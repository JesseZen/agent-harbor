package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func magpieSelfReport(pid int, serves string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"name": "magpie", "version": "0.1.3528", "pid": pid, "serves": serves, "window": false})
	}
}

func probeTestServer(t *testing.T, handler http.Handler) gatewayProbe {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return probeGateway(server.URL)
}

func freeGatewayAddress() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer listener.Close()
	return "http://" + listener.Addr().String(), nil
}

func TestGatewayAddressMustBeALoopbackIPWithAPort(t *testing.T) {
	for _, address := range []string{
		"http://localhost:3425", "https://127.0.0.1:3425", "http://0.0.0.0:3425", "http://192.168.1.5:3425",
		"http://127.0.0.1", "http://127.0.0.1:0", "http://127.0.0.1:70000", "http://user@127.0.0.1:3425",
		"http://127.0.0.1:3425/gateway", "ftp://127.0.0.1:3425", "",
	} {
		if _, err := parseGatewayAddress(address); err == nil {
			t.Errorf("accepted %q", address)
		}
	}
	for address, want := range map[string]string{
		"http://127.0.0.1:3425": "http://127.0.0.1:3425", "127.0.0.1:3425": "http://127.0.0.1:3425",
		"http://[::1]:47100": "http://[::1]:47100", "HTTP://127.0.0.1:3425/": "http://127.0.0.1:3425",
		// One listener must not gain a second instance ID from how its port
		// was typed.
		"http://127.0.0.1:03425": "http://127.0.0.1:3425", "127.0.0.1:00003425": "http://127.0.0.1:3425",
	} {
		got, err := parseGatewayAddress(address)
		if err != nil || got != want {
			t.Errorf("%q: %q, %v", address, got, err)
		}
	}
}

// A pasted address can carry the managed instance's own web key; it must not
// be echoed back in output or an error.
func TestProbeDoesNotEchoCredentialsFromTheAddress(t *testing.T) {
	for input, forbidden := range map[string]string{
		"http://127.0.0.1:3436/?k=SECRETKEY123": "SECRETKEY123",
		"http://user:pass@127.0.0.1:3436":       "pass",
		"127.0.0.1:3436?k=SECRETKEY123":         "SECRETKEY123",
	} {
		probe := probeGateway(input)
		if strings.Contains(probe.Address, forbidden) {
			t.Errorf("%q leaked into %q", forbidden, probe.Address)
		}
		if probe.State != gatewayUnknown {
			t.Errorf("%q: %s", input, probe.State)
		}
	}
}

func TestProbeRejectsANonLoopbackAddressWithoutConnecting(t *testing.T) {
	probe := probeGateway("http://192.0.2.1:3425")
	if probe.State != gatewayUnknown || !strings.Contains(probe.Reason, "loopback") {
		t.Fatalf("probe: %+v", probe)
	}
}

func TestParseLoopbackAddressAcceptsHostAndPort(t *testing.T) {
	if got, err := parseLoopbackAddress("127.0.0.1:47111"); err != nil || got != "127.0.0.1:47111" {
		t.Fatalf("%q: %v", got, err)
	}
	for _, address := range []string{"cnb.space:47111", "127.0.0.1", "localhost:3425", "127.0.0.1:0", "::1:47111"} {
		if _, err := parseLoopbackAddress(address); err == nil {
			t.Errorf("accepted %q", address)
		}
	}
}

func TestProbeReturnsOfflineWhenNothingAnswers(t *testing.T) {
	address, err := freeGatewayAddress()
	if err != nil {
		t.Fatal(err)
	}
	probe := probeGateway(address)
	if probe.State != gatewayOffline {
		t.Fatalf("state: %+v", probe)
	}
}

func TestProbeReturnsNotMagpieForAnotherService(t *testing.T) {
	probe := probeTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"name":"other","version":"1"}`))
	}))
	if probe.State != gatewayNotMagpie || !strings.Contains(probe.Reason, "did not identify itself") {
		t.Fatalf("state: %+v", probe)
	}
}

func TestProbeReportsUnknownWithoutAServingProcess(t *testing.T) {
	for _, body := range []string{
		`{"name":"magpie","version":"0.1.3528","serves":"serve"}`,
		`{"name":"magpie","version":"0.1.3528","pid":123}`,
		`{"name":"magpie","pid":0,"serves":"serve"}`,
	} {
		probe := probeTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		if probe.State != gatewayUnknown || !strings.Contains(probe.Reason, "did not report") {
			t.Fatalf("%s: %+v", body, probe)
		}
	}
}

func TestProbeReportsVerifiedIdentity(t *testing.T) {
	root := tempRoot(t)
	child := startFactsHelper(t, root, map[string]string{
		"HOME": filepath.Join(root, "home"), "XDG_CONFIG_HOME": filepath.Join(root, "xdg"), "XDG_CACHE_HOME": filepath.Join(root, "cache"),
	})
	probe := probeTestServer(t, magpieSelfReport(child.pid(), "serve"))
	if probe.State != gatewayVerified {
		t.Fatalf("state: %+v", probe)
	}
	if probe.PID != child.pid() || probe.Version != "0.1.3528" || probe.Serves != "serve" {
		t.Fatalf("self-report: %+v", probe)
	}
	if probe.Executable != mustExecutable(t) {
		t.Fatalf("executable: %q", probe.Executable)
	}
	if probe.ConfigDir != filepath.Join(root, "xdg", "magpie") || probe.ConfigEvidence != configEvidenceInstalled {
		t.Fatalf("config: %+v", probe)
	}
}

func TestProbeReportsUnknownWhenTheProcessCannotBeRead(t *testing.T) {
	probe := probeTestServer(t, magpieSelfReport(1<<30, "serve"))
	if probe.State != gatewayUnknown || probe.Reason == "" {
		t.Fatalf("state: %+v", probe)
	}
}

func TestProbeKeepsOnlyNeededVariablesForManagement(t *testing.T) {
	root := tempRoot(t)
	child := startFactsHelper(t, root, map[string]string{
		"HOME": "/kept/home", "XDG_CONFIG_HOME": "/kept/xdg", "XDG_CACHE_HOME": "/kept/cache",
		"MAGPIE_ADDR": "127.0.0.1:47111", "PRIVATE_TOKEN": "secret",
	})
	probe := probeTestServer(t, magpieSelfReport(child.pid(), "serve"))
	if probe.State != gatewayVerified {
		t.Fatalf("state: %+v", probe)
	}
	want := []string{"HOME=/kept/home", "XDG_CONFIG_HOME=/kept/xdg", "XDG_CACHE_HOME=/kept/cache", "MAGPIE_ADDR=127.0.0.1:47111"}
	if got := probe.managementEnvironment(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("environment: %v", got)
	}
}

func TestProcessFactsCarryTheExecutableAndNamedVariablesOnly(t *testing.T) {
	root := tempRoot(t)
	child := startFactsHelper(t, root, keptHelperVariables)
	facts := waitForProcessFacts(t, child.pid(), keptHelperVariables)
	if facts.Executable != mustExecutable(t) {
		t.Fatalf("executable: %q", facts.Executable)
	}
	for key, want := range map[string]string{"HOME": "/kept/home", "XDG_CONFIG_HOME": "/kept/xdg", "XDG_CACHE_HOME": "/kept/cache", "MAGPIE_ADDR": "127.0.0.1:47111"} {
		if got := facts.Variables[key]; got != want {
			t.Fatalf("%s: %q", key, got)
		}
	}
	for key := range facts.Variables {
		if key != "HOME" && key != "XDG_CONFIG_HOME" && key != "XDG_CACHE_HOME" && key != "MAGPIE_ADDR" && key != "APPIMAGE" && key != "APPDIR" {
			t.Fatalf("unneeded variable kept: %s", key)
		}
	}
	if _, ok := facts.Variables["PRIVATE_TOKEN"]; ok {
		t.Fatalf("unneeded variable kept: %v", facts.Variables)
	}
}

func tempRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// startFactsHelper runs the test binary as a supervised process whose
// environment and executable stand in for a Magpie gateway.
func startFactsHelper(t *testing.T, root string, variables map[string]string) *factsChild {
	t.Helper()
	return startFactsHelperProcess(t, root, variables)
}

// waitForProcessFacts waits until readProcessFacts sees the given environment.
// Between fork and exec the kernel can still answer with an empty or the
// parent's environment, so a successful read alone does not mean the child
// has started with the variables the test set.
// keptHelperVariables are the variables the helper process sets and that
// Harbor must keep; PRIVATE_TOKEN is set too and must be dropped.
var keptHelperVariables = map[string]string{
	"HOME": "/kept/home", "XDG_CONFIG_HOME": "/kept/xdg", "XDG_CACHE_HOME": "/kept/cache", "MAGPIE_ADDR": "127.0.0.1:47111",
}

func waitForProcessFacts(t *testing.T, pid int, want map[string]string) processFacts {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var facts processFacts
	for {
		var err error
		facts, err = readProcessFacts(pid)
		if err == nil {
			ready := true
			for key, value := range want {
				if facts.Variables[key] != value {
					ready = false
					break
				}
			}
			if ready {
				return facts
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("process facts for %d did not show %v: %+v", pid, want, facts)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestFactsHelperProcess(t *testing.T) {
	if os.Getenv("AH_FACTS_HELPER") != "1" {
		return
	}
	for {
		if _, err := os.Stat(os.Getenv("AH_FACTS_STOP")); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestProcessFactsFailForAMissingProcess(t *testing.T) {
	if _, err := readProcessFacts(1 << 30); err == nil {
		t.Fatal("reported facts for a missing process")
	}
}

func TestConfigLocationInstalledUsesXDGThenHome(t *testing.T) {
	root := tempRoot(t)
	exe := filepath.Join(root, "bin", "magpie")
	mkExecutable(t, exe)
	got := resolveMagpieConfig(exe, map[string]string{"HOME": filepath.Join(root, "home"), "XDG_CONFIG_HOME": filepath.Join(root, "xdg")})
	if got.Location != filepath.Join(root, "xdg", "magpie") || got.Reason != "" {
		t.Fatalf("xdg: %+v", got)
	}
	got = resolveMagpieConfig(exe, map[string]string{"HOME": filepath.Join(root, "home"), "XDG_CONFIG_HOME": "relative/xdg"})
	if got.Location != filepath.Join(root, "home", ".config", "magpie") {
		t.Fatalf("relative XDG must read as unset: %+v", got)
	}
	got = resolveMagpieConfig(exe, map[string]string{"HOME": filepath.Join(root, "home")})
	if got.Location != filepath.Join(root, "home", ".config", "magpie") {
		t.Fatalf("home: %+v", got)
	}
}

func TestConfigLocationPortableByMarkerAndDataDirectory(t *testing.T) {
	root := tempRoot(t)
	home := map[string]string{"HOME": filepath.Join(root, "home"), "XDG_CONFIG_HOME": filepath.Join(root, "xdg")}

	marked := filepath.Join(root, "marked", "magpie")
	mkExecutable(t, marked)
	mkExecutable(t, filepath.Join(root, "marked", ".portable"))
	if got := resolveMagpieConfig(marked, home); got.Location != filepath.Join(root, "marked", "data") || got.Evidence != configEvidencePortableMarker {
		t.Fatalf("marker: %+v", got)
	}

	besideData := filepath.Join(root, "beside", "magpie")
	mkExecutable(t, besideData)
	mkDir(t, filepath.Join(root, "beside", "data"))
	if got := resolveMagpieConfig(besideData, home); got.Location != filepath.Join(root, "beside", "data") || got.Evidence != configEvidencePortableData {
		t.Fatalf("data directory: %+v", got)
	}

	// A directory named .portable is not Magpie's marker.
	directoryMarker := filepath.Join(root, "third", "magpie")
	mkExecutable(t, directoryMarker)
	mkDir(t, filepath.Join(root, "third", ".portable"))
	if got := resolveMagpieConfig(directoryMarker, home); got.Location != filepath.Join(root, "xdg", "magpie") {
		t.Fatalf("directory marker: %+v", got)
	}
}

func TestConfigLocationPassesOverAnotherAppsDataInSharedFolders(t *testing.T) {
	root := tempRoot(t)
	home := filepath.Join(root, "home")
	for _, dir := range []string{home, filepath.Join(home, "Downloads"), filepath.Join(home, "Desktop"), filepath.Join(home, "Documents")} {
		exe := filepath.Join(dir, "magpie")
		mkExecutable(t, exe)
		mkDir(t, filepath.Join(dir, "data"))
		vars := map[string]string{"HOME": home, "XDG_CONFIG_HOME": filepath.Join(root, "xdg")}
		if got := resolveMagpieConfig(exe, vars); got.Location != filepath.Join(root, "xdg", "magpie") || got.Reason != "" {
			t.Fatalf("%s: %+v", dir, got)
		}
	}
}

func TestConfigLocationUnknownWhenSharedFolderDataHoldsMagpieFiles(t *testing.T) {
	root := tempRoot(t)
	home := filepath.Join(root, "home")
	vars := map[string]string{"HOME": home, "XDG_CONFIG_HOME": filepath.Join(root, "xdg")}

	downloads := filepath.Join(home, "Downloads")
	mkExecutable(t, filepath.Join(downloads, "magpie"))
	mkDir(t, filepath.Join(downloads, "data"))
	if err := os.WriteFile(filepath.Join(downloads, "data", "settings.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	// Magpie keeps taking a data folder that already holds its files and
	// that no installed folder competes with, even in a shared folder.
	if got := resolveMagpieConfig(filepath.Join(downloads, "magpie"), vars); got.Location != filepath.Join(downloads, "data") || got.Reason != "" {
		t.Fatalf("portable data in a shared folder: %+v", got)
	}

	desktop := filepath.Join(home, "Desktop")
	mkExecutable(t, filepath.Join(desktop, "magpie"))
	mkDir(t, filepath.Join(desktop, "data"))
	if err := os.WriteFile(filepath.Join(desktop, "data", "providers.json"), []byte("[]"), 0600); err != nil {
		t.Fatal(err)
	}
	mkDir(t, filepath.Join(root, "xdg", "magpie"))
	if err := os.WriteFile(filepath.Join(root, "xdg", "magpie", "settings.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	// Magpie warns and passes the shared-folder data over when the installed
	// folder holds its files too; the installed folder is the answer it uses.
	if got := resolveMagpieConfig(filepath.Join(desktop, "magpie"), vars); got.Location != filepath.Join(root, "xdg", "magpie") || got.Warning == "" {
		t.Fatalf("installed folder with magpie files: %+v", got)
	}
}

func TestConfigLocationFollowsTheExecutableSymlink(t *testing.T) {
	root := tempRoot(t)
	real := filepath.Join(root, "real", "magpie")
	mkExecutable(t, real)
	mkDir(t, filepath.Join(root, "real", "data"))
	link := filepath.Join(root, "link-magpie")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if got := resolveMagpieConfig(link, map[string]string{"HOME": filepath.Join(root, "home")}); got.Location != filepath.Join(root, "real", "data") {
		t.Fatalf("symlink: %+v", got)
	}
}

func TestConfigLocationUnknownWithoutAUsableHome(t *testing.T) {
	root := tempRoot(t)
	exe := filepath.Join(root, "magpie")
	mkExecutable(t, exe)
	for _, vars := range []map[string]string{{}, {"HOME": "relative/home"}, {"HOME": "relative/home", "XDG_CONFIG_HOME": "relative/xdg"}} {
		if got := resolveMagpieConfig(exe, vars); got.Reason != configUnknown {
			t.Fatalf("%v: %+v", vars, got)
		}
	}
}

func mustExecutable(t *testing.T) string {
	t.Helper()
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

func mkExecutable(t *testing.T, path string) {
	t.Helper()
	mkDir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0700); err != nil {
		t.Fatal(err)
	}
}

func mkDir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
}

// factsChild is a supervised helper process whose environment and executable
// stand in for a Magpie gateway.
type factsChild struct {
	cmd  *exec.Cmd
	stop string
}

func startFactsHelperProcess(t *testing.T, root string, variables map[string]string) *factsChild {
	t.Helper()
	child := &factsChild{stop: filepath.Join(root, "stop")}
	child.cmd = exec.Command(os.Args[0], "-test.run=^TestFactsHelperProcess$")
	child.cmd.Env = []string{"AH_FACTS_HELPER=1", "AH_FACTS_STOP=" + child.stop}
	for key, value := range variables {
		child.cmd.Env = append(child.cmd.Env, key+"="+value)
	}
	if err := child.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(child.kill)
	wanted := map[string]string{}
	for _, key := range keptProcessVariables {
		if value, ok := variables[key]; ok {
			wanted[key] = value
		}
	}
	waitForProcessFacts(t, child.cmd.Process.Pid, wanted)
	return child
}

func (c *factsChild) pid() int { return c.cmd.Process.Pid }

func (c *factsChild) alive() bool { return c.cmd.Process.Signal(syscall.Signal(0)) == nil }

func (c *factsChild) kill() {
	_ = os.WriteFile(c.stop, nil, 0600)
	_ = c.cmd.Process.Kill()
	_, _ = c.cmd.Process.Wait()
}
