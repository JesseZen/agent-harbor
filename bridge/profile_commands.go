package main

import (
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
)

func findProfileBinding(id, client string) (profileBinding, error) {
	all, err := appService.Profiles()
	if err != nil {
		return profileBinding{}, err
	}
	for _, p := range all {
		if p.ID == id {
			return bindingOf(p, client)
		}
	}
	return profileBinding{}, ErrProfileNotFound
}
func profileCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: profiles list|new|existing|select|rename|default|review")
	}
	switch args[0] {
	case "list":
		all, err := appService.Profiles()
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(all)
	case "new":
		if len(args) != 2 {
			return errors.New("usage: profiles new NAME")
		}
		p, err := appService.CreateProfile(profileCreateRequest{Name: args[1], Kind: profileIsolated})
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(p)
	case "existing":
		flags := flag.NewFlagSet("existing", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		home := flags.String("home", "", "proven original Codex home")
		sqlite := flags.String("sqlite", "", "proven original SQLite home")
		if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 1 {
			return errors.New("usage: profiles existing --home ABSOLUTE --sqlite ABSOLUTE NAME")
		}
		p, err := appService.CreateProfile(profileCreateRequest{Name: flags.Arg(0), Kind: profileExisting, ExistingHome: *home, ExistingSQLite: *sqlite})
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(p)
	case "select", "rename", "default":
		if len(args) < 2 {
			return errors.New("Profile ID required")
		}
		b, err := findProfileBinding(args[1], "codex")
		if err != nil {
			return err
		}
		if args[0] == "select" && len(args) == 2 {
			return appService.SelectProfile(b)
		}
		if args[0] == "rename" && len(args) == 3 {
			return appService.RenameProfile(b, args[2])
		}
		if args[0] == "default" && len(args) == 3 {
			id := args[2]
			if id == "shared" {
				id = ""
			}
			return appService.SetProfileDefaultInstance(b, id)
		}
		return errors.New("usage: profiles select ID | rename ID NAME | default ID INSTANCE_ID|shared")
	case "review":
		if len(args) != 4 || args[3] != "--confirm" {
			return errors.New("inspect show SESSION_ID and profiles list first; usage: profiles review SESSION_ID PROFILE_ID --confirm; existing environment only, no data is moved")
		}
		s, err := appService.Get(args[1])
		if err != nil {
			return err
		}
		b, err := findProfileBinding(args[2], s.CLI)
		if err != nil {
			return err
		}
		return appService.ReviewSessionProfile(sessionApproval{Session: s.session, Confirmed: true}, b)
	}
	return errors.New("unknown Profile action")
}
