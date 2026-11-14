package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestRegistry(t *testing.T) {
	t.Helper()
	root := tempRoot(t)
	t.Setenv("AGENT_HARBOR_BRIDGE_HOME", filepath.Join(root, "state"))
}

func verifiedProbe(address, config string) gatewayProbe {
	return gatewayProbe{Address: address, State: gatewayVerified, Version: "0.1.3528", PID: 900, Serves: "serve", Executable: config + "/magpie", ConfigDir: config, ConfigEvidence: configEvidenceInstalled}
}

func TestMagpieInstanceIDsAreOnePerAddress(t *testing.T) {
	if instanceID("external", "http://127.0.0.1:3425") == instanceID("external", "http://127.0.0.1:3426") {
		t.Fatal("different addresses shared an ID")
	}
	if instanceID("managed", "http://127.0.0.1:3435") != managedInstanceID {
		t.Fatal("managed instance must have its fixed ID")
	}
}

func TestBindExternalMagpieRecordsVerifiedIdentity(t *testing.T) {
	newTestRegistry(t)
	probe := verifiedProbe("http://127.0.0.1:3425", "/home/user/.config/magpie")
	instance, err := bindExternalMagpie(probe)
	if err != nil {
		t.Fatal(err)
	}
	if instance.Kind != "external" || instance.Address != probe.Address || instance.ConfigDir != probe.ConfigDir || instance.Version != "0.1.3528" || instance.Executable != probe.Executable {
		t.Fatalf("instance: %+v", instance)
	}
	if instance.VerifiedAt.IsZero() || time.Since(instance.VerifiedAt) > time.Minute {
		t.Fatalf("verification time: %v", instance.VerifiedAt)
	}
	registry, err := loadMagpieRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if registry.Default != instance.ID || len(registry.Instances) != 1 || registry.Instances[0] != instance {
		t.Fatalf("registry: %+v", registry)
	}
	data, err := os.ReadFile(filepath.Join(os.Getenv("AGENT_HARBOR_BRIDGE_HOME"), "magpie.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("registry file is empty")
	}
	info, err := os.Stat(filepath.Join(os.Getenv("AGENT_HARBOR_BRIDGE_HOME"), "magpie.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("registry permissions: %v %v", info.Mode().Perm(), err)
	}
}

func TestBindExternalMagpieRefusesIdentityItCannotEstablish(t *testing.T) {
	newTestRegistry(t)
	for _, probe := range []gatewayProbe{
		{Address: "http://127.0.0.1:3425", State: gatewayOffline},
		{Address: "http://127.0.0.1:3425", State: gatewayNotMagpie, Reason: reasonNotMagpie},
		{Address: "http://127.0.0.1:3425", State: gatewayUnknown, Reason: reasonNoServingProcess},
	} {
		if _, err := bindExternalMagpie(probe); err == nil {
			t.Fatalf("bound an instance without identity: %+v", probe)
		}
	}
	registry, err := loadMagpieRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Instances) != 0 {
		t.Fatalf("registry changed: %+v", registry)
	}
}

func TestBindingTheSameAddressAgainRefreshesItsRecord(t *testing.T) {
	newTestRegistry(t)
	first, err := bindExternalMagpie(verifiedProbe("http://127.0.0.1:3425", "/a"))
	if err != nil {
		t.Fatal(err)
	}
	probe := verifiedProbe("http://127.0.0.1:3425", "/b")
	probe.Version = "0.1.3600"
	second, err := bindExternalMagpie(probe)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || second.ConfigDir != "/b" || second.Version != "0.1.3600" {
		t.Fatalf("rebind: %+v", second)
	}
	registry, err := loadMagpieRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Instances) != 1 {
		t.Fatalf("duplicate records: %+v", registry)
	}
}

func TestUseSelectsTheDefaultInstance(t *testing.T) {
	newTestRegistry(t)
	a, err := bindExternalMagpie(verifiedProbe("http://127.0.0.1:3425", "/a"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := bindExternalMagpie(verifiedProbe("http://127.0.0.1:3426", "/b"))
	if err != nil {
		t.Fatal(err)
	}
	selected, err := useMagpieInstance(b.ID)
	if err != nil || selected.ID != b.ID {
		t.Fatalf("use: %+v %v", selected, err)
	}
	registry, err := loadMagpieRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if registry.Default != b.ID {
		t.Fatalf("default: %q", registry.Default)
	}
	if _, err := useMagpieInstance(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := useMagpieInstance("external-missing"); err == nil {
		t.Fatal("selected an unknown instance")
	}
}

func TestInstanceHealthComparesTheRecordedIdentity(t *testing.T) {
	recorded := magpieInstance{ID: "external-1", Kind: "external", Address: "http://127.0.0.1:3425", ConfigDir: "/recorded/magpie", Version: "0.1.3528"}
	cases := []struct {
		name   string
		probe  gatewayProbe
		state  string
		detail string
	}{
		{"same", verifiedProbe("http://127.0.0.1:3425", "/recorded/magpie"), instanceVerified, ""},
		{"changed config", verifiedProbe("http://127.0.0.1:3425", "/other/magpie"), instanceChanged, "now reads"},
		{"offline", gatewayProbe{Address: "http://127.0.0.1:3425", State: gatewayOffline}, instanceOffline, ""},
		{"unknown", gatewayProbe{Address: "http://127.0.0.1:3425", State: gatewayUnknown, Reason: reasonProcessOtherUser}, instanceUnknown, reasonProcessOtherUser},
		{"not magpie", gatewayProbe{Address: "http://127.0.0.1:3425", State: gatewayNotMagpie, Reason: reasonNotMagpie}, instanceNotMagpie, ""},
	}
	for _, c := range cases {
		health := recorded.health(c.probe)
		if health.State != c.state {
			t.Errorf("%s: state %q", c.name, health.State)
		}
		if c.detail != "" && !strings.Contains(health.Detail, c.detail) {
			t.Errorf("%s: detail %q", c.name, health.Detail)
		}
	}
}

func TestInstanceHealthWarnsAboutAVersionChangeWithoutBlocking(t *testing.T) {
	recorded := magpieInstance{ID: "external-1", Kind: "external", Address: "http://127.0.0.1:3425", ConfigDir: "/recorded/magpie", Version: "0.1.3528"}
	health := recorded.health(verifiedProbe("http://127.0.0.1:3425", "/recorded/magpie"))
	if health.State != instanceVerified || len(health.Warnings) != 0 {
		t.Fatalf("same version: %+v", health)
	}
	probe := verifiedProbe("http://127.0.0.1:3425", "/recorded/magpie")
	probe.Version = "0.1.3600"
	health = recorded.health(probe)
	if health.State != instanceVerified || len(health.Warnings) != 1 || !strings.Contains(health.Warnings[0], "0.1.3600") {
		t.Fatalf("version change: %+v", health)
	}
}

func TestManagedInstanceHealthRequiresItsOwnConfiguration(t *testing.T) {
	managed := magpieInstance{ID: managedInstanceID, Kind: "managed", Address: "http://127.0.0.1:3435", ConfigDir: "/state/magpie/bin/data"}
	if health := managed.health(verifiedProbe("http://127.0.0.1:3435", "/state/magpie/bin/data")); health.State != instanceVerified {
		t.Fatalf("managed: %+v", health)
	}
	if health := managed.health(verifiedProbe("http://127.0.0.1:3435", "/home/user/.config/magpie")); health.State != instanceChanged {
		t.Fatalf("foreign gateway on the managed port: %+v", health)
	}
}

func TestAuxiliaryAcknowledgementSurvivesServiceAndSameIdentityRebind(t *testing.T) {
	newTestRegistry(t)
	p := verifiedProbe("http://127.0.0.1:43123", "/fixture/one")
	i, err := bindExternalMagpie(p)
	if err != nil {
		t.Fatal(err)
	}
	h := harborService{}
	if got, err := h.AuxiliaryAcknowledged(i.ID); err != nil || got {
		t.Fatalf("initial: %v %v", got, err)
	}
	if err := h.AcknowledgeAuxiliary(i.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := bindExternalMagpie(p); err != nil {
		t.Fatal(err)
	}
	if got, err := (harborService{}).AuxiliaryAcknowledged(i.ID); err != nil || !got {
		t.Fatalf("persisted: %v %v", got, err)
	}
	p.ConfigDir = "/fixture/replaced"
	if _, err := bindExternalMagpie(p); err != nil {
		t.Fatal(err)
	}
	if got, err := h.AuxiliaryAcknowledged(i.ID); err != nil || got {
		t.Fatalf("changed identity: %v %v", got, err)
	}
	if err := h.AcknowledgeAuxiliary("missing"); err == nil {
		t.Fatal("accepted missing instance")
	}
}
