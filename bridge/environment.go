package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const originalEnvKey = "AGENT_HARBOR_BRIDGE_USER_PATHS"

var userPathKeys = []string{"HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_RUNTIME_DIR", "CODEX_HOME", "CODEX_SQLITE_HOME", "CLAUDE_CONFIG_DIR", "HERDR_CONFIG_PATH", "AGENT_HARBOR_HERDR_BIN"}

func managedEnvironment(root string) ([]string, string, error) {
	userPaths := map[string]*string{}
	for _, key := range userPathKeys {
		if value, ok := os.LookupEnv(key); ok {
			if !utf8.ValidString(value) {
				return nil, "", errors.New("original path environment contains invalid UTF-8")
			}
			userPaths[key] = &value
		} else {
			userPaths[key] = nil
		}
	}
	data, err := json.Marshal(userPaths)
	if err != nil {
		return nil, "", err
	}
	snapshot := base64.RawStdEncoding.EncodeToString(data)
	managed := filepath.Join(root, "herdr")
	updates := map[string]*string{}
	for _, name := range []string{"config", "state", "data", "cache", "runtime"} {
		path := filepath.Join(managed, name)
		if err := os.MkdirAll(path, 0700); err != nil {
			return nil, "", err
		}
		if name != "runtime" {
			updates["XDG_"+strings.ToUpper(name)+"_HOME"] = &path
		}
	}
	configContext, err := managedConfigContext(root)
	if err != nil {
		return nil, "", err
	}
	updates["XDG_CONFIG_HOME"] = &configContext
	runtimePath := filepath.Join(managed, "runtime")
	configPath := filepath.Join(managed, "config", "herdr.toml")
	updates["XDG_RUNTIME_DIR"] = &runtimePath
	updates["HERDR_CONFIG_PATH"] = &configPath
	updates["AGENT_HARBOR_BRIDGE_HOME"] = &root
	launchDir, err := os.Getwd()
	if err != nil {
		return nil, "", err
	}
	updates["AGENT_HARBOR_BRIDGE_LAUNCH_CWD"] = &launchDir
	updates[originalEnvKey] = &snapshot
	updates["HERDR_SESSION"] = nil
	updates["HERDR_SOCKET_PATH"] = nil
	return replaceEnvironment(os.Environ(), updates), snapshot, nil
}

func userEnvironment() ([]string, error) {
	snapshot, managed := os.LookupEnv(originalEnvKey)
	if !managed {
		return os.Environ(), nil
	}
	data, err := base64.RawStdEncoding.DecodeString(snapshot)
	if err != nil {
		return nil, errors.New("invalid managed environment")
	}
	var paths map[string]*string
	if err := json.Unmarshal(data, &paths); err != nil {
		return nil, errors.New("invalid managed environment")
	}
	updates := map[string]*string{originalEnvKey: nil}
	for _, key := range userPathKeys {
		value, ok := paths[key]
		if !ok {
			return nil, errors.New("managed environment is missing original paths")
		}
		updates[key] = value
	}
	return replaceEnvironment(os.Environ(), updates), nil
}

// Only the explicitly named path variables enter updates; all other variables
// pass directly to child processes without being copied into a value map.
func replaceEnvironment(original []string, updates map[string]*string) []string {
	var result []string
	for _, pair := range original {
		key, _, ok := strings.Cut(pair, "=")
		if _, changed := updates[key]; !ok || !changed {
			result = append(result, pair)
		}
	}
	for key, value := range updates {
		if value != nil {
			result = append(result, key+"="+*value)
		}
	}
	return result
}
