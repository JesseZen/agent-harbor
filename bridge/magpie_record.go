package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

type managedRecord struct {
	Address         string    `json:"address"`
	WebAddress      string    `json:"web_address,omitempty"`
	ConfigDir       string    `json:"config_dir"`
	Executable      string    `json:"executable"`
	Version         string    `json:"version,omitempty"`
	CodexCallerOnly bool      `json:"codex_caller_only,omitempty"`
	Digest          string    `json:"executable_sha256,omitempty"`
	PID             int       `json:"pid"`
	StartedAt       time.Time `json:"started_at"`
}

const (
	managedStopOwn          = "own"
	managedStopNotRunning   = "not-running"
	managedStopNotRecorded  = "not-recorded"
	managedStopForeign      = "foreign"
	managedStopUnverifiable = "unverifiable"
)

func readManagedRecord(layout managedLayout) (*managedRecord, error) {
	data, err := os.ReadFile(layout.Record)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var record managedRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, fmt.Errorf("invalid managed Magpie record: %w", err)
	}
	return &record, nil
}

func writeManagedRecord(layout managedLayout, record managedRecord) error {
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(layout.Root, 0700); err != nil {
		return err
	}
	return writeManagedRecordFile(layout.Record, append(data, '\n'))
}

// writeManagedRecordFile persists the ownership record. It is a variable so a
// test can fail the write after the child is ready on any platform; a
// read-only directory is not enough, because root and overlay filesystems
// ignore the permission.
var writeManagedRecordFile = writeStateFile

// recordedProcessFacts reads the recorded pid. It is a variable so a test can
// present unreadable evidence, which on macOS and Linux needs a process owned
// by another user that may not exist on the machine running the tests.
var recordedProcessFacts = readProcessFacts

// ensureWebKey keeps one private key for the managed instance's own page. It
// is generated once and never rotates while the instance exists, so a running
// gateway and its sessions keep working.
func managedProcessEnvironment(layout managedLayout, address, webKey string, startedAt time.Time) []string {
	for _, dir := range []string{layout.Home, filepath.Join(layout.Home, "config"), filepath.Join(layout.Home, "data"), filepath.Join(layout.Home, "state"), filepath.Join(layout.Home, "cache")} {
		_ = os.MkdirAll(dir, 0700)
	}
	updates := map[string]*string{
		processStartedAtVariable: new(startedAt.Format(time.RFC3339Nano)),
		"HOME":                   &layout.Home,
		"CODEX_HOME":             &layout.Home,
		"CODEX_SQLITE_HOME":      &layout.Home,
		"CLAUDE_CONFIG_DIR":      nil,
		"GEMINI_CLI_HOME":        nil,
		"PI_CODING_AGENT_DIR":    nil,
		"OPENCODE_CONFIG":        nil,
		"OPENCODE_CONFIG_DIR":    nil,
		"XDG_CONFIG_HOME":        new(filepath.Join(layout.Home, "config")),
		"XDG_DATA_HOME":          new(filepath.Join(layout.Home, "data")),
		"XDG_STATE_HOME":         new(filepath.Join(layout.Home, "state")),
		"XDG_CACHE_HOME":         new(filepath.Join(layout.Home, "cache")),
		"MAGPIE_ADDR":            new(gatewayAddressOf(address)),
		"MAGPIE_WEB_KEY":         &webKey,
		"MAGPIE_NO_STATS":        new("1"),
	}
	return replaceEnvironment(magpieBaseEnvironment(), updates)
}

// managedChildEnvironment isolates a helper invocation of the copied binary
// without claiming a gateway address.
func managedChildEnvironment(layout managedLayout) []string {
	return replaceEnvironment(magpieBaseEnvironment(), map[string]*string{
		"HOME":                &layout.Home,
		"CODEX_HOME":          &layout.Home,
		"CODEX_SQLITE_HOME":   &layout.Home,
		"CLAUDE_CONFIG_DIR":   nil,
		"GEMINI_CLI_HOME":     nil,
		"PI_CODING_AGENT_DIR": nil,
		"OPENCODE_CONFIG":     nil,
		"OPENCODE_CONFIG_DIR": nil,
		"XDG_CONFIG_HOME":     new(filepath.Join(layout.Home, "config")),
		"XDG_DATA_HOME":       new(filepath.Join(layout.Home, "data")),
		"XDG_STATE_HOME":      new(filepath.Join(layout.Home, "state")),
		"XDG_CACHE_HOME":      new(filepath.Join(layout.Home, "cache")),
	})
}

// installBundledMagpie copies the packaged Magpie beside the managed data
// directory, with the portable marker. The copy is replaced only while no
// managed process is running, because Magpie re-executes itself when its
// executable file is replaced.
func disableManagedExtras(layout managedLayout, address string) error {
	// The marker must exist before any command runs, or Magpie would write
	// settings into the user's installed configuration.
	if err := ensurePortableMarker(layout); err != nil {
		return err
	}
	for _, key := range []string{"noStats", "noAutoUpdate", "noAutoDownload"} {
		cmd := exec.Command(layout.Executable, "settings", "set", key, "on")
		cmd.Env = replaceEnvironment(managedChildEnvironment(layout), map[string]*string{"MAGPIE_ADDR": &address})
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("could not disable %s in the managed Magpie: %v: %s", key, err, out)
		}
	}
	return nil
}

// managedReady is whether a probe names the child Harbor started.
func managedReady(pid int, probe gatewayProbe) bool {
	return probe.State == gatewayVerified && probe.PID == pid
}

// managedStopAction decides whether the running process is the one Harbor
// recorded. A gateway that does not answer is not proof that its process
// exited, so an unreadable or silent gateway keeps the record instead of
// discarding ownership of a process that may still be running.
func managedStopAction(record *managedRecord, probe gatewayProbe, facts processFacts) string {
	if record == nil {
		return managedStopNotRecorded
	}
	if record.PID <= 1 {
		return managedStopNotRunning
	}
	if probe.State == gatewayOffline {
		if !processAlive(record.PID) {
			return managedStopNotRunning
		}
		state, err := readProcessFacts(record.PID)
		if err != nil {
			return managedStopUnverifiable
		}
		// The process is alive but not serving; it is still Harbor's to stop.
		if managedProcessMatchesRecord(*record, state) {
			return managedStopOwn
		}
		return managedStopForeign
	}
	if probe.State != gatewayVerified || probe.PID != record.PID {
		return managedStopForeign
	}
	if !managedProcessMatchesRecord(*record, facts) {
		return managedStopForeign
	}
	return managedStopOwn
}

// processEvidence is what can be established about the recorded child. Gone
// and unreadable are different: a replaced or exited child means the record is
// obsolete, while unreadable evidence means only that ownership is uncertain,
// and certainty is required before signalling or clearing a record.
type processEvidence int

const (
	processEvidenceMatches processEvidence = iota
	processEvidenceGone
	processEvidenceUnreadable
)

// recordedProcessEvidence reports whether the pid still holds the process
// Harbor recorded, has been taken by another process, or cannot be read.
func recordedProcessEvidence(record managedRecord) processEvidence {
	if !processAlive(record.PID) {
		return processEvidenceGone
	}
	facts, err := recordedProcessFacts(record.PID)
	if err != nil {
		if errors.Is(err, errProcessGone) {
			return processEvidenceGone
		}
		// The process exists, so a read failure is uncertainty about which
		// process it is, never proof that the recorded child exited.
		return processEvidenceUnreadable
	}
	if managedProcessMatchesRecord(record, facts) {
		return processEvidenceMatches
	}
	// The pid is alive but belongs to another process, so the recorded child
	// is gone and the record is obsolete.
	return processEvidenceGone
}

// processAlive reports whether a process with this pid exists. A gateway that
// stops answering keeps its process, so listeners are not evidence of exit.
// Another user's process answers with EPERM, which still means it exists.
func processAlive(pid int) bool {
	if pid <= 1 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// managedProcessMatchesRecord is whether the process evidence names the child
// Harbor started: the same executable path and the start time Harbor wrote
// into its environment.
func managedProcessMatchesRecord(record managedRecord, facts processFacts) bool {
	if facts.Executable != record.Executable {
		return false
	}
	return !record.StartedAt.IsZero() && facts.startedAt().Equal(record.StartedAt)
}
