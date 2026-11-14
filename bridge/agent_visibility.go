package main

import (
	"context"
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

type runtimeObservation struct {
	SnapshotAt       time.Time `json:"snapshot_at"`
	Instance         string    `json:"instance,omitempty"`
	Activity         string    `json:"activity"`
	Presence         string    `json:"presence"`
	Source           string    `json:"source"`
	ObservedAt       time.Time `json:"observed_at"`
	Workspace        string    `json:"workspace_id,omitempty"`
	Terminal         string    `json:"terminal_id,omitempty"`
	LaunchPending    bool      `json:"launch_pending"`
	InteractiveReady bool      `json:"interactive_ready"`
	Associated       bool      `json:"associated"`
}

type herdrAgent struct {
	Terminal         string         `json:"terminal_id"`
	Workspace        string         `json:"workspace_id"`
	Activity         string         `json:"agent_status"`
	LaunchPending    bool           `json:"launch_pending"`
	InteractiveReady bool           `json:"interactive_ready"`
	Reference        *herdrIdentity `json:"agent_session"`
	Pane             string         `json:"pane_id"`
	Tab              string         `json:"tab_id"`
	Client           string         `json:"agent"`
	Name             string         `json:"name"`
	Directory        string         `json:"cwd"`
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
	return agentRPCContext(context.Background(), socket, method, params, result)
}
func agentRPCContext(ctx context.Context, socket, method string, params map[string]any, result any) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	c, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "unix", socket)
	if err != nil {
		return errors.New(agentDiscoveryWarning)
	}
	defer c.Close()
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
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

func unknownRuntime(s session) runtimeObservation {
	r := runtimeObservation{Activity: "unknown", Presence: "not_observed", Source: "none"}
	if !s.Started && s.RunToken == "" {
		r.Presence = "not_started"
	}
	if s.LastExitAt != nil && s.ProcessGroup == 0 && (s.LastStartedAt == nil || !s.LastExitAt.Before(*s.LastStartedAt)) {
		r.Presence = "historical_exit"
		r.Source = "harbor.runner.history"
		r.ObservedAt = *s.LastExitAt
	}
	return r
}

func agentObservation(a herdrAgent, at time.Time, associated bool) runtimeObservation {
	activity := a.Activity
	switch activity {
	case "idle", "working", "blocked", "done", "unknown":
	default:
		activity = "unknown"
	}
	return runtimeObservation{Activity: activity, Presence: "observed", Source: "herdr.agent.list", ObservedAt: at, Workspace: a.Workspace, Terminal: a.Terminal, LaunchPending: a.LaunchPending, InteractiveReady: a.InteractiveReady, Associated: associated}
}

func mergeHerdrAgents(ctx context.Context, views []sessionView) ([]sessionView, error) {
	for i := range views {
		views[i].Runtime = unknownRuntime(views[i].session)
		views[i].Running = false
		views[i].Status = "Unknown"
	}
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
	if err := agentRPCContext(ctx, socket, "agent.list", map[string]any{}, &result); err != nil {
		return views, err
	}
	if result.Agents == nil || identity == "" || socketIdentity(socket) != identity {
		return views, errors.New(agentDiscoveryWarning)
	}
	at := time.Now().UTC()
	for i := range views {
		views[i].Runtime.SnapshotAt = at
		views[i].Runtime.Instance = identity
	}
	seen := map[string]bool{}
	for _, agent := range *result.Agents {
		if agent.Pane == "" || seen[agent.Pane] {
			continue
		}
		seen[agent.Pane] = true
		owned := false
		for i := range views {
			v := &views[i]
			// A pane number alone can be reused. Fresh official conversation evidence
			// must agree with this run before attaching its connection intent.
			c := v.Conversation
			if v.HerdrSocket == socket && v.HerdrSocketIdentity == identity && v.HerdrPane == agent.Pane && v.HerdrTab == agent.Tab && v.CLI == agent.Client && v.RunToken != "" && c != nil && c.RunToken == v.RunToken && c.SocketIdentity == identity && c.Pane == agent.Pane && agent.Reference != nil && c.Reference == *agent.Reference && agent.Terminal != "" && agent.Workspace != "" && processGroupActive(v.ProcessGroup) {
				v.Runtime = agentObservation(agent, at, true)
				v.Runtime.SnapshotAt = at
				v.Runtime.Instance = identity
				v.Running = true
				v.Status = runtimeLabel(v.Runtime)
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
		ref, _ := json.Marshal(agent.Reference)
		digest := sha256.Sum256([]byte(socket + "\x00" + identity + "\x00" + agent.Workspace + "\x00" + agent.Terminal + "\x00" + agent.Pane + "\x00" + string(ref)))
		r := agentObservation(agent, at, false)
		r.SnapshotAt = at
		r.Instance = identity
		views = append(views, sessionView{session: session{ID: fmt.Sprintf("external:%x", digest[:16]), Name: name, CLI: agent.Client, Mode: "unknown", WorkingDirectory: agent.Directory, HerdrPane: agent.Pane, HerdrTab: agent.Tab, HerdrSocket: socket, HerdrSocketIdentity: identity}, External: true, Running: true, Runtime: r, Status: runtimeLabel(r), Reason: "Unmanaged. Connection and conversation intent unknown. Enter focuses this Herdr agent; manage it in its terminal."})
	}
	return views, nil
}

func runtimeLabel(r runtimeObservation) string {
	if r.LaunchPending {
		return "Starting"
	}
	switch r.Activity {
	case "idle":
		return "Idle"
	case "working":
		return "Working"
	case "blocked":
		return "Blocked"
	case "done":
		return "Done"
	}
	return "Unknown"
}
