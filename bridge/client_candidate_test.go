package main

import (
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Controlled upstream data proves transport only, never a real future client.
func TestFutureNativeClientIsNotSilentlySubstituted(t *testing.T) {
	b := newTestBridge(t)
	b.env = append(b.env, "AGENT_HARBOR_IDENTITY_CANDIDATE=1")
	out, err := b.command("create", "--dir", b.project, "--client", "future-kind")
	if err == nil || !strings.Contains(out, "managed Herdr") {
		t.Fatalf("missing upstream context must explain recovery action: %s %v", out, err)
	}
	if _, err := os.Stat(filepath.Join(b.root, "capture")); !os.IsNotExist(err) {
		t.Fatal("substituted Codex")
	}
}

func TestFutureKindUsesSharedNativeIdentityAndQuery(t *testing.T) {
	for _, ref := range []herdrIdentity{{"official:future/v2", "future-kind", "path", "/opaque/conversation with spaces"}, {"herdr:claude", "claude", "id", "fixture-opaque-id"}} {
		t.Run(ref.Agent, func(t *testing.T) {
			b := newTestBridge(t)
			shortRoot, err := os.MkdirTemp(os.Getenv("TMPDIR"), "b52")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(shortRoot)
			if err = os.CopyFS(shortRoot, os.DirFS(b.root)); err != nil {
				t.Fatal(err)
			}
			old := b.root
			b.root = shortRoot
			b.project = strings.ReplaceAll(b.project, old, shortRoot)
			for i := range b.env {
				b.env[i] = strings.ReplaceAll(b.env[i], old, shortRoot)
			}
			os.Chmod(filepath.Join(b.root, "bin", "codex"), 0700)
			if out, err := b.command("sessions"); err != nil {
				t.Fatal(out, err)
			}
			socket := queryTestSocket(t, b)
			short, err := os.MkdirTemp(os.Getenv("TMPDIR"), "f52")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(short)
			discovery := filepath.Join(short, "s")
			listener, err := net.Listen("unix", discovery)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			// queryTestSocket owns identity association; replace its hard link with a
			// fresh listener at the same short pathname (never a live user socket).
			os.Remove(socket)
			if err = os.Link(discovery, socket); err != nil {
				t.Fatal(err)
			}
			go func() {
				for {
					c, e := listener.Accept()
					if e != nil {
						return
					}
					go func() {
						defer c.Close()
						var request map[string]any
						json.NewDecoder(c).Decode(&request)
						json.NewEncoder(c).Encode(map[string]any{"result": map[string]any{"integrations": []map[string]any{{"target": ref.Agent, "command": ref.Agent, "available": true, "state": "current"}}}})
					}()
				}
			}()
			h := filepath.Join(b.root, "bin", "herdr")
			report, _ := json.Marshal(map[string]any{"result": map[string]any{"pane": map[string]any{"agent_session": ref}}})
			plan, _ := json.Marshal(resumeCommandPlan{1, ref.Agent, []string{ref.Agent, "opaque-recover", ref.Value}})
			os.WriteFile(h, []byte("#!/bin/sh\ncase \"$*\" in\n 'agent resume-command') cat > \"$HOME/query-request\"; echo '"+string(plan)+"';;\n *) if test -f \"$CAPTURE\"; then echo '"+string(report)+"'; else echo '{\"result\":{\"pane\":{}}}'; fi;;\nesac\n"), 0700)
			writeNativeClientFixture(filepath.Join(b.root, "bin", ref.Agent), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$CAPTURE\"\n"), 0700)
			b.env = append(b.env, "AGENT_HARBOR_IDENTITY_CANDIDATE=1", "HERDR_PLUGIN_ID=agent-harbor.bridge", "HERDR_PLUGIN_ENTRYPOINT_ID=session", "HERDR_PANE_ID=opaque-pane", "HERDR_SOCKET_PATH="+socket, "HERDR_BIN_PATH="+h, "AGENT_HARBOR_HERDR_BIN="+h)
			out, err := b.command("create", "--dir", b.project, "--client", ref.Agent)
			if err != nil {
				t.Fatal(out, err)
			}
			rec := b.sessions()[0]
			if rec.Conversation == nil || rec.Conversation.Reference != ref || rec.Recovery != "ready" || rec.CodexSessionID != "" {
				t.Fatalf("opaque identity lost: %+v", rec)
			}
			os.Remove(filepath.Join(b.root, "capture"))
			cmd := exec.Command(os.Args[0], "-test.run=^TestBridgeProcess$", "--", "resume", "--development-query", rec.ID)
			cmd.Env = b.env
			cmd.Stdin = strings.NewReader("accept\nyes\n")
			outBytes, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatal(string(outBytes), err)
			}
			args, _ := os.ReadFile(filepath.Join(b.root, "capture"))
			if string(args) != "opaque-recover\n"+ref.Value+"\n" {
				t.Fatalf("upstream argv altered: %q", args)
			}
			query, _ := os.ReadFile(filepath.Join(b.root, "query-request"))
			var got herdrIdentity
			json.Unmarshal(query, &got)
			if got != ref {
				t.Fatal("complete reference not sent")
			}
		})
	}
}

// Native configuration support never grants a Magpie configuration adapter.
func TestAdditionalClientMagpieRequiresIsolationEvidence(t *testing.T) {
	b := newTestBridge(t)
	b.env = append(b.env, "AGENT_HARBOR_IDENTITY_CANDIDATE=1")
	out, err := b.command("create", "--dir", b.project, "--client", "claude", "--connection", "magpie")
	if err == nil || !strings.Contains(out, "isolation unproven") {
		t.Fatalf("Magpie incorrectly inferred from native client: %s %v", out, err)
	}
	if len(b.sessions()) != 0 {
		t.Fatal("unsupported connection created record")
	}
}
