package main

import (
	"context"
	"errors"
	"time"
)

func canEditCodexOptions(v sessionView) bool {
	return !v.External && !v.Archived && v.CLI == "codex" && v.Profile != nil && !v.Running && (v.Runtime.Presence == "not_started" || v.Runtime.Presence == "confirmed_exit" || v.Runtime.Presence == "historical_exit")
}

func (h harborService) SaveSessionCodexOptions(expected sessionView, options codexOptions) error {
	if !canEditCodexOptions(expected) {
		return errors.New("only stopped, Profile-bound Codex sessions can change options")
	}
	if err := options.validate(expected.CLI, true); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return h.WithProfileClient(ctx, profileClientRequest{*expected.Profile}, profileConfig, func(profileClientScope) error {
		lock, err := lockSession(expected.ID)
		if err != nil {
			return err
		}
		defer lock.Close()
		if err := validateLaunchState(expected.session); err != nil {
			return err
		}
		return withSessions(func(records []session) ([]session, error) {
			for index := range records {
				if records[index].ID == expected.ID && sameSession(records[index], expected.session) {
					records[index].CodexOptions = options.saved()
					return records, nil
				}
			}
			return nil, errors.New("session changed; review it again")
		})
	})
}

func (rpc *codexConfigPipe) savedCodexModel(s session) (string, string, error) {
	if s.Conversation == nil || s.Conversation.Reference.Agent != "codex" || s.Conversation.Reference.Kind != "id" || !validCodexID(s.Conversation.Reference.Value) {
		return "", "", &codexOptionError{"Reasoning", "the saved conversation's model cannot be verified"}
	}
	id := s.Conversation.Reference.Value
	var reply struct {
		Thread struct{ ID, Model, ModelProvider string }
	}
	if err := rpc.call("thread/read", map[string]string{"threadId": id}, &reply); err != nil || reply.Thread.ID != id || !selectionTextValid(reply.Thread.Model, 512) {
		return "", "", &codexOptionError{"Reasoning", "the saved conversation's model is unavailable; clear the reasoning override to resume"}
	}
	return reply.Thread.Model, reply.Thread.ModelProvider, nil
}

func (h harborService) SessionCodexReasoningChoices(s session) (codexReasoningChoices, error) {
	if s.Profile == nil || s.Profile.Client != "codex" || s.CLI != "codex" {
		return codexReasoningChoices{}, ErrProfileUnsupported
	}
	kind := newConversation
	if s.Started {
		kind = resumeConversation
	}
	var arguments []string
	if joinedCodex(s) {
		if err := requireCallerOnlyGateway(s.MagpieURL); err != nil {
			return codexReasoningChoices{}, err
		}
		arguments = joinedCodexArgs(s, kind)
	} else if s.Mode == "magpie" {
		arguments = apiOnlyCodexArgs(s, kind)
	}
	var choices codexReasoningChoices
	err := h.withCodexDraftClient(s, func(ctx context.Context, env profileClientEnvironment) error {
		return withCodexConfigPipe(ctx, "", env, s.WorkingDirectory, arguments, func(rpc *codexConfigPipe) error {
			read, err := rpc.config(s.WorkingDirectory)
			if err != nil {
				return err
			}
			model := initialCodexOptionModel(s, read)
			provider := configValue[string](read.Config, "model_provider")
			if kind == resumeConversation {
				model, provider, err = rpc.savedCodexModel(s)
				if err != nil {
					return err
				}
			}
			choices, err = sessionCodexReasoning(rpc, s, model, provider)
			return err
		})
	})
	return choices, err
}
