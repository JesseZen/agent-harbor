package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"
)

type herdrAgent struct {
	Pane      string `json:"pane_id"`
	Tab       string `json:"tab_id"`
	Client    string `json:"agent"`
	Name      string `json:"name"`
	Directory string `json:"cwd"`
}

const agentDiscoveryWarning = "Herdr agent discovery unavailable; recorded sessions are preserved. Reopen the managed workspace and refresh."

// The ambient socket is accepted only within Harbor's managed namespace.
// Outside the popup, inspect only the two supported managed socket locations.
func agentDiscoverySocket() (string, error) {
	root, err := bridgeHome()
	if err != nil {
		return "", err
	}
	if socket := os.Getenv("HERDR_SOCKET_PATH"); socket != "" {
		if !managedSocket(root, socket) {
			return "", errors.New(agentDiscoveryWarning)
		}
		return socket, nil
	}
	config := filepath.Join(root, "herdr", "config")
	if _, err := os.Stat(config); os.IsNotExist(err) {
		return "", nil
	}
	config, err = managedConfigContext(root)
	if err != nil {
		return "", errors.New(agentDiscoveryWarning)
	}
	for _, app := range []string{"herdr", "herdr-dev"} {
		socket := filepath.Join(config, app, "sessions", herdrSession, "herdr.sock")
		if managedSocket(root, socket) {
			return socket, nil
		}
	}
	return "", errors.New(agentDiscoveryWarning)
}

func agentRPC(socket, method string, params map[string]any, result any) error {
	c, err := net.DialTimeout("unix", socket, time.Second)
	if err != nil {
		return errors.New(agentDiscoveryWarning)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(2 * time.Second))
	if json.NewEncoder(c).Encode(map[string]any{"id": "harbor-agents", "method": method, "params": params}) != nil {
		return errors.New(agentDiscoveryWarning)
	}
	var response struct {
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	var body json.RawMessage
	if json.NewDecoder(io.LimitReader(c, 65537)).Decode(&body) != nil || len(body) > 65536 || json.Unmarshal(body, &response) != nil || len(response.Result) == 0 || string(response.Result) == "null" || len(response.Error) > 0 && string(response.Error) != "null" {
		return errors.New(agentDiscoveryWarning)
	}
	if json.Unmarshal(response.Result, result) != nil {
		return errors.New(agentDiscoveryWarning)
	}
	return nil
}

func mergeHerdrAgents(views []sessionView) ([]sessionView, error) {
	socket, err := agentDiscoverySocket()
	if err != nil {
		return views, err
	}
	if socket == "" {
		return views, nil
	}
	identity := socketIdentity(socket)
	var result struct {
		Agents *[]herdrAgent `json:"agents"`
	}
	if err := agentRPC(socket, "agent.list", map[string]any{}, &result); err != nil {
		return views, err
	}
	if result.Agents == nil || socketIdentity(socket) != identity {
		return views, errors.New(agentDiscoveryWarning)
	}
	seen := map[string]bool{}
	for _, agent := range *result.Agents {
		if agent.Pane == "" || seen[agent.Pane] {
			continue
		}
		seen[agent.Pane] = true
		owned := false
		for _, view := range views {
			if view.HerdrSocket == socket && view.HerdrSocketIdentity == identity && view.HerdrPane == agent.Pane && view.RunToken != "" && processGroupActive(view.ProcessGroup) {
				owned = true
				break
			}
		}
		if owned {
			continue
		}
		name := agent.Name
		if name == "" {
			name = agent.Client
		}
		if name == "" {
			name = "Herdr agent"
		}
		digest := sha256.Sum256([]byte(socket + "\x00" + identity + "\x00" + agent.Pane))
		views = append(views, sessionView{session: session{ID: fmt.Sprintf("external:%x", digest[:16]), Name: name, CLI: agent.Client, Mode: "unknown", WorkingDirectory: agent.Directory, HerdrPane: agent.Pane, HerdrTab: agent.Tab, HerdrSocket: socket, HerdrSocketIdentity: identity}, External: true, Running: true, Status: "External / unmanaged", Reason: "Connection and conversation intent unknown. Enter focuses this Herdr agent; manage it in its terminal."})
	}
	return views, nil
}
