package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/BurntSushi/toml"
)

// These are only the two nonsecret defaults in Codex's home configuration.
// Project configuration, profiles and client behavior can override them, so
// this is a review aid, not a claim about the effective provider or account.
func nativeCodexConfigSummary(env []string) string {
	home := environmentValue(env, "CODEX_HOME")
	if home == "" {
		home = filepath.Join(environmentValue(env, "HOME"), ".codex")
	}
	if !filepath.IsAbs(home) {
		return "Codex home settings unavailable; verify the provider and model."
	}
	path := filepath.Join(home, "config.toml")
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return "Codex home has no config.toml; project or profile settings may still select a provider."
	}
	if err != nil {
		return "Codex home settings unavailable; verify the provider and model."
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return "Codex home settings unavailable; verify the provider and model."
	}
	file, err := os.Open(path)
	if err != nil {
		return "Codex home settings unavailable; verify the provider and model."
	}
	defer file.Close()
	var config struct {
		Model         string `toml:"model"`
		Provider      string `toml:"model_provider"`
		OpenAIBaseURL string `toml:"openai_base_url"`
	}
	if _, err := toml.NewDecoder(io.LimitReader(file, 1<<20)).Decode(&config); err != nil {
		return "Codex home settings unavailable; verify the provider and model."
	}
	field := func(value string) string {
		value = strings.TrimSpace(strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, value))
		runes := []rune(value)
		if len(runes) > 100 {
			value = string(runes[:100]) + "…"
		}
		if value == "" {
			return "client default"
		}
		return value
	}
	baseURL := ""
	if strings.TrimSpace(config.OpenAIBaseURL) != "" {
		// URLs can contain credentials, so only indicate that an override exists.
		baseURL = " OpenAI base URL override is set."
	}
	return fmt.Sprintf("Codex home model %s; provider %s.%s Project or profile may override these; verify the actual route.", field(config.Model), field(config.Provider), baseURL)
}

func (harborService) NativeCodexConfig() string {
	env, err := userEnvironment()
	if err != nil {
		return "Codex home settings unavailable; verify the provider and model."
	}
	return nativeCodexConfigSummary(env)
}
