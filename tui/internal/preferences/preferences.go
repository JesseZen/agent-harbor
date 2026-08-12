package preferences

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Prefs holds local TUI UI preferences (not Core config).
type Prefs struct {
	Locale string `json:"locale,omitempty"`
	Theme  string `json:"theme,omitempty"`
}

func configPath() (string, error) {
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		configHome = filepath.Join(home, ".config")
	}
	return filepath.Join(configHome, "agent-harbor", "tui-preferences.json"), nil
}

// Load reads preferences from the XDG config path. Missing file yields empty Prefs.
func Load() (Prefs, error) {
	path, err := configPath()
	if err != nil {
		return Prefs{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Prefs{}, nil
		}
		return Prefs{}, err
	}
	var prefs Prefs
	if err := json.Unmarshal(data, &prefs); err != nil {
		return Prefs{}, err
	}
	return prefs, nil
}

// Save writes preferences atomically-ish via temp rename.
func Save(prefs Prefs) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
