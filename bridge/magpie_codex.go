package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const magpieCodexKey = "magpie-agent-harbor"

var errMagpieCodexModelHidden = errors.New("the selected model or group is not visible in Magpie's Codex catalog; review its visibility in Magpie")
var errMagpieCodexEffortHidden = errors.New("the saved reasoning effort is not available for this model in Magpie's Codex catalog; review it in Magpie")

// Codex saves a default selected in /model to its active profile. Keep that
// write per Harbor session while continuing to use the user's Codex home for
// authentication, conversation history, skills, plugins and base settings.
func magpieCodexProfilePath(id string, env []string) (home, path, marker string, err error) {
	if !validID(id) {
		return "", "", "", errors.New("invalid Harbor session ID for Codex profile")
	}
	home = environmentValue(env, "CODEX_HOME")
	if home == "" {
		userHome := environmentValue(env, "HOME")
		if !filepath.IsAbs(userHome) {
			return "", "", "", errors.New("Codex home is unavailable; cannot isolate Magpie model defaults")
		}
		home = filepath.Join(userHome, ".codex")
	}
	if !filepath.IsAbs(home) {
		return "", "", "", errors.New("Codex home must be absolute to isolate Magpie model defaults")
	}
	home = filepath.Clean(home)
	name := "agent-harbor-" + id
	return home, filepath.Join(home, name+".config.toml"), "# Agent Harbor session " + id, nil
}

func profileHasMarker(data []byte, marker string) bool {
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == marker {
			return true
		}
	}
	return false
}

// Open only the regular file checked at this path, and limit the read even if
// another process changes it. The profile can contain unrelated user settings.
func readOwnedCodexProfile(path, marker string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, fmt.Errorf("%w: profile path is not a regular file", errCodexSelectionConflict)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, fmt.Errorf("%w: profile path changed", errCodexSelectionConflict)
	}
	data, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return nil, fmt.Errorf("%w: profile is unreadable or too large", errCodexSelectionConflict)
	}
	if !profileHasMarker(data, marker) {
		return nil, fmt.Errorf("%w: profile name is already in use", errCodexSelectionConflict)
	}
	return data, nil
}

func magpieCodexProfile(id, model string, env []string) (string, error) {
	home, path, marker, err := magpieCodexProfilePath(id, env)
	if err != nil {
		return "", err
	}
	if !selectionTextValid(model, 512) {
		return "", errors.New("invalid initial model for the Codex session profile")
	}
	if err := os.MkdirAll(home, 0700); err != nil {
		return "", errors.New("cannot prepare Codex home for an isolated Magpie profile")
	}
	name := "agent-harbor-" + id
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err == nil {
		_, writeErr := fmt.Fprintf(file, "%s\nmodel = %s\n", marker, strconv.Quote(model))
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			return "", errors.New("cannot create the isolated Codex profile")
		}
		return name, nil
	}
	if !errors.Is(err, os.ErrExist) {
		return "", errors.New("cannot create the isolated Codex profile")
	}
	if _, err := readOwnedCodexProfile(path, marker); err != nil {
		return "", errors.New("Codex profile is unavailable or not owned by this session; review the conflict")
	}
	return name, nil
}

// Copy Magpie's own Codex-format catalog unchanged. A CLI catalog override is
// needed because a model_catalog_json in a native or project config otherwise
// takes precedence over the provider's remote model_catalog_url.
func fetchMagpieCodexCatalog(address string) ([]byte, []magpieCodexModel, error) {
	return fetchMagpieCodexCatalogVariant(address, false)
}

func fetchMagpieCodexCatalogVariant(address string, lite bool) ([]byte, []magpieCodexModel, error) {
	base, err := parseGatewayAddress(address)
	if err != nil {
		return nil, nil, errors.New("cannot use an unverified Magpie catalog address")
	}
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	endpoint := base + "/v1/codex/models"
	if lite {
		endpoint += "?responses_format=lite"
	}
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, nil, errors.New("cannot prepare the Magpie Codex catalog request")
	}
	req.Header.Set("Authorization", "Bearer "+magpieCodexKey)
	response, err := client.Do(req)
	if err != nil {
		return nil, nil, errors.New("Magpie's Codex model catalog is unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("Magpie's Codex model catalog is unavailable (HTTP %d)", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if err != nil || len(body) > 4<<20 {
		return nil, nil, errors.New("Magpie's Codex model catalog is unreadable or too large")
	}
	var catalog struct {
		Models []magpieCodexModel `json:"models"`
		Format string             `json:"magpie_responses_format"`
	}
	if json.NewDecoder(bytes.NewReader(body)).Decode(&catalog) != nil || catalog.Models == nil || !json.Valid(body) {
		return nil, nil, errors.New("Magpie returned an invalid Codex model catalog")
	}
	if lite && catalog.Format != "lite" {
		return nil, nil, errMagpieResponsesLiteUnavailable
	}
	return body, catalog.Models, nil
}

type magpieCodexModel struct {
	Slug             string `json:"slug"`
	UseResponsesLite bool   `json:"use_responses_lite"`
	Levels           []struct {
		Effort string `json:"effort"`
	} `json:"supported_reasoning_levels"`
}

func requireMagpieResponsesLite(address, model string) error {
	_, models, err := fetchMagpieCodexCatalogVariant(address, true)
	if err != nil {
		return err
	}
	return checkMagpieResponsesLite(models, model)
}

func checkMagpieResponsesLite(models []magpieCodexModel, route string) error {
	for _, model := range models {
		if model.Slug == route && model.UseResponsesLite {
			return nil
		}
	}
	return errMagpieResponsesLiteUnavailable
}

func magpieCodexCatalog(address, directory, selected, effort, liteModel string) (string, error) {
	body, models, err := fetchMagpieCodexCatalogVariant(address, liteModel != "")
	if err != nil {
		return "", err
	}
	if liteModel != "" {
		if err := checkMagpieResponsesLite(models, liteModel); err != nil {
			return "", err
		}
	}
	found := selected == ""
	for _, model := range models {
		if model.Slug == selected {
			found = true
			if effort != "" {
				available := false
				for _, level := range model.Levels {
					if level.Effort == effort {
						available = true
						break
					}
				}
				if !available {
					return "", errMagpieCodexEffortHidden
				}
			}
			break
		}
	}
	if !found {
		return "", errMagpieCodexModelHidden
	}
	file, err := os.CreateTemp(directory, "magpie-codex-catalog-*.json")
	if err != nil {
		return "", errors.New("cannot create the private Codex catalog")
	}
	path := file.Name()
	_, writeErr := file.Write(body)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(path)
		return "", errors.New("cannot save the private Codex catalog")
	}
	return path, nil
}
