package main

import (
	"bufio"
	"crypto/sha256"
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

func picker() error {
	in := bufio.NewReader(os.Stdin)
	fmt.Printf("%s\n\n  %s  New Codex session\n  %s  Sessions and recovery\n  q  Close\n\n", style("1;36", "Agent Harbor"), style("1;32", "1"), style("1;32", "2"))
	choice, err := readChoice(in, "Choose an action: ")
	if err != nil {
		return err
	}
	switch choice {
	case "1":
		return pickNew(in)
	case "2":
		return pickRecovery(in)
	case "q", "":
		return nil
	default:
		return errors.New("unknown action")
	}
}

func pickNew(in *bufio.Reader) error {
	defaultDir := os.Getenv("HERDR_ACTIVE_PANE_CWD")
	launchDir := os.Getenv("AGENT_HARBOR_BRIDGE_LAUNCH_CWD")
	if launchDir != "" && defaultDir == os.Getenv("HOME") && launchDir != defaultDir {
		fmt.Printf("\nWorking directory\n  Active pane: %q\n  Default:     %q (where Herdr was started)\n", defaultDir, launchDir)
		defaultDir = launchDir
	} else {
		fmt.Printf("\nWorking directory\n  Active pane: %q\n", defaultDir)
	}
	fmt.Println("  Enter a different project path if needed.")
	dir, err := readLine(in, "Path (Enter uses default): ")
	if err != nil {
		return err
	}
	if dir == "" {
		dir = defaultDir
	}
	dir, err = checkDirectory(dir)
	if err != nil {
		return err
	}
	fmt.Print("\nCLI\n  Codex\n\nConnection\n  1  Local Codex configuration and login\n  2  Magpie\n\n")
	mode, err := readChoice(in, "Choose connection [1]: ")
	if err != nil {
		return err
	}
	if mode == "2" {
		return pickMagpie(in, dir)
	}
	if mode != "" && mode != "1" {
		return errors.New("unknown connection")
	}
	s, err := appService.Create(sessionDraft{Directory: dir, Connection: "local"})
	if err != nil {
		return err
	}
	return openSessionPane(s, newConversation)
}

func pickRecovery(in *bufio.Reader) error {
	var all []session
	if err := withSessions(func(s []session) ([]session, error) { all = s; return nil, nil }); err != nil {
		return err
	}
	if len(all) == 0 {
		fmt.Println("\nNo sessions yet.")
		_, err := readChoice(in, "Press Enter to close: ")
		return err
	}
	const pageSize = 3
	page := 0
	for {
		start, end := page*pageSize, min((page+1)*pageSize, len(all))
		fmt.Printf("\n%s  Page %d / %d\n", style("1;36", "Sessions"), page+1, (len(all)+pageSize-1)/pageSize)
		for i := start; i < end; i++ {
			s := all[i]
			status := s.Recovery
			if !s.Started && s.CodexSessionID == "" {
				status = "not started"
			}
			fmt.Printf("\n  %s  %s\n     %q\n     %s / %s / %s\n", style("1;32", strconv.Itoa(i+1)), s.ID, s.WorkingDirectory, s.CLI, s.Mode, style("33", status))
		}
		choice, err := readChoice(in, "\nSession number, n/p for pages (Enter cancels): ")
		if err != nil || choice == "" {
			return err
		}
		if choice == "n" && end < len(all) {
			page++
			continue
		}
		if choice == "p" && page > 0 {
			page--
			continue
		}
		index, err := strconv.Atoi(choice)
		if err != nil || index <= start || index > end {
			return errors.New("invalid session number")
		}
		return recoverSelected(all[index-1], in)
	}
}

func recoverSelected(s session, in *bufio.Reader) error {
	if processGroupActive(s.ProcessGroup) {
		return errors.New("a previous session process is still running or closing")
	}
	if !s.Started && s.CodexSessionID == "" {
		fmt.Printf("\nThis session has not started.\n  Directory  %q\n  Connection %s\n", s.WorkingDirectory, s.Mode)
		if s.Mode == "magpie" {
			if err := checkMagpieModel(s.MagpieURL, s.MagpieModel); err != nil {
				return err
			}
			fmt.Printf("  Address    %s\n  Model      %q\n\nAuxiliary requests may send parts of the conversation to other models or providers.\n", s.MagpieURL, s.MagpieModel)
		}
		answer, err := readChoice(in, "Type yes to start: ")
		if err != nil || answer != "yes" {
			return errors.New("start cancelled")
		}
		return openSessionPane(s, newConversation)
	}
	if s.Recovery != "ready" {
		fmt.Printf("\nThe last identity or directory may have changed while Codex was running.\nOutside managed Herdr, choose the conversation with `codex resume`,\nthen use /status to verify its current Session UUID and directory.\n\n  Previously recorded directory  %q\n  Previously recorded Codex ID   %s\n", s.WorkingDirectory, s.CodexSessionID)
		if s.ReportedCodexID != "" {
			fmt.Printf("  Later reported Codex ID       %s\n", s.ReportedCodexID)
		}
		id, err := readChoice(in, "\nVerified Codex UUID (Enter cancels): ")
		if err != nil || id == "" {
			return err
		}
		dir, err := readLine(in, "Verified directory from /status: ")
		if err != nil {
			return err
		}
		dir, err = checkDirectory(dir)
		if err != nil {
			return err
		}
		candidate := s
		candidate.CodexSessionID, candidate.WorkingDirectory, candidate.Recovery = id, dir, "ready"
		if err := confirmRecoveryWithReader(candidate, in); err != nil {
			return err
		}
		if err := appService.Verify(s, id, dir); err != nil {
			return err
		}
		s, err = getSession(s.ID)
		if err != nil {
			return err
		}
	} else if err := confirmRecoveryWithReader(s, in); err != nil {
		return err
	}
	return openSessionPane(s, resumeConversation)
}

func confirmRecovery(s session) error { return confirmRecoveryWithReader(s, bufio.NewReader(os.Stdin)) }

func confirmRecoveryWithReader(s session, in *bufio.Reader) error {
	return confirmRecoveryReview(s, in, "")
}
func confirmRecoveryReview(s session, in *bufio.Reader, override string) error {
	if err := checkSession(s, resumeConversation, override); err != nil {
		return err
	}
	if s.Recovery != "ready" {
		return errors.New("session identity and directory need verification before recovery")
	}
	if (s.CLI != "codex" && !candidateNeutral(s)) || (s.Mode != "local" && s.Mode != "magpie") {
		return errors.New("this session's CLI or connection mode is unavailable")
	}
	fmt.Println("External writers, login and conversation existence are not verified. Confirm this conversation is not open elsewhere.")
	fmt.Printf("\nReview recovery\n\n  Session     %s\n  Directory   %q\n  CLI         %s\n  Reference   %v\n", s.ID, s.WorkingDirectory, s.CLI, s.Conversation.Reference)
	if s.Mode == "magpie" {
		fmt.Printf("  Connection  Magpie %s\n  Model       %q\n\nCheck in Magpie that this ID still points to the intended connection. Group members may change.\n", s.MagpieURL, s.MagpieModel)
		fmt.Println("Auxiliary requests may send parts of the conversation to other models or providers.")
	} else {
		fmt.Println("  Connection  Local Codex configuration and login\n\nCheck that the directory and conversation match. Local configuration may have changed.")
	}
	answer, err := readChoice(in, "Type yes to resume: ")
	if err != nil {
		return err
	}
	if answer != "yes" {
		return errors.New("recovery cancelled")
	}
	return nil
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

func openSessionPane(s session, action launchKind) error { return openPaneOptions(s, action, "") }
func openQueryPane(s session, override string) error {
	return openPaneOptions(s, launchKind("query"), override)
}
func openPaneOptions(s session, action launchKind, override string) error {
	herdr := os.Getenv("HERDR_BIN_PATH")
	if herdr == "" || os.Getenv("HERDR_SOCKET_PATH") == "" {
		return errors.New("open the session picker from managed Herdr")
	}
	entrypoint := "codex"
	if candidateNeutral(s) {
		entrypoint = "session"
	}
	cmd := exec.Command(herdr, "plugin", "pane", "open", "--plugin", "agent-harbor.bridge", "--entrypoint", entrypoint, "--placement", "tab", "--cwd", s.WorkingDirectory, "--env", "AGENT_HARBOR_BRIDGE_SESSION="+s.ID, "--env", "AGENT_HARBOR_BRIDGE_ACTION="+string(action), "--env", "AGENT_HARBOR_BRIDGE_EXPECTED="+sessionStamp(s), "--env", "AGENT_HARBOR_BRIDGE_OVERRIDE="+override, "--focus")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("could not open the Herdr session pane: %w", err)
	}
	var result struct {
		Result struct {
			PluginPane struct {
				Pane struct {
					TabID string `json:"tab_id"`
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
	if action == "query" {
		err = appService.Resume(sessionApproval{Session: s, Confirmed: true, DevelopmentQuery: true, QueryUncertaintyAccepted: true, OverrideReason: os.Getenv("AGENT_HARBOR_BRIDGE_OVERRIDE")})
	} else if action == "resume" {
		err = appService.Resume(sessionApproval{Session: s, Confirmed: true})
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
