//go:build unix

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/asheshgoplani/agent-deck/internal/testcore"
)

type config struct {
	socketPath string
	instanceID string
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "agent-harbor-fake-core:", err)
		os.Exit(1)
	}
}

func run(arguments []string, output io.Writer) error {
	config, err := parseConfig(arguments)
	if err != nil {
		return err
	}
	server, err := testcore.Start(testcore.Options{SocketPath: config.socketPath, InstanceID: config.instanceID})
	if err != nil {
		return err
	}
	defer server.Close()
	fmt.Fprintf(output, "fake Core ready: socket=%s instance=%s\n", server.SocketPath(), server.InstanceID())
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	return nil
}

func parseConfig(arguments []string) (config, error) {
	set := flag.NewFlagSet("agent-harbor-fake-core", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	value := config{}
	set.StringVar(&value.socketPath, "socket", "", "absolute Unix socket path")
	set.StringVar(&value.instanceID, "instance-id", "ins_0123456789abcdef0123456789abcdef", "fixture Core instance ID")
	if err := set.Parse(arguments); err != nil {
		return config{}, err
	}
	if value.socketPath == "" {
		return config{}, errors.New("--socket is required")
	}
	if !filepath.IsAbs(value.socketPath) || filepath.Clean(value.socketPath) != value.socketPath {
		return config{}, fmt.Errorf("--socket must be absolute and clean: %q", value.socketPath)
	}
	return value, nil
}
