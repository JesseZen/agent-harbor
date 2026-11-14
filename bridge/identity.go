package main

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"time"
)

var errPaneAssociation = errors.New("Herdr pane association changed")

type herdrIdentity struct {
	Source string `json:"source"`
	Agent  string `json:"agent"`
	Kind   string `json:"kind"`
	Value  string `json:"value"`
}

func paneIdentity(ctx context.Context, herdr, pane string) (*herdrIdentity, error) {
	request, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	out, err := exec.CommandContext(request, herdr, "pane", "get", pane).Output()
	if err != nil {
		return nil, err
	}
	var response struct {
		Result struct {
			Pane struct {
				ID       string         `json:"pane_id"`
				Identity *herdrIdentity `json:"agent_session"`
			} `json:"pane"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &response); err != nil {
		return nil, err
	}
	if response.Result.Pane.ID != "" && response.Result.Pane.ID != pane {
		return nil, errPaneAssociation
	}
	return response.Result.Pane.Identity, nil
}
