package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func launch(s session, kind launchKind) error { return launchReviewed(s, kind, "") }

func launchReviewed(s session, kind launchKind, overrideReason string) error {
	return launchWithPlan(s, kind, overrideReason, nil)
}
func launchWithPlan(s session, kind launchKind, overrideReason string, query *resumeCommandPlan) error {
	return launchWithSelection(s, kind, overrideReason, query, nil)
}
func launchWithSelection(s session, kind launchKind, overrideReason string, query *resumeCommandPlan, selection *codexSelection) error {
	if kind == resumeConversation && query == nil {
		return errors.New("replacement recovery requires explicit upstream query; use resume --development-query")
	}
	if kind == resumeConversation && s.Mode == "magpie" && s.CLI == "codex" && !joinedCodex(s) {
		if err := verifyCodexSelection(context.Background(), s, selection); err != nil {
			return err
		}
	}
	if err := checkSessionSelection(s, kind, overrideReason, selection); err != nil {
		return err
	}
	neutral := candidateNeutral(s)
	if (s.CLI != "codex" && !neutral) || (neutral && s.Mode != "local") || (s.Mode != "local" && s.Mode != "magpie") {
		return errors.New("this session's CLI or connection mode is unavailable")
	}
	lock, err := lockSession(s.ID)
	if err != nil {
		return err
	}
	defer lock.Close()
	program := "codex"
	if neutral {
		if os.Getenv("HERDR_PLUGIN_ID") != "agent-harbor.bridge" || os.Getenv("HERDR_PLUGIN_ENTRYPOINT_ID") != "session" || os.Getenv("HERDR_PANE_ID") == "" {
			return errors.New("native client launch requires a managed Herdr session pane")
		}
		root, e := bridgeHome()
		if e != nil {
			return e
		}
		if !managedSocket(root, os.Getenv("HERDR_SOCKET_PATH")) {
			return errors.New("native client launch requires the managed workspace socket")
		}
		var err error
		program, err = nativeClientProgram(s.CLI)
		if err != nil {
			return err
		}
	}
	if _, err := exec.LookPath(program); err != nil {
		return fmt.Errorf("client %q was not found on PATH", program)
	}
	args := []string{}
	if joinedCodex(s) {
		args = append(args, joinedCodexArgs(s, kind)...)
	} else if s.Mode == "magpie" && s.CodexConnection == "api" {
		args = append(args, apiOnlyCodexArgs(s, kind)...)
	} else if s.Mode == "magpie" {
		model := s.MagpieModel
		if kind == resumeConversation && selection != nil {
			model = selection.Model
		}
		base := strings.TrimRight(s.MagpieURL, "/")
		base = strings.TrimSuffix(base, "/v1") + "/v1"
		for _, setting := range []string{
			"model_provider=" + strconv.Quote("agent_harbor_magpie"),
			"model=" + strconv.Quote(model),
			"model_providers.agent_harbor_magpie.name=" + strconv.Quote("Magpie"),
			"model_providers.agent_harbor_magpie.base_url=" + strconv.Quote(base),
			"model_providers.agent_harbor_magpie.wire_api=" + strconv.Quote("responses"),
			"model_providers.agent_harbor_magpie.env_key=" + strconv.Quote("AGENT_HARBOR_MAGPIE_KEY"),
			"model_providers.agent_harbor_magpie.supports_websockets=false",
		} {
			args = append(args, "-c", setting)
		}
		if kind == resumeConversation && selection != nil && selection.Effort != "" {
			args = append(args, "-c", "model_reasoning_effort="+strconv.Quote(selection.Effort))
		}
	}
	// The remote App Server needs the same connection overrides as the TUI.
	serverSettings := append([]string(nil), args...)
	if query != nil {
		args = append(args, query.Argv[1:]...)
	} else if kind == resumeConversation && neutral {
		return errors.New("native recovery requires explicit upstream query; use resume --development-query")
	}
	binary := program
	if !neutral {
		if native, err := nativeCodexPath(); err == nil {
			binary = native
		} else {
			return err
		}
	}
	if neutral {
		resolved, e := exec.LookPath(binary)
		if e != nil || !nativeExecutable(resolved) {
			return errors.New("replacement discovery requires a native executable on PATH; install the supported client native binary explicitly")
		}
		binary = resolved
	}
	cmd := exec.Command(binary, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Dir, cmd.Stdin, cmd.Stdout, cmd.Stderr = s.WorkingDirectory, os.Stdin, os.Stdout, os.Stderr
	cmd.Env, err = userEnvironment()
	if err != nil {
		return err
	}
	cmd.Env = append(cmd.Env, "PWD="+s.WorkingDirectory)
	if os.Getenv("HERDR_PLUGIN_ID") == "agent-harbor.bridge" {
		root, err := bridgeHome()
		if err != nil {
			return err
		}
		wrapper := filepath.Join(root, "herdr", "bin", "herdr")
		path := filepath.Dir(wrapper) + string(os.PathListSeparator) + os.Getenv("PATH")
		cmd.Env = replaceEnvironment(cmd.Env, map[string]*string{"HERDR_BIN": &wrapper, "PATH": &path})
	}
	if s.Mode == "magpie" && !joinedCodex(s) {
		key := magpieCodexKey
		cmd.Env = replaceEnvironment(cmd.Env, map[string]*string{"AGENT_HARBOR_MAGPIE_KEY": &key})
		_, _, _, err := magpieCodexProfilePath(s.ID, cmd.Env)
		if err != nil {
			return err
		}
		settings := []string{"--profile", "agent-harbor-" + s.ID}
		root, err := bridgeHome()
		if err != nil {
			return err
		}
		model := s.MagpieModel
		effort := ""
		if kind == resumeConversation && selection != nil {
			model = selection.Model
			effort = selection.Effort
		}
		catalog, catalogErr := magpieCodexCatalog(s.MagpieURL, root, model, effort)
		if catalogErr == nil {
			defer os.Remove(catalog)
			settings = append(settings, "-c", "model_catalog_json="+strconv.Quote(catalog))
		} else {
			if errors.Is(catalogErr, errMagpieCodexModelHidden) || errors.Is(catalogErr, errMagpieCodexEffortHidden) {
				return catalogErr
			}
			fmt.Fprintln(os.Stderr, "Warning:", catalogErr, "; Codex's model picker may not show Magpie models. Review the instance before changing models.")
			choice, readErr := readChoice(bufio.NewReader(os.Stdin), "Type continue to open with this limitation, or anything else to cancel: ")
			if readErr != nil || choice != "continue" {
				return errors.New("Magpie catalog unavailable; opening with an uncertain model picker was not accepted")
			}
		}
		cmd.Args = append(append([]string{cmd.Path}, settings...), cmd.Args[1:]...)
	}
	experimentalAccounts := !neutral && (s.Mode == "local" || joinedCodex(s)) && os.Getenv("AGENT_HARBOR_EXPERIMENTAL_CODEX_ACCOUNTS") == "1"
	if !neutral && !experimentalAccounts {
		// Explicit per-launch configuration keeps native startup updates disabled.
		cmd.Args = append([]string{cmd.Path, "--no-daemon", "-c", "check_for_update_on_startup=false"}, cmd.Args[1:]...)
	}
	plan, err := candidateRunPlan(s, cmd.Env)
	if err != nil {
		return err
	}
	cmd.Env = append(cmd.Env, "AH_SESSION_RUN_TOKEN="+plan.Token)
	if kind == resumeConversation && s.Mode == "magpie" && s.CLI == "codex" && !joinedCodex(s) {
		if err := verifyCodexSelection(context.Background(), s, selection); err != nil {
			return err
		}
	}
	if err := checkSessionSelection(s, kind, overrideReason, selection); err != nil {
		return err
	}
	if err := waitForLaunchState(s); err != nil {
		return err
	}
	if s.Mode == "magpie" && !joinedCodex(s) {
		model := s.MagpieModel
		if kind == resumeConversation && selection != nil {
			model = selection.Model
		}
		if _, err := magpieCodexProfile(s.ID, model, cmd.Env); err != nil {
			return err
		}
	}
	if err := beginRun(s, kind, plan); err != nil {
		return err
	}
	var accountServer *ownedCodexServer
	if experimentalAccounts {
		accountServer, err = startOwnedCodexServer(binary, serverSettings, s.WorkingDirectory, cmd.Env)
		if err != nil {
			if rollbackErr := undoFailedStart(s, plan); rollbackErr != nil {
				return fmt.Errorf("Codex App Server could not start: %v; session state needs review: %w", err, rollbackErr)
			}
			return err
		}
		defer accountServer.Close()
		stopSignalWatch := watchCodexServerSignals(accountServer)
		defer stopSignalWatch()
		cmd.Args = append([]string{cmd.Path, "--remote", "unix://" + accountServer.socket, "-c", "check_for_update_on_startup=false"}, cmd.Args[1:]...)
	}
	version := ""
	if !neutral {
		version = codexVersion(cmd.Env)
	}
	activateCapture, finishCapture := candidateIdentityCapture(s.ID, plan.Token, s.CLI)
	defer func() {
		if finishCapture != nil {
			_ = finishCapture()
		}
	}()
	err = cmd.Start()
	if err != nil {
		if rollbackErr := undoFailedStart(s, plan); rollbackErr != nil {
			return fmt.Errorf("client could not start: %v; could not restore session state: %w", err, rollbackErr)
		}
		return fmt.Errorf("client could not start; retry this session: %w", err)
	}
	if activateCapture != nil {
		activateCapture()
	}
	if err := withSessions(func(all []session) ([]session, error) {
		for i := range all {
			if all[i].ID == s.ID && all[i].RunToken == plan.Token {
				now := time.Now().UTC()
				all[i].ProcessGroup = cmd.Process.Pid
				all[i].RunnerPID = os.Getpid()
				all[i].LastStartedAt = &now
				all[i].CLIVersion = version
				all[i].HerdrPane = os.Getenv("HERDR_PANE_ID")
				all[i].HerdrTab = os.Getenv("HERDR_TAB_ID")
				all[i].HerdrSocket = os.Getenv("HERDR_SOCKET_PATH")
				all[i].HerdrSocketIdentity = socketIdentity(all[i].HerdrSocket)
				all[i].CodexServerPID, all[i].CodexServerSocket, all[i].CodexServerSocketID = 0, "", ""
				if accountServer != nil {
					all[i].CodexServerPID = accountServer.cmd.Process.Pid
					all[i].CodexServerSocket = accountServer.socket
					all[i].CodexServerSocketID = accountServer.socketID
				}
				if kind == resumeConversation && s.Mode == "magpie" && selection != nil {
					all[i].MagpieModel = selection.Model
				}
				if overrideReason != "" {
					all[i].Overrides = append(all[i].Overrides, sessionOverride{At: now, Reason: overrideReason})
				}
				return all, nil
			}
		}
		return nil, errors.New("session changed during launch")
	}); err != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		_ = cmd.Wait()
		return err
	}
	restore, terminalErr := foregroundSession(cmd.Process.Pid)
	if terminalErr != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		_ = cmd.Wait()
		return terminalErr
	}
	if current, e := getSession(s.ID); e == nil && current.HerdrPane != "" {
		if e := reportSessionConnection(current); e != nil {
			fmt.Fprintln(os.Stderr, "Harbor could not update the sidebar connection label; inspect session details.")
		}
	}
	err = cmd.Wait()
	restore()
	{
		if captureErr := finishCapture(); captureErr != nil {
			fmt.Fprintln(os.Stderr, "Official identity observation could not be saved; inspect session details before recovery.")
		}
		finishCapture = nil
	}
	exitCode := cmd.ProcessState.ExitCode()
	recordErr := withSessions(func(all []session) ([]session, error) {
		for i := range all {
			if all[i].ID == s.ID && all[i].RunToken == plan.Token {
				now := time.Now().UTC()
				all[i].LastExitAt = &now
				all[i].LastExitCode = &exitCode
				all[i].ProcessGroup = 0
				all[i].CodexServerPID, all[i].CodexServerSocket, all[i].CodexServerSocketID = 0, "", ""

				return all, nil
			}
		}
		return nil, errors.New("session missing after exit")
	})
	if recordErr != nil {
		return recordErr
	}
	if err != nil {
		return fmt.Errorf("client exited: %w", err)
	}
	return nil
}
