package main

import (
	"errors"
	"fmt"
	"io"
	"os"
)

var launcherVersion = "dev"

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	manifest, err := loadManifest()
	if err != nil {
		fmt.Fprintf(stderr, "agent-harbor: %v\n", err)
		return 1
	}
	paths, err := resolveProductPaths(manifest)
	if err != nil {
		fmt.Fprintf(stderr, "agent-harbor: %v\n", err)
		return 1
	}
	command := "open"
	if len(args) > 0 {
		command, args = args[0], args[1:]
	}
	var commandErr error
	switch command {
	case "open":
		commandErr = commandOpen(manifest, paths, args, stdout, stderr)
	case "init":
		commandErr = commandInit(paths, args, stdout)
	case "status":
		commandErr = commandStatus(paths, args, stdout)
	case "stop":
		commandErr = commandStop(paths, args, stdout)
	case "doctor":
		commandErr = commandDoctor(manifest, paths, args, stdout)
	case "export":
		commandErr = commandExport(manifest, paths, args, stdout)
	case "import":
		commandErr = commandImport(manifest, paths, args, stdout)
	case "licenses":
		commandErr = commandLicenses(manifest, args, stdout)
	case "version", "--version", "-v":
		if len(args) != 0 {
			commandErr = usageError("version accepts no arguments")
		} else {
			fmt.Fprintf(stdout, "agent-harbor %s (bundle %s, product %s, %s/%s)\n", launcherVersion, manifest.BundleID, manifest.Version, manifest.TargetOS, manifest.TargetArch)
		}
	case "runtime":
		if len(args) == 0 || args[0] != "gc" {
			commandErr = usageError("usage: agent-harbor runtime gc")
		} else {
			commandErr = commandRuntimeGC(manifest, paths, args[1:], stdout)
		}
	case "help", "--help", "-h":
		printUsage(stdout)
	default:
		commandErr = usageError(fmt.Sprintf("unknown command %q", command))
	}
	if commandErr == nil {
		return 0
	}
	var usage *commandUsageError
	if errors.As(commandErr, &usage) {
		fmt.Fprintf(stderr, "agent-harbor: %v\n", commandErr)
		printUsage(stderr)
		return 2
	}
	fmt.Fprintf(stderr, "agent-harbor: %v\n", commandErr)
	return 1
}

type commandUsageError struct{ message string }

func (e *commandUsageError) Error() string { return e.message }
func usageError(message string) error      { return &commandUsageError{message: message} }

func printUsage(w io.Writer) {
	fmt.Fprintln(w, `usage: agent-harbor [command]

commands:
  open                         initialize, start, and attach to the TUI (default)
  status                       show Core status
  stop                         stop Core cleanly
  init                         create a minimal empty configuration
  doctor [--repair-launchers]  check runtime/tools; optionally repair trusted ownership
  export [options] FILE        write an encrypted portable archive
  import [options] FILE|-      import into an empty environment
  licenses                     print bundled license material
  version                      print bundle identity
  runtime gc                   remove unpacked runtimes other than this bundle`)
}
