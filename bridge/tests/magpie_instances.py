#!/usr/bin/env python3
"""Verified Magpie instances and the Harbor-managed Magpie, in isolation.

Every case uses a fresh temporary HOME, XDG roots, a private loopback port
pair and a state root, with the real pinned Magpie binary copied into place.
The script starts and stops only the processes it created.
"""
import argparse, json, os, pathlib, pty, select, shlex, shutil, signal, socket, subprocess, sys, tempfile, time, urllib.request


def wait_for_port(address, timeout=15):
    """Wait until something accepts connections on host:port."""
    host, port = address.rsplit(":", 1)
    deadline = time.time() + timeout
    while time.time() < deadline:
        with socket.socket() as sock:
            sock.settimeout(0.5)
            if sock.connect_ex((host, int(port))) == 0:
                return
        time.sleep(0.05)
    raise AssertionError(f"nothing accepted connections at {address}")


def process_executable(pid):
    try:
        return os.readlink(f"/proc/{pid}/exe")
    except Exception:
        pass
    result = subprocess.run(["ps", "-p", str(pid), "-o", "comm="], capture_output=True, text=True)
    return result.stdout.strip()

MAGPIE_VERSION = "0.1.3528"


def log(message):
    print(message, flush=True)


class Fixture:
    def __init__(self, magpie, keep=False):
        self.root = pathlib.Path(tempfile.mkdtemp(prefix="harbor-magpie-")).resolve()
        self.keep = keep
        self.root.chmod(0o700)
        self.state = self.root / "state"
        self.state.mkdir(mode=0o700)
        self.bin = self.root / "bin"
        self.bin.mkdir()
        self.magpie = self.bin / "magpie"
        shutil.copy(magpie, self.magpie)
        self.magpie.chmod(0o755)
        self.agent_harbor = self.bin / "agent-harbor"
        self.processes = []
        self.pty_logs = {}
        self.gateway_ports = []
        self.ports = []
        self.env = {
            "PATH": f"{self.bin}:{os.environ.get('PATH', '/usr/bin:/bin')}",
            "HOME": str(self.root / "home"),
            "XDG_CONFIG_HOME": str(self.root / "xdg-config"),
            "XDG_DATA_HOME": str(self.root / "xdg-data"),
            "XDG_STATE_HOME": str(self.root / "xdg-state"),
            "XDG_CACHE_HOME": str(self.root / "xdg-cache"),
            "TMPDIR": str(self.root / "tmp"),
            "XDG_RUNTIME_DIR": str(self.root / "runtime"),
            "CODEX_HOME": str(self.root / "codex"),
            "CODEX_SQLITE_HOME": str(self.root / "sqlite"),
            "AGENT_HARBOR_BRIDGE_HOME": str(self.state),
            "AGENT_HARBOR_MAGPIE_BIN": str(self.magpie),
            "MAGPIE_NO_ACCOUNTS": "1",
            "NO_COLOR": "1",
            "LANG": "en_US.UTF-8",
        }
        for key in ("HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "TMPDIR", "XDG_RUNTIME_DIR", "CODEX_HOME", "CODEX_SQLITE_HOME"):
            pathlib.Path(self.env[key]).mkdir(parents=True, exist_ok=True)

        (pathlib.Path(self.env["CODEX_HOME"]) / "config.toml").write_text("check_for_update_on_startup = false\n")

    def port(self):
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        self.ports.append(port)
        return port

    def start_external(self, config_dir, address, portable=True):
        """Run a real Magpie from a private copy so portable data stays put."""
        copy = self.root / f"external-{len(self.processes)}"/ "magpie"
        copy.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy(self.magpie, copy)
        copy.chmod(0o755)
        if portable:
            (copy.parent / ".portable").write_text("portable\n")
        if config_dir:
            shutil.copytree(config_dir, copy.parent / "data", dirs_exist_ok=True)
        log_file = open(self.root / f"external-{len(self.processes)}.log", "w")
        env = dict(self.env)
        env["MAGPIE_ADDR"] = address
        env["MAGPIE_NO_ACCOUNTS"] = "1"
        env["HOME"] = str(self.root / "external-home")
        pathlib.Path(env["HOME"]).mkdir(exist_ok=True)
        proc = subprocess.Popen([str(copy), "serve"], env=env, stdout=log_file, stderr=subprocess.STDOUT)
        self.processes.append(proc)
        self.gateway_ports.append(address)
        self.await_gateway(address)
        return proc

    def await_gateway(self, address, timeout=25):
        deadline = time.time() + timeout
        while time.time() < deadline:
            try:
                with urllib.request.urlopen(f"http://{address}/", timeout=1) as response:
                    if json.load(response).get("name") == "magpie":
                        return json.load(urllib.request.urlopen(f"http://{address}/", timeout=1))
            except Exception:
                time.sleep(0.2)
        raise RuntimeError(f"gateway at {address} did not answer: " + self.log_tail())

    def log_tail(self):
        pieces = []
        for path in sorted(self.root.glob("*.log")):
            pieces.append(f"--- {path.name}\n" + path.read_text()[-2000:])
        return "\n".join(pieces)

    def harbor(self, *args, expect=0, env=None):
        command = [str(self.agent_harbor), *args]
        result = subprocess.run(command, env=env or self.env, capture_output=True, text=True, timeout=120)
        if expect is not None and result.returncode != expect:
            raise AssertionError(f"agent-harbor {' '.join(args)} exited {result.returncode}\n{result.stdout}\n{result.stderr}")
        return result

    def tui(self, args, env, log_name):
        """Run a terminal UI under a pty; only these processes are stopped."""
        log_file = open(self.root / log_name, "w")
        if sys.platform == "darwin":
            command = ["script", "-q", "/dev/null", *args]
        else:
            command = ["script", "-q", "-e", "-c", " ".join(shlex.quote(part) for part in args), "/dev/null"]
        proc = subprocess.Popen(command, env=env, stdin=subprocess.DEVNULL, stdout=log_file,
                                stderr=subprocess.STDOUT, start_new_session=True)
        self.processes.append(proc)
        return proc

    def start_pty_tui(self, args, env, name):
        """Run a terminal UI on a pty whose output this test reads.

        A pty is required for the UI to start at all, and owning the master
        side lets the test send keys and read what the UI actually rendered.
        """
        master, slave = pty.openpty()
        proc = subprocess.Popen(args, env=env, stdin=slave, stdout=slave, stderr=slave, start_new_session=True)
        os.close(slave)
        self.processes.append(proc)
        self.pty_logs[name] = (master, b"", self.root / name)
        return proc

    def await_pty_text(self, name, expected, timeout=30, keys=()):
        """Wait until the pty shows expected content, sending keys meanwhile."""
        master, text, path = self.pty_logs[name]
        pending = list(keys)
        deadline = time.time() + timeout
        while time.time() < deadline:
            ready, _, _ = select.select([master], [], [], 0.25)
            if ready:
                try:
                    text += os.read(master, 65536)
                except OSError:
                    break
            self.pty_logs[name] = (master, text, path)
            path.write_text(text.decode(errors="replace"))
            if expected.encode() in text:
                return text.decode(errors="replace")
            if pending and b"providers" in text:
                os.write(master, pending.pop(0).encode())
        path.write_text(text.decode(errors="replace"))
        raise AssertionError(f"{name} did not show {expected!r}; last output: "
                             + text.decode(errors="replace")[-1500:])

    def stop_pty_tui(self, name):
        master, text, path = self.pty_logs.pop(name)
        os.close(master)
        path.write_text(text.decode(errors="replace"))

    def await_tui_log(self, proc, expected, name, timeout=30):
        """Wait until a named TUI log shows the expected content."""
        log = self.root / name
        text = ""
        deadline = time.time() + timeout
        while time.time() < deadline and proc.poll() is None:
            text = log.read_text(errors="replace") if log.exists() else ""
            if expected in text:
                return text
            time.sleep(0.25)
        raise AssertionError(f"{name} did not show {expected!r}; last output: "
                             + text[-1500:])

    def await_tui_ready(self, proc, expected, timeout=30):
        """Wait until the management TUI shows content from the right config.

        The TUI lists the models of the configuration it read, so a model that
        exists only in the instance's folder proves Harbor passed that
        instance's environment instead of a fixed sleep assuming it.
        """
        log = self.root / "tui.log"
        deadline = time.time() + timeout
        while time.time() < deadline and proc.poll() is None:
            text = log.read_text(errors="replace") if log.exists() else ""
            if expected in text:
                return text
            time.sleep(0.25)
        raise AssertionError(f"the management TUI did not show {expected!r}: " + self.log_tail())

    def await_catalog(self, address, model, timeout=30):
        """Wait until a gateway's catalog lists a model."""
        deadline = time.time() + timeout
        while time.time() < deadline:
            if model in self.provider_catalog(address):
                return
            time.sleep(0.25)
        raise AssertionError(f"{address} never listed {model}: " + self.log_tail())

    def provider_catalog(self, address):
        try:
            with urllib.request.urlopen(f"http://{address}/v1/models", timeout=3) as response:
                return sorted(model["id"] for model in json.load(response)["data"])
        except Exception:
            return []

    def stop_own_gateways(self):
        """Stop every gateway this fixture started, including detached children.

        Magpie's terminal UI and serve can end up in their own session, so the
        recorded addresses, not just the Popen handles, decide what is stopped.
        """
        for address in self.gateway_ports + [f"127.0.0.1:{port}" for port in self.ports]:
            self.kill_gateway_on(address)
        for pid, address in self.recorded_pids():
            self.signal_own_process(pid, address)

    def own_process(self, pid, address=None):
        """Whether pid is a process this fixture started.

        A port this fixture released can be taken by a Magpie the user runs, so
        ownership is decided by the executable living inside the fixture root,
        and by the pid the address itself reports, never by a port alone.
        """
        if not isinstance(pid, int) or pid <= 1:
            return False
        executable = process_executable(pid)
        if not executable:
            return False
        if not executable.startswith(str(self.root) + os.sep):
            return False
        if address is not None:
            reported = self.gateway_pid(address)
            if reported is not None and reported != pid:
                return False
        return True

    def signal_own_process(self, pid, address=None):
        """Terminate a process only after proving this fixture started it."""
        if not self.own_process(pid, address):
            return False
        try:
            os.kill(pid, signal.SIGTERM)
        except (ProcessLookupError, PermissionError):
            return False
        return True

    def kill_gateway_on(self, address):
        pid = self.gateway_pid(address)
        if not self.own_process(pid, address):
            return
        try:
            os.kill(pid, signal.SIGTERM)
        except (ProcessLookupError, PermissionError):
            return
        for _ in range(50):
            if self.gateway_pid(address) is None:
                return
            time.sleep(0.1)
        # The address still answers, so the pid is re-read before escalating:
        # a replacement process on that port is not this fixture's to kill.
        remaining = self.gateway_pid(address)
        if remaining == pid and self.own_process(pid, address):
            try:
                os.kill(pid, signal.SIGKILL)
            except (ProcessLookupError, PermissionError):
                pass

    def gateway_pid(self, address):
        try:
            with urllib.request.urlopen(f"http://{address}/", timeout=1) as response:
                report = json.load(response)
            pid = report.get("pid")
            return pid if isinstance(pid, int) and pid > 1 else None
        except Exception:
            return None

    def recorded_pids(self):
        pids = []
        record = self.state / "magpie" / "instance.json"
        if record.exists():
            entry = json.loads(record.read_text())
            pid = entry.get("pid")
            if isinstance(pid, int) and pid > 1:
                pids.append((pid, entry.get("address")))
        return pids

    def cleanup(self):
        self.stop_own_gateways()
        for proc in self.processes:
            try:
                if hasattr(os, "killpg"):
                    os.killpg(proc.pid, signal.SIGTERM)
                else:
                    proc.terminate()
                proc.wait(timeout=5)
            except Exception:
                try:
                    if hasattr(os, "killpg"):
                        os.killpg(proc.pid, signal.SIGKILL)
                    else:
                        proc.kill()
                except Exception:
                    pass
        if not self.keep:
            log(f"diagnostics removed: {self.root}")
            shutil.rmtree(self.root, ignore_errors=True)
        else:
            log(f"diagnostics kept: {self.root}")


class Assertions:
    def __init__(self):
        self.checks = 0

    def ok(self, condition, message):
        self.checks += 1
        if not condition:
            raise AssertionError(message)
        log(f"  ok  {message}")


def output(result):
    return result.stdout + result.stderr


def build_binary(bridge_dir, output):
    env = dict(os.environ, GOWORK="off", CGO_ENABLED="0")
    subprocess.run(["go", "build", "-o", str(output), "."], cwd=bridge_dir, env=env, check=True)
    output.chmod(0o755)


def case_same_config(fixture, check):
    """A verified external instance whose configuration is unchanged."""
    address = f"127.0.0.1:{fixture.port()}"
    fixture.start_external(None, address)
    result = fixture.harbor("magpie", "status", "--address", address, "--json")
    probe = json.loads(result.stdout)
    check.ok(probe["state"] == "verified", "same config: identity verified")
    check.ok(probe["config_dir"].endswith("data"), "same config: portable data folder reported")
    check.ok(probe["pid"] > 1 and probe["serves"] == "serve", "same config: serving process reported")
    bind = fixture.harbor("magpie", "bind", "--address", address)
    check.ok("never starts, stops or edits" in output(bind), "same config: binding explains the boundary")
    status = json.loads(fixture.harbor("magpie", "status", "--json").stdout)
    check.ok(len(status["instances"]) == 1, "same config: instance recorded")
    check.ok(status["instances"][0]["health"] == "verified", "same config: health verified")
    return address


def case_mismatched_config(fixture, check, recorded_address):
    """The recorded address now serves a gateway with another configuration."""
    check.ok(recorded_address != "", "mismatch: a recorded instance exists")
    first = fixture.processes[-1]
    first.terminate()
    first.wait(timeout=10)
    other = fixture.harbor("magpie", "status", "--json")
    other = json.loads(other.stdout)
    check.ok(other["instances"][0]["health"] in ("offline", "changed"), "mismatch: a stopped gateway reads offline")

    # A second configuration is a second portable copy beside a different
    # data folder; Magpie only recognizes its own files there.
    second_config = fixture.root / "second-data"
    second_config.mkdir(exist_ok=True)
    (second_config / "settings.json").write_text("{}\n")
    fixture.start_external(second_config, recorded_address)
    status = json.loads(fixture.harbor("magpie", "status", "--json").stdout)
    instance = status["instances"][0]
    check.ok(instance["health"] == "changed", "mismatch: a different configuration location is detected")
    check.ok(instance["config_dir"] != instance["probe"]["config_dir"], "mismatch: both locations are reported")
    manage = fixture.harbor("magpie", "manage", "--address", recorded_address, expect=1)
    check.ok("now reads" in output(manage), "mismatch: management is refused with the new location")
    fixture.processes[-1].terminate()
    fixture.processes[-1].wait(timeout=10)


def case_no_gateway(fixture, check):
    """Nothing answers: no identity, and no UI that would host the gateway."""
    address = f"127.0.0.1:{fixture.port()}"
    status = fixture.harbor("magpie", "status", "--address", address, "--json")
    check.ok(json.loads(status.stdout)["state"] == "offline", "no gateway: status reads offline")
    bind = fixture.harbor("magpie", "bind", "--address", address, expect=1)
    check.ok("does not infer an instance" in output(bind), "no gateway: binding is refused")
    manage = fixture.harbor("magpie", "manage", "--address", address, expect=1)
    check.ok("host the gateway" in output(manage), "no gateway: management is refused with the reason")
    check.ok("would stop every session using it" in output(manage), "no gateway: the consequence is explained")
    not_magpie = f"127.0.0.1:{fixture.port()}"
    server_proc = subprocess.Popen([sys.executable, "-c", (
        "import http.server\n"
        "class H(http.server.BaseHTTPRequestHandler):\n"
        "    def do_GET(self):\n"
        "        body=b'{\"name\":\"other\",\"version\":\"1\"}'\n"
        "        self.send_response(200); self.send_header('Content-Length', str(len(body))); self.end_headers(); self.wfile.write(body)\n"
        "    def log_message(self,*a): pass\n"
        f"http.server.ThreadingHTTPServer(('127.0.0.1',{not_magpie.split(':')[1]}),H).serve_forever()")])
    fixture.processes.append(server_proc)
    wait_for_port(not_magpie)
    status = fixture.harbor("magpie", "status", "--address", not_magpie, "--json")
    check.ok(json.loads(status.stdout)["state"] == "not-magpie", "no gateway: another program is not Magpie")
    server_proc.terminate()


def case_port_conflict(fixture, check):
    """A held port is reported with its holder instead of starting a waiting serve."""
    address = f"127.0.0.1:{fixture.port()}"
    fixture.start_external(None, address)
    port = int(address.split(":")[1])
    conflict = fixture.harbor("magpie", "start", "--port", str(port), "--web-port", str(port + 1), expect=1)
    check.ok("held by" in output(conflict), "port conflict: the port is reported as held")
    check.ok("Magpie" in output(conflict), "port conflict: the Magpie holder is named")
    check.ok(not (fixture.state / "magpie" / "instance.json").exists(), "port conflict: nothing was recorded")

    other = fixture.port()
    listener = socket.socket()
    listener.bind(("127.0.0.1", other))
    listener.listen(1)
    try:
        conflict = fixture.harbor("magpie", "start", "--port", str(other), "--web-port", str(other + 1), expect=1)
        check.ok("another program" in output(conflict), "port conflict: a non-Magpie holder is named")
    finally:
        listener.close()


def case_recorded_port_taken(fixture, check):
    """A recorded pair whose port is grabbed by someone else is re-picked."""
    started = fixture.harbor("magpie", "start")
    hold = output(started)
    record = json.loads((fixture.state / "magpie" / "instance.json").read_text())
    gateway = int(record["address"].rsplit(":", 1)[1])
    check.ok("Address" in hold and str(gateway) in hold, "recorded port: the managed instance started")
    fixture.harbor("magpie", "stop")

    # Something else takes the recorded gateway port while Harbor is stopped.
    listener = socket.socket()
    listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    listener.bind(("127.0.0.1", gateway))
    listener.listen(1)
    try:
        restarted = fixture.harbor("magpie", "start")
        moved = output(restarted)
        check.ok("Managed Magpie started" in moved, "recorded port: start recovered by re-picking a pair")
        check.ok(str(gateway) + " " not in moved.split("Address")[1][:8],
                 "recorded port: the held port was not reused")
        new_record = json.loads((fixture.state / "magpie" / "instance.json").read_text())
        check.ok(new_record["address"] != record["address"], "recorded port: the instance moved to a free port")
        check.ok(fixture.harbor("magpie", "stop").returncode == 0, "recorded port: the new instance stops")
    finally:
        listener.close()


def case_stop_after_restart_only(fixture, check):
    """Stop keeps working across a restart and never touches a foreign holder."""
    fixture.harbor("magpie", "start")
    first = json.loads((fixture.state / "magpie" / "instance.json").read_text())
    fixture.harbor("magpie", "stop")
    fixture.harbor("magpie", "start")
    second = json.loads((fixture.state / "magpie" / "instance.json").read_text())
    check.ok(second["pid"] != first["pid"], "restart: a new child is recorded")
    check.ok(second["started_at"] != first["started_at"], "restart: the started time moved with the child")
    stopped = fixture.harbor("magpie", "stop")
    check.ok("stopped" in output(stopped).lower(), "restart: the restarted child is stopped")
    probe = json.loads(fixture.harbor("magpie", "status", "--address", second["address"], "--json").stdout)
    check.ok(probe["state"] == "offline", "restart: its gateway is gone")


def case_shared_folder_data(fixture, check):
    """A shared-folder data folder beside Magpie falls back to the installed folder."""
    downloads = pathlib.Path(fixture.env["HOME"]) / "Downloads"
    downloads.mkdir(parents=True, exist_ok=True)
    portable = downloads / "data"
    portable.mkdir()
    (portable / "settings.json").write_text('{"theme":"light"}\n')
    installed = pathlib.Path(fixture.env["XDG_CONFIG_HOME"]) / "magpie"
    installed.mkdir(parents=True, exist_ok=True)
    (installed / "settings.json").write_text('{"theme":"dark"}\n')
    # The rule is read from the serving process, so Magpie itself must run
    # from the shared folder that holds the data folder.
    executable = downloads / "magpie"
    shutil.copy(fixture.magpie, executable)
    executable.chmod(0o755)
    address = f"127.0.0.1:{fixture.port()}"
    env = dict(fixture.env)
    env["MAGPIE_ADDR"] = address
    log_file = open(fixture.root / "shared-folder.log", "w")
    fixture.processes.append(subprocess.Popen([str(executable), "serve"], env=env,
                                              stdout=log_file, stderr=subprocess.STDOUT))
    fixture.gateway_ports.append(address)
    fixture.await_gateway(address)
    status = json.loads(fixture.harbor("magpie", "status", "--address", address, "--json").stdout)
    check.ok(status["state"] == "verified", "shared folder: the gateway is still verified")
    check.ok(status["config_dir"] == str(installed),
             "shared folder: Magpie's installed folder is reported, not an ambiguous answer")
    check.ok("installed folder" in status.get("config_warning", ""),
             "shared folder: the passed-over data folder is explained")


def case_cleanup_leaves_foreign_gateways(fixture, check):
    """Cleanup stops only processes started from inside the fixture root."""
    address = f"127.0.0.1:{fixture.port()}"
    foreign = fixture.root.parent / f"{fixture.root.name}-foreign"
    foreign.mkdir(mode=0o700, exist_ok=True)
    copy = foreign / "magpie"
    shutil.copy(fixture.magpie, copy)
    copy.chmod(0o755)
    (foreign / ".portable").write_text("portable\n")
    env = dict(fixture.env)
    env["MAGPIE_ADDR"] = address
    log_file = open(foreign / "serve.log", "w")
    proc = subprocess.Popen([str(copy), "serve"], env=env, stdout=log_file, stderr=subprocess.STDOUT)
    try:
        fixture.await_gateway(address)
        # The fixture believes it owns this port, which is exactly the state a
        # released port in a foreign Magpie's hands produces.
        fixture.ports.append(int(address.rsplit(":", 1)[1]))
        pid = fixture.gateway_pid(address)
        check.ok(not fixture.own_process(pid, address), "cleanup: a gateway outside the fixture is not owned")
        fixture.kill_gateway_on(address)
        check.ok(proc.poll() is None, "cleanup: a foreign gateway on a released port survives")
        fixture.signal_own_process(pid, address)
        check.ok(proc.poll() is None, "cleanup: a foreign pid is not signalled")
    finally:
        proc.terminate()
        proc.wait(timeout=15)
        log_file.close()
        shutil.rmtree(foreign, ignore_errors=True)


def case_manage_external_without_xdg(fixture, check):
    """Management reproduces an instance's absent XDG_CONFIG_HOME.

    A gateway started without XDG_CONFIG_HOME reads $HOME/.config/magpie. If
    Harbor let the TUI inherit the caller's XDG_CONFIG_HOME, Magpie would read
    the caller's configuration instead, which this case poisons so the
    difference is visible.
    """
    home = fixture.root / "external-no-xdg" / "home"
    (home / ".config").mkdir(parents=True, exist_ok=True)
    copy = fixture.root / "external-no-xdg" / "magpie"
    shutil.copy(fixture.magpie, copy)
    copy.chmod(0o755)
    instance_env = {"PATH": fixture.env["PATH"], "HOME": str(home), "LANG": "en_US.UTF-8", "NO_COLOR": "1"}
    subprocess.run([str(copy), "provider", "add", "ext-relay", "url=http://127.0.0.1:47998/v1",
                    "key=test-only", "models=ext-model"], env=instance_env, check=True,
                   capture_output=True, timeout=60)
    address = f"127.0.0.1:{fixture.port()}"
    instance_env["MAGPIE_ADDR"] = address
    log_file = open(fixture.root / "external-no-xdg.log", "w")
    fixture.processes.append(subprocess.Popen([str(copy), "serve"], env=instance_env,
                                              stdout=log_file, stderr=subprocess.STDOUT))
    fixture.gateway_ports.append(address)
    fixture.await_gateway(address)

    status = json.loads(fixture.harbor("magpie", "status", "--address", address, "--json").stdout)
    check.ok(status["state"] == "verified", "no-xdg: the gateway is verified")
    check.ok(status["config_dir"] == str(home / ".config" / "magpie"),
             "no-xdg: the instance's own home decides its configuration")

    poisoned = fixture.root / "caller-poison"
    (poisoned / "magpie").mkdir(parents=True, exist_ok=True)
    (poisoned / "magpie" / "providers.json").write_text("{not json")
    manage_env = dict(fixture.env)
    manage_env["XDG_CONFIG_HOME"] = str(poisoned)
    tui = fixture.tui([str(fixture.agent_harbor), "magpie", "manage", "--address", address],
                      manage_env, "tui-no-xdg.log")
    text = fixture.await_tui_log(tui, "providers", "tui-no-xdg.log")
    check.ok("can't be read" not in text and "cannot be read" not in text,
             "no-xdg: management did not inherit the caller's XDG_CONFIG_HOME")
    check.ok(tui.poll() is None, "no-xdg: the management TUI stayed open")
    os.killpg(tui.pid, signal.SIGTERM)
    try:
        tui.wait(timeout=15)
    except subprocess.TimeoutExpired:
        os.killpg(tui.pid, signal.SIGKILL)
        tui.wait(timeout=15)


def case_managed_lifecycle(fixture, check):
    """Start, verify identity, add a provider in the managed configuration only."""
    port = fixture.port()
    web_port = fixture.port()
    start = fixture.harbor("magpie", "start", "--port", str(port), "--web-port", str(web_port))
    address = f"127.0.0.1:{port}"
    record = json.loads((fixture.state / "magpie" / "instance.json").read_text())
    check.ok(record["pid"] > 1, "managed: a child PID is recorded")
    check.ok(output(start).count(address) >= 1, "managed: the address is reported")

    layout = fixture.state / "magpie"
    check.ok((layout / "bin" / ".portable").exists(), "managed: the copy is marked portable")
    check.ok((layout / "bin" / "data").is_dir(), "managed: configuration lives beside the copy")
    check.ok(not (pathlib.Path(fixture.env["XDG_CONFIG_HOME"]) / "magpie").exists(), "managed: nothing was written to the user configuration")
    mode = (layout / "web.key").stat().st_mode & 0o777
    check.ok(mode == 0o600, f"managed: the web key is private (0o{mode:o})")
    settings = json.loads((layout / "bin" / "data" / "settings.json").read_text())
    check.ok(settings.get("noStats") is True and settings.get("noAutoUpdate") is True and settings.get("noAutoDownload") is True,
             "managed: stats and self-update are disabled through Magpie settings")
    probe = json.loads(fixture.harbor("magpie", "status", "--address", address, "--json").stdout)
    check.ok(probe["pid"] == record["pid"], "managed: the running gateway is the recorded child")
    check.ok(probe["config_dir"].endswith("bin/data"), "managed: identity reports the portable configuration")

    listing = fixture.harbor("magpie", "status")
    check.ok("Managed" in output(listing) and "running" in output(listing), "managed: status shows it running")

    # A provider added through the managed configuration appears only there.
    before_external = fixture.provider_catalog(address)
    external_address = f"127.0.0.1:{fixture.port()}"
    fixture.start_external(None, external_address)
    env = dict(fixture.env)
    env["MAGPIE_ADDR"] = address
    subprocess.run([str(layout / "bin" / "magpie"), "provider", "add", "managed-relay",
                    "url=http://127.0.0.1:47998/v1", "key=test-only", "models=managed-model"],
                   env=env, check=True, capture_output=True, timeout=60)
    fixture.await_catalog(address, "managed-relay/managed-model")
    check.ok("managed-relay/managed-model" in fixture.provider_catalog(address), "managed: the new provider serves the managed gateway")
    check.ok("managed-relay/managed-model" not in fixture.provider_catalog(external_address), "managed: the external gateway is unaffected")
    check.ok(fixture.provider_catalog(address) != before_external or before_external == [], "managed: the catalog changed on the managed gateway")

    # Management goes through Harbor's own command, with a caller environment
    # that would point Magpie at a different configuration if Harbor inherited
    # it. Closing the TUI must leave the gateway serving. A terminal is
    # required: without one magpie tui refuses to start.
    manage_env = dict(fixture.env)
    manage_env["XDG_CONFIG_HOME"] = str(fixture.root / "caller-config")
    caller_config = pathlib.Path(manage_env["XDG_CONFIG_HOME"]) / "magpie"
    manage_env["XDG_CACHE_HOME"] = str(fixture.root / "caller-cache")
    # A poisoned caller configuration: if Harbor let the TUI inherit it, Magpie
    # would read this file and report that it cannot be parsed.
    caller_config.mkdir(parents=True, exist_ok=True)
    (caller_config / "providers.json").write_text("{not json")
    tui = fixture.start_pty_tui([str(fixture.agent_harbor), "magpie", "manage", "--address", address],
                                manage_env, "tui.log")
    # The routing page lists the providers of the configuration the TUI read,
    # so the managed instance's own provider appearing there is positive proof
    # of which configuration was consumed.
    rendered = fixture.await_pty_text("tui.log", "managed-relay", keys=["2"])
    check.ok("managed-relay" in rendered,
             "management TUI: it rendered the instance's own configuration")
    check.ok("can't be read" not in rendered and "cannot be read" not in rendered,
             "management TUI: it did not read the caller's configuration")
    check.ok(tui.poll() is None, "management TUI: it stayed open while the gateway served it")
    os.killpg(tui.pid, signal.SIGTERM)
    try:
        tui.wait(timeout=15)
    except subprocess.TimeoutExpired:
        os.killpg(tui.pid, signal.SIGKILL)
        tui.wait(timeout=15)
    fixture.stop_pty_tui("tui.log")
    after = json.loads(fixture.harbor("magpie", "status", "--address", address, "--json").stdout)
    check.ok(after["state"] == "verified" and after["pid"] == record["pid"],
             "management TUI: the gateway kept serving after it closed")
    check.ok((fixture.root / "tui.log").stat().st_size > 0, "management TUI: it produced output")
    check.ok("gateway stopped" not in (fixture.root / "tui.log").read_text(), "management TUI: it did not stop the gateway on exit")

    # Harbor stops only its own process.
    fixture.harbor("magpie", "stop")
    stopped = json.loads(fixture.harbor("magpie", "status", "--address", address, "--json").stdout)
    check.ok(stopped["state"] == "offline", "managed: stop ended the recorded process")
    external_still = json.loads(fixture.harbor("magpie", "status", "--address", external_address, "--json").stdout)
    check.ok(external_still["state"] == "verified", "managed: stop left the external gateway alone")
    second_stop = fixture.harbor("magpie", "stop")
    check.ok("not running" in output(second_stop), "managed: stopping again reports it is not running")
    return address


def case_refuse_foreign_stop(fixture, check):
    """A record that does not match the live process is refused, not killed."""
    address = f"127.0.0.1:{fixture.port()}"
    external = fixture.start_external(None, address)
    layout = fixture.state / "magpie"
    (layout / "bin").mkdir(parents=True, exist_ok=True)
    fake = layout / "bin" / "magpie"
    fake.write_text("#!/bin/sh\n")
    fake.chmod(0o755)
    (layout / "instance.json").write_text(json.dumps({
        "address": f"http://{address}", "config_dir": str(layout / "bin" / "data"),
        "executable": str(fake), "version": MAGPIE_VERSION,
        "pid": external.pid, "started_at": "2026-01-01T00:00:00Z",
    }))
    stop = fixture.harbor("magpie", "stop", expect=1)
    check.ok("left alone" in output(stop), "foreign stop: the command reports refusal")
    check.ok(external.poll() is None, "foreign stop: the other Magpie is still running")
    (layout / "instance.json").unlink()


def case_upgrade_only_when_stopped(fixture, check):
    """Replacing the copied executable while it runs is refused."""
    port = fixture.port()
    fixture.harbor("magpie", "start", "--port", str(port), "--web-port", str(port + 1))
    layout = fixture.state / "magpie"
    record = json.loads((layout / "instance.json").read_text())
    digest_before = (layout / "bin" / "magpie").read_bytes()
    fixture.harbor("magpie", "stop")
    fixture.harbor("magpie", "start", "--port", str(port), "--web-port", str(port + 1))
    check.ok((layout / "bin" / "magpie").read_bytes() == digest_before, "upgrade: a restart keeps the same copy")
    check.ok(record["pid"] != json.loads((layout / "instance.json").read_text())["pid"], "upgrade: a new process was started")
    fixture.harbor("magpie", "stop")


def case_external_tui(fixture, check):
    """An external gateway whose own TUI serves it: warned, and closing it stops it."""
    address = f"127.0.0.1:{fixture.port()}"
    env = dict(fixture.env)
    env["MAGPIE_ADDR"] = address
    env["HOME"] = str(fixture.root / "tui-home")
    pathlib.Path(env["HOME"]).mkdir(exist_ok=True)
    # Portable from its own copy, like any copy Harbor or a user tests.
    copy = fixture.root / "external-tui" / "magpie"
    copy.parent.mkdir()
    shutil.copy(fixture.magpie, copy)
    copy.chmod(0o755)
    (copy.parent / ".portable").write_text("portable\n")
    external_tui = fixture.tui([str(copy), "tui"], env, "external-tui.log")
    deadline = time.time() + 40
    while time.time() < deadline and external_tui.poll() is None:
        try:
            with urllib.request.urlopen(f"http://{address}/", timeout=1) as response:
                if json.load(response).get("name") == "magpie":
                    break
        except Exception:
            pass
        time.sleep(0.5)
    probe = json.loads(fixture.harbor("magpie", "status", "--address", address, "--json").stdout)
    check.ok(probe["state"] == "verified" and probe["serves"] == "tui",
             "external TUI: the gateway reports that its TUI serves it")
    if probe["state"] != "verified":
        return
    fixture.harbor("magpie", "bind", "--address", address)
    manage = fixture.harbor("magpie", "manage", "--address", address, expect=1)
    check.ok("owns the gateway" in output(manage), "external TUI: management warns that the TUI owns the gateway")
    check.ok("Closing it stops" in output(manage), "external TUI: the warning says closing it stops the gateway")

    os.killpg(external_tui.pid, signal.SIGTERM)
    try:
        external_tui.wait(timeout=15)
    except subprocess.TimeoutExpired:
        os.killpg(external_tui.pid, signal.SIGKILL)
        external_tui.wait(timeout=15)
    stopped = json.loads(fixture.harbor("magpie", "status", "--address", address, "--json").stdout)
    check.ok(stopped["state"] == "offline", "external TUI: closing it stopped the gateway it hosted")
    status = json.loads(fixture.harbor("magpie", "status", "--json").stdout)
    if len(status["instances"]) == 1:
        check.ok(status["instances"][0]["health"] == "offline",
                 "external TUI: the recorded instance reports the gateway offline")


CASES = {
    "same-config": case_same_config,
    "mismatched-config": case_mismatched_config,
    "no-gateway": case_no_gateway,
    "port-conflict": case_port_conflict,
    "managed-lifecycle": case_managed_lifecycle,
    "manage-external-no-xdg": case_manage_external_without_xdg,
    "refuse-foreign-stop": case_refuse_foreign_stop,
    "upgrade-when-stopped": case_upgrade_only_when_stopped,
    "external-tui": case_external_tui,
    "recorded-port-taken": case_recorded_port_taken,
    "stop-after-restart": case_stop_after_restart_only,
    "shared-folder-data": case_shared_folder_data,
    "cleanup-ownership": case_cleanup_leaves_foreign_gateways,
}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--magpie", required=True, help="pinned Magpie 0.1.3528 executable")
    parser.add_argument("--bridge", default=str(pathlib.Path(__file__).resolve().parent.parent))
    parser.add_argument("--binary", default="", help="prebuilt agent-harbor binary")
    parser.add_argument("--cases", default=",".join(CASES))
    parser.add_argument("--keep", action="store_true")
    args = parser.parse_args()

    fixture = Fixture(args.magpie, keep=args.keep)
    if args.binary:
        shutil.copy(args.binary, fixture.agent_harbor)
        fixture.agent_harbor.chmod(0o755)
    else:
        build_binary(args.bridge, fixture.agent_harbor)

    version = subprocess.run([str(fixture.magpie), "--version"], capture_output=True, text=True).stdout.strip()
    check = Assertions()
    log(f"fixture {fixture.root}")
    log(f"magpie  {version}")
    failed = []
    try:
        check.ok(version == f"magpie {MAGPIE_VERSION}", f"pinned Magpie {MAGPIE_VERSION} is used")
        recorded = ""
        for name in args.cases.split(","):
            log(f"case {name}")
            handler = CASES[name]
            if name == "mismatched-config":
                handler(fixture, check, recorded)
            else:
                result = handler(fixture, check)
                if result:
                    recorded = result
    except (AssertionError, Exception) as error:
        failed.append(str(error))
        log(f"FAILED: {error}")
        log(fixture.log_tail())
    finally:
        fixture.cleanup()
    log(f"{check.checks} checks, {len(failed)} failures")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
