package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

var (
	ErrProfileNotFound    = errors.New("profile not found")
	ErrProfileIdentity    = errors.New("profile or client environment identity changed; review the original environment")
	ErrProfileHome        = errors.New("profile home missing, unreadable, replaced or incompatible; repair it explicitly")
	ErrProfileAlias       = errors.New("profile client roots overlap or alias another environment")
	ErrProfileUnsupported = errors.New("client isolation is not supported")
	ErrProfileNotReady    = errors.New("profile client execution is not ready; runtime integration and isolation verification are pending")
)

type profileKind string

const (
	profileIsolated profileKind = "isolated"
	profileExisting profileKind = "existing-environment"
)

// WorkspaceID is the owning server workspace's opaque ID, never a hostname.
// EnvironmentID survives display-name changes; paths are resolved on the owner.
type profileBinding struct {
	WorkspaceID   string `json:"workspace_id"`
	ProfileID     string `json:"profile_id"`
	Client        string `json:"client"`
	EnvironmentID string `json:"environment_id"`
}

type profileClientHome struct {
	ID               string        `json:"id"`
	Client           string        `json:"client"`
	Home             string        `json:"home"`
	SQLite           string        `json:"sqlite"`
	Runtime          string        `json:"runtime,omitempty"`
	HomeIdentity     string        `json:"home_identity"`
	SQLiteIdentity   string        `json:"sqlite_identity"`
	RuntimeIdentity  string        `json:"runtime_identity,omitempty"`
	LastCodexOptions *codexOptions `json:"last_codex_options,omitempty"`
}

type profile struct {
	ID                string              `json:"id"`
	WorkspaceID       string              `json:"workspace_id"`
	Name              string              `json:"name"`
	Kind              profileKind         `json:"kind"`
	Clients           []profileClientHome `json:"clients"`
	OriginalPaths     map[string]*string  `json:"original_paths,omitempty"`
	DefaultInstanceID string              `json:"default_instance_id,omitempty"`
}

type profileCreateRequest struct {
	Name string
	Kind profileKind
	// Existing environments require explicit proven paths; no ambient defaults.
	ExistingHome   string
	ExistingSQLite string
}

type profileClientRequest struct {
	Binding profileBinding
}

type profileClientEnvironment struct {
	Binding               profileBinding
	ProfileName           string
	Kind                  profileKind
	Home, SQLite, Runtime string
	Environment           []string // ephemeral; never persist or log (may contain secrets)
	Arguments             []string // required upstream policy overrides, not a full command
	Executable            bool     // supported environment; launch still revalidates runtime/connection evidence
	Limitation            string
}

type profileClientOperation string

const (
	profileInspect         profileClientOperation = "inspect"
	profileAccountMutation profileClientOperation = "account-mutation"
	profileLaunch          profileClientOperation = "launch"
	profileConfig          profileClientOperation = "config"
)

type profileClientScope struct {
	Environment profileClientEnvironment
	Sessions    []session // all owned matching records, including stopped/pending ones
}

type profileRegistry struct {
	Version     int       `json:"version"`
	WorkspaceID string    `json:"workspace_id"`
	Profiles    []profile `json:"profiles"`
	SelectedID  string    `json:"selected_id,omitempty"`
}

func opaqueProfileID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(id[:]), nil
}

// Reads do not initialize a registry, create homes, or migrate old sessions.
func withProfiles(create bool, fn func(*profileRegistry) (bool, error)) error {
	root, err := bridgeHome()
	if err != nil {
		return err
	}
	if create {
		if err = ensureHome(root); err != nil {
			return err
		}
	}
	lock, err := os.Open(root)
	if err != nil {
		return fmt.Errorf("%w: workspace unavailable", ErrProfileHome)
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	path := filepath.Join(root, "profiles.json")
	data, err := os.ReadFile(path)
	r := profileRegistry{Version: 1, Profiles: []profile{}}
	if os.IsNotExist(err) {
		if !create {
			return ErrProfileNotFound
		}
		if _, e := os.Lstat(remoteContextPath(root)); e == nil {
			w, e := readRemoteWorkspace(root)
			if e != nil {
				return e
			}
			r.WorkspaceID = w.ID
		} else if !os.IsNotExist(e) {
			return e
		} else {
			r.WorkspaceID, err = opaqueProfileID()
			if err != nil {
				return err
			}
		}
	} else if err != nil {
		return err
	} else if json.Unmarshal(data, &r) != nil || r.Version != 1 || !validID(r.WorkspaceID) {
		return ErrProfileIdentity
	}
	if _, e := os.Lstat(remoteContextPath(root)); e == nil {
		w, e := readRemoteWorkspace(root)
		if e != nil {
			return e
		}
		if r.WorkspaceID != w.ID {
			return ErrProfileIdentity
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	changed, err := fn(&r)
	if err != nil || !changed {
		return err
	}
	data, err = json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return writeStateFile(path, append(data, '\n'))
}

// Canonical directories only: even a symlink in an ancestor is rejected.
func profileDirectory(path string, private bool) (string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", ErrProfileHome
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil || canonical != path {
		return "", ErrProfileHome
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() {
		return "", ErrProfileHome
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Getuid()) || info.Mode().Perm()&0500 != 0500 || private && info.Mode().Perm() != 0700 {
		return "", ErrProfileHome
	}
	f, err := os.Open(path)
	if err != nil {
		return "", ErrProfileHome
	}
	defer f.Close()
	if _, err = f.Readdirnames(1); err != nil && !errors.Is(err, io.EOF) {
		return "", ErrProfileHome
	}
	return fmt.Sprintf("%d:%d", stat.Dev, stat.Ino), nil
}

func profilePathsOverlap(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+string(os.PathSeparator)) || strings.HasPrefix(b, a+string(os.PathSeparator))
}

func (harborService) Profiles() ([]profile, error) {
	var result []profile
	err := withProfiles(false, func(r *profileRegistry) (bool, error) { result = append([]profile{}, r.Profiles...); return false, nil })
	if errors.Is(err, ErrProfileNotFound) {
		return []profile{}, nil
	}
	return result, err
}

func (harborService) CreateProfile(req profileCreateRequest) (profile, error) {
	var result profile
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 120 || strings.ContainsAny(name, "\r\n\x00") {
		return result, errors.New("invalid profile name")
	}
	if req.Kind != profileIsolated && req.Kind != profileExisting {
		return result, ErrProfileUnsupported
	}
	err := withProfiles(true, func(r *profileRegistry) (bool, error) {
		id, err := opaqueProfileID()
		if err != nil {
			return false, err
		}
		envID, err := opaqueProfileID()
		if err != nil {
			return false, err
		}
		root, _ := bridgeHome()
		h := profileClientHome{ID: envID, Client: "codex"}
		if req.Kind == profileIsolated {
			if req.ExistingHome != "" || req.ExistingSQLite != "" {
				return false, ErrProfileIdentity
			}
			base := filepath.Join(root, "profiles", id)
			h.Home, h.SQLite, h.Runtime = filepath.Join(base, "codex"), filepath.Join(base, "sqlite"), filepath.Join(base, "runtime")
			for _, dir := range []string{filepath.Join(root, "profiles"), base, h.Home, h.SQLite, h.Runtime} {
				if _, err := profileDirectory(filepath.Dir(dir), true); err != nil {
					return false, err
				}
				if err := os.Mkdir(dir, 0700); err != nil && !os.IsExist(err) {
					return false, err
				}
				if _, err := profileDirectory(dir, true); err != nil {
					return false, err
				}
			}
		} else {
			h.Home, h.SQLite = req.ExistingHome, req.ExistingSQLite
		}
		h.HomeIdentity, err = profileDirectory(h.Home, req.Kind == profileIsolated)
		if err != nil {
			return false, err
		}
		h.SQLiteIdentity, err = profileDirectory(h.SQLite, req.Kind == profileIsolated)
		if err != nil {
			return false, err
		}
		if h.Runtime != "" {
			h.RuntimeIdentity, err = profileDirectory(h.Runtime, true)
			if err != nil {
				return false, err
			}
		}
		for _, other := range r.Profiles {
			for _, old := range other.Clients {
				for _, a := range []string{h.Home, h.SQLite, h.Runtime} {
					for _, b := range []string{old.Home, old.SQLite, old.Runtime} {
						if a != "" && b != "" && profilePathsOverlap(a, b) {
							return false, ErrProfileAlias
						}
					}
				}
			}
		}
		result = profile{ID: id, WorkspaceID: r.WorkspaceID, Name: name, Kind: req.Kind, Clients: []profileClientHome{h}}
		if req.Kind == profileExisting {
			env, err := userEnvironment()
			if err != nil {
				return false, err
			}
			result.OriginalPaths = map[string]*string{}
			for _, key := range userPathKeys {
				result.OriginalPaths[key] = nil
				for _, pair := range env {
					k, v, ok := strings.Cut(pair, "=")
					if ok && k == key {
						copy := v
						result.OriginalPaths[key] = &copy
					}
				}
			}
		}

		r.Profiles = append(r.Profiles, result)
		return true, nil
	})
	return result, err
}

func (harborService) RenameProfile(binding profileBinding, name string) error {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 120 || strings.ContainsAny(name, "\r\n\x00") {
		return errors.New("invalid profile name")
	}
	return withProfiles(false, func(r *profileRegistry) (bool, error) {
		if binding.WorkspaceID != r.WorkspaceID {
			return false, ErrProfileIdentity
		}
		for i := range r.Profiles {
			if r.Profiles[i].ID == binding.ProfileID {
				r.Profiles[i].Name = name
				return true, nil
			}
		}
		return false, ErrProfileNotFound
	})
}

func resolveProfileRecord(binding profileBinding) (profile, profileClientHome, error) {
	var p profile
	var h profileClientHome
	err := withProfiles(false, func(r *profileRegistry) (bool, error) {
		if !selectionTextValid(binding.Client, 100) || !validID(binding.ProfileID) || !validID(binding.EnvironmentID) || binding.WorkspaceID != r.WorkspaceID {
			return false, ErrProfileIdentity
		}
		for _, candidate := range r.Profiles {
			if candidate.ID == binding.ProfileID {
				if candidate.WorkspaceID != r.WorkspaceID {
					return false, ErrProfileIdentity
				}
				p = candidate
				for _, home := range p.Clients {
					if home.ID == binding.EnvironmentID && (home.Client == binding.Client || p.Kind == profileExisting) {
						h = home
						h.Client = binding.Client
						return false, nil
					}
				}
				return false, ErrProfileIdentity
			}
		}
		return false, ErrProfileNotFound
	})
	return p, h, err
}

func (harborService) ResolveProfileClient(req profileClientRequest) (profileClientEnvironment, error) {
	p, h, err := resolveProfileRecord(req.Binding)
	if err != nil {
		return profileClientEnvironment{}, err
	}
	if p.Kind != profileIsolated && p.Kind != profileExisting {
		return profileClientEnvironment{}, ErrProfileIdentity
	}
	if h.Home == "" || h.SQLite == "" || h.HomeIdentity == "" || h.SQLiteIdentity == "" {
		return profileClientEnvironment{}, ErrProfileIdentity
	}

	if h.Client != "codex" && p.Kind != profileExisting {
		return profileClientEnvironment{}, ErrProfileUnsupported
	}
	if p.Kind == profileIsolated {
		if err := validateProfilePolicy(h); err != nil {
			return profileClientEnvironment{}, err
		}
	}
	// Recheck aliases against every registered environment on every resolution.
	profiles, err := (harborService{}).Profiles()
	if err != nil {
		return profileClientEnvironment{}, err
	}
	for _, other := range profiles {
		for _, old := range other.Clients {
			if other.ID == p.ID && old.ID == h.ID {
				continue
			}
			for _, a := range []string{h.Home, h.SQLite, h.Runtime} {
				for _, b := range []string{old.Home, old.SQLite, old.Runtime} {
					if a != "" && b != "" && profilePathsOverlap(a, b) {
						return profileClientEnvironment{}, ErrProfileAlias
					}
				}
			}
		}
	}
	if p.Kind == profileIsolated {
		root, err := bridgeHome()
		if err != nil {
			return profileClientEnvironment{}, err
		}
		base := filepath.Join(root, "profiles", p.ID)
		if h.Home != filepath.Join(base, "codex") || h.SQLite != filepath.Join(base, "sqlite") || h.Runtime != filepath.Join(base, "runtime") || h.RuntimeIdentity == "" {
			return profileClientEnvironment{}, ErrProfileIdentity
		}
	}
	for _, pair := range [][2]string{{h.Home, h.HomeIdentity}, {h.SQLite, h.SQLiteIdentity}, {h.Runtime, h.RuntimeIdentity}} {
		if pair[0] == "" && pair[1] == "" {
			continue
		}
		identity, err := profileDirectory(pair[0], p.Kind == profileIsolated)
		if err != nil {
			return profileClientEnvironment{}, err
		}
		if identity != pair[1] {
			return profileClientEnvironment{}, ErrProfileIdentity
		}
	}
	env, err := userEnvironment()
	if err != nil {
		return profileClientEnvironment{}, err
	}
	updates := map[string]*string{}
	if p.Kind == profileExisting {
		for k, v := range p.OriginalPaths {
			updates[k] = v
		}
	}
	updates["CODEX_HOME"], updates["CODEX_SQLITE_HOME"] = &h.Home, &h.SQLite
	args := []string{}
	if p.Kind == profileIsolated {
		// Preserve HOME, PATH, SSH/Git/tooling. No inherited Codex/auth selector.
		for _, pair := range env {
			key, _, _ := strings.Cut(pair, "=")
			if strings.HasPrefix(key, "CODEX_") || strings.HasPrefix(key, "OPENAI_") || strings.HasPrefix(key, "CHATGPT_") || key == "AGENT_HARBOR_MAGPIE_KEY" {
				updates[key] = nil
			}
		}
		updates["CODEX_HOME"], updates["CODEX_SQLITE_HOME"] = &h.Home, &h.SQLite
		updates["TMPDIR"], updates["XDG_RUNTIME_DIR"] = &h.Runtime, &h.Runtime
		args = []string{"--no-daemon", "-c", "cli_auth_credentials_store=\"file\"", "-c", "mcp_oauth_credentials_store=\"file\"", "-c", "sqlite_home=" + strconv.Quote(h.SQLite), "-c", "log_dir=" + strconv.Quote(filepath.Join(h.Home, "log"))}
	}
	return profileClientEnvironment{Binding: req.Binding, ProfileName: p.Name, Kind: p.Kind, Home: h.Home, SQLite: h.SQLite, Runtime: h.Runtime, Environment: replaceEnvironment(env, updates), Arguments: args, Executable: true}, nil
}

func (h harborService) ProfileSessions(req profileClientRequest) ([]session, error) {
	if _, err := h.ResolveProfileClient(req); err != nil {
		return nil, err
	}
	result := []session{}
	err := withSessions(func(all []session) ([]session, error) {
		for _, s := range all {
			if s.Profile != nil && *s.Profile == req.Binding && s.CLI == req.Binding.Client {
				result = append(result, s)
			}
		}
		return nil, nil
	})
	return result, err
}

// One cross-process lock per workspace/profile/client environment. Future launch
// integration must hold it through child start and persisted ownership evidence.
func withProfileClientLock(ctx context.Context, b profileBinding, fn func() error) error {
	release, err := acquireProfileClient(ctx, b)
	if err != nil {
		return err
	}
	defer release()
	return fn()
}

func (h harborService) WithProfileClient(ctx context.Context, req profileClientRequest, operation profileClientOperation, fn func(profileClientScope) error) error {
	if operation != profileInspect && operation != profileAccountMutation && operation != profileLaunch && operation != profileConfig {
		return ErrProfileUnsupported
	}
	if _, _, err := resolveProfileRecord(req.Binding); err != nil {
		return err
	}
	return withProfileClientLock(ctx, req.Binding, func() error {
		env, err := h.ResolveProfileClient(req)
		if err != nil {
			return err
		}
		if operation != profileInspect && !env.Executable {
			return ErrProfileNotReady
		}
		sessions, err := h.ProfileSessions(req)
		if err != nil {
			return err
		}
		if fn == nil {
			return errors.New("profile operation callback is required")
		}
		return fn(profileClientScope{Environment: env, Sessions: sessions})
	})
}

// UI checks this before applying an async result. Consequential actions also
// resolve the captured binding under WithProfileClient, never the visible row.
func (h harborService) ValidateProfileResult(captured, visible profileBinding) error {
	if captured != visible {
		return ErrProfileIdentity
	}
	_, err := h.ResolveProfileClient(profileClientRequest{Binding: captured})
	return err
}
