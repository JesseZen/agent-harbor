package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"time"
)

type profileCodexDefaults struct {
	Binding  profileBinding
	Profile  string
	Kind     profileKind
	Project  string
	Path     string
	Version  string
	Saved    codexOptions
	Resolved codexOptions
	Origins  map[string]codexLayerMetadata
}

func (h harborService) RememberCodexOptions(binding profileBinding, options codexOptions) error {
	if binding.Client != "codex" {
		return ErrProfileUnsupported
	}
	if err := options.validate(binding.Client, true); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return withProfileClientLock(ctx, binding, func() error {
		if _, err := h.ResolveProfileClient(profileClientRequest{binding}); err != nil {
			return err
		}
		return withProfiles(false, func(registry *profileRegistry) (bool, error) {
			if registry.WorkspaceID != binding.WorkspaceID {
				return false, ErrProfileIdentity
			}
			for profileIndex := range registry.Profiles {
				profile := &registry.Profiles[profileIndex]
				if profile.ID != binding.ProfileID || profile.WorkspaceID != binding.WorkspaceID {
					continue
				}
				for clientIndex := range profile.Clients {
					client := &profile.Clients[clientIndex]
					if client.Client == binding.Client && client.ID == binding.EnvironmentID {
						client.LastCodexOptions = options.saved()
						return true, nil
					}
				}
			}
			return false, ErrProfileIdentity
		})
	})
}

func readProfileCodexDefaults(rpc *codexConfigPipe, env profileClientEnvironment, project string) (profileCodexDefaults, error) {
	read, err := rpc.config(project)
	if err != nil {
		return profileCodexDefaults{}, err
	}
	path := filepath.Join(env.Home, "config.toml")
	for _, layer := range read.Layers {
		if layer.Name.Type == "user" && layer.Name.Profile == "" && layer.DisabledReason == "" && sameCodexConfigPath(layer.Name.File, path) && layer.Version != "" {
			return profileCodexDefaults{
				Binding: env.Binding, Profile: env.ProfileName, Kind: env.Kind, Project: project,
				Path: path, Version: layer.Version, Saved: codexNumberOptions(layer.Config),
				Resolved: codexNumberOptions(read.Config), Origins: read.Origins,
			}, nil
		}
	}
	return profileCodexDefaults{}, errors.New("Codex did not report the version of this Profile's user configuration")
}

func checkCodexUserConfigPath(path string) error {
	if info, err := os.Lstat(path); !os.IsNotExist(err) && (err != nil || !info.Mode().IsRegular()) {
		return errors.New("Profile config.toml is not a regular file; resolve the path before editing")
	}
	return nil
}

func (h harborService) ProfileCodexDefaults(binding profileBinding, project string) (profileCodexDefaults, error) {
	return h.withProfileCodexDefaults(binding, project, func(rpc *codexConfigPipe, env profileClientEnvironment, project string) (profileCodexDefaults, error) {
		return readProfileCodexDefaults(rpc, env, project)
	})
}

func (h harborService) SaveProfileCodexDefaults(review profileCodexDefaults, options codexOptions) (profileCodexDefaults, error) {
	if !reflect.DeepEqual(options, codexNumberOptionsFrom(options)) {
		return profileCodexDefaults{}, ErrProfileUnsupported
	}
	if err := options.validate("codex", false); err != nil {
		return profileCodexDefaults{}, err
	}
	return h.withProfileCodexDefaults(review.Binding, review.Project, func(rpc *codexConfigPipe, env profileClientEnvironment, project string) (profileCodexDefaults, error) {
		current, err := readProfileCodexDefaults(rpc, env, project)
		if err != nil {
			return profileCodexDefaults{}, err
		}
		if current.Version != review.Version {
			return profileCodexDefaults{}, errCodexConfigConflict
		}
		if current.Project != review.Project || current.Path != review.Path || current.Kind != review.Kind {
			return profileCodexDefaults{}, ErrProfileIdentity
		}
		var write struct{ FilePath, Version string }
		if err := rpc.call("config/batchWrite", map[string]any{"edits": codexNumberOverrides(options), "expectedVersion": review.Version}, &write); err != nil {
			return profileCodexDefaults{}, err
		}
		if !sameCodexConfigPath(write.FilePath, current.Path) {
			return profileCodexDefaults{}, errors.New("Codex reported writing another path; inspect this Profile before retrying")
		}
		updated, err := readProfileCodexDefaults(rpc, env, project)
		if err != nil {
			return profileCodexDefaults{}, fmt.Errorf("Codex saved the config, but verification failed: %w", err)
		}
		if updated.Version != write.Version || !reflect.DeepEqual(updated.Saved, options) {
			return profileCodexDefaults{}, errors.New("Codex config changed after save; refresh and inspect the saved values")
		}
		return updated, nil
	})
}

func codexNumberOptionsFrom(options codexOptions) codexOptions {
	return codexOptions{ContextWindow: options.ContextWindow, AutoCompactTokenLimit: options.AutoCompactTokenLimit, PostTurnCompactPercent: options.PostTurnCompactPercent}
}

func (h harborService) withProfileCodexDefaults(binding profileBinding, project string, work func(*codexConfigPipe, profileClientEnvironment, string) (profileCodexDefaults, error)) (profileCodexDefaults, error) {
	var defaults profileCodexDefaults
	if binding.Client != "codex" {
		return defaults, ErrProfileUnsupported
	}
	project, err := checkDirectory(project)
	if err != nil {
		return defaults, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	err = h.WithProfileClient(ctx, profileClientRequest{binding}, profileConfig, func(scope profileClientScope) error {
		env := scope.Environment
		if err := checkCodexUserConfigPath(filepath.Join(env.Home, "config.toml")); err != nil {
			return err
		}
		return withCodexConfigPipe(ctx, "", env, project, nil, func(rpc *codexConfigPipe) error {
			var err error
			defaults, err = work(rpc, env, project)
			return err
		})
	})
	return defaults, err
}
