"""Exercise the production Sessions preview in a disposable Herdr popup."""

import argparse
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import socket
from socketserver import StreamRequestHandler, ThreadingUnixStreamServer
import subprocess
import sys
import tempfile
from threading import Thread
import time

from pane_read_probe import until


def exercise(args, root):
    tmux_path = shutil.which('tmux') or '/usr/bin/tmux'
    env = {'PATH': os.pathsep.join(dict.fromkeys([str(Path(tmux_path).parent), '/usr/bin', '/bin', '/usr/sbin', '/sbin'])),
           'SHELL': '/bin/sh', 'TERM': 'xterm-256color',
           'COLORTERM': 'truecolor', 'LANG': 'en_US.UTF-8', 'PS1': 'fixture$ ',
           'GIT_ALLOW_PROTOCOL': 'file', 'GIT_TERMINAL_PROMPT': '0'}
    for key in ('HOME', 'XDG_CONFIG_HOME', 'XDG_DATA_HOME', 'XDG_STATE_HOME', 'XDG_CACHE_HOME',
                'XDG_RUNTIME_DIR', 'TMPDIR', 'CODEX_HOME', 'CODEX_SQLITE_HOME'):
        directory = root / key.lower()
        directory.mkdir(mode=0o700)
        env[key] = str(directory)
    harbor = root / 'h'
    harbor.mkdir(mode=0o700)
    ownership = harbor / '.agent-harbor-bridge'
    ownership.write_text('agent-harbor-bridge-state-v1\n')
    ownership.chmod(0o600)
    (harbor / 'herdr').mkdir(mode=0o700)
    source_socket = root / 'herdr.sock'
    proxy_socket = harbor / 'herdr/p.sock'
    env.update(AGENT_HARBOR_BRIDGE_HOME=str(harbor), HERDR_SOCKET_PATH=str(source_socket),
               HERDR_CONFIG_PATH=str(root / 'herdr.toml'))
    project = root / 'preview-fixture'
    project.mkdir()
    subprocess.run(['git', 'init', '-q', '-b', 'fixture-context'], cwd=project, env=env, check=True)
    with socket.socket() as port:
        port.bind(('127.0.0.1', 0))
        (Path(env['CODEX_HOME']) / 'config.toml').write_text(
            'check_for_update_on_startup = false\ncli_auth_credentials_store = "file"\n'
            'model = "gpt-5.5"\nmodel_provider = "fixture"\n[model_providers.fixture]\n'
            f'name = "Fixture"\nbase_url = "http://127.0.0.1:{port.getsockname()[1]}/v1"\nwire_api = "responses"\n'
            f'[projects.{json.dumps(str(project))}]\ntrust_level = "trusted"\n')
        run_fixture(args, root, env, harbor, source_socket, proxy_socket, project)


def run_fixture(args, root, env, harbor, source_socket, proxy_socket, project):
    binary = root / 'agent-harbor'
    shutil.copy2(args.binary, binary)
    command = shlex.join(['env', 'HERDR_SOCKET_PATH=' + str(proxy_socket), str(binary), '_app'])
    Path(env['HERDR_CONFIG_PATH']).write_text(
        'onboarding = false\n[update]\nversion_check = false\nmanifest_check = false\n'
        '[[keys.command]]\nkey = "prefix+a"\ntype = "popup"\n'
        f'command = {json.dumps(command)}\ndescription = "Preview fixture"\nwidth = "80%"\nheight = "80%"\n')
    tmux = shutil.which('tmux')
    reads = []
    state = {'offline': False}

    class Proxy(StreamRequestHandler):
        def handle(self):
            request = self.rfile.readline(65537)
            payload = json.loads(request)
            if payload['method'] == 'pane.read':
                reads.append((time.monotonic(), payload['params']['pane_id']))
            if state['offline']:
                return
            try:
                with socket.socket(socket.AF_UNIX) as upstream:
                    upstream.settimeout(2)
                    upstream.connect(str(source_socket))
                    upstream.sendall(request)
                    with upstream.makefile('rb') as response:
                        self.wfile.write(response.readline(2**22))
            except (BrokenPipeError, ConnectionResetError, TimeoutError):
                pass

    def run(*argv, check=True):
        result = subprocess.run(argv, env=env, cwd=project, capture_output=True, text=True, timeout=10)
        if check and result.returncode:
            raise RuntimeError(f'{Path(argv[0]).name} failed: {result.stderr or result.stdout}')
        return result.stdout

    def tm(*values, check=True):
        return run(tmux, '-f', '/dev/null', '-S', root / 'tmux.sock', *values, check=check)

    def capture(color=False):
        return tm('capture-pane', '-pe' if color else '-p', '-t', 'preview:0.0')

    def key(*keys):
        tm('send-keys', '-t', 'preview:0.0', *keys)

    def rpc(method, **params):
        with socket.socket(socket.AF_UNIX) as client:
            client.settimeout(3)
            client.connect(str(source_socket))
            client.sendall(json.dumps(dict(id='fixture', method=method, params=params)).encode() + b'\n')
            with client.makefile('rb') as stream:
                data = json.loads(stream.readline(2**22))
        assert 'error' not in data, (method, data)
        return data['result']

    def info(pane):
        return rpc('pane.get', pane_id=pane)['pane']

    def tab():
        prior = {item['pane_id'] for item in rpc('pane.list')['panes']}
        rpc('tab.create', cwd=str(project), focus=False)
        return next(item['pane_id'] for item in rpc('pane.list')['panes'] if item['pane_id'] not in prior)

    def send(pane, text):
        rpc('pane.send_input', pane_id=pane, text=text, keys=['Enter'])

    def visible(pane):
        return rpc('pane.read', pane_id=pane, source='visible', format='ansi', strip_ansi=False)['read']['text']

    def report(pane, activity):
        rpc('pane.report_agent', pane_id=pane, source='preview-fixture', agent='codex', state=activity)

    def save(label, columns, frame=None):
        public = capture(True) if frame is None else frame
        public = public.replace(str(root), '/fixture/isolated'.ljust(len(str(root)), 'x'))
        for private in (socket.gethostname().split('.')[0], os.environ.get('USER', '')):
            if private:
                public = public.replace(private, 'x' * len(private))
        (args.output / f'{label}-{columns}.ansi').write_text(public)

    def size():
        for line in run('/bin/ps', '-Ao', 'tty=,command=').splitlines():
            tty, _, cmd = line.strip().partition(' ')
            cmd = cmd.lstrip()
            if cmd == str(binary) + ' _app':
                with open('/dev/' + tty.removeprefix('/dev/'), 'rb', buffering=0) as terminal:
                    return os.get_terminal_size(terminal.fileno())
        return None

    proxy = ThreadingUnixStreamServer(str(proxy_socket), Proxy)
    proxy.daemon_threads = True
    Thread(target=proxy.serve_forever, daemon=True).start()
    server = subprocess.Popen([args.herdr, 'server'], env=env, cwd=project,
                              stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
        until(source_socket.exists, 'isolated Herdr API')
        tm('new-session', '-d', '-s', 'preview', '-x', '160', '-y', '45', str(args.herdr))
        prior = until(lambda: rpc('pane.list')['panes'], 'initial pane')[0]['pane_id']
        send(prior, "printf '\\033[2J\\033[H'; exec /bin/sleep 120")
        records = []
        panes = []
        inode = proxy_socket.stat()
        for index, (name, activity, mode, marker) in enumerate((
            ('Native build', 'working', 'local', 'NATIVE-FRAME'),
            ('Magpie review', 'blocked', 'magpie', 'MAGPIE-FRAME'),
        ), 1):
            pane = tab()
            panes.append(pane)
            # Readable Unicode and two color depths from a real PTY.
            show = ("printf '\\033[2J\\033[H\\n\\033[38;2;110;225;201m  %s\\033[0m\\n\\n"
                    "\\033[38;5;215m  中文 · é · 👩‍💻\\033[0m\\n'; "
                    "exec /usr/bin/env AH_SESSION_RUN_TOKEN=fixture-%d %s -c 'import time; time.sleep(120)'")
            send(pane, show % (marker, index, shlex.quote(sys.executable)))
            until(lambda: marker in visible(pane), marker + ' source')
            process = until(lambda: rpc('pane.process_info', pane_id=pane)['process_info'].get('foreground_process_group_id'), 'fixture run group')
            report(pane, activity)
            record = dict(version=3, id=f'{index:032x}', name=name, working_directory=str(project), cli='codex',
                          mode=mode, recovery='ready', started=True, created_at=f'2026-11-{24-index:02}T00:00:00Z',
                          herdr_pane=pane, herdr_tab=info(pane)['tab_id'], herdr_socket=str(proxy_socket),
                          herdr_socket_identity=f'{inode.st_dev}:{inode.st_ino}', run_token=f'fixture-{index}', process_group=process)
            if mode == 'magpie':
                record.update(instance_id='lab', magpie_model='relay/test-model')
            records.append(record)
        ended = dict(version=3, id='3' * 32, name='Stopped fixture', working_directory=str(project), cli='codex',
                     mode='local', recovery='needs_review', started=True, created_at='2026-11-19T00:00:00Z',
                     last_exit_at='2026-11-20T00:00:00Z')
        session_file = harbor / 'sessions.json'
        session_file.write_text(json.dumps(records + [ended]))
        session_file.chmod(0o600)
        def managed_runs():
            views = json.loads(run(binary, 'sessions', '--json').strip())
            return all(any(view['id'] == record['id'] and view['runtime'].get('run_verified') for view in views)
                       for record in records)
        env['HERDR_SOCKET_PATH'] = str(proxy_socket)
        try:
            try:
                until(managed_runs, 'verified fixture runs')
            except RuntimeError:
                views = json.loads(run(binary, 'sessions', '--json'))
                raise RuntimeError([(view['name'], view.get('external'), view['runtime']['presence'],
                                     view['runtime'].get('run_verified'), bool(view.get('discovery_warning')))
                                    for view in views]) from None
        finally:
            env['HERDR_SOCKET_PATH'] = str(source_socket)
        external = tab()
        send(external, shlex.join([str(args.codex), '--no-daemon']))
        rpc('tab.focus', tab_id=info(external)['tab_id'])
        ready_since = None

        def composer_ready():
            nonlocal ready_since
            frame = capture()
            if 'Use existing model' in frame:
                rpc('pane.send_keys', pane_id=external, keys=['Down'])
                time.sleep(.2)
                rpc('pane.send_keys', pane_id=external, keys=['Enter'])
                time.sleep(.5)
            if 'Ask Codex' not in frame or 'Use existing model' in frame:
                ready_since = None
                return False
            ready_since = ready_since or time.monotonic()
            return time.monotonic()-ready_since > 1

        until(composer_ready, 'stable Codex composer')
        rpc('pane.send_text', pane_id=external, text='EXTERNAL-DRAFT-FIXTURE')
        until(lambda: 'EXTERNAL-DRAFT-FIXTURE' in capture(), 'external draft')
        until(lambda: 'EXTERNAL-DRAFT-FIXTURE' in visible(external), 'draft in Herdr snapshot')
        rpc('tab.focus', tab_id=info(prior)['tab_id'])
        report(external, 'idle')
        reads.clear()
        key('C-b', 'a')
        until(lambda: 'NATIVE-FRAME' in capture(), 'managed Native preview')
        wide = until(size, 'popup dimensions')
        assert reads and all(pane == panes[0] for _, pane in reads), reads
        reads.clear()
        begin = time.monotonic()
        icons, words, samples = set(), set(), set()
        while time.monotonic() - begin < 5:
            display = capture(True)
            for line in display.splitlines():
                plain = re.sub(r'\x1b\[[0-9;:]*m', '', line)
                if '● Working' in plain:
                    indicator = line.rsplit('●', 1)
                    color = tuple(re.findall(r'\x1b\[[0-9;:]*m', indicator[0]))
                    word = indicator[1].split('│', 1)[0].rstrip()
                    if (len(icons) < 4 and color not in icons) or (len(words) < 6 and word not in words):
                        samples.add((color, word))
                        save(f'working-{len(samples):02}', 160, display)
                    icons.add(color)
                    words.add(word)
                    break
            time.sleep(.09)
        assert len(icons) >= 4 and len(words) >= 3, (len(icons), len(words))
        assert 2 <= len(reads) <= 3 and all(pane == panes[0] for _, pane in reads), reads
        assert all(later[0] - earlier[0] >= 1.9 for earlier, later in zip(reads, reads[1:])), reads
        animation_reads = len(reads)
        key('j')
        until(lambda: 'MAGPIE-FRAME' in capture(), 'managed Magpie preview')
        assert 'NATIVE-FRAME' not in capture()
        save('magpie', 160)
        collapsed = until(lambda: (frame if 'fixture-context' in (frame := capture()) and
                                    'codex · Magpie · test-model' in frame else None), 'compact project, branch and model')
        assert 'lab/relay/' not in collapsed and 'Unbound' not in collapsed
        assert str(project) not in capture()
        save('context-collapsed', 160)
        key('o')
        expanded = until(lambda: (frame if '/preview-fixture' in (frame := capture()) and
                                   'codex · Magpie · lab/relay/test-model · Unbound' in frame else None), 'expanded route and Profile on one line')
        assert 'MAGPIE-FRAME' in expanded and expanded.count('fixture-context') == 1
        assert 'Route:' not in expanded and 'Profile:' not in expanded
        assert expanded.index('Conversation identity pending') > expanded.index('lab/relay/test-model · Unbound')
        save('context-expanded', 160)
        key('o')
        until(lambda: 'codex · Magpie · test-model' in capture(), 'context collapsed again')
        key('Up', 'Down', 'Up')
        until(lambda: 'NATIVE-FRAME' in capture(), 'rapid A/B/A ends on Native')
        assert 'MAGPIE-FRAME' not in capture()
        key('Down', 'Down')
        until(lambda: 'Terminal stopped' in capture(), 'stopped placeholder')
        save('stopped', 160)
        key('Down')
        until(lambda: 'External · connection unknown' in capture(), 'external selected')
        until(lambda: any(marker in capture() and marker in visible(external) for marker in
                          ('EXTERNAL-DRAFT-FIXTURE', 'Use existing model', 'Ask Codex')), 'external Codex screen')
        save('external', 160)
        key('Up', 'Up', 'Up')
        until(lambda: 'NATIVE-FRAME' in capture(), 'return to Native')
        state['offline'] = True
        until(lambda: 'Herdr connection unavailable' in capture(), 'disconnect placeholder')
        assert 'NATIVE-FRAME' not in capture()
        save('offline', 160)
        state['offline'] = False
        until(lambda: 'NATIVE-FRAME' in capture(), 'reconnected preview')
        assert rpc('pane.current')['pane']['pane_id'] == prior
        dimensions = [(160, 45, *wide)]
        for columns, rows in ((100, 30), (80, 24), (60, 24), (160, 20)):
            previous = size()
            tm('resize-window', '-t', 'preview:0', '-x', str(columns), '-y', str(rows))
            inner = until(lambda: (current if (current := size()) and current != previous else None), 'popup resize')
            time.sleep(.3)
            read_count = len(reads)
            time.sleep(2.35)
            assert len(reads) == read_count and 'NATIVE-FRAME' not in capture(), (columns, rows, inner, reads)
            save('short' if rows == 20 else 'narrow', columns)
            dimensions.append((columns, rows, *inner))
        print('PASS Herdr popup; outer cols/rows → inner cols/rows:', dimensions,
              '; Working icon / word styles:', len(icons), len(words),
              '; preview RPCs in five seconds:', animation_reads, '; narrow preview RPCs: 0', flush=True)
        (args.output / 'dimensions.json').write_text(json.dumps(dimensions))
    except Exception:
        save('failure', 160)
        raise
    finally:
        proxy.shutdown()
        proxy.server_close()
        tm('kill-server', check=False)
        if source_socket.exists():
            run(args.herdr, 'server', 'stop', check=False)
        if server.poll() is None:
            server.terminate()
        server.wait(timeout=10)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    for option in ('binary', 'herdr', 'codex', 'output'):
        parser.add_argument('--' + option, type=lambda value: Path(value).resolve(), required=True)
    args = parser.parse_args()
    args.output.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='ah-live-', dir='/tmp') as temporary:
        exercise(args, Path(temporary).resolve())
