package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/app"
	"github.com/asheshgoplani/agent-deck/internal/coreclient"
	tea "github.com/charmbracelet/bubbletea"
)

const startupTimeout = 5 * time.Second
const startupRetryInterval = 50 * time.Millisecond

type options struct {
	adminSocket string
	instanceID  string
	coreVersion string
	coreBinary  string
	debugUI     bool
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "agent-harbor-tui:", err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	configuration, err := parseOptions(arguments)
	if err != nil {
		return err
	}

	root, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var model tea.Model
	if configuration.debugUI {
		fmt.Fprintln(os.Stderr, "agent-harbor-tui: debug-ui mode (in-process empty backend, no Core)")
		model = app.New(app.NewDebugBackend())
	} else {
		startup, cancel := context.WithTimeout(root, startupTimeout)
		defer cancel()
		clientOptions := coreclient.Options{
			SocketPath:              configuration.adminSocket,
			ExpectedInstanceID:      configuration.instanceID,
			ExpectedProtocolVersion: configuration.coreVersion,
			AttachExecutable:        configuration.coreBinary,
		}
		client, err := connectCore(startup, clientOptions)
		if err != nil {
			return fmt.Errorf("connect to Core: %w", err)
		}
		defer client.Close()
		model = app.New(client)
	}

	// AllMotion so Spotlight [x]/row hover tracks the pointer (Grok CommandPalette).
	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseAllMotion(), tea.WithContext(root))
	if _, err := program.Run(); err != nil {
		return fmt.Errorf("run terminal UI: %w", err)
	}
	return nil
}

func connectCore(ctx context.Context, options coreclient.Options) (*coreclient.Client, error) {
	var lastErr error
	for {
		client, err := coreclient.NewUnixBackend(ctx, options)
		if err == nil {
			return client, nil
		}
		lastErr = err
		if errors.Is(err, coreclient.ErrWrongSocketOwner) ||
			errors.Is(err, coreclient.ErrInstanceMismatch) ||
			errors.Is(err, coreclient.ErrSocketMismatch) ||
			errors.Is(err, coreclient.ErrProtocolIncompatible) {
			return nil, err
		}

		timer := time.NewTimer(startupRetryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, lastErr
		case <-timer.C:
		}
	}
}

func parseOptions(arguments []string) (options, error) {
	defaults, err := defaultOptions()
	if err != nil {
		return options{}, err
	}
	flags := flag.NewFlagSet("agent-harbor-tui", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.StringVar(&defaults.adminSocket, "admin-socket", defaults.adminSocket, "absolute same-UID Agent Harbor Admin Unix socket")
	flags.StringVar(&defaults.instanceID, "instance-id", defaults.instanceID, "expected immutable Core instance ID")
	flags.StringVar(&defaults.coreVersion, "core-version", defaults.coreVersion, "expected Core protocol/binary version")
	flags.StringVar(&defaults.coreBinary, "core-binary", defaults.coreBinary, "absolute path to the separately supplied Core binary")
	flags.BoolVar(&defaults.debugUI, "debug-ui", defaults.debugUI, "run empty in-process TUI without connecting to Core")
	if err := flags.Parse(arguments); err != nil {
		return options{}, err
	}
	if flags.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected positional arguments: %v", flags.Args())
	}
	if defaults.debugUI {
		return defaults, nil
	}
	if !filepath.IsAbs(defaults.adminSocket) || filepath.Clean(defaults.adminSocket) != defaults.adminSocket {
		return options{}, fmt.Errorf("--admin-socket must be an absolute clean path")
	}
	if defaults.instanceID == "" {
		return options{}, fmt.Errorf("--instance-id is required")
	}
	if defaults.coreVersion == "" {
		return options{}, fmt.Errorf("--core-version is required")
	}
	if !filepath.IsAbs(defaults.coreBinary) || filepath.Clean(defaults.coreBinary) != defaults.coreBinary {
		return options{}, fmt.Errorf("--core-binary must be an absolute clean path")
	}
	return defaults, nil
}

func defaultOptions() (options, error) {
	executable, err := os.Executable()
	if err != nil {
		return options{}, fmt.Errorf("resolve TUI executable: %w", err)
	}
	return options{
		adminSocket: envOr("AGENT_HARBOR_ADMIN_SOCKET", ""),
		instanceID:  envOr("AGENT_HARBOR_INSTANCE_ID", ""),
		coreVersion: envOr("AGENT_HARBOR_CORE_VERSION", coreclient.AdminProtocolVersion),
		coreBinary:  envOr("AGENT_HARBOR_CORE_BINARY", filepath.Join(filepath.Dir(executable), "agent-harbor-core")),
	}, nil
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
