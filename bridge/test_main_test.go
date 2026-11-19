package main

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if pidFile := os.Getenv("AH_MANAGED_READY_FIXTURE"); pidFile != "" {
		if len(os.Args) > 1 && os.Args[1] == "serve" {
			if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
				panic(err)
			}
			err := http.ListenAndServe(os.Getenv("MAGPIE_ADDR"), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"name": "magpie", "version": "0.1.3528", "pid": os.Getpid(), "serves": "serve"})
			}))
			panic(err)
		}
		os.Exit(0) // settings invocations before serve have no side effects.
	}

	// Version probes have no lifecycle side effects in the native fixture.
	if len(os.Args) == 2 && os.Args[1] == "--version" && os.Getenv("BRIDGE_NATIVE_CLIENT") == "1" {
		os.Stdout.WriteString("codex-cli 0.167.0\n")
		os.Exit(0)
	}
	if os.Getenv("BRIDGE_STUB_HOLD") == "1" && len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-test.") {
		var b [1]byte
		_, _ = os.Stdin.Read(b[:])
		os.Exit(0)
	}

	if executable, err := os.Executable(); err == nil {
		if _, err := os.Stat(executable + ".fixture"); err == nil {
			if len(os.Args) > 1 && os.Args[1] == "app-server" {
				os.Exit(1)
			}
			cmd := exec.Command(executable+".fixture", os.Args[1:]...)
			cmd.Env = os.Environ()
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				os.Exit(1)
			}
			os.Exit(0)
		}
	}

	if os.Getenv("BRIDGE_NATIVE_CLIENT") == "1" && !strings.HasPrefix(os.Args[1], "-test.") {
		args := os.Args[1:]
		if args[0] == "app-server" {
			os.Exit(1)
		}
		if len(args) == 1 && args[0] == "--version" {
			os.Stdout.WriteString("codex-cli 0.167.0\n")
			os.Exit(0)
		}
		for len(args) > 0 && (args[0] == "--no-daemon" || args[0] == "-c") {
			if args[0] == "-c" && len(args) > 1 {
				args = args[2:]
			} else {
				args = args[1:]
			}
		}
		first, second := "", ""
		if len(args) > 0 {
			first = args[0]
		}
		if len(args) > 1 {
			second = args[1]
		}
		cwd, _ := os.Getwd()
		text := cwd + "\n" + first + "\n" + second + "\n"
		if value, ok := os.LookupEnv("XDG_CONFIG_HOME"); ok {
			text += "set:" + value + "\n"
		} else {
			text += ":\n"
		}
		_ = os.WriteFile(os.Getenv("CAPTURE"), []byte(text), 0600)
		os.Exit(0)
	}

	if os.Getenv("BRIDGE_STUB_VERSION") == "1" && len(os.Args) == 2 && os.Args[1] == "--version" {
		if os.Getenv("BRIDGE_STUB_VERSION_DELAY") == "1" {
			time.Sleep(3 * time.Second)
		}
		os.Stdout.WriteString("codex-cli 0.167.0\n")
		os.Exit(0)
	}
	if os.Getenv("BRIDGE_STUB_HOLD") == "1" && len(os.Args) > 1 && os.Args[1] == "-c" {
		var b [1]byte
		_, _ = os.Stdin.Read(b[:])
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// Only test-owned native process fixtures use these sidecars. Production never
// reads them: it executes the native fixture, which owns its controlled behavior.
func writeNativeClientFixture(path string, script []byte, mode os.FileMode) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		return err
	}
	if err = os.WriteFile(path+".fixture", script, mode); err != nil {
		return err
	}
	return os.WriteFile(path, data, mode)
}
