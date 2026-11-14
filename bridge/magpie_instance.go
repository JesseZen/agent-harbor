package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const managedInstanceID = "managed"

const (
	instanceVerified  = "verified"
	instanceChanged   = "changed"
	instanceOffline   = "offline"
	instanceUnknown   = "unknown"
	instanceNotMagpie = "not-magpie"
)

// A Magpie instance is a gateway Harbor knows about. Its identity is the pair
// (configuration location, address) read from the serving process; a display
// name, a catalog ID or an address alone never stands in for it.
type magpieInstance struct {
	AuxiliaryAcknowledged bool      `json:"auxiliary_acknowledged,omitempty"`
	ID                    string    `json:"id"`
	Kind                  string    `json:"kind"`
	Name                  string    `json:"name"`
	Address               string    `json:"address"`
	ConfigDir             string    `json:"config_dir,omitempty"`
	Executable            string    `json:"executable,omitempty"`
	Version               string    `json:"version,omitempty"`
	VerifiedAt            time.Time `json:"verified_at,omitempty"`
}

type magpieRegistry struct {
	Default   string           `json:"default,omitempty"`
	Instances []magpieInstance `json:"instances"`
}

func instanceID(kind, address string) string {
	if kind == "managed" {
		return managedInstanceID
	}
	digest := sha256.Sum256([]byte(address))
	return "external-" + hex.EncodeToString(digest[:4])
}

func instanceName(kind, address string) string {
	if kind == "managed" {
		return "Harbor-managed Magpie"
	}
	return "Magpie at " + gatewayAddressOf(address)
}

// withMagpieRegistry serializes readers and writers of magpie.json; a
// successful update is atomic, like the session store.
func withMagpieRegistry(update func(*magpieRegistry) error) error {
	root, err := bridgeHome()
	if err != nil {
		return err
	}
	if err := ensureHome(root); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(root, "magpie.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)

	path := filepath.Join(root, "magpie.json")
	registry := magpieRegistry{Instances: []magpieInstance{}}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil {
		if err := json.Unmarshal(data, &registry); err != nil {
			return fmt.Errorf("invalid Magpie instance state: %w", err)
		}
		if registry.Instances == nil {
			return errors.New("invalid Magpie instance state: expected an array")
		}
	}
	if err := update(&registry); err != nil {
		return err
	}
	data, err = json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}
	return writeStateFile(path, append(data, '\n'))
}

func loadMagpieRegistry() (magpieRegistry, error) {
	registry := magpieRegistry{Instances: []magpieInstance{}}
	err := withMagpieRegistry(func(state *magpieRegistry) error {
		registry = *state
		return nil
	})
	return registry, err
}

func (r magpieRegistry) find(id string) (magpieInstance, bool) {
	for _, instance := range r.Instances {
		if instance.ID == id {
			return instance, true
		}
	}
	return magpieInstance{}, false
}

func (r magpieRegistry) defaultInstance() (magpieInstance, bool) {
	if r.Default != "" {
		return r.find(r.Default)
	}
	if len(r.Instances) == 1 {
		return r.Instances[0], true
	}
	return magpieInstance{}, false
}

// bindExternalMagpie records a user-managed gateway once its identity is
// verified. An address Harbor cannot identify is refused rather than stored
// with a guess.
func bindExternalMagpie(probe gatewayProbe) (magpieInstance, error) {
	if probe.State != gatewayVerified || probe.ConfigDir == "" {
		reason := probe.Reason
		if reason == "" {
			reason = "its configuration location is unknown"
		}
		return magpieInstance{}, fmt.Errorf("cannot bind the Magpie at %s: %s", probe.Address, reason)
	}
	instance := magpieInstance{
		ID: instanceID("external", probe.Address), Kind: "external",
		Name: instanceName("external", probe.Address), Address: probe.Address,
		ConfigDir: probe.ConfigDir, Executable: probe.Executable, Version: probe.Version,
		VerifiedAt: time.Now().UTC(),
	}
	err := withMagpieRegistry(func(registry *magpieRegistry) error {
		for i, existing := range registry.Instances {
			if existing.ID == instance.ID {
				if existing.Address == instance.Address && existing.ConfigDir == instance.ConfigDir && existing.Executable == instance.Executable {
					instance.AuxiliaryAcknowledged = existing.AuxiliaryAcknowledged
				}
				registry.Instances[i] = instance
				if registry.Default == "" {
					registry.Default = instance.ID
				}
				return nil
			}
		}
		registry.Instances = append(registry.Instances, instance)
		if registry.Default == "" {
			registry.Default = instance.ID
		}
		return nil
	})
	return instance, err
}

// registerManagedMagpie records the instance Harbor owns, keeping its ID and
// verification time up to date after a probe.
func registerManagedMagpie(probe gatewayProbe) (magpieInstance, error) {
	instance := magpieInstance{
		ID: managedInstanceID, Kind: "managed", Name: instanceName("managed", probe.Address),
		Address: probe.Address, ConfigDir: probe.ConfigDir, Executable: probe.Executable, Version: probe.Version,
		VerifiedAt: time.Now().UTC(),
	}
	if probe.State != gatewayVerified {
		instance.ConfigDir, instance.Executable, instance.Version, instance.VerifiedAt = "", "", "", time.Time{}
	}
	err := withMagpieRegistry(func(registry *magpieRegistry) error {
		for i, existing := range registry.Instances {
			if existing.ID == managedInstanceID {
				if probe.State != gatewayVerified {
					// Keep the recorded identity while the instance is stopped;
					// only its address is known without a probe.
					instance.ConfigDir, instance.Executable, instance.Version, instance.VerifiedAt = existing.ConfigDir, existing.Executable, existing.Version, existing.VerifiedAt
					if instance.Address == "" {
						instance.Address = existing.Address
					}
				}
				if existing.Address == instance.Address && existing.ConfigDir == instance.ConfigDir && existing.Executable == instance.Executable {
					instance.AuxiliaryAcknowledged = existing.AuxiliaryAcknowledged
				}
				registry.Instances[i] = instance
				return nil
			}
		}
		registry.Instances = append(registry.Instances, instance)
		return nil
	})
	return instance, err
}

func useMagpieInstance(id string) (magpieInstance, error) {
	var selected magpieInstance
	err := withMagpieRegistry(func(registry *magpieRegistry) error {
		instance, ok := registry.find(id)
		if !ok {
			return fmt.Errorf("no Magpie instance %q; run `agent-harbor magpie status` to list them", id)
		}
		registry.Default = instance.ID
		selected = instance
		return nil
	})
	return selected, err
}

type instanceHealth struct {
	State    string
	Detail   string
	Warnings []string
	Probe    gatewayProbe
}

// health compares a live probe with the recorded identity. A different
// process or version is reported but does not change the identity; a
// different configuration location does.
func (instance magpieInstance) health(probe gatewayProbe) instanceHealth {
	health := instanceHealth{State: probe.State, Detail: probe.Reason, Probe: probe}
	switch probe.State {
	case gatewayVerified:
		health.State = instanceVerified
		health.Detail = ""
		if probe.Serves == "tui" {
			health.Warnings = append(health.Warnings, "Another Magpie TUI owns the gateway. Closing that original TUI stops the gateway and its sessions.")
		}
		if instance.ConfigDir != "" && instance.ConfigDir != probe.ConfigDir {
			health.State = instanceChanged
			health.Detail = fmt.Sprintf("the gateway at %s now reads %s; this instance was recorded with %s", instance.Address, probe.ConfigDir, instance.ConfigDir)
			return health
		}
		if instance.Version != "" && probe.Version != "" && instance.Version != probe.Version {
			health.Warnings = append(health.Warnings, fmt.Sprintf("Magpie version changed from %s to %s; this combination was not tested", instance.Version, probe.Version))
		}
	case gatewayOffline:
		health.State = instanceOffline
	case gatewayNotMagpie:
		health.State = instanceNotMagpie
	default:
		health.State = instanceUnknown
	}
	return health
}

func (h instanceHealth) needsAttention() bool {
	return h.State != instanceVerified
}
