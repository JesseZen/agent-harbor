package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type sessionOverride struct {
	At     time.Time `json:"at"`
	Reason string    `json:"reason"`
}

type candidateEvidence struct {
	ID        string    `json:"id"`
	Directory string    `json:"directory"`
	LastSeen  time.Time `json:"last_seen"`
}

type endedEvidence struct {
	Source         string         `json:"source"`
	At             time.Time      `json:"at"`
	StartedAt      *time.Time     `json:"started_at,omitempty"`
	Reference      *herdrIdentity `json:"reference,omitempty"`
	RunToken       string         `json:"run_token,omitempty"`
	SocketIdentity string         `json:"socket_identity"`
	Pane           string         `json:"pane"`
}

type session struct {
	Profile   *profileBinding `json:"profile,omitempty"`
	RunnerPID int             `json:"runner_pid,omitempty"`
	Ended     *endedEvidence  `json:"ended_evidence,omitempty"`

	Reassociations []reassociationDecision `json:"reassociations,omitempty"`
	Conversation   *conversationEvidence   `json:"conversation,omitempty"`

	HerdrAgentName       string              `json:"herdr_agent_name,omitempty"`
	Candidates           []candidateEvidence `json:"candidate_evidence,omitempty"`
	CLIVersion           string              `json:"cli_version,omitempty"`
	HerdrSocket          string              `json:"herdr_socket,omitempty"`
	HerdrSocketIdentity  string              `json:"herdr_socket_identity,omitempty"`
	RunToken             string              `json:"run_token,omitempty"`
	CodexHome            string              `json:"codex_home,omitempty"`
	CodexServerPID       int                 `json:"codex_server_pid,omitempty"`
	CodexServerSocket    string              `json:"codex_server_socket,omitempty"`
	CodexServerSocketID  string              `json:"codex_server_socket_identity,omitempty"`
	IdentityWarning      string              `json:"identity_warning,omitempty"`
	NotifyWarning        string              `json:"notify_warning,omitempty"`
	LastActivityAt       *time.Time          `json:"last_activity_at,omitempty"`
	Version              int                 `json:"version"`
	Name                 string              `json:"name"`
	InstanceID           string              `json:"instance_id,omitempty"`
	InstanceSnapshot     *magpieInstance     `json:"instance_snapshot,omitempty"`
	ConversationIDSource string              `json:"conversation_id_source,omitempty"`
	CandidateIDs         []string            `json:"candidate_ids,omitempty"`
	HerdrPane            string              `json:"herdr_pane,omitempty"`
	HerdrTab             string              `json:"herdr_tab,omitempty"`
	LastStartedAt        *time.Time          `json:"last_started_at,omitempty"`
	LastExitAt           *time.Time          `json:"last_exit_at,omitempty"`
	LastExitCode         *int                `json:"last_exit_code,omitempty"`
	Overrides            []sessionOverride   `json:"overrides,omitempty"`
	ID                   string              `json:"id"`
	WorkingDirectory     string              `json:"working_directory"`
	CLI                  string              `json:"cli"`
	Mode                 string              `json:"mode"`
	CodexConnection      string              `json:"codex_connection,omitempty"`
	MagpieURL            string              `json:"magpie_url,omitempty"`
	MagpieModel          string              `json:"magpie_model,omitempty"`
	CodexSessionID       string              `json:"codex_session_id,omitempty"`
	ReportedCodexID      string              `json:"reported_codex_session_id,omitempty"`
	Recovery             string              `json:"recovery"`
	Started              bool                `json:"started"`
	ProcessGroup         int                 `json:"process_group,omitempty"`
	CreatedAt            time.Time           `json:"created_at"`
}

func bridgeHome() (string, error) {
	root := os.Getenv("AGENT_HARBOR_BRIDGE_HOME")
	if root == "" {
		state := os.Getenv("XDG_STATE_HOME")
		if state == "" {
			home, err := os.UserHomeDir()
			if err != nil || home == "" {
				return "", errors.New("HOME is required to find bridge state")
			}
			state = filepath.Join(home, ".local", "state")
		}
		root = filepath.Join(state, "agent-harbor-bridge")
	}
	if !filepath.IsAbs(root) {
		return "", errors.New("bridge state directory must be absolute")
	}
	return filepath.Clean(root), nil
}

func ensureHome(root string) error {
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("bridge state path is not a directory")
	}
	if info.Mode().Perm() != 0700 {
		return errors.New("bridge state directory must be private (mode 0700); choose a dedicated directory")
	}
	dir, err := os.Open(root)
	if err != nil {
		return err
	}
	defer dir.Close()
	if err := syscall.Flock(int(dir.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(dir.Fd()), syscall.LOCK_UN)
	const markerContent = "agent-harbor-bridge-state-v1\n"
	marker := filepath.Join(root, ".agent-harbor-bridge")
	markerInfo, err := os.Lstat(marker)
	if os.IsNotExist(err) {
		entries, readErr := os.ReadDir(root)
		if readErr != nil {
			return readErr
		}
		if len(entries) != 0 {
			return errors.New("bridge state directory is not empty and has no ownership marker; choose a dedicated directory")
		}
		f, createErr := os.OpenFile(marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if createErr == nil {
			_, createErr = f.WriteString(markerContent)
			if closeErr := f.Close(); createErr == nil {
				createErr = closeErr
			}
			return createErr
		}
		if !os.IsExist(createErr) {
			return createErr
		}
		markerInfo, err = os.Lstat(marker)
	}
	if err != nil {
		return err
	}
	if !markerInfo.Mode().IsRegular() || markerInfo.Mode().Perm() != 0600 {
		return errors.New("invalid bridge state ownership marker")
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		return err
	}
	if string(data) != markerContent {
		return errors.New("invalid bridge state ownership marker")
	}
	return nil
}

// withSessions serializes readers and writers; a successful update is atomic.
func withSessions(update func([]session) ([]session, error)) error {
	root, err := bridgeHome()
	if err != nil {
		return err
	}
	if err := ensureHome(root); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(root, "sessions.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)

	path := filepath.Join(root, "sessions.json")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	sessions := []session{}
	if err == nil {
		if err := json.Unmarshal(data, &sessions); err != nil {
			return fmt.Errorf("invalid session state: %w", err)
		}
		if sessions == nil {
			return errors.New("invalid session state: expected an array")
		}
	}
	migrated := false
	for i := range sessions {
		if sessions[i].Version > 3 {
			return errors.New("session state version is newer than this Harbor")
		}
		if sessions[i].Version < 2 {
			sessions[i].Version = 2
			sessions[i].Name = filepath.Base(sessions[i].WorkingDirectory) + " · " + sessions[i].Mode
			if sessions[i].CodexSessionID != "" {
				sessions[i].ConversationIDSource = "legacy-unverified"
			}
			if sessions[i].ReportedCodexID != "" && sessions[i].ReportedCodexID != sessions[i].CodexSessionID {
				sessions[i].CandidateIDs = []string{sessions[i].ReportedCodexID}
			}
			migrated = true
		}
	}
	// Persist migrations while holding the same lock, including read-only calls.
	if migrated {
		encoded, err := json.MarshalIndent(sessions, "", "  ")
		if err != nil {
			return err
		}
		if err := writeStateFile(path, append(encoded, '\n')); err != nil {
			return err
		}
	}
	next, err := update(sessions)
	if err != nil || next == nil {
		return err
	}
	data, err = json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	return writeStateFile(path, append(data, '\n'))
}

// writeStateFile replaces path atomically and durably so an interrupted
// write leaves the previous state intact.
func writeStateFile(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".state-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func newSession(dir string) (session, error) {
	return createSession(dir, "local", "", "")
}

func createSession(dir, mode, address, model string) (session, error) {
	return createSessionWithInstance(dir, mode, address, model, nil)
}

func createSessionWithInstance(dir, mode, address, model string, selected *magpieInstance) (session, error) {
	return createSessionIntent(sessionDraft{Directory: dir, Connection: mode, Model: model, Client: "codex"}, address, selected)
}

func createSessionIntent(d sessionDraft, address string, selected *magpieInstance) (session, error) {
	if d.Profile != nil {
		return session{}, ErrProfileNotReady
	}
	dir, mode, model := d.Directory, d.Connection, d.Model
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return session{}, err
	}
	s := session{Version: 2, Name: filepath.Base(dir) + " · " + mode, ID: hex.EncodeToString(id[:]), WorkingDirectory: dir, CLI: d.Client, Mode: mode, MagpieURL: address, MagpieModel: model, Recovery: "needs_id", CreatedAt: time.Now().UTC()}
	if mode == "magpie" && d.Client == "codex" {
		s.CodexConnection = "caller"
	}
	if d.Name != "" {
		s.Name = d.Name
	}
	s.HerdrAgentName = agentName(s.Name, s.ID)
	if selected != nil {
		copy := *selected
		s.InstanceID, s.InstanceSnapshot = copy.ID, &copy
	} else if mode == "magpie" {
		probe := probeGateway(address)
		if probe.State != gatewayVerified {
			s.InstanceID = instanceID("external", address)
			s.InstanceSnapshot = &magpieInstance{ID: s.InstanceID, Kind: "external", Address: address}
		}
		if probe.State == gatewayVerified {
			registry, err := loadMagpieRegistry()
			if err != nil {
				return session{}, err
			}
			for _, instance := range registry.Instances {
				if gatewayAddressOf(instance.Address) == gatewayAddressOf(address) {
					copy := instance
					s.InstanceID = instance.ID
					s.InstanceSnapshot = &copy
					break
				}
			}
			if s.InstanceSnapshot == nil {
				instance, err := bindExternalMagpie(probe)
				if err != nil {
					return session{}, err
				}
				s.InstanceID = instance.ID
				s.InstanceSnapshot = &instance
			}
		}
	}
	err := withSessions(func(all []session) ([]session, error) { return append(all, s), nil })
	return s, err
}

func getSession(id string) (session, error) {
	if !validID(id) {
		return session{}, errors.New("invalid bridge session ID")
	}
	var found session
	err := withSessions(func(all []session) ([]session, error) {
		for _, s := range all {
			if s.ID == id {
				found = s
				return nil, nil
			}
		}
		return nil, errors.New("session not found")
	})
	return found, err
}

func bindSession(id, codexID string) error {
	if !validID(id) {
		return errors.New("invalid bridge session ID")
	}
	if !validCodexID(codexID) {
		return errors.New("Codex session ID must be a UUID")
	}
	return withSessions(func(all []session) ([]session, error) {
		for i := range all {
			if all[i].ID == id {
				if all[i].CodexSessionID != "" && all[i].CodexSessionID != codexID {
					return nil, errors.New("session already has a different Codex session ID")
				}
				all[i].CodexSessionID, all[i].Recovery = codexID, "ready"
				all[i].ConversationIDSource = "manual"
				if processGroupActive(all[i].ProcessGroup) {
					all[i].Recovery = "needs_review"
				}
				return all, nil
			}
		}
		return nil, errors.New("session not found")
	})
}

func validID(id string) bool {
	if len(id) != 32 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil && strings.ToLower(id) == id
}

func validCodexID(id string) bool {
	if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		return false
	}
	_, err := hex.DecodeString(id[:8] + id[9:13] + id[14:18] + id[19:23] + id[24:])
	return err == nil
}
