package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

type conversationEvidence struct {
	ObservedReference *herdrIdentity `json:"observed_reference,omitempty"`
	Reference         herdrIdentity  `json:"reference"`
	Provenance        string         `json:"provenance"`
	ObservedAt        time.Time      `json:"observed_at"`
	Pane              string         `json:"pane"`
	SocketIdentity    string         `json:"socket_identity"`
	RunToken          string         `json:"run_token"`
	Status            string         `json:"status"`
}
type resumeCommandPlan struct {
	Version int      `json:"version"`
	Kind    string   `json:"kind"`
	Argv    []string `json:"argv"`
}

func neutralReferenceCheck(s session) error {
	e := s.Conversation
	if e == nil {
		return nil
	}
	if s.Version != 3 {
		return errors.New("neutral conversation record version unsupported; review record")
	}
	if e.Reference.Agent != s.CLI || (s.CodexSessionID != "" && (e.Reference.Kind != "id" || e.Reference.Value != s.CodexSessionID)) {
		return errors.New("conflicting legacy and neutral conversation references; explicitly review identity")
	}
	return nil
}
func queryReferenceCheck(s session) error {
	if err := neutralReferenceCheck(s); err != nil {
		return err
	}
	e := s.Conversation
	if e != nil && e.Provenance == "reviewed-compatibility" && (e.Status == "reviewed" || e.Status == "observed") {
		for _, d := range s.Reassociations {
			if d.Accepted && d.Reference == e.Reference {
				return nil
			}
		}
		return errors.New("compatibility reference has no matching approval; use reassociate")
	}
	if e == nil || e.Provenance != "herdr-pane-observation" || e.ObservedAt.IsZero() || e.Pane == "" || e.SocketIdentity == "" || e.RunToken == "" {
		return errors.New("query recovery requires an upstream-observed reference; legacy records require explicit review and reassociate before query recovery")
	}
	if e.Status != "observed" {
		return errors.New("conversation observation needs review")
	}
	return nil
}
func queryResume(s session) (*resumeCommandPlan, error) {
	if err := queryReferenceCheck(s); err != nil {
		return nil, err
	}
	root, err := bridgeHome()
	if err != nil {
		return nil, err
	}
	env, _, err := managedEnvironment(root)
	if err != nil {
		return nil, err
	}
	command := herdrCommand(env, "agent", "resume-command")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, command.Path, command.Args[1:]...)
	cmd.Env = env
	request, _ := json.Marshal(s.Conversation.Reference)
	cmd.Stdin = strings.NewReader(string(request))
	out, err := cmd.Output()
	if err != nil {
		return nil, errors.New("Herdr query unavailable or reference unsupported; install the matching development candidate and review the complete reference; no fallback was launched")
	}
	var plan resumeCommandPlan
	if len(out) > 65536 || json.Unmarshal(out, &plan) != nil || plan.Version != 1 || plan.Kind != s.CLI || len(plan.Argv) < 2 {
		return nil, errors.New("incompatible Herdr query response; nothing launched")
	}
	for _, arg := range plan.Argv {
		if arg == "" || strings.ContainsAny(arg, "\x00\r\n") {
			return nil, errors.New("invalid Herdr query arguments; nothing launched")
		}
	}
	if plan.Argv[0] != s.CLI {
		return nil, errors.New("missing query program")
	}
	return &plan, nil
}
func launchQuery(a sessionApproval) error {
	if err := queryReferenceCheck(a.Session); err != nil {
		return err
	}
	if !a.QueryUncertaintyAccepted {
		return errors.New("query proves syntax only; explicitly accept conversation, login and external writer uncertainty")
	}
	if os.Getenv("HERDR_PLUGIN_ID") != "agent-harbor.bridge" || (os.Getenv("HERDR_PLUGIN_ENTRYPOINT_ID") != "codex" && os.Getenv("HERDR_PLUGIN_ENTRYPOINT_ID") != "session") || os.Getenv("HERDR_PANE_ID") == "" || os.Getenv("HERDR_SOCKET_PATH") == "" {
		return errors.New("query recovery requires a managed Herdr session pane")
	}
	root, err := bridgeHome()
	if err != nil {
		return err
	}
	if !managedSocket(root, os.Getenv("HERDR_SOCKET_PATH")) {
		return errors.New("query recovery requires the managed workspace socket")
	}
	if err := checkSession(a.Session, resumeConversation, a.OverrideReason); err != nil {
		return err
	}
	plan, err := queryResume(a.Session)
	if err != nil {
		return err
	}
	return launchWithPlan(a.Session, resumeConversation, a.OverrideReason, plan)
}
