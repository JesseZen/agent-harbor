#!/usr/bin/env python3
"""Exercise account selection in real Herdr with only synthetic saved logins."""
import argparse
import base64
import contextlib
import datetime
import getpass
import http.server
import io
import json
import pathlib
import re
import shlex
import shutil
import socket
import subprocess
import sys
import tempfile
import threading
import time

sys.dont_write_bytecode = True
from e2e_local import ANSWER, MockResponses
from e2e_package import MockResponses as MockCompletions


class Discovery(MockResponses):
    def do_GET(self):
        path = self.path.split("?")[0]
        account = self.caller()
        if path.endswith("/accounts/check"):
            self.reply(json.dumps({"accounts": [{"id": "fixture-" + account,
                "workspace_backend_origin": "https://chatgpt.com", "account_routing_override": "NO_CONSTRAINT"}]}).encode(), "application/json")
        elif path.endswith("/wham/usage"):
            self.server.quota_calls.append(account)
            self.reply(json.dumps({"account_id": "fixture-" + account, "plan_type": "plus"}).encode(), "application/json")
        elif path.endswith("/models"):
            self.reply(b'{"models":[]}', "application/json")
        elif self.headers.get("Upgrade", "").lower() == "websocket":
            self.send_error(426)
        else:
            self.send_error(404)

    def caller(self):
        for label in ("a", "b"):
            if self.headers.get("Authorization") == "Bearer " + account_token(label) and self.headers.get("chatgpt-account-id") == "fixture-" + label:
                return label
        return "unknown"

    def do_POST(self):
        raw = self.rfile.read(int(self.headers.get("Content-Length", 0)))
        path = self.path.split("?")[0]
        chat = path.endswith("/chat/completions")
        if not chat and not path.endswith("/responses"):
            self.send_error(404)
            return
        if self.headers.get("Content-Encoding") == "zstd":
            from compression import zstd
            raw = zstd.decompress(raw)
        body = json.loads(raw)
        latest = next((item for item in reversed(body.get("messages" if chat else "input", [])) if item.get("role") == "user"), {})
        match = re.search(r"\bAH_ACCOUNT_(?:FIRST|SECOND)\b", json.dumps(latest.get("content", [])))
        marker = match.group() if match else None
        if marker and not any(entry[0] == marker for entry in self.server.turns):
            identity = self.caller()
            if self.headers.get("Authorization") == "Bearer fixture-provider-key":
                identity = "provider"
            self.server.turns.append((marker, identity, body.get("model")))
            if marker == "AH_ACCOUNT_FIRST" and not self.server.release_turn.wait(40):
                self.send_error(503)
                return
        if chat:
            self.rfile = io.BytesIO(raw)
            MockCompletions.do_POST(self)
        else:
            self.respond(body)


def account_token(label):
    claims = {"email": f"{label}@lab.test", "exp": 4102444800, "https://api.openai.com/auth": {
        "chatgpt_plan_type": "plus", "chatgpt_account_id": f"fixture-{label}", "chatgpt_user_id": f"user-{label}",
    }}

    def encode(value):
        return base64.urlsafe_b64encode(json.dumps(value).encode()).decode().rstrip("=")

    return ".".join((encode({"alg": "none"}), encode(claims), "Zml4dHVyZQ"))


def import_account(root, auth, label, run, env):
    token = account_token(label)
    fixture = root / f"account-{label}.json"
    fixture.write_text(json.dumps({"auth_mode": "chatgpt", "tokens": {
        "id_token": token, "access_token": token, "refresh_token": "fixture-only", "account_id": f"fixture-{label}",
    }, "last_refresh": datetime.datetime.now(datetime.timezone.utc).isoformat()}))
    run(auth, "import", fixture, "--alias", f"Fixture {label.upper()}", child_env=env)


def main(args, discovery):
    root = pathlib.Path(tempfile.mkdtemp(prefix="ahacct-", dir="/tmp")).resolve()
    print(f"fixture: {root}", flush=True)
    env = {
        "TERM": "xterm-256color", "SHELL": "/bin/sh", "LANG": "en_US.UTF-8",
        "HTTP_PROXY": "http://127.0.0.1:1", "HTTPS_PROXY": "http://127.0.0.1:1", "NO_PROXY": "127.0.0.1,localhost",
    }
    env.update({"NO_COLOR": "1"} if args.no_color else {"COLORTERM": "truecolor"})
    for key, folder in (("HOME", "home"), ("XDG_CONFIG_HOME", "config"), ("XDG_DATA_HOME", "data"),
                        ("XDG_STATE_HOME", "state"), ("XDG_CACHE_HOME", "cache"), ("XDG_RUNTIME_DIR", "runtime"),
                        ("TMPDIR", "tmp"), ("CODEX_HOME", "codex"), ("CODEX_SQLITE_HOME", "sqlite")):
        (root / folder).mkdir(mode=0o700)
        env[key] = str(root / folder)
    discovery_config = root / "config/magpie"
    discovery_config.mkdir()
    (discovery_config / "settings.json").write_text(json.dumps({"port": discovery.server_port}))
    bin_dir = root / "bin"
    bin_dir.mkdir()
    for name, source in (("agent-harbor", args.binary), ("herdr", args.herdr), ("codex", args.codex), ("codex-auth", args.codex_auth)):
        shutil.copy2(source, bin_dir / name)
        (bin_dir / name).chmod(0o700)
    if args.magpie:
        shutil.copy2(args.magpie, bin_dir / "magpie")
        (bin_dir / "magpie").chmod(0o700)
        env["AGENT_HARBOR_MAGPIE_BIN"] = str(bin_dir / "magpie")
    with (bin_dir / "codex").open("rb") as native:
        assert native.read(4) in (b"\x7fELF", b"\xcf\xfa\xed\xfe", b"\xca\xfe\xba\xbe"), "use native Codex"
    env.update(PATH=f"{bin_dir}:/usr/bin:/bin:/usr/sbin:/sbin", AGENT_HARBOR_HERDR_BIN=str(bin_dir / "herdr"),
               AGENT_HARBOR_BRIDGE_HOME=str(root / "harbor"), AGENT_HARBOR_CODEX_AUTH_BIN=str(bin_dir / "codex-auth"))
    tmux = shutil.which("tmux")
    project = root / "project"
    project.mkdir()

    def run(*command, child_env=env, check=True):
        process = subprocess.run(list(map(str, command)), env=child_env, cwd=project, text=True, capture_output=True, timeout=30)
        if check and process.returncode:
            raise RuntimeError(f"fixture command failed: {command[0]} {process.stderr}")
        return process.stdout

    def tm(*command, check=True):
        return run(tmux, "-S", root / "tmux.sock", "-f", "/dev/null", *command, check=check)

    def capture(*flags):
        return tm("capture-pane", "-p", *flags, "-t", "app:0.0")

    def row(label):
        return next((line for line in capture().splitlines() if label in line), "")

    def sanitize(screen):
        return screen.replace(socket.gethostname().split(".")[0], "fixture-host").replace(getpass.getuser() + "$", "fixture$")

    def wait(predicate, label):
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            if predicate():
                return
            time.sleep(0.1)
        raise RuntimeError(f"missing {label}\n{sanitize(capture())}")

    def see(label):
        wait(lambda: row(label), label)

    def keys(*items):
        tm("send-keys", "-t", "app:0.0", *items)
        time.sleep(0.25)

    def click(label):
        lines = capture().splitlines()
        offset, line = next((offset, line) for offset, line in enumerate(lines) if label in line)
        column = line.index(label) + 1
        keys("-l", f"\x1b[<0;{column};{offset+1}M\x1b[<0;{column};{offset+1}m")

    def save(label, width):
        (root / f"{label}-{width}.ansi").write_text(sanitize(capture("-e")))

    def resize(width, height):
        tm("resize-window", "-t", "app:0", "-x", width, "-y", height)
        time.sleep(0.7)

    harbor = bin_dir / "agent-harbor"
    auth = bin_dir / "codex-auth"
    profile = json.loads(run(harbor, "profiles", "new", "Fixture"))
    run(harbor, "profiles", "select", profile["id"])
    home = pathlib.Path(profile["clients"][0]["home"])
    auth_path = home / "auth.json"
    account_env = dict(env, CODEX_HOME=str(home), CODEX_SQLITE_HOME=profile["clients"][0]["sqlite"])
    base = f"http://127.0.0.1:{discovery.server_port}"
    if args.live_switch:
        (home / "config.toml").write_text(
            'model = "gpt-5.5"\nopenai_base_url = ' + json.dumps(base + "/v1")
            + '\nchatgpt_base_url = ' + json.dumps(base + "/backend-api")
            + '\ncheck_for_update_on_startup = false\n[features]\napps = false\nplugins = false\n'
            + '\n[projects.' + json.dumps(str(project)) + ']\ntrust_level = "trusted"\n')

    def login_is(label):
        if not auth_path.exists():
            return label is None
        return json.loads(auth_path.read_text())["tokens"]["account_id"] == f"fixture-{label}"

    launcher = shlex.join(["env", *(key+"="+value for key, value in env.items()), str(harbor)]) + "; read fixture_exit"
    try:
        tm("new-session", "-d", "-s", "app", "-x", 160, "-y", 45, "-c", project, launcher)
        wrapper = root / "harbor/herdr/bin/herdr"
        wait(lambda: wrapper.exists() and run(wrapper, "pane", "list", check=False), "Herdr workspace")
        keys("C-b", "a")
        see("Search sessions")
        keys("a")
        see("No saved logins")
        assert "✓" in row("Not signed in") and login_is(None)
        save("accounts-empty", 160)
        for label in ("a", "b"):
            import_account(root, auth, label, run, account_env)
        roster = json.loads(run(auth, "list", "--skip-api", "--json", child_env=account_env))
        first = next(account["account_key"] for account in roster["accounts"] if account["email"] == "a@lab.test")
        run(auth, "switch", first, "--no-restart-daemon", child_env=account_env)
        keys("r")

        if args.live_switch:
            exercise_live_switch(args, discovery, root, harbor, wrapper, run, wait, see, row, keys, click, login_is, save, resize)
            return

        for width, height in ((160, 45), (100, 30), (80, 24), (60, 24)):
            resize(width, height)
            wait(lambda: "✓" in row("a@lab"), "account A selected")
            see("Not signed in")
            if "> Not signed in" in row("Not signed in"):
                keys("Down")
            save("accounts-selected", width)
            click("Not signed in")
            see("> Not signed in")
            assert login_is("a")
            keys("Enter")
            see("Sign out of Codex")
            save("accounts-signout-confirm", width)
            keys("Escape")
            see("> Not signed in")
            assert login_is("a")
            click("Not signed in")
            see("Sign out of Codex")
            keys("y")
            see("Account action result")
            assert login_is(None)
            keys("Escape")
            wait(lambda: "✓" in row("Not signed in"), "confirmed sign-out")
            assert row("a@lab") and row("b@lab")
            save("accounts-signed-out", width)
            keys("Down", "Down")
            see("> b@lab")
            save("accounts-use-login", width)
            keys("s")
            see("Account action result")
            assert login_is("b")
            keys("Escape")
            wait(lambda: "✓" in row("b@lab"), "account B selected")
            keys("Enter")
            see("Account details")
            see("b@lab.test")
            save("accounts-details", width)
            keys("Escape")
            see("> b@lab")
            keys("Up")
            see("> a@lab")
            keys("s")
            see("Account action result")
            assert login_is("a")
            keys("Escape")
        print(f"PASS isolated Herdr: A to Not signed in to B, cancel, selection and details at four sizes: {root}")
    finally:
        discovery.release_turn.set()
        if args.magpie:
            run(harbor, "magpie", "stop", check=False)
        run(harbor, "stop", check=False)
        tm("kill-server", check=False)


def exercise_live_switch(args, provider, root, harbor, wrapper, run, wait, see, row, keys, click, login_is, save, resize):
    joined = args.live_switch == "magpie"
    if joined:
        def available_port():
            with contextlib.closing(socket.socket()) as sock:
                sock.bind(("127.0.0.1", 0))
                return sock.getsockname()[1]

        run(harbor, "magpie", "start", "--port", available_port(), "--web-port", available_port())
        run(root / "harbor/magpie/bin/magpie", "provider", "add", "fixture",
            f"url=http://127.0.0.1:{provider.server_port}/v1", "key=fixture-provider-key", "models=test-model")
    wait(lambda: "✓" in row("a@lab"), "initial login A")
    keys("Escape")
    see("Search sessions")
    keys("n")
    see("New session")
    if joined:
        click("Connection")
        if not row("Choose instance and model"):
            keys("Enter")
        see("Choose instance and model")
        click("Choose instance and model")
        if row("Choose instance and model"):
            keys("Enter")
        see("Choose instance")
        keys("/")
        keys("-l", "managed")
        keys("Enter", "Enter")
        see("Model or routing group")
        keys("/")
        keys("-l", "fixture/test-model")
        keys("Enter")
        click("Acknowledge & apply" if row("Acknowledge & apply") else "Apply selection")
        see("New session")
    keys("C-s")
    wait(lambda: row("Conversation recovery") or row("Ask Codex"), "Codex launch")
    if row("Conversation recovery"):
        keys("y")
    ready_since = None

    def startup_ready():
        nonlocal ready_since
        trust = row("Trust all and continue")
        existing = row("Use existing model")
        if existing:
            if re.search(r"[›>]\s+2\. Use existing model", existing):
                keys("Enter")
            else:
                keys("Down")
        elif trust:
            if re.search(r"[›>]\s+2\. Trust all and continue", trust):
                keys("Enter")
            else:
                keys("Down")
        elif row("Ask Codex"):
            ready_since = ready_since or time.monotonic()
            return time.monotonic() - ready_since > 1.5
        ready_since = None
        return False

    wait(startup_ready, "Codex startup")

    def record():
        return next(iter(json.loads(run(harbor, "sessions", "--json"))), {})

    wait(lambda: record().get("process_group"), "Harbor recorded the running Codex")
    started = record()
    assert started.get("codex_server_pid"), "eligible launch did not start its own App Server"
    if joined:
        assert started["codex_connection"] == "caller", "not a signed-in Magpie connection"
    def status():
        agents = json.loads(run(wrapper, "agent", "list"))["result"]["agents"]
        return next((agent.get("agent_status") for agent in agents if agent.get("pane_id") == started["herdr_pane"]), "unknown")

    def turn(marker):
        keys("-l", marker)
        see(marker)
        keys("Enter")
        wait(lambda: any(entry[0] == marker for entry in provider.turns), marker)

    def choose_backup():
        keys("C-b", "a")
        wait(lambda: row("Search sessions") or row("b@lab"), "Harbor popup")
        if not row("b@lab"):
            keys("a")
        see("b@lab")
        click("b@lab")
        keys("s")
        see("Waiting to change login")
        assert login_is("a"), "login changed during active turn"

    turn("AH_ACCOUNT_FIRST")
    wait(lambda: status() == "working", "Herdr Working while provider holds the turn")
    choose_backup()
    sizes = ((160, 45), (100, 30), (80, 24), (60, 24))
    for width, height in sizes:
        resize(width, height)
        see("Waiting to change login")
        see("Cancel switch")
        save(f"{args.live_switch}-waiting", width)
    keys("Escape")
    resize(160, 45)
    see("b@lab")
    assert login_is("a")
    keys("s")
    see("Waiting to change login")
    provider.release_turn.set()
    see("Saved login: b@lab.test")
    see("Selected login verified in this window")
    assert login_is("b")
    for width, height in sizes:
        resize(width, height)
        see("Saved login: b@lab.test")
        see("Back to accounts")
        save(f"{args.live_switch}-switched", width)
    resize(160, 45)
    keys("Escape")
    see("b@lab.test")
    keys("q")
    if row("Search sessions"):
        keys("q")
    wait(lambda: not row("Harbor · Accounts") and not row("Search sessions"), "return to the same Codex window")
    see(ANSWER)
    wait(lambda: status() in ("idle", "done"), "Herdr finished")
    conversation = record().get("codex_session_id")
    turn("AH_ACCOUNT_SECOND")
    wait(lambda: status() in ("idle", "done"), "second turn completed")
    after = record()
    for field in ("id", "herdr_pane", "process_group", "codex_server_pid"):
        assert after[field] == started[field], f"{field} changed during live switch"
    assert conversation and after.get("codex_session_id") == conversation, "Codex changed its Conversation"
    first, second = provider.turns[:2]
    assert first[2] == second[2], "switch changed the model"
    assert (first[1], second[1]) == (("provider", "provider") if joined else ("a", "b")), provider.turns
    if joined:
        assert "b" in provider.quota_calls, "running server did not use B for official quota"
    print(f"PASS {args.live_switch}: Working → cancel → wait → B, same window, verified requests: {root}")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    for dependency in ("binary", "herdr", "codex", "codex-auth"):
        parser.add_argument(f"--{dependency}", required=True)
    parser.add_argument("--no-color", action="store_true")
    parser.add_argument("--live-switch", choices=("native", "magpie"))
    parser.add_argument("--magpie", help="native Magpie caller-only candidate for --live-switch magpie")
    args = parser.parse_args()
    if args.live_switch == "magpie" and not args.magpie:
        parser.error("--live-switch magpie requires --magpie")
    with http.server.ThreadingHTTPServer(("127.0.0.1", 0), Discovery) as server:
        server.observed, server.events, server.turns, server.quota_calls = [], [], [], []
        server.release_turn = threading.Event()
        threading.Thread(target=server.serve_forever, daemon=True).start()
        try:
            main(args, server)
        finally:
            server.shutdown()
