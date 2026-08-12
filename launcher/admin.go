package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type readinessDocument struct {
	Identity struct {
		InstanceID string `json:"instance_id"`
	} `json:"identity"`
	Snapshot struct {
		Generation int64 `json:"generation"`
	} `json:"snapshot"`
}

func adminSocket(paths productPaths) string {
	return filepath.Join(paths.StateRoot, "admin", "agent-harbor-admin.sock")
}

func adminRequest(paths productPaths, method, requestPath string, body []byte, output any) (int, error) {
	socket := adminSocket(paths)
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		dialer := net.Dialer{Timeout: 2 * time.Second}
		return dialer.DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	request, err := http.NewRequest(method, "http://agent-harbor"+requestPath, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	request.Header.Set("Accept", "application/json")
	if len(body) != 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	limited, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return response.StatusCode, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response.StatusCode, fmt.Errorf("Core Admin returned HTTP %d: %s", response.StatusCode, compactBody(limited))
	}
	if output != nil && len(limited) != 0 {
		if err := json.Unmarshal(limited, output); err != nil {
			return response.StatusCode, err
		}
	}
	return response.StatusCode, nil
}

func compactBody(body []byte) string {
	if len(body) > 512 {
		body = body[:512]
	}
	return string(bytes.TrimSpace(body))
}
func readReadiness(paths productPaths) (readinessDocument, error) {
	var result readinessDocument
	_, err := adminRequest(paths, http.MethodGet, "/v1/readiness", nil, &result)
	return result, err
}
func coreRunning(paths productPaths) bool { _, err := readReadiness(paths); return err == nil }

func commandStatus(paths productPaths, args []string, stdout io.Writer) error {
	if len(args) != 0 {
		return usageError("status accepts no arguments")
	}
	readiness, err := readReadiness(paths)
	if err != nil {
		fmt.Fprintln(stdout, "Agent Harbor is stopped.")
		return errors.New("Core is not ready")
	}
	fmt.Fprintf(stdout, "Agent Harbor is running (%s).\n", readiness.Identity.InstanceID)
	return nil
}

func commandStop(paths productPaths, args []string, stdout io.Writer) error {
	if len(args) != 0 {
		return usageError("stop accepts no arguments")
	}
	readiness, err := readReadiness(paths)
	if err != nil {
		fmt.Fprintln(stdout, "Agent Harbor is already stopped.")
		return nil
	}
	body, _ := json.Marshal(map[string]any{"instance_id": readiness.Identity.InstanceID, "drain_timeout_ms": 10000})
	if _, err := adminRequest(paths, http.MethodPost, "/v1/shutdown", body, nil); err != nil {
		return err
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if !coreRunning(paths) {
			fmt.Fprintln(stdout, "Agent Harbor stopped.")
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("Core did not stop within 15 seconds")
}

func readInstanceID(paths productPaths) string {
	source, err := os.ReadFile(filepath.Join(paths.StateRoot, "instance.json"))
	if err != nil {
		return ""
	}
	var value struct {
		InstanceID string `json:"instance_id"`
	}
	if json.Unmarshal(source, &value) != nil {
		return ""
	}
	return value.InstanceID
}
