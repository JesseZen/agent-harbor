package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestProfileMagpieRequiresDiscoveryWriteCapabilityAndOwnManagementHome(t *testing.T) {
	h, _ := profileFixture(t)
	a := mustProfile(t, h, "A")
	var safe atomic.Bool
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if safe.Load() {
			w.Write([]byte(`{"name":"magpie","codex_caller_only":true,"codex_profile_isolation":true}`))
		} else {
			w.Write([]byte(`{"name":"magpie","codex_caller_only":true}`))
		}
	}))
	defer gateway.Close()
	if err := requireProfileSafeGateway(gateway.URL); err == nil {
		t.Fatal("forwarding-only candidate accepted")
	}
	safe.Store(true)
	if err := requireProfileSafeGateway(gateway.URL); err != nil {
		t.Fatal(err)
	}
	manager := t.TempDir()
	env := []string{"CODEX_HOME=" + manager, "MAGPIE_CODEX_CALLER_ONLY=1"}
	if err := validateProfileManagementEnvironment(gateway.URL, env); err != nil {
		t.Fatal(err)
	}
	env[0] = "CODEX_HOME=" + a.Clients[0].Home
	if err := validateProfileManagementEnvironment(gateway.URL, env); !errors.Is(err, ErrProfileAlias) {
		t.Fatal("manager accepted Profile home", err)
	}
	env = []string{"CODEX_HOME=" + manager}
	if err := validateProfileManagementEnvironment(gateway.URL, env); err == nil {
		t.Fatal("manager dropped caller-only policy")
	}
}

func TestProfileMagpieDoesNotInheritCallerSelectors(t *testing.T) {
	_, root := profileFixture(t)
	t.Setenv("CODEX_HOME", filepath.Join(root, "caller"))
	t.Setenv("AGENT_HARBOR_MAGPIE_KEY", "synthetic-gateway")
	t.Setenv("OPENAI_API_KEY", "synthetic-caller")
	t.Setenv("CHATGPT_ACCOUNT_ID", "synthetic-account")
	t.Setenv("CODEX_AUTH_JSON", "synthetic-value")
	t.Setenv("AH_PROFILE_ID", "synthetic-profile")
	t.Setenv(originalEnvKey, "synthetic-snapshot")
	env := magpieBaseEnvironment()
	for _, key := range []string{"AGENT_HARBOR_MAGPIE_KEY", "CODEX_HOME", "OPENAI_API_KEY", "CHATGPT_ACCOUNT_ID", "CODEX_AUTH_JSON", "AH_PROFILE_ID", originalEnvKey} {
		if environmentValue(env, key) != "" {
			t.Fatalf("caller selector retained: %s", key)
		}
	}
	if environmentValue(env, "HOME") != os.Getenv("HOME") {
		t.Fatal("unrelated inherited tools lost")
	}
}
