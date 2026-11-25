package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const codexDefaultModel = "@codex-default"

// joinedCodex is a new session whose built-in OpenAI provider talks to a
// caller-only Magpie gateway. Empty means a pre-existing separate provider.
func joinedCodex(s session) bool {
	return s.CLI == "codex" && s.Mode == "magpie" && s.CodexConnection == "caller"
}

func joinedCodexArgs(s session, kind launchKind) []string {
	base := strings.TrimRight(s.MagpieURL, "/") + "/backend-api/codex"
	if s.ResponsesLiteModel != "" {
		base += "/harbor-responses-lite"
	}
	args := []string{}
	if kind == newConversation {
		args = append(args, "-c", `model_provider="openai"`)
	}
	// On resume the saved thread already owns its provider. Even repeating
	// "openai" is an override that makes Codex discard the saved model/effort.
	args = append(args, "-c", "openai_base_url="+strconv.Quote(base))
	if kind == newConversation && s.MagpieModel != codexDefaultModel {
		args = append(args, "-c", "model="+strconv.Quote(s.MagpieModel))
	}
	return args
}

// Codex keeps its last official catalog in its own home. Use the visible
// bare model IDs for initial choices; Codex still checks the current account.
func cachedCodexModels() []catalogModel {
	env, err := userEnvironment()
	if err != nil {
		return nil
	}
	return cachedCodexModelsEnvironment(env)
}

func cachedCodexModelsEnvironment(env []string) []catalogModel {
	home := environmentValue(env, "CODEX_HOME")
	if home == "" {
		home = filepath.Join(environmentValue(env, "HOME"), ".codex")
	}
	return cachedCodexModelsIn(home)
}

func cachedCodexModelsIn(home string) []catalogModel {
	models := []catalogModel{{ID: codexDefaultModel, DisplayName: "Codex default (choose a model in Codex)"}}
	file, err := os.Open(filepath.Join(home, "models_cache.json"))
	if err != nil {
		return models
	}
	defer file.Close()
	var cache struct {
		Models []struct {
			Slug        string `json:"slug"`
			DisplayName string `json:"display_name"`
			Visibility  string `json:"visibility"`
		} `json:"models"`
	}
	if json.NewDecoder(io.LimitReader(file, 16<<20)).Decode(&cache) != nil {
		return models
	}
	for _, model := range cache.Models {
		if model.Visibility == "list" && model.Slug != "" && !strings.Contains(model.Slug, "/") {
			models = append(models, catalogModel{ID: model.Slug, DisplayName: model.DisplayName + " · Codex (cached)"})
		}
	}
	return models
}

// An unpatched or differently configured gateway may replace the caller's
// account. Do not start a joined session unless it reports this mode active.
func requireCallerOnlyGateway(address string) error {
	base, err := parseGatewayAddress(address)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Get(base + "/api/hello")
	if err != nil {
		return errors.New("caller-only Magpie gateway is unavailable")
	}
	defer response.Body.Close()
	var info struct {
		Name       string `json:"name"`
		CallerOnly bool   `json:"codex_caller_only"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&info) != nil || info.Name != "magpie" || !info.CallerOnly {
		return errors.New("this Magpie gateway does not report caller-only Codex mode; start a compatible gateway with that mode enabled")
	}
	return nil
}
