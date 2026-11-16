package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/BurntSushi/toml"
)

// This is the user's per-session Codex profile, not Magpie's observed route.
// The fingerprint detects a change between review and the final launch.
type codexSelection struct {
	ThreadID    string `json:"thread_id"`
	Model       string `json:"model"`
	Effort      string `json:"effort,omitempty"`
	ProfileHash string `json:"profile_hash,omitempty"`
	Fallback    bool   `json:"fallback,omitempty"`
}

var errCodexSelectionConflict = errors.New("Codex session profile or identity conflicts")
var errCodexProfileMissing = errors.New("this session has no saved model profile")

func selectionTextValid(s string, max int) bool {
	return s != "" && len(s) <= max && utf8.ValidString(s) && strings.TrimSpace(s) == s && strings.IndexFunc(s, unicode.IsControl) < 0
}

func codexReference(s session) (string, error) {
	if s.CLI != "codex" || s.Mode != "magpie" || s.Conversation == nil || s.Conversation.Reference.Agent != "codex" || s.Conversation.Reference.Kind != "id" {
		return "", fmt.Errorf("%w: conversation identity unavailable", errCodexSelectionConflict)
	}
	id := s.Conversation.Reference.Value
	if !validCodexID(id) || s.CodexSessionID != "" && s.CodexSessionID != id {
		return "", fmt.Errorf("%w: conversation identity ambiguous", errCodexSelectionConflict)
	}
	return id, nil
}

// Read only the named profile Codex itself writes when the user chooses a
// default. A session-only /model change deliberately does not update it.
func readCodexSelection(_ context.Context, s session) (codexSelection, error) {
	id, err := codexReference(s)
	if err != nil {
		return codexSelection{}, err
	}
	environment, err := sessionClientEnvironment(s)
	env := environment.Environment
	if err != nil {
		return codexSelection{}, errors.New("Codex environment is unavailable for model review")
	}
	home, path, marker, err := magpieCodexProfilePath(s.ID, env)
	if err != nil {
		return codexSelection{}, err
	}
	if s.CodexHome != "" && filepath.Clean(home) != filepath.Clean(s.CodexHome) {
		return codexSelection{}, fmt.Errorf("%w: Codex home changed", errCodexSelectionConflict)
	}
	data, err := readOwnedCodexProfile(path, marker)
	if errors.Is(err, os.ErrNotExist) {
		return codexSelection{}, errCodexProfileMissing
	}
	if err != nil {
		return codexSelection{}, fmt.Errorf("%w: cannot read the session's model profile", errCodexSelectionConflict)
	}
	var config struct {
		Model  string `toml:"model"`
		Effort string `toml:"model_reasoning_effort"`
	}
	if _, err := toml.Decode(string(data), &config); err != nil || !selectionTextValid(config.Model, 512) || config.Effort != "" && !selectionTextValid(config.Effort, 32) {
		return codexSelection{}, fmt.Errorf("%w: profile model or reasoning effort is invalid", errCodexSelectionConflict)
	}
	hash := sha256.Sum256(data)
	return codexSelection{ThreadID: id, Model: config.Model, Effort: config.Effort, ProfileHash: hex.EncodeToString(hash[:])}, nil
}

func (harborService) CodexSelection(ctx context.Context, s session) (codexSelection, error) {
	return readCodexSelection(ctx, s)
}

func verifyCodexSelection(ctx context.Context, s session, reviewed *codexSelection) error {
	if s.Mode != "magpie" || s.CLI != "codex" || joinedCodex(s) {
		return nil
	}
	if reviewed == nil || !selectionTextValid(reviewed.Model, 512) {
		return errors.New("review the session's saved model before recovery")
	}
	id, err := codexReference(s)
	if err != nil || reviewed.ThreadID != id {
		return errors.New("conversation identity changed; review the model again")
	}
	current, readErr := readCodexSelection(ctx, s)
	if reviewed.Fallback {
		if reviewed.Model != s.MagpieModel || reviewed.Effort != "" || !errors.Is(readErr, errCodexProfileMissing) {
			return errors.New("Codex profile evidence changed; review recovery again")
		}
	} else if readErr != nil || current != *reviewed {
		return errors.New("Codex session profile changed or is unavailable; review recovery again")
	}
	return nil
}
