package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

func magpieCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: agent-harbor magpie {status|bind|start|stop|manage|web|use}")
	}
	switch args[0] {
	case "status":
		return magpieStatusCommand(args[1:])
	case "bind":
		return magpieBindCommand(args[1:])
	case "start":
		return magpieStartCommand(args[1:])
	case "stop":
		if len(args) != 1 {
			return errors.New("usage: agent-harbor magpie stop")
		}
		return stopManagedMagpie()
	case "manage":
		return magpieManageCommand(args[1:])
	case "web":
		if len(args) != 1 {
			return errors.New("usage: agent-harbor magpie web")
		}
		return openManagedWebPage()
	case "use":
		return magpieUseCommand(args[1:])
	default:
		return errors.New("usage: agent-harbor magpie {status|bind|start|stop|manage|web|use}")
	}
}

type instanceReport struct {
	magpieInstance
	Health   string       `json:"health"`
	Detail   string       `json:"detail,omitempty"`
	Warnings []string     `json:"warnings,omitempty"`
	Probe    gatewayProbe `json:"probe"`
}

func magpieStatusCommand(args []string) error {
	flags := flag.NewFlagSet("magpie status", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	address := flags.String("address", "", "probe one address")
	jsonOutput := flags.Bool("json", false, "JSON output")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return errors.New("usage: agent-harbor magpie status [--address http://127.0.0.1:PORT] [--json]")
	}
	if *address != "" {
		probe := probeGateway(*address)
		if *jsonOutput {
			return json.NewEncoder(os.Stdout).Encode(probe)
		}
		printGatewayProbe(probe)
		return nil
	}
	return magpieStatus(*jsonOutput)
}

func magpieStatus(jsonOutput bool) error {
	registry, err := loadMagpieRegistry()
	if err != nil {
		return err
	}
	reports := make([]instanceReport, 0, len(registry.Instances))
	for _, instance := range registry.Instances {
		probe := probeGateway(instance.Address)
		health := instance.health(probe)
		reports = append(reports, instanceReport{magpieInstance: instance, Health: health.State, Detail: health.Detail, Warnings: health.Warnings, Probe: probe})
	}
	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(struct {
			Default   string           `json:"default,omitempty"`
			Instances []instanceReport `json:"instances"`
		}{Default: registry.Default, Instances: reports})
	}
	if len(reports) == 0 {
		fmt.Println(style("1;36", "Magpie instances"))
		fmt.Println("\n  No Magpie instance is recorded.")
		fmt.Println("\n  Bind a running Magpie after checking it in Magpie:")
		fmt.Println("    agent-harbor magpie bind --address http://127.0.0.1:3425")
		fmt.Println("  Or create one Harbor owns and runs in the background:")
		fmt.Println("    agent-harbor magpie start")
		return nil
	}
	fmt.Println(style("1;36", "Magpie instances"))
	for _, report := range reports {
		printInstanceReport(report, report.ID == registry.Default)
	}
	return nil
}

func printInstanceReport(report instanceReport, selected bool) {
	kind := "External"
	state := report.Health
	if report.Kind == "managed" {
		kind = "Managed"
		if report.Probe.State == gatewayVerified {
			state = "running"
		} else if report.Probe.State == gatewayOffline {
			state = "stopped"
		}
	}
	heading := report.Name
	if selected {
		heading += " (selected)"
	}
	fmt.Printf("\n  %s  %s\n", style("1;32", kind), style("1", heading))
	printRow("Address", report.Address)
	printRow("State", stateWord(state))
	if report.Probe.State == gatewayVerified {
		printRow("Identity", "verified")
		printRow("Config", report.Probe.ConfigDir)
		printRow("Version", report.Probe.Version)
		printRow("Process", fmt.Sprintf("PID %d, serving %s", report.Probe.PID, report.Probe.Serves))
	} else if report.Kind == "managed" && report.Probe.State == gatewayOffline {
		printRow("Identity", "recorded configuration: "+orUnknown(report.ConfigDir))
	} else {
		printRow("Identity", report.Health+" ("+orUnknown(report.Detail)+")")
	}
	if report.Kind == "managed" && report.Probe.State == gatewayVerified {
		layout, err := managedInstanceState()
		if err == nil {
			record, err := readManagedRecord(layout)
			if err == nil && record != nil && record.WebAddress != "" {
				printRow("Web", record.WebAddress+" (key stored privately)")
			}
		}
	}
	for _, warning := range report.Warnings {
		fmt.Printf("  %s %s\n", style("33", "Warning"), warning)
	}
}

func printGatewayProbe(probe gatewayProbe) {
	fmt.Println(style("1;36", "Magpie gateway"))
	printRow("Address", probe.Address)
	printRow("State", stateWord(probe.State))
	switch probe.State {
	case gatewayVerified:
		printRow("Version", probe.Version)
		printRow("Process", fmt.Sprintf("PID %d, serving %s", probe.PID, probe.Serves))
		printRow("Executable", probe.Executable)
		printRow("Config", probe.ConfigDir)
		printRow("Evidence", probe.ConfigEvidence)
		if probe.ConfigWarning != "" {
			fmt.Printf("    %s %s\n", style("33", "Warning"), probe.ConfigWarning)
		}
	case gatewayUnknown, gatewayNotMagpie:
		printRow("Reason", probe.Reason)
		if probe.PID > 1 {
			printRow("Process", fmt.Sprintf("PID %d, serving %s", probe.PID, probe.Serves))
		}
	}
}

func stateWord(state string) string {
	switch state {
	case gatewayVerified, "running":
		return style("1;32", state) // instanceVerified shares this value
	case gatewayOffline, "stopped":
		return style("2", state)
	case gatewayNotMagpie, instanceChanged, instanceUnknown:
		return style("33", state)
	default:
		return state
	}
}

func printRow(label, value string) {
	fmt.Printf("    %s %s\n", style("2", fmt.Sprintf("%-10s", label)), value)
}

func orUnknown(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

func magpieBindCommand(args []string) error {
	flags := flag.NewFlagSet("magpie bind", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	address := flags.String("address", "", "running Magpie address")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *address == "" {
		return errors.New("usage: agent-harbor magpie bind --address http://127.0.0.1:PORT")
	}
	probe := probeGateway(*address)
	if probe.State != gatewayVerified {
		return fmt.Errorf("cannot bind the Magpie at %s: %s\n\nHarbor records the configuration location read from the gateway's own process; it does not infer an instance from an address or a model list.", probe.Address, orUnknown(probe.Reason))
	}
	instance, err := bindExternalMagpie(probe)
	if err != nil {
		return err
	}
	fmt.Println(style("1;36", "Magpie instance recorded"))
	printRow("ID", instance.ID)
	printRow("Address", instance.Address)
	printRow("Config", instance.ConfigDir)
	printRow("Version", instance.Version)
	fmt.Println("\nHarbor never starts, stops or edits this Magpie. New sessions follow the selected instance.")
	return nil
}

func magpieUseCommand(args []string) error {
	if len(args) > 1 {
		return errors.New("usage: agent-harbor magpie use [INSTANCE_ID]")
	}
	if len(args) == 0 {
		registry, err := loadMagpieRegistry()
		if err != nil {
			return err
		}
		if len(registry.Instances) == 0 {
			return errors.New("no Magpie instance is recorded; bind one with `agent-harbor magpie bind --address ADDRESS`")
		}
		fmt.Println(style("1;36", "Select a Magpie instance"))
		for _, instance := range registry.Instances {
			marker := " "
			if instance.ID == registry.Default {
				marker = "*"
			}
			fmt.Printf("\n  %s %s  %s\n    %s\n    Config %s\n", marker, style("1", instance.ID), instance.Kind, instance.Address, orUnknown(instance.ConfigDir))
		}
		fmt.Println("\nRun `agent-harbor magpie use INSTANCE_ID` to select one for new sessions.")
		return nil
	}
	instance, err := useMagpieInstance(args[0])
	if err != nil {
		return err
	}
	fmt.Printf("New sessions use %s (%s).\n", instance.ID, instance.Address)
	return nil
}

func magpieStartCommand(args []string) error {
	flags := flag.NewFlagSet("magpie start", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	port := flags.Int("port", 0, "gateway port")
	webPort := flags.Int("web-port", 0, "web page port")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return errors.New("usage: agent-harbor magpie start [--port N] [--web-port N]")
	}
	return startManagedMagpie(managedStartOptions{Port: *port, WebPort: *webPort})
}

func magpieManageCommand(args []string) error {
	flags := flag.NewFlagSet("magpie manage", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	id := flags.String("instance", "", "instance ID")
	address := flags.String("address", "", "instance address")
	unbound := flags.Bool("unbound", false, "open Magpie with your own settings when identity is unknown")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return errors.New("usage: agent-harbor magpie manage [--instance ID | --address ADDRESS] [--unbound]")
	}
	instance, probe, err := resolveManagementInstance(*id, *address)
	if err != nil {
		return err
	}
	// The bundled CLI is only needed to open a UI, not to explain a refusal.
	bundled, _ := bundledMagpiePath()
	decision := planManagement(instance, probe, bundled)
	chosen := &decision
	if !decision.Allowed {
		if decision.Unbound != nil && *unbound {
			chosen = decision.Unbound
		} else {
			fmt.Printf("\nCannot open Magpie management\n\n  Address  %s\n  Reason   %s\n", instance.Address, orUnknown(decision.Reason))
			if decision.Unbound != nil {
				fmt.Println("\n  Harbor cannot verify which configuration this gateway reads. To open Magpie with your own settings anyway, accepting that it may not manage the instance your sessions use:")
				fmt.Println("    agent-harbor magpie manage --address " + instance.Address + " --unbound")
			}
			return errors.New("Magpie management was not opened")
		}
	}
	if chosen.Executable == "" {
		return errors.New("the packaged Magpie CLI was not found; set AGENT_HARBOR_MAGPIE_BIN or reinstall Agent Harbor")
	}
	if err := runManagement(chosen); err != nil {
		return err
	}
	return refreshInstanceAfterManagement(instance, probeGateway(instance.Address))
}
