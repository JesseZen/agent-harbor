#!/usr/bin/env python3
"""Isolated, disposable manual Bridge fixture. Run with --help for commands."""
import argparse
import http.server
import io
import json
import os
import pathlib
import shutil
import socket
import subprocess
import sys
import tempfile
import threading
import time
import urllib.request

sys.dont_write_bytecode = True
from e2e_package import MockResponses as ChatMock
from e2e_local import MockResponses as ResponsesMock
from fixture_process import stop_codex
from fixture_requests import evidence

ROOT = pathlib.Path(__file__).resolve().parents[1]


def env_for(root):
    return {"PATH": str(root / "bin") + ":/usr/bin:/bin:/usr/sbin:/sbin", "HOME": str(root / "home"),
            "XDG_CONFIG_HOME": str(root / "config"), "XDG_STATE_HOME": str(root / "state"),
            "XDG_DATA_HOME": str(root / "data"), "XDG_CACHE_HOME": str(root / "cache"),
            "XDG_RUNTIME_DIR": str(root / "runtime"), "CODEX_HOME": str(root / "codex"),
            "CODEX_SQLITE_HOME": str(root / "sqlite"), "TMPDIR": str(root / "tmp"),
            "AGENT_HARBOR_BRIDGE_HOME": str(root / "bridge"), "MAGPIE_NO_ACCOUNTS": "1",
            "MAGPIE_ADDR": json.loads((root / "ports.json").read_text())["magpie"],
            "TERM": os.getenv("TERM", "xterm-256color"), "SHELL": "/bin/sh", "LANG": "C.UTF-8"}


def control(root, action):
    with socket.socket(socket.AF_UNIX) as sock:
        sock.settimeout(40)
        sock.connect(str(root / "control.sock"))
        sock.sendall((action + "\n").encode())
        result = sock.recv(8192).decode()
        if not result.startswith("OK"):
            raise RuntimeError(result)
        print(result.strip())


def daemon(root):
    (root / "requests.log").touch(mode=0o600, exist_ok=True)
    servers = []
    for label, handler in (("A", ChatMock), ("B", ChatMock), ("C", ResponsesMock)):
        class LoggingHandler(handler):
            def do_POST(self):
                raw = self.rfile.read(int(self.headers.get("Content-Length", "0")))
                self.rfile = io.BytesIO(raw)
                super().do_POST()
                # Only synthetic marker names are saved; no request bodies or credentials.
                markers = [m for m in ("AH_LOCAL_FIRST", "AH_LOCAL_SECOND", "AH_RELAY_B", "AH_LOCAL_C", "AH_RESUME_B", "AH_RESUME_C") if m.encode() in raw]
                classified = evidence(raw)
                with open(root / "requests.log", "a") as log:
                    log.write(json.dumps({"destination": self.server.label, "kind": classified['kind'],
                                          "latest_user_markers": classified['current'], "markers": markers}) + "\n")
        server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), LoggingHandler)
        server.label, server.observed, server.events = label, [], []
        threading.Thread(target=server.serve_forever, daemon=True).start()
        servers.append(server)
    with socket.socket() as probe:
        probe.bind(("127.0.0.1", 0))
        port = probe.getsockname()[1]
    (root / "ports.json").write_text(json.dumps({"magpie": f"127.0.0.1:{port}", **{s.label: s.server_port for s in servers}}))
    env = env_for(root)
    magpie = root / "bin/magpie"
    versions = {}
    for name in ("herdr", "magpie", "codex"):
        result = subprocess.run([root / "bin" / name, "--version"], env=env, text=True, capture_output=True, check=True)
        versions[name] = result.stdout.strip()
    (root / "versions.json").write_text(json.dumps(versions, indent=2))
    for label, server in zip(("a", "b"), servers):
        subprocess.run([magpie, "provider", "add", "relay-" + label, f"url=http://127.0.0.1:{server.server_port}/v1", "key=test-only", "models=test-model"], env=env, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    (root / "codex/config.toml").write_text('check_for_update_on_startup = false\nmodel = "test-model"\nmodel_provider = "isolated"\ncli_auth_credentials_store = "file"\n[model_providers.isolated]\nname = "isolated"\nbase_url = "http://127.0.0.1:' + str(servers[2].server_port) + '/v1"\nrequires_openai_auth = false\nwire_api = "responses"\n')
    log = open(root / "magpie.log", "a")
    def start_magpie():
        proc = subprocess.Popen([magpie, "serve"], env=env, stdout=log, stderr=subprocess.STDOUT)
        for _ in range(100):
            if proc.poll() is not None: raise RuntimeError("Magpie exited; see magpie.log")
            try:
                urllib.request.urlopen(f"http://127.0.0.1:{port}/api/hello", timeout=1).close()
                return proc
            except Exception: time.sleep(.1)
        proc.terminate()
        raise RuntimeError("Magpie did not become ready")
    proc = start_magpie()
    with socket.socket(socket.AF_UNIX) as listener:
        listener.bind(str(root / "control.sock"))
        os.chmod(root / "control.sock", 0o600)
        listener.listen()
        (root / "ready").write_text("ready")
        while True:
            conn, _ = listener.accept()
            with conn:
                try:
                    action = conn.recv(128).decode().strip()
                    if action in ("restart", "stop"):
                        subprocess.run([root / "bin/agent-harbor", "stop"], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
                        proc.terminate(); proc.wait(timeout=10)
                        stop_codex(root, env)
                        if action == "restart": proc = start_magpie()
                        conn.sendall(b"OK " + action.encode())
                        if action == "stop": break
                    elif action == "status": conn.sendall(b"OK fixture running")
                    else: conn.sendall(b"ERROR unknown action")
                except Exception as exc:
                    conn.sendall(("ERROR " + str(exc)).encode())
    for server in servers: server.shutdown()


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("action", choices=("start", "open", "status", "restart", "stop", "_daemon"))
    p.add_argument("root", nargs="?")
    p.add_argument("--binary", help="prebuilt agent-harbor, copied into the fixture"); p.add_argument("--herdr"); p.add_argument("--magpie"); p.add_argument("--codex")
    a = p.parse_args()
    if a.action == "start":
        if not all((a.herdr, a.magpie, a.codex)): p.error("start requires --herdr, --magpie and --codex")
        if pathlib.Path(a.codex).read_bytes()[:4] not in (b'\x7fELF', b'\xcf\xfa\xed\xfe'):
            raise RuntimeError("copied native Codex required; JavaScript wrappers are unsupported in tests")
        root = pathlib.Path(tempfile.mkdtemp(prefix="ah-manual-", dir="/tmp")).resolve()
        for name in ("bin", "home", "project", "config", "state", "data", "cache", "runtime", "codex", "sqlite", "tmp", "build-cache"): (root / name).mkdir(mode=0o700)
        # Copy Magpie away from any portable-mode data directory.
        for name, path in (("herdr", a.herdr), ("magpie", a.magpie)):
            shutil.copy2(path, root / "bin" / name)
            (root / "bin" / name).chmod(0o700)
        shutil.copy2(a.codex, root / "bin/codex")
        (root / "bin/codex").chmod(0o700)
        if a.binary:
            shutil.copy2(a.binary,root / "bin/agent-harbor")
        else:
            go = shutil.which("go")
            if not go: raise RuntimeError("go is required when --binary is omitted")
            build = {
                "PATH": str(pathlib.Path(go).parent) + ":/usr/bin:/bin:/usr/sbin:/sbin",
                "GOWORK": "off", "GOCACHE": str(root / "build-cache"), "GOPATH": str(root / "go")}
            for key, leaf in (("HOME", "home"), ("TMPDIR", "tmp"), ("XDG_CONFIG_HOME", "config"),
                              ("XDG_DATA_HOME", "data"), ("XDG_STATE_HOME", "state"), ("XDG_CACHE_HOME", "cache"),
                              ("XDG_RUNTIME_DIR", "runtime"), ("CODEX_HOME", "codex"), ("CODEX_SQLITE_HOME", "sqlite")):
                build[key] = str(root / leaf)
            subprocess.run([go, "build", "-o", str(root / "bin/agent-harbor"), "."], cwd=ROOT, env=build, check=True)
        daemon_env = {"PATH": str(root / "bin") + ":/usr/bin:/bin:/usr/sbin:/sbin", "HOME": str(root / "home"),
                      "XDG_CONFIG_HOME": str(root / "config"), "XDG_STATE_HOME": str(root / "state"),
                      "XDG_DATA_HOME": str(root / "data"), "XDG_CACHE_HOME": str(root / "cache"),
                      "XDG_RUNTIME_DIR": str(root / "runtime"), "CODEX_HOME": str(root / "codex"),
                      "CODEX_SQLITE_HOME": str(root / "sqlite"), "TMPDIR": str(root / "tmp"), "LANG": "C.UTF-8"}
        with open(root / "fixture.log", "w") as log:
            proc = subprocess.Popen([sys.executable, __file__, "_daemon", str(root)], env=daemon_env, stdin=subprocess.DEVNULL, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
        for _ in range(300):
            if (root / "ready").exists(): break
            if proc.poll() is not None: raise RuntimeError(f"fixture failed; see {root}/fixture.log")
            time.sleep(.1)
        else: raise RuntimeError(f"fixture did not start; see {root}/fixture.log")
        print(f"Fixture: {root}\nProject: {root}/project\nRun: python3 {__file__} open {root}\nRequests: {root}/requests.log\nTry: Ctrl+B a (sessions), Ctrl+B m (providers), Ctrl+B ? (help).\nCreate Magpie relay-a/test-model and relay-b/test-model sessions and one Local session.\nSend AH_LOCAL_FIRST to A, AH_RELAY_B to B, AH_LOCAL_C to Local.\nRead requests.log: kind separates main and auxiliary-title; latest_user_markers identifies the current request; markers may include history.\nRestart: python3 {__file__} restart {root}; reopen and explicitly resume each conversation.\nStatus: python3 {__file__} status {root}\nStop: python3 {__file__} stop {root}\nDiagnostics retained: fixture.log, magpie.log, requests.log, bridge/sessions.json")
        return
    if not a.root: p.error("this action requires the fixture root")
    root = pathlib.Path(a.root).resolve()
    if a.action == "_daemon": daemon(root); return
    if not root.name.startswith("ah-manual-") or root.parent != pathlib.Path("/tmp").resolve(): raise RuntimeError("invalid fixture root")
    if a.action == "open":
        control(root, "status")
        os.chdir(root / "project")
        os.execve(str(root / "bin/agent-harbor"), [str(root / "bin/agent-harbor"), "open"], env_for(root))
    control(root, a.action)


if __name__ == "__main__": main()
