package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const remoteUsage = `usage:
  agent-harbor remote add NAME --ssh HOST --binary /remote/install/agent-harbor [--state /remote/state] [--native]
  agent-harbor remote list [--json]
  agent-harbor remote status [NAME_OR_ID] [--json]
  agent-harbor remote remove PROFILE_ID
  agent-harbor remote prepare [--state /absolute/state] [--native]

Install the matching Harbor candidate on the server first. Add prepares its
owned workspace and saves a Herdr machine. Open agent-harbor and select that
machine in the sidebar. The Harbor popup, projects and clients run there.
Disconnecting or removing a machine leaves its remote tasks running.
--native explicitly selects the server's own client configuration for first use.`

func remoteCommand(args []string) error {
	if len(args) == 0 {
		fmt.Println(remoteUsage)
		return nil
	}
	switch args[0] {
	case "help", "--help", "-h":
		fmt.Println(remoteUsage)
		return nil
	case "prepare":
		f := flag.NewFlagSet("remote prepare", flag.ContinueOnError)
		f.SetOutput(io.Discard)
		root := f.String("state", "", "remote state directory")
		native := f.Bool("native", false, "select remote native configuration")
		if err := f.Parse(args[1:]); err != nil {
			return err
		}
		if f.NArg() != 0 {
			return errors.New(remoteUsage)
		}
		if *root == "" {
			var err error
			*root, err = bridgeHome()
			if err != nil {
				return err
			}
		}
		w, err := prepareRemoteWorkspace(*root, *native)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(w)
	case "add":
		return addRemote(args[1:])
	case "list", "status", "remove", "enable", "disable", "reconnect":
		return remoteMachineCommand(args)
	default:
		return errors.New(remoteUsage)
	}
}

func remoteMachineCommand(args []string) error {
	root, err := bridgeHome()
	if err != nil {
		return err
	}
	if err := ensureHome(root); err != nil {
		return err
	}
	env, _, err := managedEnvironment(root)
	if err != nil {
		return err
	}
	cmd := herdrCommand(env, append([]string{"machine"}, args...)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

func validRemotePath(path string) bool {
	return filepath.IsAbs(path) && !strings.ContainsAny(path, "\x00\r\n")
}

func remoteSSH(ctx context.Context, target string, args ...string) *exec.Cmd {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = shellQuote(arg)
	}
	return exec.CommandContext(ctx, "ssh", "-T", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "ConnectTimeout=10", "--", target, strings.Join(quoted, " "))
}

func addRemote(args []string) error {
	if len(args) == 0 {
		return errors.New(remoteUsage)
	}
	name := args[0]
	if strings.TrimSpace(name) == "" || strings.ContainsAny(name, "\x00\r\n") || strings.HasPrefix(name, "-") {
		return errors.New("invalid machine name")
	}
	f := flag.NewFlagSet("remote add", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	target := f.String("ssh", "", "SSH target")
	binary := f.String("binary", "", "installed remote Harbor executable")
	state := f.String("state", "", "remote state directory")
	native := f.Bool("native", false, "select remote native configuration")
	if err := f.Parse(args[1:]); err != nil {
		return err
	}
	if f.NArg() != 0 || *target == "" || !validRemotePath(*binary) || (*state != "" && !validRemotePath(*state)) {
		return errors.New(remoteUsage)
	}
	if strings.HasPrefix(*target, "-") || strings.ContainsAny(*target, "\x00\r\n \t") {
		return errors.New("invalid SSH target")
	}
	authority := strings.TrimPrefix(*target, "ssh://")
	if user, _, ok := strings.Cut(authority, "@"); ok && strings.Contains(user, ":") {
		return errors.New("SSH target must not include a password")
	}
	root, err := bridgeHome()
	if err != nil {
		return err
	}
	if err := ensureHome(root); err != nil {
		return err
	}
	env, _, err := managedEnvironment(root)
	if err != nil {
		return err
	}
	help, err := herdrCommand(env, "machine", "add", "--help").Output()
	if err != nil || !strings.Contains(string(help), "--remote-executable") {
		return errors.New("native remote setup requires the Herdr remote-executable candidate; install the matching Harbor package")
	}
	// Validate names before preparing any server, then let Herdr own catalog writes.
	rows, err := herdrCommand(env, "machine", "list", "--json").Output()
	if err != nil {
		return errors.New("cannot read saved machines")
	}
	var profiles []struct {
		Label string `json:"label"`
	}
	if err := json.Unmarshal(rows, &profiles); err != nil {
		return errors.New("invalid saved machine list")
	}
	for _, p := range profiles {
		if p.Label == name {
			return errors.New("machine name already exists; review or remove that profile before adding another workspace")
		}
	}
	remoteArgs := []string{*binary, "remote", "prepare"}
	if *state != "" {
		remoteArgs = append(remoteArgs, "--state", *state)
	}
	if *native {
		remoteArgs = append(remoteArgs, "--native")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := remoteSSH(ctx, *target, remoteArgs...)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("remote preparation failed; check SSH access, the installed package and remote onboarding: %w", err)
	}
	var w remoteWorkspace
	if err := json.Unmarshal(output, &w); err != nil || w.Version != 1 || !validRemoteID(w.ID) || w.Session != herdrSession || !validRemotePath(w.State) || w.Executable != filepath.Join(w.State, "herdr", "bin", "remote-herdr-"+w.ID) || len(w.Environment) != 0 {
		return errors.New("remote returned an invalid workspace description; no machine was saved")
	}
	save := herdrCommand(env, "machine", "add", *target, "--label", name, "--remote-session", w.Session, "--remote-executable", w.Executable)
	save.Stdin, save.Stdout, save.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := save.Run(); err != nil {
		return fmt.Errorf("remote workspace prepared, but machine was not saved: %w", err)
	}
	fmt.Printf("Open agent-harbor and select %q in Herdr's machine sidebar. Tasks run on that server.\n", name)
	return nil
}
