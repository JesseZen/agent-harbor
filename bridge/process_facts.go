package main

import (
	"errors"
	"strings"
	"time"
)

// Reading another process's executable and a few path variables is the only
// evidence Harbor accepts for a gateway's configuration location. The
// variables below are the ones Magpie itself builds its configuration path
// from; nothing else is kept, logged or reported.
var errProcessGone = errors.New("process is gone")
var errProcessDifferentUser = errors.New("process belongs to another user")
var errProcessUnsupported = errors.New("process evidence is unsupported on this platform")
var errProcessUnreadable = errors.New("process evidence is not in the expected form")
var errProcessReplaced = errors.New("process executable was replaced after it started")

type processFacts struct {
	PID        int
	Executable string
	Variables  map[string]string
}

// processStartedAtVariable carries the time Harbor started the managed child.
// A record's PID can be reused by an unrelated Magpie after the child exits,
// so the started time, not the PID alone, is what marks the child as Harbor's.
const processStartedAtVariable = "AH_MANAGED_STARTED_AT"

// startedAt reports when the process was started, as selected from its own
// environment. It is empty when the process did not come from Harbor.
func (f processFacts) startedAt() time.Time {
	value := f.Variables[processStartedAtVariable]
	started, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return started.UTC()
}

var keptProcessVariables = []string{"MAGPIE_CODEX_CALLER_ONLY", "AH_PROFILE_WORKSPACE", "AH_PROFILE_ID", "AH_PROFILE_ENVIRONMENT", "HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "CODEX_HOME", "CODEX_SQLITE_HOME", "CLAUDE_CONFIG_DIR", "GEMINI_CLI_HOME", "PI_CODING_AGENT_DIR", "OPENCODE_CONFIG", "OPENCODE_CONFIG_DIR", "MAGPIE_ADDR", "APPIMAGE", "APPDIR", "AH_MANAGED_STARTED_AT", "AH_SESSION_RUN_TOKEN", "AGENT_HARBOR_BRIDGE_SESSION", "HERDR_PANE_ID", "HERDR_SOCKET_PATH"}

// selectProcessVariables keeps only the variables Harbor reasons from.
func selectProcessVariables(environ []string) map[string]string {
	selected := map[string]string{}
	for _, entry := range environ {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		for _, name := range keptProcessVariables {
			if key == name {
				selected[key] = value
				break
			}
		}
	}
	return selected
}
