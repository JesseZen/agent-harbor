package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type codexWindowObservation struct {
	RunToken string
	ThreadID string
	Tokens   int64
}

func (h harborService) ObservedCodexWindow(s session) codexWindowObservation {
	if s.Profile == nil || s.CLI != "codex" || s.CodexOptions == nil || s.CodexOptions.ContextWindow == nil || s.LastStartedAt == nil || s.RunToken == "" || queryReferenceCheck(s) != nil {
		return codexWindowObservation{}
	}
	ref := s.Conversation.Reference
	if ref.Agent != "codex" || ref.Kind != "id" || !validCodexID(ref.Value) || s.Conversation.RunToken != s.RunToken {
		return codexWindowObservation{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var observed codexWindowObservation
	_ = h.WithProfileClient(ctx, profileClientRequest{*s.Profile}, profileConfig, func(scope profileClientScope) error {
		if s.CodexHome != scope.Environment.Home {
			return ErrProfileIdentity
		}
		return withCodexConfigPipe(ctx, "", scope.Environment, s.WorkingDirectory, nil, func(rpc *codexConfigPipe) error {
			var reply struct{ Thread struct{ ID, Path string } }
			if err := rpc.call("thread/read", map[string]string{"threadId": ref.Value}, &reply); err != nil {
				return err
			}
			if reply.Thread.ID == ref.Value {
				if tokens := lastCodexWindow(scope.Environment.Home, reply.Thread.Path, ref.Value, *s.LastStartedAt); tokens > 0 {
					observed = codexWindowObservation{s.RunToken, ref.Value, tokens}
				}
			}
			return nil
		})
	})
	return observed
}

func lastCodexWindow(home, path, thread string, started time.Time) int64 {
	if !filepath.IsAbs(path) || !strings.HasSuffix(filepath.Base(path), "-"+thread+".jsonl") {
		return 0
	}
	path, err := filepath.EvalSymlinks(path)
	if err != nil {
		return 0
	}
	container, err := filepath.Rel(home, path)
	if err != nil || !(strings.HasPrefix(container, "sessions"+string(filepath.Separator)) || strings.HasPrefix(container, "archived_sessions"+string(filepath.Separator))) {
		return 0
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return 0
	}
	file, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer file.Close()
	const limit = 512 << 10
	length := min(info.Size(), limit)
	tail := make([]byte, length)
	read, _ := file.ReadAt(tail, info.Size()-length)
	lines := bytes.Split(tail[:read], []byte{'\n'})
	first := 0
	if info.Size() > length {
		first = 1
	}
	for index := len(lines) - 2; index >= first; index-- {
		var record struct {
			Timestamp time.Time
			Type      string
			Payload   struct {
				Type string
				Info struct {
					Window int64 `json:"model_context_window"`
				}
			}
		}
		if bytes.Contains(lines[index], []byte(`"token_count"`)) && json.Unmarshal(lines[index], &record) == nil && record.Type == "event_msg" && record.Payload.Type == "token_count" && !record.Timestamp.Before(started) && record.Payload.Info.Window > 0 {
			return record.Payload.Info.Window
		}
	}
	return 0
}
