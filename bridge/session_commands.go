package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

func newSessionCommand(alias string, args []string) error {
	flags := flag.NewFlagSet(alias, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dir := flags.String("dir", "", "project directory")
	flags.StringVar(dir, "directory", "", "project directory")
	connection := flags.String("connection", "local", "connection")
	flags.StringVar(connection, "mode", "local", "connection")
	model := flags.String("model", "", "model or group ID")
	instance := flags.String("instance", "", "Magpie instance ID")
	client := flags.String("client", "codex", "upstream client target")
	name := flags.String("name", "", "session name")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return errors.New("usage: new --dir PATH --connection local|magpie [--instance ID --model ID] [--name NAME]")
	}
	if *client == "codex" && alias == "create" && *connection == "magpie" && *instance == "" {
		return errors.New("choose Magpie and its model in the managed Herdr session menu, or use new --instance ID --model ID")
	}
	s, err := appService.Create(sessionDraft{Client: *client, Directory: *dir, Name: *name, Connection: *connection, Instance: *instance, Model: *model})
	if err != nil {
		return err
	}
	fmt.Printf("Session: %s\nDirectory: %q\nConnection: %s\n", s.ID, s.WorkingDirectory, s.Mode)
	if s.Mode == "magpie" {
		fmt.Println("Magpie may send auxiliary requests containing parts of this conversation to other models or providers. Review the instance and selected model.")
		var reply string
		fmt.Print("Type yes to start: ")
		if _, err := fmt.Scanln(&reply); err != nil || reply != "yes" {
			return errors.New("start cancelled")
		}
	}
	if *client != "codex" && os.Getenv("HERDR_PLUGIN_ENTRYPOINT_ID") != "session" {
		return openSessionPane(s, newConversation)
	}
	return appService.Start(sessionApproval{Session: s, Confirmed: true})
}
func resumeCommand(args []string) error {
	flags := flag.NewFlagSet("resume", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	query := flags.Bool("development-query", false, "local query candidate")
	override := flags.String("override-instance", "", "reason for explicitly accepting changed or unknown instance")
	if err := flags.Parse(args); err != nil || flags.NArg() != 1 {
		return errors.New("usage: resume [--development-query] [--override-instance REASON] SESSION_ID")
	}
	view, err := appService.Get(flags.Arg(0))
	if err != nil {
		return err
	}
	s := view.session
	if processGroupActive(s.ProcessGroup) || sessionLaunchPending(s) {
		return errors.New("a previous session process is still running or closing")
	}
	if *query {
		if err := queryReferenceCheck(s); err != nil {
			return err
		}
		fmt.Println("Development candidate. Query proves syntax only. Existence, login and external writers cannot be verified. Type accept to attempt recovery:")
		var answer string
		fmt.Scanln(&answer)
		if answer != "accept" {
			return errors.New("query uncertainty not accepted")
		}
	}
	if *override != "" {
		fmt.Printf("Explicit instance override: %q\n", *override)
	}
	if err := confirmRecoveryReview(s, bufio.NewReader(os.Stdin), *override); err != nil {
		return err
	}
	approval := sessionApproval{Session: s, Observation: &view.Runtime, Confirmed: true, OverrideReason: *override, DevelopmentQuery: *query, QueryUncertaintyAccepted: *query}
	if *query && os.Getenv("HERDR_PLUGIN_ENTRYPOINT_ID") != "codex" && os.Getenv("HERDR_PLUGIN_ENTRYPOINT_ID") != "session" {
		return appService.Open(approval)
	}
	return appService.Resume(approval)
}
func stopSessionCommand(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: stop-session SESSION_ID")
	}
	view, err := appService.Get(args[0])
	if err != nil {
		return err
	}
	fmt.Printf("Stop %q? The conversation stays resumable. Type yes: ", view.Name)
	var reply string
	if _, err := fmt.Scanln(&reply); err != nil || reply != "yes" {
		return errors.New("stop cancelled")
	}
	return appService.Stop(sessionApproval{Session: view.session, Observation: &view.Runtime, Confirmed: true})
}
