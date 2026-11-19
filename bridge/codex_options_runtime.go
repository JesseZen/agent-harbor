package main

import (
	"context"
	"encoding/json"
	"errors"
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
		_, _, err := codexOptionLaunchArguments(ctx, "", env, s, newConversation, args, options, false)
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

func codexOptionLaunchArguments(ctx context.Context, binary string, env profileClientEnvironment, s session, kind launchKind, connectionArgs []string, options codexOptions, remoteTUI bool) ([]string, []string, error) {
	if err := options.validate(s.CLI, true); err != nil || options.empty() {
		return nil, nil, err
	}
	var inheritedDirs []string
	var resumeModel string
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
		return nil, nil, err
	}
	server := options.arguments(true, inheritedDirs)
	if remoteTUI {
		options.SandboxMode = ""
		options.ApprovalPolicy = ""
		options.WritableDirs = nil
	}
	tui := options.arguments(remoteTUI, nil)
	if resumeModel != "" {
		tui = codexConfigArg(tui, "model", strconv.Quote(resumeModel))
		server = codexConfigArg(server, "model", strconv.Quote(resumeModel))
	}
	return tui, server, nil
}
