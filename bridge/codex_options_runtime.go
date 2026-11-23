package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

type codexReasoningChoices struct {
	Model   string
	Efforts []string
}

func appendCodexEffort(efforts []string, value string) []string {
	if value != "" && slices.Contains(codexEfforts, value) && !slices.Contains(efforts, value) {
		return append(efforts, value)
	}
	return efforts
}

func (rpc *codexConfigPipe) reasoningFor(model, provider string) (codexReasoningChoices, error) {
	params := map[string]any{"limit": 100, "includeHidden": model != ""}
	if provider != "" {
		params["modelProvider"] = provider
	}
	for range 20 {
		var response struct {
			Data []struct {
				ID      string `json:"model"`
				Default bool   `json:"isDefault"`
				Efforts []struct {
					Effort string `json:"reasoningEffort"`
				} `json:"supportedReasoningEfforts"`
			}
			NextCursor string
		}
		if err := rpc.call("model/list", params, &response); err != nil {
			return codexReasoningChoices{}, err
		}
		for _, candidate := range response.Data {
			if candidate.ID != "" && (candidate.ID == model || model == "" && candidate.Default) {
				choices := codexReasoningChoices{Model: candidate.ID}
				for _, item := range candidate.Efforts {
					choices.Efforts = appendCodexEffort(choices.Efforts, item.Effort)
				}
				return choices, nil
			}
		}
		if response.NextCursor == "" || params["cursor"] == response.NextCursor {
			break
		}
		params["cursor"] = response.NextCursor
	}
	return codexReasoningChoices{Model: model}, nil
}

func sessionCodexReasoning(rpc *codexConfigPipe, s session, model, provider string) (codexReasoningChoices, error) {
	if s.Mode != "magpie" || joinedCodex(s) && !strings.Contains(model, "/") {
		return rpc.reasoningFor(model, provider)
	}
	_, models, err := fetchMagpieCodexCatalog(s.MagpieURL)
	if err != nil {
		return codexReasoningChoices{}, err
	}
	result := codexReasoningChoices{Model: model}
	for _, candidate := range models {
		if candidate.Slug == model {
			for _, level := range candidate.Levels {
				result.Efforts = appendCodexEffort(result.Efforts, level.Effort)
			}
			break
		}
	}
	return result, nil
}

func (h harborService) codexDraftScope(d sessionDraft) (session, []string, error) {
	s := session{CLI: d.Client, Profile: d.Profile, Mode: d.Connection, CodexConnection: d.CodexConnection, MagpieModel: d.Model, WorkingDirectory: d.Directory}
	if s.CLI == "" {
		s.CLI = "codex"
	}
	if s.CLI != "codex" || s.Profile == nil || s.Profile.Client != "codex" {
		return s, nil, ErrProfileUnsupported
	}
	if d.Connection != "magpie" {
		return s, nil, nil
	}
	if d.Model == "" || d.Instance == "" {
		return s, nil, errors.New("choose a Magpie instance and model first")
	}
	registry, err := loadMagpieRegistry()
	if err != nil {
		return s, nil, err
	}
	instance, ok := registry.find(d.Instance)
	if !ok {
		return s, nil, errors.New("Magpie instance not found")
	}
	s.MagpieURL = instance.Address
	if s.CodexConnection == "" {
		s.CodexConnection, err = h.ProfileCodexConnection(context.Background(), profileClientRequest{*d.Profile})
		if err != nil {
			return s, nil, err
		}
	}
	if joinedCodex(s) {
		if err := requireCallerOnlyGateway(s.MagpieURL); err != nil {
			return s, nil, err
		}
		return s, joinedCodexArgs(s, newConversation), nil
	}
	return s, apiOnlyCodexArgs(s, newConversation), nil
}

func (h harborService) CodexReasoningChoices(d sessionDraft) (codexReasoningChoices, error) {
	s, args, err := h.codexDraftScope(d)
	if err != nil {
		return codexReasoningChoices{}, err
	}
	var choices codexReasoningChoices
	err = h.withCodexDraftClient(s, func(ctx context.Context, env profileClientEnvironment) error {
		return withCodexConfigPipe(ctx, "", env, s.WorkingDirectory, args, func(rpc *codexConfigPipe) error {
			read, err := rpc.config(s.WorkingDirectory)
			if err != nil {
				return err
			}
			choices, err = sessionCodexReasoning(rpc, s, initialCodexOptionModel(s, read), configValue[string](read.Config, "model_provider"))
			return err
		})
	})
	return choices, err
}

func initialCodexOptionModel(s session, read codexConfigRead) string {
	if s.Mode == "magpie" && s.MagpieModel != codexDefaultModel {
		return s.MagpieModel
	}
	return configValue[string](read.Config, "model")
}

func (h harborService) withCodexDraftClient(s session, work func(context.Context, profileClientEnvironment) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 16*time.Second)
	defer cancel()
	return h.WithProfileClient(ctx, profileClientRequest{*s.Profile}, profileConfig, func(scope profileClientScope) error {
		if scope.Environment.Kind == profileIsolated && s.Mode == "magpie" {
			if err := requireProfileSafeGateway(s.MagpieURL); err != nil {
				return err
			}
		}
		return work(ctx, scope.Environment)
	})
}

func (h harborService) validateDraftCodexOptions(d sessionDraft) error {
	options := codexOptionsValue(d.CodexOptions)
	if err := options.validate(d.Client, true); err != nil || options.empty() {
		return err
	}
	s, args, err := h.codexDraftScope(d)
	if err != nil {
		return err
	}
	return h.withCodexDraftClient(s, func(ctx context.Context, env profileClientEnvironment) error {
		_, err := codexOptionLaunchPlans(ctx, "", env, s, newConversation, args, options)
		return err
	})
}

func (o codexOptions) checkPermissions(rpc *codexConfigPipe, read codexConfigRead) error {
	if o.SandboxMode != "" || len(o.WritableDirs) != 0 {
		if configValue[string](read.Config, "default_permissions") != "" {
			return &codexOptionError{"Sandbox", "Codex default_permissions is configured; inherit it without overriding sandbox or writable dirs"}
		}
	}
	if len(o.WritableDirs) != 0 && o.SandboxMode == "" && configValue[string](read.Config, "sandbox_mode") != "workspace-write" {
		return &codexOptionError{"Writable dirs", "could not verify inherited Workspace write; select it explicitly or remove the directories"}
	}
	if o.SandboxMode == "" && o.ApprovalPolicy == "" && len(o.WritableDirs) == 0 {
		return nil
	}
	var response struct {
		Requirements struct {
			Sandbox  []string          `json:"allowedSandboxModes"`
			Approval []json.RawMessage `json:"allowedApprovalPolicies"`
		} `json:"requirements"`
	}
	if err := rpc.call("configRequirements/read", map[string]any{}, &response); err != nil {
		return &codexOptionError{"Options", err.Error()}
	}
	sandbox := o.SandboxMode
	if sandbox == "" && len(o.WritableDirs) != 0 {
		sandbox = "workspace-write"
	}
	if sandbox != "" && response.Requirements.Sandbox != nil && !slices.Contains(response.Requirements.Sandbox, sandbox) {
		return &codexOptionError{"Sandbox", "the selected mode is disallowed by Codex managed requirements"}
	}
	if o.ApprovalPolicy != "" && response.Requirements.Approval != nil {
		for _, policy := range response.Requirements.Approval {
			if string(policy) == strconv.Quote(o.ApprovalPolicy) {
				return nil
			}
		}
		return &codexOptionError{"Approval", "the selected policy is disallowed by Codex managed requirements"}
	}
	return nil
}

type codexLaunchOptions struct {
	local, remote, server []string
	remoteProfile         bool
	localOnly             bool
}

func codexLayerBeatsProfile(origin codexLayerMetadata) bool {
	switch origin.Name.Type {
	case "", "user", "profile":
		return false
	default:
		return true
	}
}

func codexHasUnrelatedProfileLayer(layers []codexConfigLayer, ownedProfile string) bool {
	for _, layer := range layers {
		if layer.Name.Type == "profile" && layer.DisabledReason == "" && layer.Name.Profile != "" && layer.Name.Profile != ownedProfile {
			return true
		}
	}
	return false
}

func codexPermissionProfile(id string, options codexOptions, env []string) (string, error) {
	home, path, marker, err := magpieCodexProfilePath(id, env)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(home, 0700); err != nil {
		return "", errors.New("cannot prepare Codex home for an isolated session profile")
	}
	existing, err := readOwnedCodexProfile(path, marker)
	if err != nil && !os.IsNotExist(err) {
		return "", errors.New("Codex profile is unavailable or not owned by this session; review the conflict")
	}
	content := updateCodexPermissionProfileContent(existing, marker, options)
	if err := writePrivate(path, content); err != nil {
		return "", errors.New("cannot save the isolated Codex session profile")
	}
	return "agent-harbor-" + id, nil
}

func clearCodexPermissionProfile(id string, env []string) error {
	_, path, marker, err := magpieCodexProfilePath(id, env)
	if err != nil {
		return nil
	}
	existing, err := readOwnedCodexProfile(path, marker)
	if err != nil {
		return nil
	}
	content := updateCodexPermissionProfileContent(existing, marker, codexOptions{})
	if string(content) == string(existing) {
		return nil
	}
	return writePrivate(path, content)
}

func updateCodexPermissionProfileContent(existing []byte, marker string, options codexOptions) []byte {
	var preserved []string
	if len(existing) > 0 {
		inTable := false
		for _, line := range strings.Split(strings.TrimRight(string(existing), "\n"), "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == marker {
				continue
			}
			if strings.HasPrefix(trimmed, "[") {
				inTable = true
			}
			if !inTable {
				if key, _, ok := strings.Cut(trimmed, "="); ok {
					switch strings.TrimSpace(key) {
					case "sandbox_mode", "approval_policy":
						continue
					}
				}
			}
			preserved = append(preserved, line)
		}
	}
	lines := []string{marker}
	if options.SandboxMode != "" {
		lines = append(lines, "sandbox_mode = "+strconv.Quote(options.SandboxMode))
	}
	if options.ApprovalPolicy != "" {
		lines = append(lines, "approval_policy = "+strconv.Quote(options.ApprovalPolicy))
	}
	lines = append(lines, preserved...)
	return []byte(strings.Join(lines, "\n") + "\n")
}

func codexOptionLaunchPlans(ctx context.Context, binary string, env profileClientEnvironment, s session, kind launchKind, connectionArgs []string, options codexOptions) (codexLaunchOptions, error) {
	if err := options.validate(s.CLI, true); err != nil || options.empty() {
		return codexLaunchOptions{}, err
	}
	var inheritedDirs []string
	var resumeModel string
	var remoteProfile, localOnly bool
	err := withCodexConfigPipe(ctx, binary, env, s.WorkingDirectory, connectionArgs, func(rpc *codexConfigPipe) error {
		read, err := rpc.config(s.WorkingDirectory)
		if err != nil {
			return &codexOptionError{"Options", err.Error()}
		}
		if err := options.checkPermissions(rpc, read); err != nil {
			return err
		}
		if options.PostTurnCompactPercent != nil {
			if _, supported := read.Config["model_post_turn_compact_threshold_percent"]; !supported {
				return &codexOptionError{"Post-turn compact", "this Codex client does not advertise the setting"}
			}
		}
		workspace := configValue[struct {
			Roots []string `json:"writable_roots"`
		}](read.Config, "sandbox_workspace_write")
		inheritedDirs = workspace.Roots
		if options.hasPermissionOverrides() {
			owned := ""
			if s.ID != "" {
				owned = "agent-harbor-" + s.ID
			}
			conflict := codexHasUnrelatedProfileLayer(read.Layers, owned)
			if !conflict && options.SandboxMode != "" && codexLayerBeatsProfile(read.Origins["sandbox_mode"]) && configValue[string](read.Config, "sandbox_mode") != options.SandboxMode {
				conflict = true
			}
			if !conflict && options.ApprovalPolicy != "" && codexLayerBeatsProfile(read.Origins["approval_policy"]) && configValue[string](read.Config, "approval_policy") != options.ApprovalPolicy {
				conflict = true
			}
			if conflict {
				localOnly = true
			} else {
				remoteProfile = true
			}
		}
		if options.ReasoningEffort == "" {
			return nil
		}
		model := initialCodexOptionModel(s, read)
		provider := configValue[string](read.Config, "model_provider")
		if kind == resumeConversation {
			model, provider, err = rpc.savedCodexModel(s)
			if err != nil {
				return err
			}
			resumeModel = model
		}
		choices, err := sessionCodexReasoning(rpc, s, model, provider)
		if err != nil {
			return &codexOptionError{"Reasoning", err.Error()}
		}
		return options.validateEffort(choices.Model, choices.Efforts)
	})
	if err != nil {
		return codexLaunchOptions{}, err
	}
	// Passing CLI permission overrides (-c sandbox_mode/approval_policy) to a
	// --remote TUI makes Codex reject Command Center and /resume switches with
	// "Permission overrides are not supported when resuming a remote task."
	// Supply them via a session-owned -p profile instead, while the private App
	// Server keeps the full overrides and writable roots. If a higher config
	// layer (such as project .codex/config.toml) would beat -p, fall back to
	// in-process (--no-daemon) mode where local CLI flags override it safely.
	remote := options
	remote.WritableDirs = nil
	remote.SandboxMode = ""
	remote.ApprovalPolicy = ""
	plan := codexLaunchOptions{
		local:         options.arguments(false, nil),
		remote:        remote.arguments(true, nil),
		server:        options.arguments(true, inheritedDirs),
		remoteProfile: remoteProfile,
		localOnly:     localOnly,
	}
	if resumeModel != "" {
		model := strconv.Quote(resumeModel)
		plan.local = codexConfigArg(plan.local, "model", model)
		plan.remote = codexConfigArg(plan.remote, "model", model)
		plan.server = codexConfigArg(plan.server, "model", model)
	}
	return plan, nil
}
