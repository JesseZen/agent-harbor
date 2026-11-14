#!/usr/bin/env python3
"""Exercise a packaged Bridge with isolated Herdr, Magpie, Codex and MCP.

Requires tmux, Node, Codex 0.167.0 and a built StGerman/herdr-mcp 0.2.0.
The temporary test root is retained for diagnostics. No real accounts are used.
"""

import argparse
import http.server
import io
import json
import os
import pathlib
import shutil
import shlex
import socket
import subprocess
import tempfile
import tarfile
import sys
sys.dont_write_bytecode = True
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
        content = json.dumps(body)
        self.server.observed.append((self.path, [marker for marker in (FIRST, SECOND, "AH_RELAY_B", "AH_LOCAL_C", "AH_MCP_test-a", "AH_MCP_test-b", "AH_RESUME_B", "AH_GROUP_TEST") if marker in content]))
        self.server.events.append({"title": "title" in content.lower(), "markers": [marker for marker in (FIRST, SECOND, "AH_RELAY_B", "AH_MCP_test-a", "AH_MCP_test-b", "AH_RESUME_B") if marker in content]})
        def chunk(delta, finish=None):
            return {"id":"chatcmpl-test","object":"chat.completion.chunk","created":1,"model":"test-model","choices":[{"index":0,"delta":delta,"finish_reason":finish}]}
        data = "".join("data: "+json.dumps(x)+"\n\n" for x in [chunk({"role":"assistant","content":""}),chunk({"content":ANSWER}),chunk({},"stop")])+"data: [DONE]\n\n"
        self.reply(data.encode(),"text/event-stream")

    def reply(self, body, content_type):
        self.send_response(200)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--package", required=True, help="Agent Harbor package tar.gz for this platform")
    parser.add_argument("--mcp", required=True, help="built herdr-mcp dist/index.js")
    parser.add_argument("--codex", required=True, help="Codex 0.167.0 executable")
    args = parser.parse_args()
    root = pathlib.Path(tempfile.mkdtemp(prefix="ah-bridge-e2e-", dir="/tmp")).resolve()
    for name in ("bin", "app", "home", "project", "codex", "sqlite", "tmp"):
        (root / name).mkdir(mode=0o700)
    with tarfile.open(args.package) as archive:
        archive.extractall(root / "app", filter="data")
    tmux = shutil.which("tmux")
    node = shutil.which("node")
    if not all((tmux, node)):
        raise RuntimeError("tmux and node must be on PATH")
    for name, target in (("herdr", root / "app" / "herdr"), ("codex", args.codex), ("node", node)):
        (root / "bin" / name).symlink_to(pathlib.Path(target).resolve())
    bridge = root / "app" / "agent-harbor"
    env = {
        "PATH": str(root / "bin") + ":/usr/bin:/bin:/usr/sbin:/sbin", "HOME": str(root / "home"),
        "XDG_CONFIG_HOME": str(root / "user-config"), "XDG_STATE_HOME": str(root / "user-state"),
        "XDG_DATA_HOME": str(root / "user-data"), "XDG_CACHE_HOME": str(root / "user-cache"),
        "CODEX_HOME": str(root / "codex"), "CODEX_SQLITE_HOME": str(root / "sqlite"),
        "AGENT_HARBOR_BRIDGE_HOME": str(root / "bridge"), "TMPDIR": str(root / "tmp"),
        "TERM": "xterm-256color", "SHELL": "/bin/sh", "LANG": "C.UTF-8",
    }
    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), MockResponses)
    server.observed = []
    threading.Thread(target=server.serve_forever, daemon=True).start()
    server.events = []
    import importlib.util
    spec = importlib.util.spec_from_file_location("local_test", str(pathlib.Path(__file__).with_name("e2e_local.py")))
    local_test = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(local_test)
    local_test.FIRST = "AH_LOCAL_C"
    local_test.SECOND = "AH_RESUME_C"
    server_b = http.server.ThreadingHTTPServer(("127.0.0.1", 0), MockResponses)
    server_b.events = []
    class MCPProbe(local_test.MockResponses):
        def do_POST(self):
            raw = self.rfile.read(int(self.headers.get("Content-Length", "0")))
            self.server.markers.append([m for m in (FIRST, "AH_RELAY_B", "AH_LOCAL_C") if m.encode() in raw])
            body = json.loads(raw)
            inputs = body.get("input", [])
            outputs = [x for x in inputs if isinstance(x, dict) and x.get("type") == "function_call_output" and x.get("call_id") == "call_probe"]
            if outputs:
                self.server.tool_output = outputs[-1].get("output", "")
            elif "AH_MCP_PROBE" in raw.decode() and body.get("tools"):
                tools = body.get("tools", [])
                namespace = next((x for x in tools if x.get("name") == "mcp__herdr"), {})
                name = next((x.get("name") for x in namespace.get("tools", []) if x.get("name") == "herdr_pane_list"), None)
                if not name:
                    self.server.tool_error = "Herdr MCP tool missing from Codex request: " + repr([(x.get("type"), x.get("name")) for x in tools])
                else:
                    item = {"id":"fc_probe", "type":"function_call", "status":"completed", "call_id":"call_probe", "namespace":"mcp__herdr", "name":name, "arguments":"{}"}
                    response = {"id":"resp_probe", "object":"response", "status":"completed", "model":"test-model", "output":[item], "usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}
                    events = [{"type":"response.created", "response":{"id":"resp_probe","object":"response","status":"in_progress","model":"test-model","output":[]}}, {"type":"response.output_item.added","output_index":0,"item":dict(item,status="in_progress",arguments="")}, {"type":"response.function_call_arguments.delta","item_id":"fc_probe","output_index":0,"delta":"{}"}, {"type":"response.function_call_arguments.done","item_id":"fc_probe","output_index":0,"arguments":"{}"}, {"type":"response.output_item.done","output_index":0,"item":item}, {"type":"response.completed","response":response}]
                    self.reply("".join("event: "+e["type"]+"\ndata: "+json.dumps(e)+"\n\n" for e in events).encode(), "text/event-stream")
                    return
            self.rfile = io.BytesIO(raw)
            super().do_POST()
    server_c = http.server.ThreadingHTTPServer(("127.0.0.1", 0), MCPProbe)
    server_c.tool_output, server_c.tool_error, server_c.markers = "", "", []
    for extra in (server_b, server_c):
        extra.observed = []
        threading.Thread(target=extra.serve_forever, daemon=True).start()
    tmux_socket = str(root / "tmux.sock")
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        magpie_port = sock.getsockname()[1]
    magpie = root / "bin" / "magpie"
    shutil.copy(root / "app" / "magpie", magpie)
    env["MAGPIE_NO_ACCOUNTS"] = "1"
    env["MAGPIE_ADDR"] = f"127.0.0.1:{magpie_port}"
    subprocess.run([magpie, "provider", "add", "relay-a", f"url=http://127.0.0.1:{server.server_port}/v1", "key=test-only", "models=test-model"], env=env, check=True, capture_output=True)
    subprocess.run([magpie, "provider", "add", "relay-b", f"url=http://127.0.0.1:{server_b.server_port}/v1", "key=test-only", "models=test-model"], env=env, check=True, capture_output=True)
    subprocess.run([magpie, "group", "add", "test-group", "models=relay-a/test-model,relay-b/test-model", "routing=rotate", "stays=off"], env=env, check=True, capture_output=True)
    (root / "user-config" / "magpie" / "settings.json").write_text(json.dumps({"port":magpie_port}))
    magpie_log = open(root / "magpie.log", "w")
    magpie_proc = subprocess.Popen([magpie, "serve"], env=env, stdout=magpie_log, stderr=subprocess.STDOUT)
    import urllib.request
    for _ in range(100):
        try:
            urllib.request.urlopen(f"http://127.0.0.1:{magpie_port}/api/hello", timeout=1).close()
            break
        except Exception: time.sleep(.1)
    else: raise RuntimeError("Magpie did not start")
    with urllib.request.urlopen(f"http://127.0.0.1:{magpie_port}/v1/models") as response:
        catalog_ids = [m["id"] for m in json.load(response)["data"]]


    def run(command, **options):
        return subprocess.run([str(x) for x in command], env=env, check=True, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, **options)

    def tmux_run(*command):
        return run([tmux, "-S", tmux_socket, *command]).stdout

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
        run([tmux, "-S", tmux_socket, "-f", "/dev/null", "new-session", "-d", "-s", "bridge-test", "-x", "140", "-y", "40", bridge, "open"], cwd=root / "project")
        wait_screen("Press Enter to open Herdr")
        send("Enter")
        wait_screen("spaces")

    try:
        if run([root / "bin/herdr", "--version"]).stdout.strip() != "herdr 0.9.5":
            raise RuntimeError("this test requires Herdr 0.9.5")
        if run([root / "bin/codex", "--version"]).stdout.strip() != "codex-cli 0.167.0":
            raise RuntimeError("this test requires Codex 0.167.0")

        (root / "codex/config.toml").write_text(
            'model = "test-model"\nmodel_provider = "isolated"\ncli_auth_credentials_store = "file"\n'
            '[model_providers.isolated]\nname = "isolated"\nbase_url = "http://127.0.0.1:' + str(server_c.server_port) + '/v1"\n'
            'requires_openai_auth = false\nwire_api = "responses"\n'
            '[mcp_servers.herdr]\ncommand = ' + json.dumps(str(root / 'bin/node')) + '\nargs = [' + json.dumps(str(pathlib.Path(args.mcp).resolve())) + ']\n'
        )
        run([root / "bin/herdr", "integration", "install", "codex"])
        (root / "bin/herdr").unlink()
        open_ui()
        send("C-b", "a")
        wait_screen("Choose an action")
        send("1", "Enter")
        wait_screen("Path (Enter uses default)")
        send("-l", str(root / "project"))
        send("Enter")
        wait_screen("Choose connection")
        send("2", "Enter")
        wait_screen("Address (Enter accepts proposed)")
        send("Enter")
        wait_screen("Model number")
        send(str(catalog_ids.index("relay-a/test-model") + 1), "Enter")
        wait_screen("Type yes to start")
        send("yes", "Enter")
        wait_for(lambda: "Hooks need review" in screen() or "1 hook needs review" in screen(), "hook review")
        assert len(sessions()) == 1 and sessions()[0]["recovery"] == "needs_id"
        if "Hooks need review" in screen():
            send("Enter")
            wait_screen("1 hook needs review")
        send("Enter")
        wait_screen("New hook - review required")
        send("t")
        wait_screen("Trusted")
        send("Escape")
        wait_screen("Lifecycle hooks")
        send("Escape")
        wait_screen("Ask Codex")
        send("-l", FIRST)
        wait_screen(FIRST)
        send("Enter")
        wait_for(lambda: any(FIRST in marks for _, marks in server.observed), "first local request")
        wait_screen(ANSWER)
        wait_screen("Ask Codex")
        wait_for(lambda: sessions()[0]["recovery"] == "needs_review" and sessions()[0].get("codex_session_id"), "official session identity")
        session_id = sessions()[0]["codex_session_id"]
        # Keep the first process alive while opening a second Magpie connection.
        send("C-b", "a")
        wait_screen("Choose an action")
        send("1", "Enter")
        wait_screen("Path (Enter uses default)")
        send("-l", str(root / "project"))
        send("Enter")
        wait_screen("Choose connection")
        send("2", "Enter")
        wait_screen("Address (Enter accepts proposed)")
        send("Enter")
        wait_screen("Model number")
        send(str(catalog_ids.index("relay-b/test-model") + 1), "Enter")
        wait_screen("Type yes to start")
        send("yes", "Enter")
        wait_screen("relay-b/test-model")
        wait_screen("Ask Codex")
        time.sleep(1)
        send("-l", "AH_RELAY_B")
        wait_screen("AH_RELAY_B")
        time.sleep(1)
        send("Enter")
        wait_for(lambda: any("AH_RELAY_B" in marks for _, marks in server_b.observed), "relay B request")
        # A local Codex connection uses a third mock independently.
        send("C-b", "a")
        wait_screen("Choose an action")
        send("1", "Enter")
        wait_screen("Path (Enter uses default)")
        send("-l", str(root / "project"))
        send("Enter")
        wait_screen("Choose connection")
        send("Enter")
        wait_for(lambda: len(sessions()) == 3 and sessions()[2]["started"], "local session start")
        wait_screen("│  test-model default")
        wait_screen("Ask Codex")
        send("-l", "AH_LOCAL_C")
        wait_screen("AH_LOCAL_C")
        time.sleep(1)
        send("Enter")
        wait_for(lambda: any("AH_LOCAL_C" in marks for _, marks in server_c.observed), "local C request")
        for label, observed, wrong in (("A", [m for _, m in server.observed], ("AH_RELAY_B", "AH_LOCAL_C")), ("B", [m for _, m in server_b.observed], (FIRST, "AH_LOCAL_C")), ("C", server_c.markers, (FIRST, "AH_RELAY_B"))):
            if any(marker in marks for marks in observed for marker in wrong):
                raise RuntimeError("unexpected synthetic conversation marker at " + label)
        wait_screen("Ask Codex")
        send("-l", "AH_MCP_PROBE")
        wait_screen("AH_MCP_PROBE")
        time.sleep(1)
        send("Enter")
        wait_screen('Allow the herdr MCP server')
        send("Enter")
        wait_for(lambda: server_c.tool_output or server_c.tool_error, "real Codex MCP tool call")
        if server_c.tool_error or 'isError' in str(server_c.tool_output) or 'pane' not in str(server_c.tool_output).lower():
            raise RuntimeError("Codex MCP discovery failed: " + server_c.tool_error + " " + str(server_c.tool_output))
        print("PASS: real Codex called Herdr MCP pane discovery", flush=True)
        assert len(sessions()) == 3
        # Connect the third-party MCP server only to this isolated Herdr server.
        wrapper = root / "bin" / "test-herdr"
        wrapper.write_text("#!/bin/sh\nexec " + shlex.quote(str(root / "app" / "herdr")) + " --session agent-harbor \"$@\"\n")
        wrapper.chmod(0o700)
        mcp_env = dict(env, HERDR_BIN=str(wrapper), HERDR_CONFIG_PATH=str(root / "bridge/herdr/config/herdr.toml"))
        for key, name in (("XDG_CONFIG_HOME","config"),("XDG_STATE_HOME","state"),("XDG_DATA_HOME","data"),("XDG_CACHE_HOME","cache"),("XDG_RUNTIME_DIR","runtime")):
            mcp_env[key] = str(root / "bridge/herdr" / name)
        mcp = subprocess.Popen([node, str(pathlib.Path(args.mcp).resolve())], env=mcp_env, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, bufsize=1)
        def request(mid, method, params):
            mcp.stdin.write(json.dumps({"jsonrpc":"2.0","id":mid,"method":method,"params":params})+"\n")
            mcp.stdin.flush()
            return json.loads(mcp.stdout.readline())
        try:
            request(1,"initialize",{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"isolated-test","version":"1"}})
            mcp.stdin.write(json.dumps({"jsonrpc":"2.0","method":"notifications/initialized"})+"\n"); mcp.stdin.flush()
            agents=request(2,"tools/call",{"name":"herdr_agent_list","arguments":{}})
            panes=request(3,"tools/call",{"name":"herdr_pane_list","arguments":{}})
            (root / "mcp-list.json").write_text(json.dumps({"agents":agents,"panes":panes},indent=2))
            if agents.get("result",{}).get("isError") or panes.get("result",{}).get("isError"): raise RuntimeError("MCP discovery failed")
            discovered = json.loads(agents["result"]["content"][0]["text"])["result"]["agents"]
            if not discovered or not any(a["pane_id"] in str(server_c.tool_output) for a in discovered):
                raise RuntimeError("Codex MCP returned panes from a different Herdr instance")
            wait_for(lambda: all(s.get("codex_session_id") for s in sessions()[:2]), "Magpie session identities")
            live = sessions()
            for index, label, upstream in ((0,"test-a",server),(1,"test-b",server_b)):
                match = next(a for a in discovered if a.get("agent_session",{}).get("value") == live[index]["codex_session_id"])
                renamed = request(10+index, "tools/call", {"name":"herdr_agent_rename","arguments":{"target":match["pane_id"],"name":label}})
                if renamed.get("result",{}).get("isError"): raise RuntimeError("MCP rename failed: "+str(renamed))
                sent = request(20+index, "tools/call", {"name":"herdr_relay","arguments":{"target":label,"text":"AH_MCP_"+label}})
                if sent.get("result",{}).get("isError"): raise RuntimeError("MCP send failed: "+str(sent))
                wait_for(lambda: any(("AH_MCP_"+label) in marks for _, marks in upstream.observed), "MCP request at "+label)
                read = request(30+index, "tools/call", {"name":"herdr_agent_read","arguments":{"target":label}})
                if read.get("result",{}).get("isError") or ("AH_MCP_"+label) not in json.dumps(read): raise RuntimeError("MCP read failed: "+str(read))
            print("PASS: MCP discovery, name targeting, send and read",flush=True)
        finally:
            mcp.terminate()
            mcp.wait(timeout=10)


        wait_for(lambda: all(s.get("codex_session_id") for s in sessions()), "all session identities")
        before_restart = sessions()
        group = before_restart[0]["process_group"]
        run([bridge, "stop"])
        wait_for(lambda: subprocess.run([tmux, "-S", tmux_socket, "has-session", "-t", "bridge-test"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode != 0, "Herdr stop")
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
        magpie_proc.terminate()
        magpie_proc.wait(timeout=10)
        magpie_proc = subprocess.Popen([magpie, "serve"], env=env, stdout=magpie_log, stderr=subprocess.STDOUT)
        for _ in range(100):
            try:
                urllib.request.urlopen(f"http://127.0.0.1:{magpie_port}/api/hello", timeout=1).close()
                break
            except Exception: time.sleep(.1)
        else: raise RuntimeError("Magpie did not restart")
        open_ui()
        time.sleep(1)
        assert len(server.observed) == count, "Herdr restored an agent automatically"
        send("C-b", "a")
        wait_screen("Choose an action")
        send("2", "Enter")
        wait_screen("Session number")
        send("1", "Enter")
        wait_screen(session_id)
        wait_screen("Verified Codex UUID")
        send("-l", session_id)
        send("Enter")
        wait_screen("Verified directory from /status")
        send("-l", str(root / "project"))
        send("Enter")
        wait_screen("Type yes to resume")
        send("yes", "Enter")
        wait_for(lambda: FIRST in screen() or "Update available" in screen(), "resume or update prompt")
        if "Update available" in screen():
            send("Escape")
        wait_screen(FIRST)
        send("-l", SECOND)
        wait_screen(SECOND)
        send("Enter")
        wait_for(lambda: any(FIRST in marks and SECOND in marks for _, marks in server.observed), "resumed conversation at mock server")
        wait_screen(SECOND)
        for index, upstream, original, marker in ((1,server_b,"AH_RELAY_B","AH_RESUME_B"),(2,server_c,"AH_LOCAL_C","AH_RESUME_C")):
            saved = before_restart[index]
            send("C-b", "a")
            wait_screen("Choose an action")
            send("2", "Enter")
            wait_screen("Session number")
            send(str(index+1), "Enter")
            wait_screen("Verified Codex UUID")
            send("-l", saved["codex_session_id"])
            send("Enter")
            wait_screen("Verified directory from /status")
            send("-l", str(root / "project"))
            send("Enter")
            wait_screen("Type yes to resume")
            send("yes", "Enter")
            time.sleep(3)
            if "Update available" in screen():
                send("Escape")
            wait_screen("Ask Codex")
            time.sleep(2)
            send("-l", marker)
            wait_screen(marker)
            time.sleep(1)
            send("Enter")
            wait_for(lambda: any(original in marks and marker in marks for _, marks in upstream.observed), "resumed history and request "+marker)
            assert sessions()[index]["codex_session_id"] == saved["codex_session_id"]
        print("PASS: three sessions and recovery after both server restarts")
        send("C-b", "a")
        wait_screen("Choose an action")
        send("1", "Enter")
        wait_screen("Path (Enter uses default)")
        send("-l", str(root / "project"))
        send("Enter")
        wait_screen("Choose connection")
        send("2", "Enter")
        wait_screen("Address (Enter accepts proposed)")
        send("Enter")
        wait_screen("Model number")
        send(str(catalog_ids.index("group/test-group") + 1), "Enter")
        wait_screen("Type yes to start")
        send("yes", "Enter")
        wait_for(lambda: len(sessions()) == 4 and sessions()[3]["magpie_model"] == "group/test-group", "routing group selection")
        wait_screen("group/test-group")
        wait_screen("Ask Codex")
        time.sleep(1)
        send("-l", "AH_GROUP_TEST")
        wait_screen("AH_GROUP_TEST")
        time.sleep(1)
        send("Enter")
        wait_for(lambda: any("AH_GROUP_TEST" in marks for _, marks in server.observed + server_b.observed), "group request at a relay")
        wait_screen(ANSWER)
        print("PASS: Bridge selected a Magpie routing group and Codex received a reply")
    except Exception:
        try:
            (root / "failure-screen.txt").write_text(screen())
        except Exception:
            pass
        raise
    finally:
        subprocess.run([str(bridge), "stop"], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL) if bridge.exists() else None
        subprocess.run([tmux, "-S", tmux_socket, "kill-server"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        (root / "relay-events.json").write_text(json.dumps({"A":server.events,"B":server_b.events},indent=2))
        magpie_proc.terminate()
        magpie_proc.wait(timeout=10)
        server_b.shutdown()
        server_c.shutdown()
        server.shutdown()
        stop_codex(root, env)
        print("Retained isolated diagnostics:", root)


if __name__ == "__main__":
    main()
