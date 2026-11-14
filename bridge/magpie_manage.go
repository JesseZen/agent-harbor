package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// A management decision is whether Magpie's own TUI can be opened for an
// instance, with which environment, and what the user must know first.
type managementDecision struct {
	Allowed     bool
	Reason      string
	Probe       gatewayProbe
	Executable  string
	Arguments   []string
	Environment []string // overrides copied from the verified process
	Unset       []string // configuration variables the process does not have
	Warning     string
	Unbound     *managementDecision
}

// planManagement decides whether Harbor can open a Magpie UI that truly
// manages the instance's configuration. It never opens a UI that would host
// the gateway itself, and never claims to manage an unverified instance.
func planManagement(instance magpieInstance, probe gatewayProbe, bundled string) managementDecision {
	decision := managementDecision{Probe: probe}
	switch probe.State {
	case gatewayOffline:
		decision.Reason = fmt.Sprintf("no Magpie answers at %s. Opening Magpie's TUI there would make it host the gateway, and closing it would stop every session using it. Start that Magpie first.", instance.Address)
		return decision
	case gatewayNotMagpie:
		decision.Reason = fmt.Sprintf("%s; another program may hold %s", reasonNotMagpie, instance.Address)
		return decision
	case gatewayUnknown:
		decision.Reason = probe.Reason
		if bundled != "" {
			decision.Unbound = unboundDecision(instance, bundled, probe.Reason)
		}
		return decision
	}
	if instance.ConfigDir != "" && instance.ConfigDir != probe.ConfigDir {
		decision.Reason = fmt.Sprintf("the gateway at %s now reads %s, but this instance was recorded with %s. Check Magpie, then bind the address again before relying on this instance.", instance.Address, probe.ConfigDir, instance.ConfigDir)
		return decision
	}
	decision.Allowed = true
	decision.Executable = probe.Executable
	decision.Arguments = []string{"tui"}
	decision.Environment, decision.Unset = instanceManagementEnvironment(probe)
	if probe.Serves == "tui" {
		decision.Warning = fmt.Sprintf("Another Magpie TUI at %s owns the gateway. Closing it stops the gateway and every session using it.", instance.Address)
	}
	return decision
}

// instanceManagementEnvironment overrides only the paths Magpie reads its
// configuration from and the address it talks to, reproducing the environment
// of the verified process. A variable the process does not have must be
// absent, not inherited from the terminal, or Magpie would read a different
// configuration than the instance sessions use.
func instanceManagementEnvironment(probe gatewayProbe) (overrides, unset []string) {
	for _, key := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME"} {
		if value := probe.variables[key]; value != "" {
			overrides = append(overrides, key+"="+value)
		} else {
			unset = append(unset, key)
		}
	}
	return append(overrides, "MAGPIE_ADDR="+gatewayAddressOf(probe.Address)), unset
}

// unboundDecision is the only option for an instance whose identity cannot be
// verified: Magpie's own settings, clearly labelled as possibly a different
// Magpie from the one sessions use.
func unboundDecision(instance magpieInstance, bundled, reason string) *managementDecision {
	return &managementDecision{
		Allowed: true, Executable: bundled, Arguments: []string{"tui"},
		Environment: []string{"MAGPIE_ADDR=" + gatewayAddressOf(instance.Address)},
		Warning:     fmt.Sprintf("This Magpie uses your own settings and may not be the instance your sessions use (identity unknown: %s). If it starts a gateway on this address, closing it stops that gateway.", reason),
	}
}

func mergedManagementEnvironment(decision *managementDecision) []string {
	updates := map[string]*string{}
	for _, pair := range decision.Environment {
		if key, value, ok := strings.Cut(pair, "="); ok {
			updates[key] = new(value)
		}
	}
	removed := map[string]bool{}
	for _, key := range decision.Unset {
		removed[key] = true
	}
	base := make([]string, 0, len(os.Environ()))
	for _, pair := range os.Environ() {
		if key, _, ok := strings.Cut(pair, "="); ok && removed[key] {
			continue
		}
		base = append(base, pair)
	}
	return replaceEnvironment(base, updates)
}

// runManagement opens Magpie's own TUI and reports the gateway state after it
// closes. It never stops the gateway. Quitting the UI is not a failure.
func runManagement(decision *managementDecision) error {
	if decision.Warning != "" {
		fmt.Println(decision.Warning)
	}
	cmd := exec.Command(decision.Executable, decision.Arguments...)
	cmd.Env = mergedManagementEnvironment(decision)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	probe := probeGateway(decision.Probe.Address)
	if probe.State == gatewayVerified {
		fmt.Printf("\nMagpie closed. The gateway is still serving at %s (PID %d).\n", probe.Address, probe.PID)
	} else {
		fmt.Printf("\nMagpie closed. The gateway at %s is now %s.\n", decision.Probe.Address, probe.State)
	}
	if err == nil || isInterruptedExit(err) {
		return nil
	}
	return fmt.Errorf("Magpie's management UI failed: %w", err)
}

// isInterruptedExit reports whether Magpie's TUI ended because the user quit
// it (control key or signal) rather than because it failed to start or run.
func isInterruptedExit(err error) bool {
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return false
	}
	return interruptedExit(exit)
}

// refreshInstanceAfterManagement updates what a re-probe verified, without
// changing a recorded configuration location or a selected default.
func refreshInstanceAfterManagement(instance magpieInstance, probe gatewayProbe) error {
	if instance.ID == "" || probe.State != gatewayVerified || instance.ConfigDir != probe.ConfigDir {
		return nil
	}
	return withMagpieRegistry(func(registry *magpieRegistry) error {
		for i, existing := range registry.Instances {
			if existing.ID != instance.ID {
				continue
			}
			registry.Instances[i].Executable = probe.Executable
			registry.Instances[i].Version = probe.Version
			registry.Instances[i].VerifiedAt = time.Now().UTC()
			return nil
		}
		return nil
	})
}

// resolveManagementInstance chooses the instance a command acts on: an
// explicit ID, an explicit address, or the selected default.
func resolveManagementInstance(id, address string) (magpieInstance, gatewayProbe, error) {
	registry, err := loadMagpieRegistry()
	if err != nil {
		return magpieInstance{}, gatewayProbe{}, err
	}
	if id != "" {
		instance, ok := registry.find(id)
		if !ok {
			return magpieInstance{}, gatewayProbe{}, fmt.Errorf("no Magpie instance %q; run `agent-harbor magpie status` to list them", id)
		}
		return instance, probeGateway(instance.Address), nil
	}
	if address != "" {
		probe := probeGateway(address)
		if probe.State == gatewayUnknown && probe.Address == "" {
			return magpieInstance{}, probe, fmt.Errorf("%s", probe.Reason)
		}
		for _, instance := range registry.Instances {
			if instance.Address == probe.Address {
				return instance, probe, nil
			}
		}
		return magpieInstance{Address: probe.Address, ConfigDir: probe.ConfigDir, Version: probe.Version}, probe, nil
	}
	instance, ok := registry.defaultInstance()
	if !ok {
		return magpieInstance{}, gatewayProbe{}, errors.New("no Magpie instance is selected; run `agent-harbor magpie status` or `agent-harbor magpie bind --address ADDRESS`")
	}
	return instance, probeGateway(instance.Address), nil
}
