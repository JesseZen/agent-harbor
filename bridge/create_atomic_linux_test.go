package main

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

// Observe durable store replacements at the application-service boundary.
func TestCreatePublishesCompleteIntentOnce(t *testing.T) {
	b := newTestBridge(t)
	for _, e := range b.env {
		if k, v, ok := strings.Cut(e, "="); ok {
			t.Setenv(k, v)
		}
	}
	b.sessions()
	fd, err := syscall.InotifyInit1(syscall.IN_NONBLOCK)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(fd)
	if _, err = syscall.InotifyAddWatch(fd, filepath.Join(b.root, "bridge"), syscall.IN_MOVED_TO|syscall.IN_CREATE); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp("/tmp", "create-review-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "s")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	t.Setenv("HERDR_SOCKET_PATH", socket)
	go func() {
		c, e := listener.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		var req any
		json.NewDecoder(c).Decode(&req)
		c.Write([]byte(`{"result":{"integrations":[{"target":"future-kind","command":"future-kind","available":true}]}}` + "\n"))
	}()
	h := harborService{}
	s, err := h.Create(sessionDraft{Directory: b.project, Connection: "local", Name: "Selected project", Client: "future-kind"})
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 8192)
	n, err := syscall.Read(fd, buf)
	if err != nil {
		t.Fatal(err)
	}
	writes := 0
	for off := 0; off < n; {
		e := (*syscall.InotifyEvent)(unsafe.Pointer(&buf[off]))
		name := strings.TrimRight(string(buf[off+syscall.SizeofInotifyEvent:off+syscall.SizeofInotifyEvent+int(e.Len)]), "\x00")
		if name == "sessions.json" {
			writes++
		}
		off += syscall.SizeofInotifyEvent + int(e.Len)
	}
	got, err := h.Get(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if writes != 1 || got.Name != "Selected project" || got.CLI != "future-kind" || got.Mode != "local" || got.WorkingDirectory != b.project {
		t.Fatalf("intent published in %d writes: %+v", writes, got.session)
	}
}
