package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (h harborService) InspectExtensions(ctx context.Context, binding profileBinding) (extensionSnapshot, error) {
	var snap extensionSnapshot
	err := h.WithProfileClient(ctx, profileClientRequest{Binding: binding}, profileInspect, func(scope profileClientScope) error {
		env := scope.Environment
		mode, cliPath := resolveSkillsCLI(env.Environment)
		snap = extensionSnapshot{
			Profile:       env.Binding,
			ProfileName:   env.ProfileName,
			CodexHome:     env.Home,
			SkillsCLI:     mode,
			SkillsCLIPath: cliPath,
		}
		userHome := environmentValue(env.Environment, "HOME")
		skills, warnings := scanLocalSkills(userHome, env.Home)
		snap.Warnings = append(snap.Warnings, warnings...)

		// Enrich with Codex App Server skills/list when native Codex is available.
		if rpcSkills, rpcErr := fetchCodexRPCSkills(ctx, env); rpcErr == nil && len(rpcSkills) > 0 {
			skills = mergeSkillItems(skills, rpcSkills)
		}
		snap.Skills = skills

		// Query Codex plugins & marketplaces in this Profile's environment.
		plugins, pluginWarn := fetchCodexPlugins(ctx, env)
		if pluginWarn != "" {
			snap.Warnings = append(snap.Warnings, pluginWarn)
		}
		snap.Plugins = plugins

		marketplaces, _ := fetchCodexMarketplaces(ctx, env)
		snap.Marketplaces = marketplaces
		return nil
	})
	return snap, err
}

func fetchCodexRPCSkills(ctx context.Context, env profileClientEnvironment) ([]extensionItem, error) {
	codex, err := profileAccountCodex(env)
	if err != nil {
		return nil, err
	}
	var out []extensionItem
	err = withCodexConfigPipe(ctx, codex, env, env.Home, nil, func(rpc *codexConfigPipe) error {
		var raw json.RawMessage
		if err := rpc.call("skills/list", map[string]any{}, &raw); err != nil {
			return err
		}
		out = parseRPCSkillsResponse(raw, environmentValue(env.Environment, "HOME"), env.Home)
		return nil
	})
	return out, err
}

func parseRPCSkillsResponse(raw json.RawMessage, userHome, codexHome string) []extensionItem {
	type rpcSkill struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Path        string `json:"path"`
		Scope       string `json:"scope"`
		Enabled     *bool  `json:"enabled"`
	}
	var direct []rpcSkill
	var wrapped struct {
		Data []struct {
			Skills []rpcSkill `json:"skills"`
		} `json:"data"`
		Skills []rpcSkill `json:"skills"`
	}
	if json.Unmarshal(raw, &wrapped) == nil {
		if len(wrapped.Skills) > 0 {
			direct = wrapped.Skills
		}
		for _, entry := range wrapped.Data {
			direct = append(direct, entry.Skills...)
		}
	}
	if len(direct) == 0 {
		_ = json.Unmarshal(raw, &direct)
	}
	sharedRoot := ""
	if userHome != "" {
		sharedRoot = canonicalSkillPath(filepath.Join(userHome, ".agents", "skills"))
	}
	items := make([]extensionItem, 0, len(direct))
	for _, s := range direct {
		if s.Name == "" && s.Path == "" {
			continue
		}
		canonical := canonicalSkillPath(s.Path)
		enabled := true
		if s.Enabled != nil {
			enabled = *s.Enabled
		}
		scope := s.Scope
		source := scope
		if sharedRoot != "" && canonical != "" && strings.HasPrefix(canonical, sharedRoot+string(filepath.Separator)) {
			scope = "global-shared"
			source = "shared (~/.agents/skills)"
		} else if codexHome != "" && canonical != "" && strings.HasPrefix(canonical, canonicalSkillPath(codexHome)+string(filepath.Separator)) {
			scope = "profile"
			source = "profile ($CODEX_HOME/skills)"
		} else if source == "" {
			source = "codex"
		}
		id := canonical
		if id == "" {
			id = s.Name
		}
		items = append(items, extensionItem{
			Kind:        extSkill,
			ID:          id,
			Name:        cleanUI(s.Name),
			Description: cleanUI(s.Description),
			Source:      source,
			Path:        s.Path,
			Enabled:     enabled,
			Toggleable:  s.Path != "",
			Scope:       scope,
			Clients:     []string{"codex"},
		})
	}
	return items
}

func mergeSkillItems(local, rpc []extensionItem) []extensionItem {
	byID := make(map[string]extensionItem, len(local)+len(rpc))
	for _, item := range local {
		byID[item.ID] = item
	}
	for _, item := range rpc {
		if prev, ok := byID[item.ID]; ok {
			prev.Enabled = item.Enabled
			if prev.Description == "" {
				prev.Description = item.Description
			}
			if prev.Name == "" {
				prev.Name = item.Name
			}
			byID[item.ID] = prev
		} else {
			byID[item.ID] = item
		}
	}
	out := make([]extensionItem, 0, len(byID))
	for _, item := range byID {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

func fetchCodexPlugins(ctx context.Context, env profileClientEnvironment) ([]extensionItem, string) {
	codex, err := profileAccountCodex(env)
	if err != nil {
		return nil, "Codex CLI unavailable for plugin inspection."
	}
	runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	args := append(profilePolicyArguments(env), "plugin", "list", "--json")
	cmd := exec.CommandContext(runCtx, codex, args...)
	cmd.Env, cmd.Dir = env.Environment, env.Home
	data, err := cmd.Output()
	if err != nil {
		return nil, "Could not list Codex plugins in this Profile."
	}
	return parseCodexPluginsJSON(data), ""
}

func parseCodexPluginsJSON(data []byte) []extensionItem {
	type rawPlugin struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Marketplace string `json:"marketplace"`
		Source      string `json:"source"`
		Path        string `json:"path"`
		Enabled     *bool  `json:"enabled"`
	}
	var payload struct {
		Installed []rawPlugin `json:"installed"`
		Plugins   []rawPlugin `json:"plugins"`
	}
	if json.Unmarshal(data, &payload) != nil {
		return nil
	}
	rawList := payload.Installed
	if len(rawList) == 0 {
		rawList = payload.Plugins
	}
	items := make([]extensionItem, 0, len(rawList))
	for _, p := range rawList {
		name := cleanUI(p.Name)
		id := cleanUI(p.ID)
		if id == "" {
			id = name
			if p.Marketplace != "" {
				id = name + "@" + cleanUI(p.Marketplace)
			}
		}
		if name == "" {
			name = id
		}
		if id == "" {
			continue
		}
		enabled := true
		if p.Enabled != nil {
			enabled = *p.Enabled
		}
		src := cleanUI(p.Source)
		if src == "" && p.Marketplace != "" {
			src = "marketplace: " + cleanUI(p.Marketplace)
		}
		if src == "" {
			src = "profile ($CODEX_HOME/plugins)"
		}
		items = append(items, extensionItem{
			Kind:        extPlugin,
			ID:          id,
			Name:        name,
			Description: cleanUI(p.Description),
			Source:      src,
			Path:        p.Path,
			Enabled:     enabled,
			Toggleable:  false,
			Scope:       "profile",
			Clients:     []string{"codex"},
			Marketplace: cleanUI(p.Marketplace),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
	return items
}

func fetchCodexMarketplaces(ctx context.Context, env profileClientEnvironment) ([]extensionMarketplace, error) {
	codex, err := profileAccountCodex(env)
	if err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	args := append(profilePolicyArguments(env), "plugin", "marketplace", "list", "--json")
	cmd := exec.CommandContext(runCtx, codex, args...)
	cmd.Env, cmd.Dir = env.Environment, env.Home
	data, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return parseCodexMarketplacesJSON(data), nil
}

func parseCodexMarketplacesJSON(data []byte) []extensionMarketplace {
	var payload struct {
		Marketplaces []struct {
			Name   string `json:"name"`
			Source string `json:"source"`
			URL    string `json:"url"`
			Root   string `json:"root"`
			Path   string `json:"path"`
		} `json:"marketplaces"`
	}
	if json.Unmarshal(data, &payload) != nil {
		return nil
	}
	out := make([]extensionMarketplace, 0, len(payload.Marketplaces))
	for _, m := range payload.Marketplaces {
		src := cleanUI(m.Source)
		if src == "" {
			src = cleanUI(m.URL)
		}
		root := cleanUI(m.Root)
		if root == "" {
			root = cleanUI(m.Path)
		}
		if m.Name == "" && src == "" {
			continue
		}
		out = append(out, extensionMarketplace{Name: cleanUI(m.Name), Source: src, Root: root})
	}
	return out
}

func (h harborService) ToggleExtension(ctx context.Context, binding profileBinding, item extensionItem, enabled bool) error {
	if item.Kind != extSkill {
		return errors.New("Codex CLI does not support per-plugin enable/disable; use remove/add instead")
	}
	if item.Path == "" {
		return errors.New("skill path is unavailable; cannot update Profile config")
	}
	return h.WithProfileClient(ctx, profileClientRequest{Binding: binding}, profileConfig, func(scope profileClientScope) error {
		env := scope.Environment
		if codex, err := profileAccountCodex(env); err == nil {
			rpcErr := withCodexConfigPipe(ctx, codex, env, env.Home, nil, func(rpc *codexConfigPipe) error {
				var resp json.RawMessage
				return rpc.call("skills/config/write", map[string]any{
					"path":    item.Path,
					"enabled": enabled,
				}, &resp)
			})
			if rpcErr == nil {
				return nil
			}
		}
		if err := writeCodexSkillConfigFallback(env.Home, item.Path, enabled); err != nil {
			return err
		}
		if env.Kind == profileIsolated {
			return validateProfilePolicy(profileClientHome{Home: env.Home, SQLite: env.SQLite, Runtime: env.Runtime})
		}
		return nil
	})
}

func buildPluginSyncPlan(source, target extensionSnapshot) pluginSyncPlan {
	plan := pluginSyncPlan{
		SourceProfile: source.Profile,
		TargetProfile: target.Profile,
		SourceName:    source.ProfileName,
		TargetName:    target.ProfileName,
	}
	targetMkts := map[string]bool{}
	for _, m := range target.Marketplaces {
		if m.Name != "" {
			targetMkts[strings.ToLower(m.Name)] = true
		}
		if m.Source != "" {
			targetMkts[strings.ToLower(m.Source)] = true
		}
	}
	for _, m := range source.Marketplaces {
		if m.Source == "" {
			continue
		}
		if targetMkts[strings.ToLower(m.Name)] || targetMkts[strings.ToLower(m.Source)] {
			continue
		}
		plan.MarketplacesToAdd = append(plan.MarketplacesToAdd, m)
	}
	targetPlugins := map[string]bool{}
	for _, p := range target.Plugins {
		targetPlugins[strings.ToLower(p.ID)] = true
		targetPlugins[strings.ToLower(p.Name)] = true
	}
	for _, p := range source.Plugins {
		if targetPlugins[strings.ToLower(p.ID)] || targetPlugins[strings.ToLower(p.Name)] {
			plan.AlreadyInstalled = append(plan.AlreadyInstalled, p.Name)
			continue
		}
		plan.PluginsToAdd = append(plan.PluginsToAdd, p)
	}
	return plan
}

func (h harborService) PlanPluginSync(ctx context.Context, source, target profileBinding) (pluginSyncPlan, error) {
	if source == target {
		return pluginSyncPlan{}, errors.New("choose a different source Profile to sync plugins from")
	}
	srcSnap, err := h.InspectExtensions(ctx, source)
	if err != nil {
		return pluginSyncPlan{}, fmt.Errorf("inspect source Profile: %w", err)
	}
	dstSnap, err := h.InspectExtensions(ctx, target)
	if err != nil {
		return pluginSyncPlan{}, fmt.Errorf("inspect target Profile: %w", err)
	}
	return buildPluginSyncPlan(srcSnap, dstSnap), nil
}

func (h harborService) ExecutePluginSync(ctx context.Context, plan pluginSyncPlan) (pluginSyncResult, error) {
	result := pluginSyncResult{Plan: plan}
	if plan.SourceProfile == plan.TargetProfile {
		return result, errors.New("source and target Profile must differ")
	}
	err := h.WithProfileClient(ctx, profileClientRequest{Binding: plan.TargetProfile}, profileConfig, func(scope profileClientScope) error {
		env := scope.Environment
		codex, err := profileAccountCodex(env)
		if err != nil {
			return err
		}
		policy := profilePolicyArguments(env)
		for _, m := range plan.MarketplacesToAdd {
			if m.Source == "" {
				result.Outcomes = append(result.Outcomes, pluginSyncOutcome{Kind: "marketplace", Name: m.Name, State: "skipped", Detail: "no marketplace source URL/path"})
				continue
			}
			cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			args := append(append([]string{}, policy...), "plugin", "marketplace", "add", m.Source)
			cmd := exec.CommandContext(cmdCtx, codex, args...)
			cmd.Env, cmd.Dir = env.Environment, env.Home
			out, runErr := cmd.CombinedOutput()
			cancel()
			if runErr != nil {
				result.Outcomes = append(result.Outcomes, pluginSyncOutcome{Kind: "marketplace", Name: m.Name, State: "failed", Detail: cleanUI(strings.TrimSpace(string(out)))})
			} else {
				result.Outcomes = append(result.Outcomes, pluginSyncOutcome{Kind: "marketplace", Name: m.Name, State: "installed"})
			}
		}
		for _, p := range plan.PluginsToAdd {
			spec := p.ID
			if spec == "" {
				spec = p.Name
			}
			cmdCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
			args := append(append([]string{}, policy...), "plugin", "add", spec)
			cmd := exec.CommandContext(cmdCtx, codex, args...)
			cmd.Env, cmd.Dir = env.Environment, env.Home
			out, runErr := cmd.CombinedOutput()
			cancel()
			if runErr != nil {
				result.Outcomes = append(result.Outcomes, pluginSyncOutcome{Kind: "plugin", Name: p.Name, State: "failed", Detail: cleanUI(strings.TrimSpace(string(out)))})
			} else {
				result.Outcomes = append(result.Outcomes, pluginSyncOutcome{Kind: "plugin", Name: p.Name, State: "installed"})
			}
		}
		for _, name := range plan.AlreadyInstalled {
			result.Outcomes = append(result.Outcomes, pluginSyncOutcome{Kind: "plugin", Name: name, State: "skipped", Detail: "already installed"})
		}
		return nil
	})
	return result, err
}

func (h harborService) ExtensionExecCommand(ctx context.Context, binding profileBinding, action string, kind extensionKind, target string) (*exec.Cmd, error) {
	env, err := h.ResolveProfileClient(profileClientRequest{Binding: binding})
	if err != nil {
		return nil, err
	}
	target = strings.TrimSpace(target)
	switch kind {
	case extSkill:
		mode, bin := resolveSkillsCLI(env.Environment)
		if mode == "unavailable" || bin == "" {
			return nil, errors.New("skills CLI not found; install Node.js (npx) or run `npm i -g skills` to search, install or update skills")
		}
		var args []string
		if mode == "npx" {
			args = append(args, "--yes", "skills")
		}
		switch action {
		case "add":
			if target == "" {
				args = append(args, "find")
			} else {
				args = append(args, "add", target, "-g", "-a", "codex")
			}
		case "update":
			args = append(args, "update")
			if target != "" {
				args = append(args, target)
			}
		case "remove":
			if target == "" {
				return nil, errors.New("select a skill to remove")
			}
			args = append(args, "remove", target, "-g")
		default:
			return nil, errors.New("unsupported skill action")
		}
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Env, cmd.Dir = env.Environment, env.Home
		return cmd, nil

	case extPlugin:
		codex, err := profileAccountCodex(env)
		if err != nil {
			return nil, err
		}
		args := append(profilePolicyArguments(env), "plugin")
		switch action {
		case "add":
			if target == "" {
				return nil, errors.New("enter a plugin name or spec to add")
			}
			args = append(args, "add", target)
		case "remove":
			if target == "" {
				return nil, errors.New("select a plugin to remove")
			}
			args = append(args, "remove", target)
		case "marketplace":
			if target == "" {
				args = append(args, "marketplace", "list")
			} else {
				args = append(args, "marketplace", "add", target)
			}
		default:
			return nil, errors.New("unsupported plugin action")
		}
		cmd := exec.CommandContext(ctx, codex, args...)
		cmd.Env, cmd.Dir = env.Environment, env.Home
		return cmd, nil
	}
	return nil, errors.New("unknown extension kind")
}
