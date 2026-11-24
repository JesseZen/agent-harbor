package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (h harborService) InspectExtensionsLocal(_ context.Context, binding profileBinding) (extensionSnapshot, error) {
	env, err := h.ResolveProfileClient(profileClientRequest{Binding: binding})
	if err != nil {
		return extensionSnapshot{}, err
	}
	mode, _ := resolveSkillsCLI(env.Environment)
	skills, warnings := scanLocalSkills(environmentValue(env.Environment, "HOME"), env.Home)
	return extensionSnapshot{ProfileName: env.ProfileName, CodexHome: env.Home, SkillsCLI: mode, Skills: skills, Warnings: warnings}, nil
}

func (h harborService) InspectCodexSkills(ctx context.Context, binding profileBinding) ([]extensionItem, error) {
	var skills []extensionItem
	err := h.WithProfileClient(ctx, profileClientRequest{Binding: binding}, profileInspect, func(scope profileClientScope) error {
		var err error
		skills, err = fetchCodexRPCSkills(ctx, scope.Environment)
		return err
	})
	return skills, err
}

func (h harborService) SearchSkills(ctx context.Context, binding profileBinding, query string) ([]skillCatalogItem, error) {
	query = strings.TrimSpace(query)
	if len([]rune(query)) < 2 {
		return nil, errors.New("enter at least two characters to search Skills")
	}
	env, err := h.ResolveProfileClient(profileClientRequest{Binding: binding})
	if err != nil {
		return nil, err
	}
	base := environmentValue(env.Environment, "AGENT_HARBOR_SKILLS_API_URL")
	if base == "" {
		base = environmentValue(env.Environment, "SKILLS_API_URL")
	}
	if base == "" {
		base = "https://skills.sh"
	}
	endpoint, err := url.Parse(strings.TrimRight(base, "/") + "/api/search")
	if err != nil {
		return nil, errors.New("Skills search endpoint is invalid")
	}
	params := endpoint.Query()
	params.Set("q", query)
	params.Set("limit", "50")
	endpoint.RawQuery = params.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 8 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, errors.New("Skills search is unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("Skills search returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, errors.New("Skills search response unavailable")
	}
	return parseSkillsCatalogResponse(body)
}

func parseSkillsCatalogResponse(data []byte) ([]skillCatalogItem, error) {
	var payload struct {
		Skills []struct {
			ID       string `json:"id"`
			Source   string `json:"source"`
			SkillID  string `json:"skillId"`
			Name     string `json:"name"`
			Installs int    `json:"installs"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, errors.New("Skills search returned invalid JSON")
	}
	result := make([]skillCatalogItem, 0, len(payload.Skills))
	for _, item := range payload.Skills {
		id := cleanUI(item.ID)
		name := cleanUI(item.Name)
		if id == "" || name == "" {
			continue
		}
		skillID := cleanUI(item.SkillID)
		if skillID == "" {
			parts := strings.Split(id, "/")
			if len(parts) > 2 {
				skillID = parts[len(parts)-1]
			}
		}
		result = append(result, skillCatalogItem{
			ID:       id,
			Name:     name,
			Source:   cleanUI(item.Source),
			Skill:    skillID,
			Installs: item.Installs,
		})
	}
	return result, nil
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

func (h harborService) ToggleExtension(ctx context.Context, binding profileBinding, item extensionItem, enabled bool) error {
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

func (h harborService) ExtensionExecCommand(ctx context.Context, binding profileBinding, action, target string) (*exec.Cmd, error) {
	env, err := h.ResolveProfileClient(profileClientRequest{Binding: binding})
	if err != nil {
		return nil, err
	}
	mode, bin := resolveSkillsCLI(env.Environment)
	if mode == "unavailable" {
		return nil, errors.New("skills CLI unavailable; install Node.js (npx) or run npm i -g skills")
	}
	args, err := buildSkillsCLIArgs(mode, action, strings.TrimSpace(target))
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env, cmd.Dir = env.Environment, env.Home
	return cmd, nil
}

func buildSkillsCLIArgs(mode, action, target string) ([]string, error) {
	args := []string{}
	if mode == "npx" {
		args = append(args, "--yes", "skills")
	}
	switch action {
	case "add":
		if !skillInputIsSource(target) {
			return nil, errors.New("enter a Skill repository, Git URL or local path")
		}
		args = append(args, "add", target, "-g", "-a", "codex")
	case "install-skill":
		parts := strings.SplitN(target, "\x00", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return nil, errors.New("catalog result has no install source")
		}
		args = append(args, "add", strings.TrimSpace(parts[0]), "--skill", strings.TrimSpace(parts[1]), "-g", "-a", "codex", "--json", "-y")
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
	return args, nil
}
