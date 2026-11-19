package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

type codexOptions struct {
	SandboxMode            string   `json:"sandbox_mode,omitempty"`
	ApprovalPolicy         string   `json:"approval_policy,omitempty"`
	ReasoningEffort        string   `json:"model_reasoning_effort,omitempty"`
	ContextWindow          *int64   `json:"model_context_window,omitempty"`
	AutoCompactTokenLimit  *int64   `json:"model_auto_compact_token_limit,omitempty"`
	PostTurnCompactPercent *int64   `json:"model_post_turn_compact_threshold_percent,omitempty"`
	WritableDirs           []string `json:"writable_dirs,omitempty"`
}

var codexSandboxModes = []string{"", "read-only", "workspace-write", "danger-full-access"}
var codexApprovalPolicies = []string{"", "on-request", "never"}
var codexEfforts = []string{"", "none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"}

type codexOptionError struct{ field, reason string }

func (err *codexOptionError) Error() string { return err.field + ": " + err.reason }

func (o codexOptions) empty() bool {
	return o.SandboxMode == "" && o.ApprovalPolicy == "" && o.ReasoningEffort == "" && o.ContextWindow == nil && o.AutoCompactTokenLimit == nil && o.PostTurnCompactPercent == nil && len(o.WritableDirs) == 0
}

func (o codexOptions) saved() *codexOptions {
	if o.empty() {
		return nil
	}
	o.WritableDirs = slices.Clone(o.WritableDirs)
	return &o
}

func codexOptionsValue(saved *codexOptions) codexOptions {
	if saved == nil {
		return codexOptions{}
	}
	return *saved
}

func (o codexOptions) validate(client string, checkPaths bool) error {
	if !o.empty() && client != "codex" {
		return &codexOptionError{"Options", "launch overrides are only supported for Codex"}
	}
	if !slices.Contains(codexSandboxModes, o.SandboxMode) {
		return &codexOptionError{"Sandbox", "choose a supported Codex sandbox mode"}
	}
	if !slices.Contains(codexApprovalPolicies, o.ApprovalPolicy) {
		return &codexOptionError{"Approval", "choose On request, Never or Inherited"}
	}
	if !slices.Contains(codexEfforts, o.ReasoningEffort) {
		return &codexOptionError{"Reasoning", "unsupported Codex reasoning effort"}
	}
	if o.ContextWindow != nil && *o.ContextWindow <= 0 {
		return &codexOptionError{"Context window", "enter a positive token count or leave empty"}
	}
	if o.AutoCompactTokenLimit != nil && *o.AutoCompactTokenLimit <= 0 {
		return &codexOptionError{"Auto compact", "enter a positive token count or leave empty"}
	}
	if o.PostTurnCompactPercent != nil && (*o.PostTurnCompactPercent < 0 || *o.PostTurnCompactPercent > 100) {
		return &codexOptionError{"Post-turn compact", "enter 0–100 or leave empty"}
	}
	if len(o.WritableDirs) != 0 && o.SandboxMode != "" && o.SandboxMode != "workspace-write" {
		return &codexOptionError{"Writable dirs", "requires Workspace write or inherited Workspace write"}
	}
	seen := make(map[string]bool)
	for _, directory := range o.WritableDirs {
		if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory || strings.ContainsAny(directory, "\x00\r\n") {
			return &codexOptionError{"Writable dirs", "enter a clean, absolute directory on this workspace's server"}
		}
		if seen[directory] {
			return &codexOptionError{"Writable dirs", "duplicate directory " + directory}
		}
		seen[directory] = true
		if checkPaths {
			info, err := os.Stat(directory)
			if err != nil || !info.IsDir() {
				return &codexOptionError{"Writable dirs", "directory unavailable on this server: " + directory}
			}
		}
	}
	return nil
}

func codexConfigArg(arguments []string, key, value string) []string {
	return append(arguments, "-c", key+"="+value)
}

func codexNumberOverrides(options codexOptions) []codexConfigEdit {
	return []codexConfigEdit{
		{"model_context_window", options.ContextWindow, "replace"},
		{"model_auto_compact_token_limit", options.AutoCompactTokenLimit, "replace"},
		{"model_post_turn_compact_threshold_percent", options.PostTurnCompactPercent, "replace"},
	}
}

func codexNumberOptions(config map[string]json.RawMessage) codexOptions {
	return codexOptions{
		ContextWindow:          configValue[*int64](config, "model_context_window"),
		AutoCompactTokenLimit:  configValue[*int64](config, "model_auto_compact_token_limit"),
		PostTurnCompactPercent: configValue[*int64](config, "model_post_turn_compact_threshold_percent"),
	}
}

func (o codexOptions) arguments(server bool, inheritedDirs []string) []string {
	var args []string
	for _, edit := range codexNumberOverrides(o) {
		if value := edit.Value.(*int64); value != nil {
			args = codexConfigArg(args, edit.KeyPath, strconv.FormatInt(*value, 10))
		}
	}
	if o.ReasoningEffort != "" {
		args = codexConfigArg(args, "model_reasoning_effort", strconv.Quote(o.ReasoningEffort))
	}
	if o.SandboxMode != "" {
		if server {
			args = codexConfigArg(args, "sandbox_mode", strconv.Quote(o.SandboxMode))
		} else {
			args = append(args, "--sandbox", o.SandboxMode)
		}
	}
	if o.ApprovalPolicy != "" {
		if server {
			args = codexConfigArg(args, "approval_policy", strconv.Quote(o.ApprovalPolicy))
		} else {
			args = append(args, "--ask-for-approval", o.ApprovalPolicy)
		}
	}
	if server && len(o.WritableDirs) > 0 {
		dirs := slices.Clone(inheritedDirs)
		for _, directory := range o.WritableDirs {
			if !slices.Contains(dirs, directory) {
				dirs = append(dirs, directory)
			}
		}
		encoded, _ := json.Marshal(dirs)
		args = codexConfigArg(args, "sandbox_workspace_write.writable_roots", string(encoded))
	} else if !server {
		for _, directory := range o.WritableDirs {
			args = append(args, "--add-dir", directory)
		}
	}
	return args
}

func (o codexOptions) validateEffort(model string, efforts []string) error {
	if o.ReasoningEffort == "" || slices.Contains(efforts, o.ReasoningEffort) {
		return nil
	}
	if model == "" {
		return &codexOptionError{"Reasoning", "cannot verify the active model; choose Inherited or check the Codex catalog"}
	}
	return &codexOptionError{"Reasoning", fmt.Sprintf("%s is not advertised for %s; choose Inherited or a supported effort", o.ReasoningEffort, model)}
}
