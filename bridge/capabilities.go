package main

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"time"
)

// Integration records are a hook-target subset, never a client registry or
// proof of executable kind, identity observation, recovery or connection support.
type clientCapability struct {
	Target      string `json:"target"`
	Label       string `json:"label"`
	Command     string `json:"command"`
	Available   bool   `json:"available"`
	Integration string `json:"state"`
}

func discoverClientSubset() ([]clientCapability, string) {
	unavailable := "Herdr integration discovery unavailable; reopen managed Herdr and retry. Only the supported Codex path is listed."
	clients, err := listHerdrIntegrations()
	if err != nil {
		return nil, unavailable
	}
	return clients, "Herdr hook integration subset; additional clients need explicit upstream startup and kind evidence."
}

// One bounded public transport serves both capability display and native launch.
func listHerdrIntegrations() ([]clientCapability, error) {
	socket := os.Getenv("HERDR_SOCKET_PATH")
	if socket == "" {
		return nil, errors.New("Herdr integration discovery unavailable")
	}
	c, err := net.DialTimeout("unix", socket, time.Second)
	if err != nil {
		return nil, errors.New("Herdr integration discovery unavailable")
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(2 * time.Second))
	if json.NewEncoder(c).Encode(map[string]any{"id": "harbor-capabilities", "method": "integration.list", "params": map[string]any{}}) != nil {
		return nil, errors.New("Herdr integration discovery unavailable")
	}
	var response struct {
		Result *struct {
			Integrations []clientCapability `json:"integrations"`
		} `json:"result"`
		Error json.RawMessage `json:"error"`
	}
	var body json.RawMessage
	err = json.NewDecoder(io.LimitReader(c, 65537)).Decode(&body)
	if err != nil || len(body) > 65536 || json.Unmarshal(body, &response) != nil || response.Result == nil || len(response.Error) > 0 && string(response.Error) != "null" {
		return nil, errors.New("Herdr integration discovery unavailable")
	}
	return response.Result.Integrations, nil
}

func (m harborModel) clientPickerView() string {
	lines := []string{"🤖 CLI", "", "Herdr hook integration subset (not a complete registry)", ""}
	mark := "  "
	if m.clientCursor == 0 {
		mark = "› "
	}
	lines = append(lines, mark+"Codex  "+orUnknown(m.environment.CodexVersion), "  Existing supported path; native and Magpie configuration available.")
	start := max(0, m.clientCursor-1-max(1, (m.height-17)/2))
	end := min(len(m.environment.Clients), start+max(1, m.height-17))
	for i := start; i < end; i++ {
		c := m.environment.Clients[i]
		mark = "  "
		if m.clientCursor == i+1 {
			mark = "› "
		}
		label := c.Label
		if label == "" {
			label = c.Target
		}
		startup := "Native start: unknown · upstream kind/start contract absent"
		if c.nativeExecutableReady() {
			startup = "Native start: available · development executable subset"
		}
		lines = append(lines, mark+cleanUI(label)+" · "+cleanUI(c.Target))
		if m.clientCursor == i+1 {
			magpie := "Magpie: unavailable · session isolation unproven"
			if c.Target == "codex" {
				magpie = "Magpie: available · existing Codex configuration adapter"
			}
			lines = append(lines, "  Integration: "+orUnknown(c.Integration), "  "+startup, "  Identity: unknown · observe this session through Herdr", "  Recovery: unknown · requires a saved reference and query", "  "+magpie, "  Inspect upstream support or choose supported Codex.")
		}
	}
	lines = append(lines, "", m.environment.ClientDiscovery, "↑↓ move  enter select supported path  esc return")
	return strings.Join(lines, "\n")
}

func (c clientCapability) nativeExecutableReady() bool {
	return c.Available && c.Target != "" && c.Command == c.Target && !strings.ContainsAny(c.Command, " /\\\x00\r\n")
}
func (m harborModel) selectedClient() string {
	if m.draft.Client == "" {
		return "codex"
	}
	return m.draft.Client
}
