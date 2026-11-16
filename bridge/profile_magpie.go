package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// The original caller-only flag proves forwarding, not discovery/write guards.
func requireProfileSafeGateway(address string) error {
	base, err := parseGatewayAddress(address)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Get(base + "/api/hello")
	if err != nil {
		return errors.New("Profile-safe Magpie gateway is unavailable")
	}
	defer response.Body.Close()
	var hello struct {
		Name        string `json:"name"`
		CallerOnly  bool   `json:"codex_caller_only"`
		ProfileSafe bool   `json:"codex_profile_isolation"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&hello) != nil || hello.Name != "magpie" || !hello.CallerOnly || !hello.ProfileSafe {
		return errors.New("Magpie does not verify Profile discovery/write isolation; use the Profile isolation candidate before starting this session")
	}
	return nil
}

func (h harborService) ProfileCatalog(b profileBinding, id string) ([]catalogModel, error) {
	e, err := h.ResolveProfileClient(profileClientRequest{Binding: b})
	if err != nil {
		return nil, err
	}
	r, err := loadMagpieRegistry()
	if err != nil {
		return nil, err
	}
	i, ok := r.find(id)
	if !ok {
		return nil, errors.New("Magpie instance not found")
	}
	if e.Kind == profileIsolated {
		if err := requireProfileSafeGateway(i.Address); err != nil {
			return nil, err
		}
	}
	return fetchCatalogEnvironment(i.Address, e.Environment)
}

func profileManagementSafety(address string) error {
	profiles, err := (harborService{}).Profiles()
	if err != nil {
		if errors.Is(err, ErrProfileHome) {
			return nil
		}
		return err
	}
	for _, p := range profiles {
		if p.Kind == profileIsolated {
			return requireProfileSafeGateway(address)
		}
	}
	return nil
}

// Neither service startup nor its management shell inherits a caller's client
// state selectors/auth. Verified instance-owned overrides are applied afterward.
func magpieBaseEnvironment() []string {
	result := []string{}
	for _, pair := range os.Environ() {
		key, _, _ := strings.Cut(pair, "=")
		if key == originalEnvKey || key == "AGENT_HARBOR_MAGPIE_KEY" || key == "MAGPIE_CODEX_CALLER_ONLY" || strings.HasPrefix(key, "CODEX_") || strings.HasPrefix(key, "OPENAI_") || strings.HasPrefix(key, "CHATGPT_") || strings.HasPrefix(key, "AH_PROFILE_") {
			continue
		}
		result = append(result, pair)
	}
	return result
}

func validateProfileManagementEnvironment(address string, env []string) error {
	profiles, err := (harborService{}).Profiles()
	if err != nil {
		if errors.Is(err, ErrProfileHome) {
			return nil
		}
		return err
	}
	isolated := false
	for _, p := range profiles {
		isolated = isolated || p.Kind == profileIsolated
	}
	if !isolated {
		return nil
	}
	if err := requireProfileSafeGateway(address); err != nil {
		return err
	}
	if environmentValue(env, "MAGPIE_CODEX_CALLER_ONLY") != "1" {
		return errors.New("Magpie management process does not preserve verified caller-only isolation")
	}
	home := environmentValue(env, "CODEX_HOME")
	if home == "" {
		return errors.New("Magpie management client home is unverified")
	}
	for _, p := range profiles {
		for _, client := range p.Clients {
			for _, root := range []string{client.Home, client.SQLite, client.Runtime} {
				if root != "" && profilePathsOverlap(home, root) {
					return ErrProfileAlias
				}
			}
		}
	}
	return nil
}
