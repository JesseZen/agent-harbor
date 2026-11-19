#!/usr/bin/env python3
"""Live popup migration with a real previous package and isolated workspace."""

import argparse
import errno
import hashlib
import http.server
import json
import os
from pathlib import Path
import platform
import shlex
import shutil
import signal
import subprocess
import sys
import tarfile
import tempfile
import threading
import time


def wait_for(check, description):
    deadline = time.monotonic() + 25
    while time.monotonic() < deadline:
        if result := check():
            return result
        time.sleep(0.15)
    raise AssertionError(f"timed out waiting for {description}")


def processes(fragment):
    output = subprocess.check_output(["/bin/ps", "-axo", "pid=,command="], text=True)
    found = []
    for line in output.splitlines():
        pid, command = line.strip().split(maxsplit=1)
        if fragment in command:
            found.append(int(pid))
    return sorted(found)


def make_archives(root, source, installed, candidate):
    dist = root / "repo"
    system = platform.system().lower()
    arch = {"aarch64": "arm64", "arm64": "arm64", "x86_64": "amd64"}[platform.machine()]
    for tag, binary in [("v0.1.0-dev.8", installed / "agent-harbor"), ("v0.1.0-popup-test", candidate)]:
        directory = dist / tag
        directory.mkdir(parents=True)
        archive = directory / f"agent-harbor-{tag}-{system}-{arch}.tar.gz"
        with tarfile.open(archive, "w:gz", compresslevel=1) as package:
            for name, path in {
                "agent-harbor": binary, "herdr": installed / "herdr", "magpie": installed / "magpie",
                "codex-auth": installed / "codex-auth", "README.md": source / "bridge/PACKAGE-README.md",
                "install.sh": source / "bridge/install.sh", "manage.sh": source / "distribution/install.sh",
                "licenses": source / "bridge/licenses",
            }.items():
                package.add(path, arcname=name)
        with archive.open("rb") as stream:
            digest = hashlib.file_digest(stream, "sha256").hexdigest()
        (directory / "SHA256SUMS").write_text(f"{digest}  {archive.name}\n")
    return dist


def homebrew_formula(source, dist, tag, base_url, formula):
    digest = (dist / tag / "SHA256SUMS").read_text().split()[0]
    sums = dist / "homebrew-checksums"
    sums.write_text("".join(f"{digest}  agent-harbor-{tag}-{system}-{arch}.tar.gz\n"
                            for system in ("darwin", "linux") for arch in ("arm64", "amd64")))
    recipe = subprocess.check_output([sys.executable, source / "distribution/homebrew.py", tag, sums], text=True)
    formula.write_text(recipe.replace("https://github.com/JesseZen/agent-harbor/releases/download", base_url))


def exercise(args, root):
    root = root.resolve()
    installed = args.old_libexec.resolve()
    candidate = args.binary.resolve()
    source = Path(__file__).resolve().parents[2]
    env = {"PATH": f"{root}/tools:/usr/bin:/bin:/usr/sbin:/sbin", "TERM": "xterm-256color",
           "SHELL": "/bin/sh", "LANG": "en_US.UTF-8", "NO_COLOR": "1"}
    for key, leaf in [("HOME", "home"), ("XDG_CONFIG_HOME", "config"), ("XDG_DATA_HOME", "data"),
                      ("XDG_STATE_HOME", "state"), ("XDG_CACHE_HOME", "cache"), ("XDG_RUNTIME_DIR", "runtime"),
                      ("TMPDIR", "tmp"), ("CODEX_HOME", "codex"), ("CODEX_SQLITE_HOME", "sqlite")]:
        (root / leaf).mkdir(mode=0o700)
        env[key] = str(root / leaf)
    (root / "tools").mkdir()
    (root / "tools/codex").symlink_to(args.codex.resolve())
    project = root / "project"
    project.mkdir()
    codex_config = (
        'check_for_update_on_startup = false\nmodel = "test-model"\nmodel_provider = "fixture"\n'
        '[features]\nhooks = true\n[model_providers.fixture]\nname = "fixture"\n'
        'base_url = "http://127.0.0.1:1/v1"\nwire_api = "responses"\nrequires_openai_auth = false\n'
        f'[projects.{json.dumps(str(project))}]\ntrust_level = "trusted"\n'
    )
    (root / "codex/config.toml").write_text(codex_config)
    workspace = root / "harbor"
    env["AGENT_HARBOR_BRIDGE_HOME"] = str(workspace)

    def run(argv, check=True, **kwargs):
        result = subprocess.run(list(map(str, argv)), env=env, cwd=project, capture_output=True, text=True, **kwargs)
        if check and result.returncode:
            raise RuntimeError(f"{argv} failed: {result.stdout}{result.stderr}")
        return result

    downloads = None
    if args.method == "script" or args.brew:
        dist = make_archives(root, source, installed, candidate)

        class DownloadHandler(http.server.SimpleHTTPRequestHandler):
            def __init__(self, *items, **kwargs):
                super().__init__(*items, directory=dist, **kwargs)

            def copyfile(self, source, outputfile):
                try:
                    super().copyfile(source, outputfile)
                except (BrokenPipeError, ConnectionResetError):
                    pass

            def log_message(self, *_):
                pass

        downloads = http.server.ThreadingHTTPServer(("127.0.0.1", 0), DownloadHandler)
        threading.Thread(target=downloads.serve_forever, daemon=True).start()
        download_base = f"http://127.0.0.1:{downloads.server_port}"
    if args.method == "script":
        env["AGENT_HARBOR_DOWNLOAD_BASE"] = download_base
        env["AGENT_HARBOR_INSTALL_ROOT"] = str(root / "installation")
        env["AGENT_HARBOR_BIN_DIR"] = str(root / "commands")
        old_bin = root / "installation/releases/v0.1.0-dev.8/agent-harbor"
        new_bin = root / "installation/releases/v0.1.0-popup-test/agent-harbor"
        ah = root / "commands/ah"
        run(["/bin/sh", source / "distribution/install.sh", "install", "--version", "v0.1.0-dev.8"])
    elif args.brew:
        brew = args.brew.resolve()
        prefix = brew.parent.parent
        assert brew == prefix / "bin/brew"
        assert (prefix / ".harbor-brew-fixture").read_text() == "agent-harbor-brew-fixture-v1\n"
        env.update(HOMEBREW_NO_AUTO_UPDATE="1", HOMEBREW_NO_ANALYTICS="1",
                   HOMEBREW_NO_INSTALL_CLEANUP="1", HOMEBREW_NO_INSTALLED_DEPENDENTS_CHECK="1",
                   HOMEBREW_CACHE=str(root / "cache/brew"),
                   HOMEBREW_LOGS=str(root / "logs"), HOMEBREW_TEMP=str(root / "tmp"))

        def brew_command(*parts):
            with subprocess.Popen([brew, *parts], env=env, cwd=project, text=True,
                                  stdout=subprocess.PIPE, stderr=subprocess.PIPE, start_new_session=True) as process:
                try:
                    stdout, stderr = process.communicate(timeout=90)
                except subprocess.TimeoutExpired:
                    os.killpg(process.pid, signal.SIGKILL)
                    process.communicate()
                    raise
                if process.returncode:
                    raise RuntimeError(f"brew {parts} failed: {stdout}{stderr}")

        assert run([brew, "--prefix"]).stdout.strip() == str(prefix)
        cellar = prefix / "Cellar/agent-harbor"
        ah = prefix / "bin/ah"
        tap = prefix / "Library/Taps/harbortest/homebrew-harbor"
        assert all(not os.path.lexists(path) for path in (cellar, ah, prefix / "opt/agent-harbor", tap))
        formula = tap / "Formula/agent-harbor.rb"
        formula.parent.mkdir(parents=True)
        run(["/usr/bin/git", "-c", "core.hooksPath=/dev/null", "init", tap])
        name = "harbortest/harbor/agent-harbor"
        homebrew_formula(source, dist, "v0.1.0-dev.8", download_base, formula)
        brew_command("install", name)
        old_bin = cellar / "0.1.0-dev.8/libexec/agent-harbor"
        new_bin = cellar / "0.1.0-popup-test/libexec/agent-harbor"
        assert ah.resolve() == old_bin
    else:
        cellar = root / "Cellar/agent-harbor"
        old_bin = cellar / "0.1.0-dev.8/libexec/agent-harbor"
        new_bin = cellar / "0.1.0-popup-test/libexec/agent-harbor"
        for binary, source_bin in [(old_bin, installed / "agent-harbor"), (new_bin, candidate)]:
            binary.parent.mkdir(parents=True)
            for name, executable in [("agent-harbor", source_bin), *[(n, installed / n) for n in ("herdr", "magpie", "codex-auth")]]:
                target = binary.parent / name
                try:
                    os.link(executable, target)
                except OSError:
                    shutil.copy2(executable, target)
        selector = root / "opt/agent-harbor"
        selector.parent.mkdir()
        selector.symlink_to(old_bin.parent.parent)
        ah = root / "tools/ah"
        ah.symlink_to(selector / "libexec/agent-harbor")

    tmux = shutil.which("tmux")
    assert tmux, "tmux is required"
    tmux_socket = root / "tmux.sock"

    def tm(*argv, check=True):
        return run([tmux, "-f", "/dev/null", "-S", tmux_socket, *argv], check=check).stdout

    def press(*keys):
        for key in keys:
            tm("send-keys", "-t", "fixture:0.0", key)
            time.sleep(0.15)

    def capture():
        return tm("capture-pane", "-p", "-t", "fixture:0.0")

    def visible(text):
        return wait_for(lambda: text in capture(), text)

    def visible_draft(draft):
        wait_for(lambda: any(line.rstrip().endswith(draft) for line in capture().splitlines()), "unchanged Codex draft")

    def attach():
        tm("send-keys", "-t", "fixture:0.0", "-l", shlex.quote(str(ah)))
        press("Enter")

    def owned_herdr(*argv):
        output = run([workspace / "herdr/bin/herdr", *argv]).stdout
        return json.loads(output)["result"] if output.strip() else None

    def plugin_command():
        plugins = owned_herdr("plugin", "list", "--json")["plugins"]
        plugin, = (p for p in plugins if p["plugin_id"] == "agent-harbor.bridge")
        return plugin["panes"][0]["command"][0]

    def own_pid(fragment):
        pids = processes(fragment)
        assert len(pids) == 1, (fragment, pids)
        return pids[0]

    try:
        workspace.mkdir(mode=0o700)
        marker = workspace / ".agent-harbor-bridge"
        marker.write_text("agent-harbor-bridge-state-v1\n")
        marker.chmod(0o600)
        shortcuts = workspace / "shortcuts.json"
        shortcuts.write_text('{"harbor":"prefix+t","settings":"ctrl+g","magpie":"prefix+f"}\n')
        launcher = root / "launch.sh"
        launcher.write_text("#!/bin/sh\n" + "".join(f"export {key}={shlex.quote(value)}\n" for key, value in env.items()) +
                            f"cd {shlex.quote(str(project))}\nexec /bin/sh\n")
        launcher.chmod(0o700)
        tm("new-session", "-d", "-s", "fixture", "-x", "160", "-y", "45", launcher)
        attach()
        visible("Harbor ^B t")
        press("C-b", "t")
        visible("Search sessions")
        own_pid(f"{old_bin} _app")
        press("q")
        panes = owned_herdr("pane", "list")["panes"]
        pane_id = panes[0]["pane_id"]
        executable = root / "tools/codex"
        owned_herdr("pane", "run", pane_id, f"cd {shlex.quote(str(project))} && exec {shlex.quote(str(executable))} --no-daemon")
        visible("Ask Codex")
        draft = f"UNSENT_HARBOR_{args.method.upper()}_DRAFT"
        tm("send-keys", "-t", "fixture:0.0", "-l", draft)
        visible_draft(draft)
        old_herdr_pid = own_pid(f"{old_bin.parent}/herdr --session agent-harbor server")
        codex_pid = own_pid(f"{executable} --no-daemon")
        config = workspace / "herdr/config/herdr.toml"
        manifest = workspace / "herdr/plugin/herdr-plugin.toml"
        old_files = [file.read_bytes() for file in (config, manifest, shortcuts)]
        assert plugin_command() == str(old_bin)
        press("C-b", "t")
        visible("Search sessions")
        old_popup_pid = own_pid(f"{old_bin} _app")

        if args.method == "script":
            run([ah, "update", "--version", "v0.1.0-popup-test"])
        elif args.brew:
            homebrew_formula(source, dist, "v0.1.0-popup-test", download_base, formula)
            brew_command("upgrade", name)
            assert ah.resolve() == new_bin
            brew_command("cleanup", name)
            assert not old_bin.exists()
        else:
            next_selector = selector.with_name("next")
            next_selector.symlink_to(new_bin.parent.parent)
            next_selector.replace(selector)
            shutil.rmtree(old_bin.parent.parent)
            assert not old_bin.exists()

        def migrate(expect_error=None):
            process = subprocess.Popen([ah], env=env, cwd=project, stdin=subprocess.DEVNULL,
                                       stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
            try:
                stdout, stderr = process.communicate(timeout=25)
                if expect_error:
                    assert expect_error in stderr, (stdout, stderr)
                else:
                    assert "popup upgrade" not in stderr, (stdout, stderr)
            finally:
                if process.poll() is None:
                    process.kill()
                    process.wait()

        if args.method == "script":
            herdr = new_bin.parent / "herdr"
            real_herdr = herdr.with_name("herdr-real")
            herdr.rename(real_herdr)
            reload_fault = root / "reload-fault"
            reload_fault.touch()
            herdr.write_text(f'''#!/bin/sh
case "$*" in
    *'server reload-config')
        if test -f {shlex.quote(str(reload_fault))}; then
            /bin/rm {shlex.quote(str(reload_fault))}
            {shlex.quote(str(real_herdr))} "$@" >/dev/null || exit 1
            printf '%s\\n' '{{"result":{{"type":"config_reload","status":"partial"}}}}'
            exit 0
        fi;;
esac
exec {shlex.quote(str(real_herdr))} "$@"
''')
            herdr.chmod(0o700)
            migrate("previous configuration and registration restored")
            assert not reload_fault.exists()
            assert [file.read_bytes() for file in (config, manifest, shortcuts)] == old_files
            assert plugin_command() == str(old_bin)
            real_herdr.replace(herdr)
            registry, = (workspace / "herdr/config").glob("*/plugins.json")
            prior_registry = registry.read_bytes()
            blocker = registry.with_suffix(".json.tmp")
            blocker.mkdir()
            migrate("previous configuration and registration restored")
            assert [file.read_bytes() for file in (config, manifest, shortcuts)] == old_files
            assert plugin_command() == str(old_bin)
            assert registry.read_bytes() == prior_registry
            blocker.rmdir()

        migrate()
        stable = workspace / "herdr/bin/agent-harbor"
        wait_for(lambda: str(stable) in config.read_text(), "stable workspace entry")
        assert plugin_command() == str(stable)
        assert shortcuts.read_bytes() == old_files[2]
        assert own_pid(f"{old_bin} _app") == old_popup_pid
        config_inode = config.stat().st_ino
        migrate()
        assert config.stat().st_ino == config_inode, "repeat open rewrote the already migrated configuration"
        assert plugin_command() == str(stable)
        press("q")
        visible_draft(draft)
        press("C-b", "t")
        visible("Search sessions")
        new_popup_pid = own_pid(f"{new_bin} _app")
        assert new_popup_pid != old_popup_pid
        press("q")
        visible_draft(draft)
        press("C-b", "f")
        visible("No recorded instances.")
        press("Escape")
        visible("Search sessions")
        press("q")
        wait_for(lambda: not processes(f"{new_bin} _magpie"), "closed Magpie popup")
        visible_draft(draft)
        press("C-g")
        visible("Harbor · Settings")
        press("C-s")
        visible("Saved for this workspace")
        assert str(stable) in config.read_text()
        press("Escape")
        wait_for(lambda: not processes(f"{new_bin} _settings"), "closed Settings popup")
        visible_draft(draft)
        assert own_pid(f"{old_bin.parent}/herdr --session agent-harbor server") == old_herdr_pid
        assert own_pid(f"{executable} --no-daemon") == codex_pid
        assert pane_id in [p["pane_id"] for p in owned_herdr("pane", "list")["panes"]]
        press("C-b", "t")
        visible("Search sessions")
        press("n")
        visible("New session")
        press("C-s")
        visible("Conversation recovery")
        press("y")
        wait_for(lambda: "Ask Codex" in capture() or "Hooks need review" in capture(), "new managed Codex pane")
        sessions = json.loads((workspace / "sessions.json").read_text())
        assert len(sessions) == 1 and sessions[0]["herdr_pane"] != pane_id
        assert sessions[0]["process_group"] > 1
        assert any((root / "codex").glob("hooks*.json")), "Harbor did not install recovery integration through the current Herdr companion"
        press("C-b", "q")
        assert own_pid(f"{old_bin.parent}/herdr --session agent-harbor server") == old_herdr_pid
        run([ah, "stop"])
        wait_for(lambda: old_herdr_pid not in processes(f"{old_bin.parent}/herdr --session agent-harbor server"), "owned Herdr stop")
        print(json.dumps({"method": args.method, "old": run([installed / "agent-harbor", "version"]).stdout.strip(),
                          "new": run([ah, "version"]).stdout.strip(), "herdr": run([installed / "herdr", "--version"]).stdout.strip(),
                          "codex": run([executable, "--version"]).stdout.strip(), "herdr_pid": old_herdr_pid,
                          "codex_pid": codex_pid, "old_popup_pid": old_popup_pid, "new_popup_pid": new_popup_pid}))
    except Exception:
        (root / "failure-screen.txt").write_text(tm("capture-pane", "-p", "-t", "fixture:0.0", check=False))
        for log in (workspace / "herdr").rglob("herdr-server.log"):
            print(f"{log.name}\n" + "\n".join(log.read_text().splitlines()[-25:]))
        raise
    finally:
        run([ah, "stop"], check=False)
        tm("kill-server", check=False)
        if downloads:
            downloads.shutdown()
            downloads.server_close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--method", choices=("script", "homebrew"), required=True)
    parser.add_argument("--brew", type=Path, help="run Homebrew from a clean, marked fixture prefix")
    parser.add_argument("--old-libexec", type=Path, required=True)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--codex", type=Path, required=True)
    args = parser.parse_args()
    if args.brew and args.method != "homebrew":
        parser.error("--brew requires --method homebrew")
    class DiscoveryHandler(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            self.send_error(404)

        def log_message(self, *_):
            pass

    try:
        discovery = http.server.ThreadingHTTPServer(("127.0.0.1", 3425), DiscoveryHandler)
    except OSError as error:
        reason = "in use" if error.errno == errno.EADDRINUSE else error.strerror
        print(f"Blocked: cannot reserve discovery port 3425 ({reason}). No existing service was contacted.")
        raise SystemExit(77)
    threading.Thread(target=discovery.serve_forever, daemon=True).start()
    with tempfile.TemporaryDirectory(prefix="popup-upgrade-", dir="/tmp") as scratch:
        try:
            exercise(args, Path(scratch))
        except Exception:
            screen = Path(scratch) / "failure-screen.txt"
            if screen.exists():
                print(screen.read_text())
            raise
        finally:
            discovery.shutdown()
            discovery.server_close()


if __name__ == "__main__":
    main()
