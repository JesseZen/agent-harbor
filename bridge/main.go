package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf8"
)

var version = "dev-development-candidate"

func main() { os.Exit(main1(os.Args[1:])) }

func main1(args []string) int {
	if err := run(args); err != nil {
		fmt.Fprintf(os.Stderr, "\nError\n  %v\n\nRecovery\n  Check the session details and try again.\n\n", err)
		return 1
	}
	return 0
}

func run(args []string) error {
	if len(args) == 0 {
		return openHerdr()
	}
	switch args[0] {
	case "update", "uninstall":
		return distributionCommand(args)
	case "profiles":
		return profileCommand(args[1:])
	case "remote":
		return remoteCommand(args[1:])
	case "_remote-herdr":
		return remoteHerdr(args[1:])
	case "version", "--version":
		fmt.Println("agent-harbor-bridge " + version)
		return nil
	case "_herdr-config-check":
		if len(args) != 3 {
			return errors.New("invalid managed Herdr context check")
		}
		if err := ensureHome(args[1]); err != nil {
			return err
		}
		config, err := managedConfigContext(args[1])
		if err != nil {
			return err
		}
		if config != args[2] {
			return errors.New("managed Herdr config context changed; reopen workspace")
		}
		return nil
	case "_focus":
		return focusAfterPopup(args[1:])
	case "_picker", "_app":
		return runHarborApp()
	case "settings", "_settings":
		if len(args) != 1 {
			return errors.New("usage: settings")
		}
		return runSettingsApp()
	case "settings-recover":
		if len(args) != 1 {
			return errors.New("usage: settings-recover")
		}
		return recoverShortcuts()
	case "_status":
		fmt.Println(harborStatus())
		return nil
	case "_magpie":
		return runMagpieApp()
	case "_pane":
		return interactiveResult(herdrPane())
	case "open":
		if len(args) != 1 {
			return errors.New("usage: open")
		}
		return openHerdr()
	case "stop":
		if len(args) != 1 {
			return errors.New("usage: stop")
		}
		return stopHerdr()
	case "_refresh-session-names":
		if len(args) != 1 {
			return errors.New("usage: _refresh-session-names")
		}
		return refreshAutomaticSessionNames()
	case "magpie":
		return magpieCommand(args[1:])
	case "models":
		return showModels(args[1:])
	case "delete-history":
		return errors.New(historyRemovalExplanation)
	case "rename", "remove":
		return recordCommand(args)
	case "create", "new":
		return newSessionCommand(args[0], args[1:])
	case "sessions":
		if len(args) > 2 || len(args) == 2 && args[1] != "--json" {
			return errors.New("usage: sessions [--json]")
		}
		return listSessions(len(args) == 2)
	case "show":
		if len(args) != 2 {
			return errors.New("usage: show SESSION_ID")
		}
		view, err := appService.Get(args[1])
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(view)
	case "reassociate":
		return reassociateCommand(args[1:])
	case "bind":
		if len(args) != 3 {
			return errors.New("usage: bind SESSION_ID CODEX_SESSION_ID; first verify the Codex ID and working directory")
		}
		if err := appService.Bind(args[1], args[2]); err != nil {
			return err
		}
		fmt.Println("Codex session ID recorded. Verify the working directory before recovery.")
		return nil
	case "resume":
		return resumeCommand(args[1:])
	case "stop-session":
		return stopSessionCommand(args[1:])
	case "focus":
		if len(args) != 2 {
			return errors.New("usage: focus SESSION_ID")
		}
		return appService.Focus(args[1])
	case "route":
		if len(args) != 2 {
			return errors.New("usage: route SESSION_ID")
		}
		route, err := appService.ObservedRoute(args[1])
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(route)
	case "verify":
		return verifyCommand(args[1:])
	case "start":
		if len(args) != 2 {
			return errors.New("usage: start SESSION_ID")
		}
		view, err := appService.Get(args[1])
		s := view.session
		if err != nil {
			return err
		}
		if s.Started {
			return errors.New("session was already started; use explicit recovery")
		}
		return appService.Start(sessionApproval{Session: s, Observation: &view.Runtime, Confirmed: true})
	case "help", "--help", "-h":
		fmt.Println("  update [--version TAG] | uninstall  manage a script installation (ah is an alias)")
		fmt.Println("agent-harbor-bridge: open, stop, settings, settings-recover, create, start, sessions, show, bind, verify, reassociate, resume, stop-session, rename, remove, focus, route, new, models, magpie, profiles")
		fmt.Println("  magpie status|bind|start|stop|manage|web|use  verified Magpie instances")
		fmt.Println("  profiles list|new|existing|select|rename|default|review  client environments")
		fmt.Println(`
First use:
  Install Codex on PATH, then run agent-harbor with no arguments.
  Dependency checks open the workspace directly when ready.
  Choose native configuration or Magpie separately for each new session.
  In Connections, start a managed Magpie or bind an external gateway when needed.
  Managed Magpie has its own configuration and keeps running until stopped.
  External processes remain under your control.

Inside the workspace:
  Ctrl+B a opens Harbor; p manages Profiles; n creates a named session.
  New sessions capture the selected Profile; existing sessions keep their own.
  Ctrl+B , opens Harbor settings (also agent-harbor settings).
  Ctrl+B s opens Herdr settings. Workspace shortcuts are configurable.
  Ctrl+B m opens Magpie management for the current pane's connection.
  Ctrl+B ? lists workspace shortcuts; ? inside Harbor shows screen help.
  Enter focuses a running session or reviews recovery with explicit confirmation.
  s stops a session while preserving its conversation; x removes only its record.
  Manage conversation history with the client’s own tools.
  Details distinguish selected connection from observed main/auxiliary routes.
  Magpie may send conversation snippets to other providers for auxiliary tasks.

Recovery:
  Reopen agent-harbor after a restart, select a session and review Resume.
  Harbor retains the conversation UUID and connection; uncertainty is refused
  with a recovery message. c reviews an old restored shell before closing it.
  Native sessions share settings/login only within their bound Profile.
  Old unbound records require review of their original existing environment.`)
		return nil
	default:
		return usage()
	}
}

func usage() error {
	return errors.New("usage: agent-harbor-bridge {open|stop|settings|settings-recover|create|start|sessions|show|bind|verify|resume|stop-session|rename|remove|focus|route|new|models|magpie|profiles|remote}")
}

func checkDirectory(path string) (string, error) {
	if path == "" {
		return "", errors.New("a working directory is required")
	}
	if !utf8.ValidString(path) {
		return "", errors.New("working directory must be valid UTF-8")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("working directory is unavailable: %w", err)
	}
	if !utf8.ValidString(abs) {
		return "", errors.New("resolved working directory must be valid UTF-8")
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("working directory is unavailable: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("working directory is not a directory")
	}
	return abs, nil
}

func listSessions(jsonOutput bool) error {
	sessions, err := appService.List()
	if err != nil {
		return err
	}
	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(sessions)
	}
	if len(sessions) == 0 {
		fmt.Println("No sessions yet.")
		return nil
	}
	for _, s := range sessions {
		fmt.Printf("Session  %q\n  Name       %q\n  Directory  %q\n  CLI        %q\n  Connection %q\n  Status     %q\n", s.ID, s.Name, s.WorkingDirectory, s.CLI, s.Mode, s.Status)
		fmt.Printf("  Profile    %q\n", profileLabel(s.Profile))
		if s.DiscoveryWarning != "" {
			fmt.Println("  Warning    " + s.DiscoveryWarning)
		}
		if s.Reason != "" {
			fmt.Printf("  Health     %q\n  Reason     %q\n", "Needs attention", s.Reason)
		}
		if s.Mode == "magpie" {
			fmt.Printf("  Instance   %q\n  Address    %q\n  Model      %q\n", s.InstanceID, s.MagpieURL, s.MagpieModel)
		}
		if s.CodexSessionID != "" {
			fmt.Printf("  Codex ID   %s\n", s.CodexSessionID)
		}
		if s.ReportedCodexID != "" {
			fmt.Printf("  Later ID   %s\n", s.ReportedCodexID)
		}
		fmt.Println()
	}
	return nil
}
