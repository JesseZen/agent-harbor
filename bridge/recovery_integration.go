package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

const missingConversationRecovery = "Conversation identity was not captured. Automatic recovery is unavailable; explicitly associate the original conversation before retrying."

// These commands operate in the Session's resolved client environment, never
// the selected UI Profile or the runner's ambient CODEX_HOME. Installation is
// Herdr's responsibility; Codex retains responsibility for hook trust.
func codexIntegrationStatus(herdr, dir string, env []string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, herdr, "integration", "status")
	cmd.Env, cmd.Dir = env, dir
	out, err := cmd.Output()
	if err == nil && len(out) <= 65536 {
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.ToLower(strings.TrimSpace(line))
			if strings.HasPrefix(line, "codex: ") {
				return strings.HasPrefix(line, "codex: current"), nil
			}
		}
	}
	return false, errors.New("could not inspect Herdr Codex identity integration in this Profile")
}

func codexHooksEnabled(home string) (*bool, error) {
	path := filepath.Join(home, "config.toml")
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, errors.New("Codex config is unreadable or unsafe to update; repair this Profile's config.toml")
	}
	var config struct {
		Features struct {
			Hooks *bool `toml:"hooks"`
		} `toml:"features"`
	}
	if _, err := toml.DecodeFile(path, &config); err != nil {
		return nil, errors.New("Codex config is invalid; repair this Profile's config.toml")
	}
	return config.Features.Hooks, nil
}

func prepareCodexRecovery(s session, e profileClientEnvironment, herdr string, env []string, in io.Reader, out io.Writer) error {
	if s.CLI != "codex" || s.Profile == nil || herdr == "" {
		return nil
	}
	if e.Binding != *s.Profile || e.Home == "" || environmentValue(env, "CODEX_HOME") != e.Home {
		return ErrProfileIdentity
	}
	current, err := codexIntegrationStatus(herdr, s.WorkingDirectory, env)
	if err != nil {
		return err
	}
	enabled, err := codexHooksEnabled(e.Home)
	if err != nil {
		return err
	}
	if current && enabled != nil && *enabled {
		return nil
	}
	// New isolated homes get the official integration as part of startup.
	// Existing configuration and an explicit hooks=false require a choice.
	if e.Kind != profileIsolated || enabled != nil && !*enabled {
		fmt.Fprintln(out, "Conversation recovery needs Herdr's Codex identity hooks in this Profile. Codex will ask you to trust them.")
		fmt.Fprint(out, "Type yes to prepare recovery, skip to open without automatic recovery, or Enter to cancel: ")
		choice, _ := bufio.NewReader(in).ReadString('\n')
		switch strings.TrimSpace(choice) {
		case "skip":
			fmt.Fprintln(out, "Opening without automatic recovery. No conversation identity will be inferred.")
			return nil
		case "yes":
		default:
			return errors.New("recovery setup cancelled; client was not started")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, herdr, "integration", "install", "codex")
	cmd.Env, cmd.Dir = env, s.WorkingDirectory
	// Upstream output may contain configuration paths; report only the outcome.
	if err := cmd.Run(); err != nil {
		return errors.New("Herdr could not prepare Codex identity hooks in this Profile; repair the integration before retrying")
	}
	current, err = codexIntegrationStatus(herdr, s.WorkingDirectory, env)
	enabled, configErr := codexHooksEnabled(e.Home)
	if err != nil || configErr != nil || !current || enabled == nil || !*enabled {
		return errors.New("Codex recovery integration is not ready in this Profile; client was not started")
	}
	fmt.Fprintln(out, "Recovery identity hooks prepared. Approve the hook in Codex when prompted.")
	return nil
}

func sessionAttention(s sessionView) string {
	if s.DiscoveryWarning != "" {
		return s.DiscoveryWarning
	}
	if s.Conversation == nil && s.Started {
		if s.Running {
			return "Conversation identity pending; approve the client identity hook if prompted."
		}
		return missingConversationRecovery
	}
	if s.Reason != "" {
		return s.Reason
	}
	if s.Recovery != "ready" && s.IdentityWarning != "" {
		return "Conversation identity needs review; inspect session details."
	}
	return ""
}
