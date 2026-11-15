package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestManageRefusesWhenNoGatewayAnswers(t *testing.T) {
	recorded := magpieInstance{ID: "external-1", Kind: "external", Address: "http://127.0.0.1:47111", ConfigDir: "/recorded/magpie", Version: "0.1.3528"}
	decision := planManagement(recorded, gatewayProbe{Address: recorded.Address, State: gatewayOffline}, "/opt/magpie")
	if decision.Allowed || decision.Reason == "" {
		t.Fatalf("decision: %+v", decision)
	}
	if !strings.Contains(decision.Reason, "host the gateway") {
		t.Fatalf("reason: %q", decision.Reason)
	}
	if decision.Unbound != nil {
		t.Fatalf("offered an unbound TUI without a gateway: %+v", decision.Unbound)
	}
}

func TestManageWarnsWhenTheExternalTUIOwnsTheGateway(t *testing.T) {
	probe := verifiedProbe("http://127.0.0.1:3425", "/recorded/magpie")
	probe.Serves = "tui"
	probe.variables = map[string]string{"HOME": "/home/user"}
	recorded := magpieInstance{ID: "external-1", Kind: "external", Address: probe.Address, ConfigDir: probe.ConfigDir, Version: probe.Version}
	decision := planManagement(recorded, probe, "/opt/magpie")
	if !decision.Allowed {
		t.Fatalf("decision: %+v", decision)
	}
	if !strings.Contains(decision.Warning, "owns the gateway") || !strings.Contains(decision.Warning, "Closing it stops") {
		t.Fatalf("warning: %q", decision.Warning)
	}
}

func TestManageOpensTheTUIWithTheVerifiedInstanceEnvironment(t *testing.T) {
	probe := verifiedProbe("http://127.0.0.1:3425", "/recorded/magpie")
	probe.Serves = "serve"
	probe.variables = map[string]string{"HOME": "/home/user", "XDG_CONFIG_HOME": "/home/user/.config", "XDG_CACHE_HOME": "/home/user/.cache"}
	recorded := magpieInstance{ID: "external-1", Kind: "external", Address: probe.Address, ConfigDir: probe.ConfigDir, Version: probe.Version}
	decision := planManagement(recorded, probe, "/opt/magpie")
	if !decision.Allowed || decision.Executable != probe.Executable || !strings.Contains(decision.Warning, "client configuration") {
		t.Fatalf("decision: %+v", decision)
	}
	if len(decision.Arguments) != 1 || decision.Arguments[0] != "tui" {
		t.Fatalf("arguments: %v", decision.Arguments)
	}
	values := environmentValues(decision.Environment)
	if len(values) != 4 {
		t.Fatalf("extra variables: %v", values)
	}
	for key, want := range map[string]string{
		"HOME": "/home/user", "XDG_CONFIG_HOME": "/home/user/.config", "XDG_CACHE_HOME": "/home/user/.cache",
		"MAGPIE_ADDR": "127.0.0.1:3425",
	} {
		if values[key] != want {
			t.Fatalf("%s: %q", key, values[key])
		}
	}
}

func TestManageRefusesAChangedConfiguration(t *testing.T) {
	recorded := magpieInstance{ID: "external-1", Kind: "external", Address: "http://127.0.0.1:3425", ConfigDir: "/recorded/magpie", Version: "0.1.3528"}
	probe := verifiedProbe("http://127.0.0.1:3425", "/other/magpie")
	decision := planManagement(recorded, probe, "/opt/magpie")
	if decision.Allowed {
		t.Fatalf("decision: %+v", decision)
	}
	if !strings.Contains(decision.Reason, "/other/magpie") || !strings.Contains(decision.Reason, "/recorded/magpie") {
		t.Fatalf("reason: %q", decision.Reason)
	}
}

func TestManageOffersOnlyALabelledUnboundTUIWhenIdentityIsUnknown(t *testing.T) {
	recorded := magpieInstance{ID: "external-1", Kind: "external", Address: "http://127.0.0.1:3425", ConfigDir: "/recorded/magpie", Version: "0.1.3528"}
	probe := gatewayProbe{Address: recorded.Address, State: gatewayUnknown, Reason: reasonProcessOtherUser}
	decision := planManagement(recorded, probe, "/opt/magpie")
	if decision.Allowed {
		t.Fatalf("decision: %+v", decision)
	}
	if decision.Reason != reasonProcessOtherUser {
		t.Fatalf("reason: %q", decision.Reason)
	}
	unbound := decision.Unbound
	if unbound == nil || !unbound.Allowed || unbound.Executable != "/opt/magpie" {
		t.Fatalf("unbound: %+v", unbound)
	}
	if !strings.Contains(unbound.Warning, "may not be the instance") {
		t.Fatalf("unbound warning: %q", unbound.Warning)
	}
	values := environmentValues(unbound.Environment)
	if values["MAGPIE_ADDR"] != "127.0.0.1:3425" || len(values) != 1 {
		t.Fatalf("unbound overrides: %v", values)
	}
	if environmentValues(mergedManagementEnvironment(unbound))["HOME"] != os.Getenv("HOME") {
		t.Fatal("unbound TUI lost the user's own environment")
	}
}

func TestManageRefusesAGatewayThatIsNotMagpie(t *testing.T) {
	recorded := magpieInstance{ID: "external-1", Kind: "external", Address: "http://127.0.0.1:3425", ConfigDir: "/recorded/magpie"}
	decision := planManagement(recorded, gatewayProbe{Address: recorded.Address, State: gatewayNotMagpie, Reason: reasonNotMagpie}, "/opt/magpie")
	if decision.Allowed || decision.Unbound != nil {
		t.Fatalf("decision: %+v", decision)
	}
}

func TestManageUsesTheServingExecutable(t *testing.T) {
	root := tempRoot(t)
	t.Setenv("AGENT_HARBOR_BRIDGE_HOME", root)
	layout := managedLayoutFor(filepath.Join(root, "magpie"))
	probe := verifiedProbe("http://127.0.0.1:3435", layout.DataDir)
	probe.Executable = "/Applications/Magpie.app/Contents/MacOS/magpie"
	probe.Serves = "serve"
	recorded := magpieInstance{ID: managedInstanceID, Kind: "managed", Address: probe.Address, ConfigDir: probe.ConfigDir}
	decision := planManagement(recorded, probe, "/opt/magpie")
	if !decision.Allowed || decision.Executable != probe.Executable {
		t.Fatalf("decision: %+v", decision)
	}
}

// A management UI that the user quits is not a failure, and one that fails to
// start is, even though both leave the gateway serving.
func TestManageDistinguishesQuittingTheUIFromItsFailure(t *testing.T) {
	probe := verifiedProbe("http://127.0.0.1:3425", "/recorded/magpie")
	probe.Serves = "serve"
	recorded := magpieInstance{ID: "external-1", Kind: "external", Address: probe.Address, ConfigDir: probe.ConfigDir, Version: probe.Version}
	executable := writeExitScript(t, "exit 0")

	decision := planManagement(recorded, probe, executable)
	if !decision.Allowed {
		t.Fatalf("decision: %+v", decision)
	}
	decision.Executable = executable
	if err := runManagement(&decision); err != nil {
		t.Fatalf("closing the UI: %v", err)
	}

	decision.Executable = writeExitScript(t, "exit 2")
	err := runManagement(&decision)
	if err == nil || !strings.Contains(err.Error(), "failed") {
		t.Fatalf("a failing UI reported %v", err)
	}
}

func TestInterruptedExitIsNotFailure(t *testing.T) {
	if !isInterruptedExit(exec.Command("sh", "-c", "kill -INT $$").Run()) {
		t.Fatal("a control-C exit was reported as failure")
	}
	if isInterruptedExit(exec.Command("sh", "-c", "exit 3").Run()) {
		t.Fatal("exit status 3 is a failure")
	}
	if isInterruptedExit(nil) || isInterruptedExit(exec.ErrNotFound) {
		t.Fatal("a missing executable is a failure")
	}
}

func writeExitScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(tempRoot(t), "magpie")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// A gateway without XDG_CONFIG_HOME or XDG_CACHE_HOME must not inherit the
// terminal's, or the TUI would read a different configuration than the
// instance Harbor verified.
func TestManageUnsetsConfigurationVariablesTheGatewayDoesNotHave(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/caller/config")
	t.Setenv("XDG_CACHE_HOME", "/caller/cache")
	t.Setenv("HOME", "/caller/home")
	probe := verifiedProbe("http://127.0.0.1:3425", "/recorded/magpie")
	probe.Serves = "serve"
	probe.variables = map[string]string{"HOME": "/gateway/home"}
	recorded := magpieInstance{ID: "external-1", Kind: "external", Address: probe.Address, ConfigDir: probe.ConfigDir}
	decision := planManagement(recorded, probe, "/opt/magpie")
	if !decision.Allowed {
		t.Fatalf("decision: %+v", decision)
	}
	values := environmentValues(mergedManagementEnvironment(&decision))
	if values["HOME"] != "/gateway/home" || values["MAGPIE_ADDR"] != "127.0.0.1:3425" {
		t.Fatalf("observed values: %v", values)
	}
	for _, key := range []string{"XDG_CONFIG_HOME", "XDG_CACHE_HOME"} {
		if value, present := values[key]; present {
			t.Fatalf("%s leaked from the caller: %q", key, value)
		}
	}
}
