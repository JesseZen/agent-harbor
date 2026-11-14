package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStatusReportsRecordedGatewayWithoutChangingState(t *testing.T) {
	b := newTestBridge(t)
	b.sessions()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"name":"magpie"}`)) }))
	defer srv.Close()
	data, _ := json.Marshal(magpieRegistry{Default: "test", Instances: []magpieInstance{{ID: "test", Address: srv.URL}}})
	path := filepath.Join(b.root, "bridge", "magpie.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	out, err := b.command("_status")
	if err != nil || !strings.Contains(out, "online") || !strings.Contains(out, "identity unchecked") {
		t.Fatal(out, err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(data) {
		t.Fatal("status mutated registry")
	}
	srv.Close()
	out, err = b.command("_status")
	if err != nil || !strings.Contains(out, "offline") {
		t.Fatal(out, err)
	}
}

func TestStatusDoesNotWaitForUnresponsiveGateway(t *testing.T) {
	b := newTestBridge(t)
	b.sessions()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
	defer srv.Close()
	defer close(release)
	data, _ := json.Marshal(magpieRegistry{Default: "slow", Instances: []magpieInstance{{ID: "slow", Address: srv.URL}}})
	if err := os.WriteFile(filepath.Join(b.root, "bridge", "magpie.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	out, err := b.command("_status")
	if err != nil || !strings.Contains(out, "offline") || time.Since(start) > time.Second {
		t.Fatalf("status took %s: %s %v", time.Since(start), out, err)
	}
}
