package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

func bindingOf(p profile, client string) (profileBinding, error) {
	for _, c := range p.Clients {
		if c.Client == client {
			return profileBinding{WorkspaceID: p.WorkspaceID, ProfileID: p.ID, Client: client, EnvironmentID: c.ID}, nil
		}
	}
	if p.Kind == profileExisting && len(p.Clients) > 0 && client != "" {
		c := p.Clients[0]
		return profileBinding{WorkspaceID: p.WorkspaceID, ProfileID: p.ID, Client: client, EnvironmentID: c.ID}, nil
	}
	return profileBinding{}, ErrProfileUnsupported
}

// Default initialization is an explicit application effect, never a side effect
// of ResolveProfileClient or recovery. An upgrade keeps its original data.
func (h harborService) EnsureProfiles() (profile, error) {
	root, err := bridgeHome()
	if err != nil {
		return profile{}, err
	}
	if err := ensureHome(root); err != nil {
		return profile{}, err
	}
	initLock, err := os.OpenFile(filepath.Join(root, "profiles-init.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return profile{}, err
	}
	defer initLock.Close()
	if err := syscall.Flock(int(initLock.Fd()), syscall.LOCK_EX); err != nil {
		return profile{}, err
	}
	defer syscall.Flock(int(initLock.Fd()), syscall.LOCK_UN)

	profiles, err := h.Profiles()
	if err != nil && !errors.Is(err, ErrProfileHome) {
		return profile{}, err
	}
	if len(profiles) == 0 {
		env, err := userEnvironment()
		if err != nil {
			return profile{}, err
		}
		home := environmentValue(env, "CODEX_HOME")
		if home == "" {
			base := environmentValue(env, "HOME")
			if !filepath.IsAbs(base) {
				return profile{}, ErrProfileHome
			}
			home = filepath.Join(base, ".codex")
		}
		sqlite := environmentValue(env, "CODEX_SQLITE_HOME")
		if sqlite == "" {
			sqlite = home
		}
		var records []session
		if err := withSessions(func(all []session) ([]session, error) { records = all; return nil, nil }); err != nil {
			return profile{}, err
		}
		entries, readErr := os.ReadDir(home)
		req := profileCreateRequest{Name: "Default", Kind: profileIsolated}
		if len(records) > 0 || len(entries) > 0 || readErr != nil && !os.IsNotExist(readErr) {
			req = profileCreateRequest{Name: "Existing environment", Kind: profileExisting, ExistingHome: home, ExistingSQLite: sqlite}
		}
		p, err := h.CreateProfile(req)
		if err != nil {
			return profile{}, err
		}
		b, err := bindingOf(p, "codex")
		if err != nil {
			return profile{}, err
		}
		if err = h.SelectProfile(b); err != nil {
			return profile{}, err
		}
		return p, nil
	}
	selected := profiles[0]
	err = withProfiles(false, func(r *profileRegistry) (bool, error) {
		if r.SelectedID == "" {
			return false, nil
		}
		for _, p := range r.Profiles {
			if p.ID == r.SelectedID {
				selected = p
				return false, nil
			}
		}
		return false, ErrProfileIdentity
	})
	return selected, err
}

func (harborService) SelectProfile(b profileBinding) error {
	if _, _, err := resolveProfileRecord(b); err != nil {
		return err
	}
	return withProfiles(false, func(r *profileRegistry) (bool, error) {
		if r.WorkspaceID != b.WorkspaceID {
			return false, ErrProfileIdentity
		}
		for _, p := range r.Profiles {
			if p.ID == b.ProfileID {
				r.SelectedID = p.ID
				return true, nil
			}
		}
		return false, ErrProfileNotFound
	})
}

func (harborService) SetProfileDefaultInstance(b profileBinding, id string) error {
	if _, _, err := resolveProfileRecord(b); err != nil {
		return err
	}
	if id != "" {
		registry, err := loadMagpieRegistry()
		if err != nil {
			return err
		}
		instance, ok := registry.find(id)
		if !ok {
			return errors.New("Magpie instance is not registered")
		}
		probe := probeGateway(instance.Address)
		if probe.State != gatewayVerified || instance.health(probe).needsAttention() {
			return errors.New("verify the selected Magpie instance before using it as a Profile default")
		}
	}
	return withProfiles(false, func(r *profileRegistry) (bool, error) {
		if r.WorkspaceID != b.WorkspaceID {
			return false, ErrProfileIdentity
		}
		for i := range r.Profiles {
			if r.Profiles[i].ID == b.ProfileID {
				r.Profiles[i].DefaultInstanceID = id
				return true, nil
			}
		}
		return false, ErrProfileNotFound
	})
}

func (harborService) RememberMagpieSelection(b profileBinding, instanceID, modelID string) error {
	if !selectionTextValid(instanceID, 512) || !selectionTextValid(modelID, 512) {
		return errors.New("invalid Magpie selection")
	}
	if _, _, err := resolveProfileRecord(b); err != nil {
		return err
	}
	return withProfiles(false, func(r *profileRegistry) (bool, error) {
		if r.WorkspaceID != b.WorkspaceID {
			return false, ErrProfileIdentity
		}
		for i := range r.Profiles {
			if r.Profiles[i].ID != b.ProfileID {
				continue
			}
			for j := range r.Profiles[i].Clients {
				client := &r.Profiles[i].Clients[j]
				if client.ID == b.EnvironmentID && (client.Client == b.Client || r.Profiles[i].Kind == profileExisting) {
					client.LastMagpieInstanceID = instanceID
					client.LastMagpieModelID = modelID
					return true, nil
				}
			}
			return false, ErrProfileIdentity
		}
		return false, ErrProfileNotFound
	})
}

// Explicit adoption records the user's reviewed original environment. It never
// moves client data or changes a Conversation/connection. Running records stay put.
func (h harborService) ReviewSessionProfile(a sessionApproval, b profileBinding) error {
	if !a.Confirmed {
		return errors.New("Profile review cancelled")
	}
	e, err := h.ResolveProfileClient(profileClientRequest{Binding: b})
	if err != nil {
		return err
	}
	if e.Kind != profileExisting || b.Client != a.Session.CLI {
		return ErrProfileIdentity
	}
	if a.Session.Profile != nil {
		return errors.New("session already has a Profile; moving Conversations is unsupported")
	}
	if a.Session.CodexHome != "" && a.Session.CodexHome != e.Home {
		return errors.New("selected environment differs from the recorded original client home")
	}
	return withProfileClientLock(context.Background(), b, func() error {
		return withSessions(func(all []session) ([]session, error) {
			for i := range all {
				if all[i].ID == a.Session.ID {
					if !sameSession(all[i], a.Session) {
						return nil, errors.New("session changed; review its original environment again")
					}
					if processGroupActive(all[i].ProcessGroup) || sessionLaunchPending(all[i]) {
						return nil, errors.New("stop the owned session before reviewing its Profile binding")
					}
					copy := b
					all[i].Profile = &copy
					return all, nil
				}
			}
			return nil, errors.New("session not found")
		})
	})
}
