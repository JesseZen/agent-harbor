package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func confirmRecoveryReview(s session, in *bufio.Reader, override string) (*codexSelection, error) {
	if err := checkSessionBase(s, resumeConversation, override, false); err != nil {
		return nil, err
	}
	var selection *codexSelection
	if s.Mode == "magpie" && s.CLI == "codex" && !joinedCodex(s) {
		last, err := appService.CodexSelection(context.Background(), s)
		if err != nil {
			if !errors.Is(err, errCodexProfileMissing) {
				return nil, err
			}
			id, referenceErr := codexReference(s)
			if referenceErr != nil {
				return nil, referenceErr
			}
			fmt.Printf("Codex's session profile is unavailable: %v. Harbor originally recorded %q; later selections are unknown.\n", err, s.MagpieModel)
			answer, readErr := readChoice(in, "Type recorded to explicitly use Harbor's recorded model, or anything else to cancel: ")
			if readErr != nil || answer != "recorded" {
				return nil, errors.New("model selection review cancelled")
			}
			last = codexSelection{ThreadID: id, Model: s.MagpieModel, Fallback: true}
		}
		selection = &last
	}
	if err := checkSessionSelection(s, resumeConversation, override, selection); err != nil {
		return nil, err
	}
	if s.Recovery != "ready" {
		return nil, errors.New("session identity and directory need verification before recovery")
	}
	if (s.CLI != "codex" && !candidateNeutral(s)) || (s.Mode != "local" && s.Mode != "magpie") {
		return nil, errors.New("this session's CLI or connection mode is unavailable")
	}
	fmt.Println("External writers, login and conversation existence are not verified. Confirm this conversation is not open elsewhere.")
	fmt.Printf("\nReview recovery\n\n  Session     %s\n  Directory   %q\n  CLI         %s\n  Reference   %v\n", s.ID, s.WorkingDirectory, s.CLI, s.Conversation.Reference)
	if s.Mode == "magpie" {
		if joinedCodex(s) {
			fmt.Printf("  Connection  Magpie %s\n  Model       restored by Codex\n\n", s.MagpieURL)
		} else {
			model, effort := s.MagpieModel, "not recorded"
			if selection != nil {
				model, effort = selection.Model, orUnknown(selection.Effort)
			}
			fmt.Printf("  Connection  Magpie %s\n  Model       %q\n  Effort      %s\n\nCheck in Magpie that this ID still points to the intended connection. Group members may change.\n", s.MagpieURL, model, effort)
		}
		fmt.Println("Auxiliary requests may send parts of the conversation to other models or providers.")
	} else {
		fmt.Printf("  Connection  Native %s configuration and login\n\nCheck that the directory and conversation match. Native route or login may have changed; verify both. A provider name does not verify its destination.\n", s.CLI)
		if s.CLI == "codex" {
			environment, err := sessionClientEnvironment(s)
			if err != nil {
				return nil, err
			}
			fmt.Println(nativeCodexConfigSummary(environment.Environment))
		}
	}
	answer, err := readChoice(in, "Type yes to resume: ")
	if err != nil {
		return nil, err
	}
	if answer != "yes" {
		return nil, errors.New("recovery cancelled")
	}
	return selection, nil
}

func readChoice(in *bufio.Reader, prompt string) (string, error) {
	line, err := readLine(in, prompt)
	return strings.TrimSpace(line), err
}

func readLine(in *bufio.Reader, prompt string) (string, error) {
	fmt.Print(prompt)
	line, err := in.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), nil
}

func style(code, text string) string {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return text
	}
	return "\033[" + code + "m" + text + "\033[0m"
}

func sessionStamp(s session) string {
	data, _ := json.Marshal(s)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func openSessionPane(s session, action launchKind) error { return openPaneOptions(s, action, "", nil) }
func openQueryPane(s session, override string, selection *codexSelection) error {
	return openPaneOptions(s, launchKind("query"), override, selection)
}
func openPaneOptions(s session, action launchKind, override string, selection *codexSelection) error {
	if err := waitForLaunchState(s); err != nil {
		return err
	}
	herdr := os.Getenv("HERDR_BIN_PATH")
	if herdr == "" || os.Getenv("HERDR_SOCKET_PATH") == "" {
		return errors.New("open the session picker from managed Herdr")
	}
	entrypoint := "codex"
	if candidateNeutral(s) {
		entrypoint = "session"
	}
	args := []string{"plugin", "pane", "open", "--plugin", "agent-harbor.bridge", "--entrypoint", entrypoint, "--placement", "tab", "--cwd", s.WorkingDirectory, "--env", "AGENT_HARBOR_BRIDGE_SESSION=" + s.ID, "--env", "AGENT_HARBOR_BRIDGE_ACTION=" + string(action), "--env", "AGENT_HARBOR_BRIDGE_EXPECTED=" + sessionStamp(s), "--env", "AGENT_HARBOR_BRIDGE_OVERRIDE=" + override}
	if selection != nil {
		data, err := json.Marshal(selection)
		if err != nil || len(data) > 4096 {
			return errors.New("invalid reviewed Codex model selection")
		}
		args = append(args, "--env", "AGENT_HARBOR_BRIDGE_SELECTION="+base64.RawURLEncoding.EncodeToString(data))
	}
	args = append(args, "--focus")
	cmd := exec.Command(herdr, args...)
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("could not open the Herdr session pane: %w", err)
	}
	var result struct {
		Result struct {
			PluginPane struct {
				Pane struct {
					PaneID string `json:"pane_id"`
					TabID  string `json:"tab_id"`
				} `json:"pane"`
			} `json:"plugin_pane"`
		} `json:"result"`
	}
	if json.Unmarshal(out, &result) != nil || result.Result.PluginPane.Pane.TabID == "" {
		return errors.New("Herdr did not return the new session tab")
	}
	// The returned tab belongs to this just-opened plugin pane.
	if err := exec.Command(herdr, "tab", "rename", result.Result.PluginPane.Pane.TabID, s.Name).Run(); err != nil {
		return errors.New("session opened, but its tab name could not be set")
	}
	if result.Result.PluginPane.Pane.PaneID != "" {
		if err := exec.Command(herdr, "pane", "rename", result.Result.PluginPane.Pane.PaneID, s.Name).Run(); err != nil {
			return errors.New("session opened, but its pane name could not be set")
		}
	}
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	helper := exec.Command(bin, "_focus", strconv.Itoa(os.Getpid()), result.Result.PluginPane.Pane.TabID)
	helper.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := helper.Start(); err != nil {
		return fmt.Errorf("session opened but could not focus its tab: %w", err)
	}
	return helper.Process.Release()
}

// Herdr restores the old client focus when a popup closes, so focus the new tab
// only after the picker process has exited.
func focusAfterPopup(args []string) error {
	if len(args) != 2 {
		return errors.New("invalid focus request")
	}
	pid, err := strconv.Atoi(args[0])
	if err != nil || pid <= 1 {
		return errors.New("invalid picker process")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			herdr := os.Getenv("HERDR_BIN_PATH")
			if herdr == "" || os.Getenv("HERDR_SOCKET_PATH") == "" {
				return errors.New("managed Herdr context is missing")
			}
			return exec.Command(herdr, "tab", "focus", args[1]).Run()
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errors.New("picker did not close")
}

func herdrPane() error {
	id, action := os.Getenv("AGENT_HARBOR_BRIDGE_SESSION"), os.Getenv("AGENT_HARBOR_BRIDGE_ACTION")
	if action != "new" && action != "resume" && action != "query" {
		return errors.New("invalid pane action")
	}
	s, err := getSession(id)
	if err != nil {
		return err
	}
	if os.Getenv("AGENT_HARBOR_BRIDGE_EXPECTED") != sessionStamp(s) {
		return errors.New("session changed; open the menu and review it again")
	}
	fmt.Printf("Agent Harbor session %s\n\n", s.ID)
	var selection *codexSelection
	if encoded := os.Getenv("AGENT_HARBOR_BRIDGE_SELECTION"); encoded != "" {
		data, err := base64.RawURLEncoding.DecodeString(encoded)
		if err != nil || len(data) > 4096 {
			return errors.New("invalid reviewed Codex model selection")
		}
		var value codexSelection
		if json.Unmarshal(data, &value) != nil {
			return errors.New("invalid reviewed Codex model selection")
		}
		selection = &value
	}
	if action == "query" {
		err = appService.Resume(sessionApproval{Session: s, MagpieSelection: selection, Confirmed: true, DevelopmentQuery: true, QueryUncertaintyAccepted: true, OverrideReason: os.Getenv("AGENT_HARBOR_BRIDGE_OVERRIDE")})
	} else if action == "resume" {
		err = appService.Resume(sessionApproval{Session: s, MagpieSelection: selection, Confirmed: true})
	} else {
		err = appService.Start(sessionApproval{Session: s, Confirmed: true})
	}
	if err == nil && action == "new" {
		current, readErr := getSession(s.ID)
		if readErr != nil {
			return readErr
		}
		if current.CLI == "codex" && current.CodexSessionID == "" {
			_, _ = readChoice(bufio.NewReader(os.Stdin), "Press Enter after reading the recovery instructions: ")
		}
	}
	return err
}

func interactiveResult(err error) error {
	if err == nil {
		return nil
	}
	fmt.Fprintf(os.Stderr, "\n\033[1;31mError\033[0m\n  %v\n\nRecovery\n  Check the session details and try again.\n\n", err)
	_, _ = readChoice(bufio.NewReader(os.Stdin), "Press Enter to close: ")
	return nil
}
