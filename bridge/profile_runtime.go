package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/BurntSushi/toml"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"syscall"
	"time"
)

// Nil is legacy evidence, never the visible Profile. Once a registry exists,
// review must establish its original environment before another client starts.
func sessionClientEnvironment(s session) (profileClientEnvironment, error) {
	if s.Profile != nil {
		if s.CLI != s.Profile.Client {
			return profileClientEnvironment{}, ErrProfileIdentity
		}
		return (harborService{}).ResolveProfileClient(profileClientRequest{Binding: *s.Profile})
	}
	profiles, err := (harborService{}).Profiles()
	if err != nil && !errors.Is(err, ErrProfileHome) {
		return profileClientEnvironment{}, err
	}
	if len(profiles) > 0 {
		return profileClientEnvironment{}, errors.New("legacy session has no proven Profile binding; explicitly review its original environment")
	}
	env, err := userEnvironment()
	return profileClientEnvironment{Environment: env, Executable: err == nil}, err
}

func profileProcessMatches(s session, facts processFacts) bool {
	if s.Profile == nil {
		return true
	}
	b := s.Profile
	return facts.Variables["AH_PROFILE_WORKSPACE"] == b.WorkspaceID && facts.Variables["AH_PROFILE_ID"] == b.ProfileID && facts.Variables["AH_PROFILE_ENVIRONMENT"] == b.EnvironmentID && (s.CLI != "codex" || facts.Variables["CODEX_HOME"] == s.CodexHome)
}

func profileProcessEnvironment(env []string, b *profileBinding) []string {
	if b == nil {
		return env
	}
	return replaceEnvironment(env, map[string]*string{"AH_PROFILE_WORKSPACE": &b.WorkspaceID, "AH_PROFILE_ID": &b.ProfileID, "AH_PROFILE_ENVIRONMENT": &b.EnvironmentID})
}

func acquireProfileClient(ctx context.Context, b profileBinding) (func(), error) {
	if !validID(b.ProfileID) || !validID(b.EnvironmentID) {
		return nil, ErrProfileIdentity
	}
	root, err := bridgeHome()
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(root, "profile-client-"+b.ProfileID+"-"+b.EnvironmentID+fmt.Sprintf("-%x", sha256.Sum256([]byte(b.Client)))+".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	for {
		if err := ctx.Err(); err != nil {
			f.Close()
			return nil, err
		}
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}

func profilePolicyArguments(e profileClientEnvironment) []string {
	// --no-daemon is a TUI flag, not an App Server subcommand flag.
	result := []string{}
	for _, a := range e.Arguments {
		if a != "--no-daemon" {
			result = append(result, a)
		}
	}
	return result
}

func sameProfileBinding(a, b *profileBinding) bool { return reflect.DeepEqual(a, b) }

func profileLabel(b *profileBinding) string {
	if b == nil {
		return "Existing environment · review required"
	}
	p, _, err := resolveProfileRecord(*b)
	if err != nil {
		return "Unavailable Profile · " + b.ProfileID
	}
	if p.Kind == profileExisting {
		return p.Name + " · shared existing environment"
	}
	return p.Name
}

func validateProfileLaunch(s session) error {
	e, err := sessionClientEnvironment(s)
	if err != nil {
		return err
	}
	if !e.Executable {
		return ErrProfileNotReady
	}
	if s.Profile != nil && s.Mode == "magpie" && e.Kind == profileIsolated {
		if err := requireProfileSafeGateway(s.MagpieURL); err != nil {
			return err
		}
	}
	return nil
}

// Per-session endpoint names stay private and short enough for Unix sockets.
func profileSocketParent(env []string) string { return environmentValue(env, "TMPDIR") }

func profileBindingText(b *profileBinding) string {
	if b == nil {
		return "unbound"
	}
	return b.WorkspaceID + ":" + b.ProfileID + ":" + b.EnvironmentID + ":" + strconv.Quote(b.Client)
}

// Account tools may invoke Codex login without CLI overrides. Refuse config
// that selects another credential backend or database root before yielding scope.
func validateProfilePolicy(home profileClientHome) error {
	path := filepath.Join(home.Home, "config.toml")
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return ErrProfileHome
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return ErrProfileHome
	}
	var config struct {
		Auth   string `toml:"cli_auth_credentials_store"`
		MCP    string `toml:"mcp_oauth_credentials_store"`
		SQLite string `toml:"sqlite_home"`
		Log    string `toml:"log_dir"`
	}
	if _, err := toml.DecodeFile(path, &config); err != nil {
		return ErrProfileHome
	}
	if config.Auth != "" && config.Auth != "file" || config.MCP != "" && config.MCP != "file" {
		return errors.New("isolated Profile requires file credential backends; repair its Codex configuration")
	}
	if config.SQLite != "" && config.SQLite != home.SQLite || config.Log != "" && config.Log != filepath.Join(home.Home, "log") {
		return errors.New("isolated Profile configuration redirects its database or logs; repair the Profile configuration")
	}
	return nil
}
