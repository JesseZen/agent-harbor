package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func notMagpieHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"name":"other","version":"1"}`))
	}
}

// cliGateway stands in for a running Magpie whose process identity the CLI can
// read: a helper process started with the environment the instance would have.
type cliGateway struct {
	child   *factsChild
	address string
	root    string
}

func newCLIGateway(t *testing.T, b testBridge, variables map[string]string) cliGateway {
	t.Helper()
	root := tempRoot(t)
	all := map[string]string{"HOME": filepath.Join(root, "home"), "XDG_CONFIG_HOME": filepath.Join(root, "xdg"), "XDG_CACHE_HOME": filepath.Join(root, "cache")}
	for key, value := range variables {
		all[key] = value
	}
	child := startFactsHelperProcess(t, root, all)
	server := httptest.NewServer(magpieSelfReport(child.pid(), "serve"))
	t.Cleanup(server.Close)
	_ = b
	return cliGateway{child: child, address: server.URL, root: root}
}

func (g cliGateway) configDir() string {
	return filepath.Join(g.root, "xdg", "magpie")
}

func TestMagpieStatusReportsOneAddressWithoutWritingState(t *testing.T) {
	b := newTestBridge(t)
	gateway := newCLIGateway(t, b, nil)
	out, err := b.command("magpie", "status", "--address", gateway.address)
	if err != nil {
		t.Fatalf("status: %s: %v", out, err)
	}
	for _, want := range []string{"verified", gateway.configDir(), "0.1.3528", "serve"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %s", want, out)
		}
	}
	if _, err := os.Stat(filepath.Join(b.root, "bridge", "magpie.json")); !os.IsNotExist(err) {
		t.Fatalf("status wrote instance state: %v", err)
	}
}

func TestMagpieStatusReportsOfflineAndNotMagpieWithoutFailing(t *testing.T) {
	b := newTestBridge(t)
	address, err := freeGatewayAddress()
	if err != nil {
		t.Fatal(err)
	}
	out, err := b.command("magpie", "status", "--address", address)
	if err != nil || !strings.Contains(out, "offline") {
		t.Fatalf("offline: %s: %v", out, err)
	}
	server := httptest.NewServer(notMagpieHandler())
	defer server.Close()
	out, err = b.command("magpie", "status", "--address", server.URL)
	if err != nil || !strings.Contains(out, "not-magpie") {
		t.Fatalf("not magpie: %s: %v", out, err)
	}
}

func TestMagpieStatusJSONKeepsIdentityFieldsAndNoEnvironment(t *testing.T) {
	b := newTestBridge(t)
	gateway := newCLIGateway(t, b, map[string]string{"PRIVATE_TOKEN": "secret-value"})
	out, err := b.command("magpie", "status", "--address", gateway.address, "--json")
	if err != nil {
		t.Fatalf("status: %s: %v", out, err)
	}
	var probe gatewayProbe
	if err := json.Unmarshal([]byte(out), &probe); err != nil {
		t.Fatalf("json: %s", out)
	}
	if probe.State != gatewayVerified || probe.ConfigDir != gateway.configDir() || probe.PID == 0 {
		t.Fatalf("probe: %+v", probe)
	}
	if strings.Contains(out, "secret-value") || strings.Contains(out, "PRIVATE_TOKEN") {
		t.Fatalf("environment leaked: %s", out)
	}
}

func TestMagpieBindRecordsAnInstanceAndSelectsIt(t *testing.T) {
	b := newTestBridge(t)
	gateway := newCLIGateway(t, b, nil)
	out, err := b.command("magpie", "bind", "--address", gateway.address)
	if err != nil {
		t.Fatalf("bind: %s: %v", out, err)
	}
	if !strings.Contains(out, gateway.configDir()) || !strings.Contains(out, "never starts, stops or edits") {
		t.Fatalf("bind output: %s", out)
	}
	out, err = b.command("magpie", "status", "--json")
	if err != nil {
		t.Fatalf("status: %s: %v", out, err)
	}
	var report struct {
		Default   string           `json:"default"`
		Instances []instanceReport `json:"instances"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("json: %s", out)
	}
	if len(report.Instances) != 1 || report.Default != report.Instances[0].ID {
		t.Fatalf("registry: %+v", report)
	}
	if report.Instances[0].Health != instanceVerified || report.Instances[0].ConfigDir != gateway.configDir() {
		t.Fatalf("instance: %+v", report.Instances[0])
	}
}

func TestMagpieBindRefusesAnAddressItCannotIdentify(t *testing.T) {
	b := newTestBridge(t)
	address, err := freeGatewayAddress()
	if err != nil {
		t.Fatal(err)
	}
	out, err := b.command("magpie", "bind", "--address", address)
	if err == nil || !strings.Contains(out, "does not infer an instance") {
		t.Fatalf("offline bind: %s: %v", out, err)
	}
	server := httptest.NewServer(notMagpieHandler())
	defer server.Close()
	out, err = b.command("magpie", "bind", "--address", server.URL)
	if err == nil || !strings.Contains(out, "did not identify itself") {
		t.Fatalf("not magpie bind: %s: %v", out, err)
	}
	if _, err := os.Stat(filepath.Join(b.root, "bridge", "magpie.json")); !os.IsNotExist(err) {
		t.Fatalf("refused bind still recorded state: %v", err)
	}
	out, err = b.command("magpie", "bind", "--address", "http://192.0.2.1:3425")
	if err == nil || !strings.Contains(out, "loopback") {
		t.Fatalf("non-loopback bind: %s: %v", out, err)
	}
}

func TestMagpieStartRefusesWhenThePortIsHeld(t *testing.T) {
	b := newTestBridge(t)
	gateway := newCLIGateway(t, b, nil)
	bundleDir := filepath.Join(b.root, "package")
	if err := os.MkdirAll(bundleDir, 0700); err != nil {
		t.Fatal(err)
	}
	// A stand-in packaged Magpie: it must never be started here.
	if err := os.WriteFile(filepath.Join(bundleDir, "magpie"), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	b.env = append(b.env, "AGENT_HARBOR_MAGPIE_BIN="+filepath.Join(bundleDir, "magpie"))
	port, err := portOfAddress(gateway.address)
	if err != nil {
		t.Fatal(err)
	}
	out, err := b.command("magpie", "start", "--port", strconv.Itoa(port), "--web-port", strconv.Itoa(port+1))
	if err == nil || !strings.Contains(out, "held by") {
		t.Fatalf("port conflict: %s: %v", out, err)
	}
	if !strings.Contains(out, "Magpie") {
		t.Fatalf("holder not named: %s", out)
	}
	if _, err := os.Stat(filepath.Join(b.root, "bridge", "magpie", "instance.json")); !os.IsNotExist(err) {
		t.Fatalf("a record was written despite the conflict: %v", err)
	}
}

func TestMagpieStopReportsNothingRecordedAndNeverKillsOthers(t *testing.T) {
	b := newTestBridge(t)
	gateway := newCLIGateway(t, b, nil)
	out, err := b.command("magpie", "stop")
	if err == nil || !strings.Contains(out, "nothing was stopped") {
		t.Fatalf("stop: %s: %v", out, err)
	}
	// A record whose process is someone else's must be refused, not killed.
	if err := os.MkdirAll(filepath.Join(b.root, "bridge", "magpie"), 0700); err != nil {
		t.Fatal(err)
	}
	record := managedRecord{Address: gateway.address, ConfigDir: gateway.configDir(), Executable: filepath.Join(b.root, "bridge", "magpie", "bin", "magpie"), PID: gateway.child.pid(), StartedAt: time.Now().UTC()}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b.root, "bridge", "magpie", "instance.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	out, err = b.command("magpie", "stop")
	if err == nil || !strings.Contains(out, "not the managed Magpie") {
		t.Fatalf("foreign stop: %s: %v", out, err)
	}
	if !gateway.child.alive() {
		t.Fatal("stopped a process Harbor did not start")
	}
}

func TestMagpieManageRefusesWithoutAGateway(t *testing.T) {
	b := newTestBridge(t)
	address, err := freeGatewayAddress()
	if err != nil {
		t.Fatal(err)
	}
	out, err := b.command("magpie", "manage", "--address", address)
	if err == nil || !strings.Contains(out, "host the gateway") {
		t.Fatalf("manage: %s: %v", out, err)
	}
}

func TestMagpieWebRequiresTheManagedInstance(t *testing.T) {
	b := newTestBridge(t)
	out, err := b.command("magpie", "web")
	if err == nil || !strings.Contains(out, "not running") {
		t.Fatalf("web: %s: %v", out, err)
	}
}

func TestMagpieUseSelectsARecordedInstance(t *testing.T) {
	b := newTestBridge(t)
	for _, gateway := range []cliGateway{newCLIGateway(t, b, nil), newCLIGateway(t, b, nil)} {
		if out, err := b.command("magpie", "bind", "--address", gateway.address); err != nil {
			t.Fatalf("bind: %s: %v", out, err)
		}
	}
	out, err := b.command("magpie", "status", "--json")
	if err != nil {
		t.Fatalf("status: %s: %v", out, err)
	}
	var report struct {
		Default   string           `json:"default"`
		Instances []instanceReport `json:"instances"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Instances) != 2 {
		t.Fatalf("instances: %+v", report)
	}
	var other string
	for _, instance := range report.Instances {
		if instance.ID != report.Default {
			other = instance.ID
		}
	}
	out, err = b.command("magpie", "use", other)
	if err != nil || !strings.Contains(out, other) {
		t.Fatalf("use: %s: %v", out, err)
	}
	out, err = b.command("magpie", "use", "external-does-not-exist")
	if err == nil || !strings.Contains(out, "no Magpie instance") {
		t.Fatalf("use unknown: %s: %v", out, err)
	}
}

func TestMagpieUseWithoutAnArgumentListsInstances(t *testing.T) {
	b := newTestBridge(t)
	out, err := b.command("magpie", "use")
	if err == nil || !strings.Contains(out, "no Magpie instance is recorded") {
		t.Fatalf("use: %s: %v", out, err)
	}
	gateway := newCLIGateway(t, b, nil)
	if out, err := b.command("magpie", "bind", "--address", gateway.address); err != nil {
		t.Fatalf("bind: %s: %v", out, err)
	}
	out, err = b.command("magpie", "use")
	if err != nil || !strings.Contains(out, "* ") || !strings.Contains(out, gateway.address) {
		t.Fatalf("list: %s: %v", out, err)
	}
}

func TestMagpieCommandRejectsUnknownSubcommands(t *testing.T) {
	b := newTestBridge(t)
	out, err := b.command("magpie", "frobnicate")
	if err == nil || !strings.Contains(out, "usage: agent-harbor magpie") {
		t.Fatalf("unknown subcommand: %s: %v", out, err)
	}
}

func TestNewMagpieSessionsFollowTheSelectedInstance(t *testing.T) {
	b := newTestBridge(t)
	selected := newCLIGateway(t, b, nil)
	if out, err := b.command("magpie", "bind", "--address", selected.address); err != nil {
		t.Fatalf("bind: %s: %v", out, err)
	}
	t.Setenv("AGENT_HARBOR_BRIDGE_HOME", filepath.Join(b.root, "bridge"))
	// The picker must propose the recorded instance rather than the ambient
	// environment, which points nowhere here.
	address, err := magpieAddress()
	if err != nil || address != selected.address {
		t.Fatalf("picker address: %q %v", address, err)
	}
}

// A corrupt registry must stop the session picker, not silently send the
// session to the environment's Magpie instead of the selected instance.
func TestNewMagpieSessionsReportACorruptRegistry(t *testing.T) {
	root := tempRoot(t)
	home := filepath.Join(root, "state")
	if err := os.MkdirAll(home, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".agent-harbor-bridge"), []byte("agent-harbor-bridge-state-v1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "magpie.json"), []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_HARBOR_BRIDGE_HOME", home)
	t.Setenv("MAGPIE_ADDR", "127.0.0.1:3425")
	if _, err := magpieAddress(); err == nil || !strings.Contains(err.Error(), "registry") {
		t.Fatalf("a corrupt registry was ignored: %v", err)
	}
}
