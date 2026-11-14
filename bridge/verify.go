package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
)

func verifyCommand(args []string) error {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	directory := flags.String("directory", "", "directory shown by Codex /status")
	if err := flags.Parse(args); err != nil || flags.NArg() != 2 {
		return errors.New("usage: verify --directory PATH SESSION_ID CODEX_SESSION_UUID")
	}
	s, err := getSession(flags.Arg(0))
	if err != nil {
		return err
	}
	if processGroupActive(s.ProcessGroup) {
		return errors.New("a previous session process is still running or closing")
	}
	dir, err := checkDirectory(*directory)
	if err != nil {
		return err
	}
	candidate := s
	candidate.WorkingDirectory, candidate.CodexSessionID, candidate.Recovery = dir, flags.Arg(1), "ready"
	fmt.Printf("\nVerify these values against Codex /status before continuing.\n  Previously recorded directory  %q\n  Previously recorded Codex ID   %s\n", s.WorkingDirectory, s.CodexSessionID)
	if s.ReportedCodexID != "" {
		fmt.Printf("  Later reported Codex ID       %s\n", s.ReportedCodexID)
	}
	if !validCodexID(candidate.CodexSessionID) {
		return errors.New("Codex session ID must be a UUID")
	}
	fmt.Printf("\nManual verification: directory %q, Codex ID %s. Type yes: ", dir, candidate.CodexSessionID)
	var reply string
	if _, err := fmt.Scanln(&reply); err != nil || reply != "yes" {
		return errors.New("verification cancelled")
	}
	if err := appService.Verify(s, candidate.CodexSessionID, dir); err != nil {
		return err
	}
	fmt.Println("Verified session details recorded.")
	return nil
}
