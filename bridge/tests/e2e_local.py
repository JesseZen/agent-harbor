#!/usr/bin/env python3
"""Exercise a real, isolated Herdr/Codex session against a local mock.

Usage: python3 tests/e2e_local.py --herdr /path/to/herdr --codex /path/to/codex
The temporary test root is retained for diagnostics. No real accounts are used.
"""

import argparse
import http.server
import json
import os
import pathlib
import shutil
import subprocess
import sys
sys.dont_write_bytecode = True
import tempfile
import threading
import time
from fixture_process import stop_codex

FIRST = "AH_LOCAL_FIRST"
SECOND = "AH_LOCAL_SECOND"
ANSWER = "HELLO_FROM_ISOLATED_MOCK"


class MockResponses(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_args):
        pass

    def do_GET(self):
        self.reply(json.dumps({"object": "list", "data": [{"id": "test-model", "object": "model", "owned_by": "mock"}]}).encode(), "application/json")

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers.get("Content-Length", "0"))))
        self.respond(body)

    def respond(self, body):
        content = json.dumps(body)
        self.server.observed.append((self.path, [marker for marker in (FIRST, SECOND) if marker in content]))
        answer = ANSWER
        schema = ((body.get('text') or {}).get('format') or {}).get('schema') or {}
        if getattr(self.server, 'automatic_title', None) and 'title' in schema.get('properties', {}):
            answer = json.dumps({'title': self.server.automatic_title})
            self.server.title_calls = getattr(self.server, 'title_calls', 0) + 1
        message = {"id": "msg_test", "type": "message", "status": "completed", "role": "assistant", "content": [{"type": "output_text", "text": answer, "annotations": []}]}
        response = {"id": "resp_test", "object": "response", "status": "completed", "model": "test-model", "output": [message], "usage": {"input_tokens": 10, "output_tokens": 5, "total_tokens": 15}}
        events = [
            {"type": "response.created", "response": {"id": "resp_test", "object": "response", "status": "in_progress", "model": "test-model", "output": []}},
            {"type": "response.output_item.added", "output_index": 0, "item": dict(message, status="in_progress", content=[])},
            {"type": "response.content_part.added", "item_id": "msg_test", "output_index": 0, "content_index": 0, "part": {"type": "output_text", "text": "", "annotations": []}},
            {"type": "response.output_text.delta", "item_id": "msg_test", "output_index": 0, "content_index": 0, "delta": answer},
            {"type": "response.output_text.done", "item_id": "msg_test", "output_index": 0, "content_index": 0, "text": answer},
            {"type": "response.content_part.done", "item_id": "msg_test", "output_index": 0, "content_index": 0, "part": message["content"][0]},
            {"type": "response.output_item.done", "output_index": 0, "item": message},
            {"type": "response.completed", "response": response},
        ]
        stream = "".join("event: " + event["type"] + "\ndata: " + json.dumps(event) + "\n\n" for event in events).encode()
        self.reply(stream, "text/event-stream")

    def reply(self, body, content_type):
        self.send_response(200)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--herdr", required=True, help="Herdr 0.9.5 executable")
    parser.add_argument("--codex", required=True, help="Codex 0.167.0 executable")
    args = parser.parse_args()
    root = pathlib.Path(tempfile.mkdtemp(prefix="ah-bridge-e2e-", dir="/tmp")).resolve()
    for name in ("bin", "home", "project", "codex", "sqlite", "tmp"):
        (root / name).mkdir(mode=0o700)
    tmux = shutil.which("tmux")
    node = shutil.which("node")
    go = shutil.which("go")
    if not all((tmux, node, go)):
        raise RuntimeError("tmux, node, and go must be on PATH")
    for name,target in (("herdr",args.herdr),("codex",args.codex)):
        source=pathlib.Path(target).resolve()
        if name=="codex" and source.read_bytes()[:4] not in (b"\x7fELF",b"\xcf\xfa\xed\xfe"):
            raise RuntimeError("native Codex required")
        shutil.copy2(source,root/"bin"/name)
        (root/"bin"/name).chmod(0o700)
    (root/"bin/node").symlink_to(node)
    bridge = root / "bin" / "agent-harbor-bridge"
    env = {
        "PATH": str(root / "bin") + ":/usr/bin:/bin:/usr/sbin:/sbin", "HOME": str(root / "home"),
        "XDG_CONFIG_HOME": str(root / "user-config"), "XDG_STATE_HOME": str(root / "user-state"),
        "XDG_DATA_HOME": str(root / "user-data"), "XDG_CACHE_HOME": str(root / "user-cache"),
        "XDG_RUNTIME_DIR": str(root / "runtime"), "CODEX_HOME": str(root / "codex"), "CODEX_SQLITE_HOME": str(root / "sqlite"),
        "AGENT_HARBOR_BRIDGE_HOME": str(root / "bridge"), "TMPDIR": str(root / "tmp"),
        "TERM": "xterm-256color", "SHELL": "/bin/sh", "LANG": "C.UTF-8",
    }
    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), MockResponses)
    server.observed = []
    threading.Thread(target=server.serve_forever, daemon=True).start()
    socket = str(root / "tmux.sock")

    def run(command, **options):
        return subprocess.run([str(x) for x in command], env=env, check=True, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, **options)

    def tmux_run(*command):
        return run([tmux, "-S", socket, *command]).stdout

    def send(*keys):
        tmux_run("send-keys", "-t", "bridge-test", *keys)

    def screen():
        return tmux_run("capture-pane", "-p", "-t", "bridge-test")

    def wait_for(predicate, description, timeout=30):
        end = time.monotonic() + timeout
        while time.monotonic() < end:
            if predicate():
                return
            time.sleep(0.1)
        raise RuntimeError("timed out waiting for " + description)

    def wait_screen(text):
        wait_for(lambda: text in screen(), text)

    def sessions():
        return json.loads(run([bridge, "sessions", "--json"]).stdout)

    def open_ui():
        run([tmux, "-S", socket, "-f", "/dev/null", "new-session", "-d", "-s", "bridge-test", "-x", "140", "-y", "40", bridge, "open"], cwd=root / "project")
        wait_screen("Press Enter to open Herdr")
        send("Enter")
        wait_screen("spaces")

    try:
        if run([root / "bin/herdr", "--version"]).stdout.strip() != "herdr 0.9.5":
            raise RuntimeError("this test requires Herdr 0.9.5")
        if run([root / "bin/codex", "--version"]).stdout.strip() != "codex-cli 0.167.0":
            raise RuntimeError("this test requires Codex 0.167.0")
        build_env = dict(os.environ, GOWORK="off")
        subprocess.run([go, "build", "-o", str(bridge), "."], cwd=pathlib.Path(__file__).resolve().parents[1], env=build_env, check=True)
        (root / "codex/config.toml").write_text(
            'check_for_update_on_startup = false\nmodel = "test-model"\nmodel_provider = "isolated"\ncli_auth_credentials_store = "file"\n'
            '[model_providers.isolated]\nname = "isolated"\nbase_url = "http://127.0.0.1:' + str(server.server_port) + '/v1"\n'
            'requires_openai_auth = false\nwire_api = "responses"\n'
            '[projects.' + json.dumps(str(root / 'project')) + ']\ntrust_level = "trusted"\n'
        )
        open_ui()
        send("C-b", "a")
        wait_screen("Choose an action")
        send("1", "Enter")
        wait_screen("Path (Enter uses default)")
        send("-l", str(root / "project"))
        send("Enter")
        wait_screen("Choose connection")
        send("Enter")
        wait_screen("Ask Codex")
        send("-l", FIRST)
        wait_screen(FIRST)
        send("Enter")
        wait_for(lambda: any(FIRST in marks for _, marks in server.observed), "first local request")
        wait_screen(ANSWER)
        wait_screen("Ask Codex")
        wait_for(lambda: sessions()[0]["recovery"] == "ready" and sessions()[0].get("conversation_id_source")=="captured" and sessions()[0].get("codex_session_id"), "official session identity")
        current = sessions()[0]
        assert current.get('herdr_pane') and current.get('herdr_tab'), 'pane/tab not recorded'
        run([bridge, 'rename', current['id'], 'Renamed Harbor session'])
        named = run([root / 'bridge/herdr/bin/herdr', 'tab', 'get', current['herdr_tab']]).stdout
        assert 'Renamed Harbor session' in named, 'Herdr tab did not rename'
        run([bridge, 'focus', current['id']])
        session_id = sessions()[0]["codex_session_id"]
        group = sessions()[0]["process_group"]
        run([bridge, "stop"])
        wait_for(lambda: subprocess.run([tmux, "-S", socket, "has-session", "-t", "bridge-test"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode != 0, "Herdr stop")
        def group_gone():
            try:
                os.killpg(group, 0)
                return False
            except ProcessLookupError:
                return True
            except PermissionError:
                return False
        wait_for(group_gone, "old Codex process group to exit")
        count = len(server.observed)
        open_ui()
        time.sleep(1)
        assert len(server.observed) == count, "Herdr restored an agent automatically"
        send("C-b", "a")
        wait_screen("Choose an action")
        send("2", "Enter")
        wait_screen("Session number")
        send("1", "Enter")
        wait_screen(session_id)
        wait_screen("Type yes to resume")
        send("yes", "Enter")
        wait_screen(FIRST)
        send("-l", SECOND)
        wait_screen(SECOND)
        send("Enter")
        wait_for(lambda: any(FIRST in marks and SECOND in marks for _, marks in server.observed), "resumed conversation at mock server")
        wait_screen(SECOND)
        run([bridge, 'stop-session', sessions()[0]['id']], input='yes\n')
        print("PASS: real Herdr rename/focus/stop/restart, automatic capture, no UUID typing, exact history continuity")
    except Exception as exc:
        print(type(exc).__name__ + ": " + str(exc), flush=True)
        if isinstance(exc, subprocess.CalledProcessError): print(exc.stdout, exc.stderr, flush=True)
        try:
            (root / "failure-screen.txt").write_text(screen())
        except Exception:
            pass
        raise
    finally:
        subprocess.run([str(bridge), "stop"], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL) if bridge.exists() else None
        subprocess.run([tmux, "-S", socket, "kill-server"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        server.shutdown()
        stop_codex(root, env)
        print("Retained isolated diagnostics:", root)


if __name__ == "__main__":
    main()
