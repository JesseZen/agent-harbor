package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"time"
)

type codexConfigPipe struct {
	input  *json.Encoder
	output *bufio.Scanner
	id     int
}

type codexConfigEdit struct {
	KeyPath       string `json:"keyPath"`
	Value         any    `json:"value"`
	MergeStrategy string `json:"mergeStrategy"`
}

type codexLayerSource struct {
	Type    string `json:"type"`
	File    string `json:"file"`
	Project string `json:"dotCodexFolder"`
	Profile string `json:"profile"`
	ID      string `json:"id"`
	Domain  string `json:"domain"`
}

type codexLayerMetadata struct {
	Name    codexLayerSource `json:"name"`
	Version string           `json:"version"`
}

type codexConfigLayer struct {
	codexLayerMetadata
	Config         map[string]json.RawMessage `json:"config"`
	DisabledReason string                     `json:"disabledReason"`
}

type codexConfigRead struct {
	Config  map[string]json.RawMessage    `json:"config"`
	Origins map[string]codexLayerMetadata `json:"origins"`
	Layers  []codexConfigLayer            `json:"layers"`
}

func sameCodexConfigPath(actual, expected string) bool {
	if actual == expected {
		return true
	}
	actualParent, err := filepath.EvalSymlinks(filepath.Dir(actual))
	if err != nil || !filepath.IsAbs(actual) {
		return false
	}
	expectedParent, err := filepath.EvalSymlinks(filepath.Dir(expected))
	return err == nil && actualParent == expectedParent && filepath.Base(actual) == filepath.Base(expected)
}

func withCodexConfigPipe(ctx context.Context, binary string, env profileClientEnvironment, directory string, args []string, work func(*codexConfigPipe) error) error {
	if binary == "" {
		var err error
		binary, err = nativeCodexPath()
		if err != nil {
			return err
		}
	}
	argv := append([]string{"app-server", "--listen", "stdio://"}, profilePolicyArguments(env)...)
	argv = append(argv, args...)
	argv = append(argv, "-c", "check_for_update_on_startup=false", "-c", "features.remote_control=false")
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, argv...)
	cmd.Dir, cmd.Env, cmd.WaitDelay = directory, env.Environment, 2*time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return errors.New("cannot open Codex App Server input")
	}
	defer stdin.Close()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return errors.New("cannot open Codex App Server output")
	}
	defer stdout.Close()
	if err := cmd.Start(); err != nil {
		return errors.New("cannot start Codex App Server for configuration")
	}
	stopPipeWatch := context.AfterFunc(ctx, func() {
		_ = stdout.Close()
		_ = stdin.Close()
	})
	defer stopPipeWatch()
	defer func() {
		_ = stdin.Close()
		cancel()
		_ = cmd.Wait()
	}()
	rpc := &codexConfigPipe{input: json.NewEncoder(stdin), output: bufio.NewScanner(stdout)}
	rpc.output.Buffer(make([]byte, 4096), 8<<20)
	var initialized struct{ CodexHome string }
	if err := rpc.call("initialize", map[string]any{
		"clientInfo":   map[string]string{"name": "agent-harbor", "version": version},
		"capabilities": map[string]bool{"experimentalApi": true},
	}, &initialized); err != nil {
		return err
	}
	if initialized.CodexHome == "" || !sameCodexConfigPath(filepath.Join(initialized.CodexHome, "config.toml"), filepath.Join(env.Home, "config.toml")) {
		return errors.New("Codex App Server reported a different Profile home")
	}
	if err := rpc.input.Encode(map[string]string{"method": "initialized"}); err != nil {
		return errors.New("cannot initialize Codex App Server")
	}
	return work(rpc)
}

var errCodexConfigConflict = errors.New("Codex configuration changed; refresh it before saving")

func (rpc *codexConfigPipe) call(method string, params any, result any) error {
	rpc.id++
	if err := rpc.input.Encode(map[string]any{"id": rpc.id, "method": method, "params": params}); err != nil {
		return fmt.Errorf("Codex %s request unavailable", method)
	}
	for rpc.output.Scan() {
		var reply struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Data struct {
					Code string `json:"config_write_error_code"`
				} `json:"data"`
			} `json:"error"`
		}
		if json.Unmarshal(rpc.output.Bytes(), &reply) != nil || reply.Method != "" || reply.ID != rpc.id {
			continue
		}
		if reply.Error != nil {
			if reply.Error.Data.Code == "configVersionConflict" {
				return errCodexConfigConflict
			}
			return fmt.Errorf("Codex rejected %s; check its configuration or managed requirements", method)
		}
		if len(reply.Result) == 0 || json.Unmarshal(reply.Result, result) != nil {
			return fmt.Errorf("Codex %s returned an unexpected response", method)
		}
		return nil
	}
	return fmt.Errorf("Codex %s response unavailable: %w", method, io.ErrUnexpectedEOF)
}

func (rpc *codexConfigPipe) config(directory string) (codexConfigRead, error) {
	var result codexConfigRead
	err := rpc.call("config/read", map[string]any{"cwd": directory, "includeLayers": true}, &result)
	return result, err
}

func configValue[T any](config map[string]json.RawMessage, key string) T {
	var value T
	_ = json.Unmarshal(config[key], &value)
	return value
}
