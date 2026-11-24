"""Exercise public pane.read against a disposable Herdr server and native Codex."""

import argparse
import fcntl
import json
from pathlib import Path
import re
import shlex
import shutil
import socket
import struct
import subprocess
import sys
import tempfile
import termios
import time


def until(probe, label):
    deadline = time.monotonic() + 20
    while time.monotonic() < deadline:
        value = probe()
        if value:
            return value
        time.sleep(0.1)
    raise RuntimeError('timed out waiting for ' + label)


def exercise(root, herdr, codex, tmux, port):
    env = {'PATH': '/usr/bin:/bin:/usr/sbin:/sbin', 'SHELL': '/bin/sh',
           'TERM': 'xterm-256color', 'COLORTERM': 'truecolor', 'LANG': 'en_US.UTF-8',
           'GIT_ALLOW_PROTOCOL': 'file', 'GIT_TERMINAL_PROMPT': '0'}
    for key in ('HOME', 'XDG_CONFIG_HOME', 'XDG_DATA_HOME', 'XDG_STATE_HOME', 'XDG_CACHE_HOME',
                'XDG_RUNTIME_DIR', 'TMPDIR', 'CODEX_HOME', 'CODEX_SQLITE_HOME'):
        directory = root / key.lower()
        directory.mkdir(mode=0o700)
        env[key] = str(directory)
    project = root / 'preview-fixture'
    project.mkdir()
    config = root / 'herdr.toml'
    config.write_text('onboarding = false\n[update]\nversion_check = false\nmanifest_check = false\n')
    env['HERDR_CONFIG_PATH'] = str(config)
    (Path(env['CODEX_HOME']) / 'config.toml').write_text(
        'check_for_update_on_startup = false\ncli_auth_credentials_store = "file"\n'
        'model = "gpt-5.5"\nmodel_provider = "fixture"\n[model_providers.fixture]\n'
        f'name = "Fixture"\nbase_url = "http://127.0.0.1:{port}/v1"\nwire_api = "responses"\n'
        f'[projects.{json.dumps(str(project))}]\ntrust_level = "trusted"\n')

    def run(*argv, check=True):
        return subprocess.run(argv, env=env, cwd=project, capture_output=True, text=True,
                              timeout=10, check=check).stdout

    def tm(*args, check=True):
        return run(tmux, '-f', '/dev/null', '-S', root / 'tmux.sock', *args, check=check)

    api = root / 'herdr.sock'
    env['HERDR_SOCKET_PATH'] = str(api)

    def rpc(method, **params):
        with socket.socket(socket.AF_UNIX) as client:
            client.settimeout(2)
            client.connect(str(api))
            client.sendall(json.dumps(dict(id='probe', method=method, params=params)).encode() + b'\n')
            with client.makefile('rb') as stream:
                response = json.loads(stream.readline(2**22))
        if 'error' in response:
            raise ValueError(method, response['error']['code'])
        return response['result']

    def capture():
        return tm('capture-pane', '-p', '-t', 'probe:0.0')

    def emit(pane, command):
        rpc('pane.send_input', pane_id=pane, text=command, keys=['Enter'])

    def read(pane):
        result = rpc('pane.read', pane_id=pane, source='visible', format='ansi', strip_ansi=False)['read']
        info = rpc('pane.get', pane_id=pane)['pane']
        assert all(result[key] == info[key] for key in ('workspace_id', 'tab_id', 'pane_id'))
        assert (result['source'], result['format']) == ('visible', 'ansi')
        return result

    def content_revision(pane):
        return rpc('pane.copy_motion', pane_id=pane, motion='line_end', cursor={'row': 0, 'col': 0})['content_revision']

    def facts(pane):
        info = rpc('pane.get', pane_id=pane)['pane']
        process = rpc('pane.process_info', pane_id=pane)['process_info']
        terminal = (root / 'target-tty').read_text().strip()
        with open(terminal, 'rb', buffering=0) as tty:
            rows, cols, _, _ = struct.unpack('HHHH', fcntl.ioctl(tty, termios.TIOCGWINSZ, bytes(8)))
        foreground = run('ps', '-t', terminal.removeprefix('/dev/'), '-o', 'tpgid=').split()[0]
        agents = rpc('agent.list')['agents']
        agent = next((item for item in agents if item['pane_id'] == pane), {})
        processes = [item['pid'] for item in process.get('foreground_processes', []) if item['name'] == 'codex']
        focused = rpc('pane.current')['pane']['pane_id']
        return dict(focus=info['focused'], focused_pane=focused, terminal=info['terminal_id'], scroll=info['scroll'],
                    workspace=info['workspace_id'], tab=info['tab_id'], pane=info['pane_id'],
                    instance=api.stat().st_ino, client=agent.get('agent'), revision=info['revision'],
                    size=(cols, rows), pgrp=foreground, codex=processes,
                    activity=agent.get('agent_status'), conversation=agent.get('agent_session'))

    print(run(herdr, '--version').strip(), run(codex, '--version').strip(), run(tmux, '-V').strip(), flush=True)
    server = subprocess.Popen([herdr, 'server'], cwd=project, env=env,
                              stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
        until(api.exists, 'isolated API socket')
        tm('new-session', '-d', '-s', 'probe', '-x', '160', '-y', '45', str(herdr))
        until(lambda: server.poll() is None and capture().strip(), 'Herdr client')
        existing = rpc('pane.list')['panes']
        if not existing:
            rpc('workspace.create', cwd=str(project), focus=True)
        primary = until(lambda: rpc('pane.list')['panes'], 'shell pane')[0]['pane_id']
        tab = rpc('pane.get', pane_id=primary)['pane']['tab_id']
        rpc('pane.split', pane_id=primary, direction='right', focus=False, cwd=str(project))
        target = next(info['pane_id'] for info in rpc('pane.list')['panes'] if info['pane_id'] != primary)
        emit(primary, "printf '\\033[2J\\033[H\\033[31mSHELL-PRIMARY\\033[0m\\n'")
        emit(target, 'tty > ' + shlex.quote(str(root / 'target-tty')) + ' && cd ' + shlex.quote(str(project)) + ' && ' + shlex.quote(str(codex)) + ' --no-daemon')
        until(lambda: any(label in capture() for label in ('Ask Codex', 'Use existing model')), 'Codex startup')
        time.sleep(1)
        if 'Use existing model' in capture():
            rpc('pane.send_keys', pane_id=target, keys=['Down'])
            time.sleep(0.2)
            rpc('pane.send_keys', pane_id=target, keys=['Enter'])
        until(lambda: 'Ask Codex' in capture(), 'real Codex screen')
        time.sleep(1)
        rpc('pane.send_text', pane_id=target, text='UNSENT-PREVIEW-DRAFT')
        time.sleep(1)
        until(lambda: 'UNSENT-PREVIEW-DRAFT' in capture(), 'Codex composer draft')
        shell = read(primary)
        assert '\x1b[38;5;1mSHELL-PRIMARY' in shell['text'] and 'UNSENT-PREVIEW-DRAFT' not in shell['text']
        rpc('tab.create', cwd=str(project), focus=True)
        for seq, state in enumerate(('working', 'idle'), 1):
            rpc('pane.report_agent', pane_id=target, source='preview-probe', agent='codex', state=state, seq=seq)
        before = facts(target)
        assert not before['focus'] and before['activity'] == 'done', before
        screen = read(target)
        after = facts(target)
        draft = 'UNSENT-PREVIEW-DRAFT' in screen['text']
        sgr = re.findall(r'\x1b\[[0-9;:]*m', screen['text'])
        print('nonfocused Codex', 'draft', draft, 'styles', len(set(sgr)), 'size', before['size'],
              'activity', before['activity'], '->', after['activity'], 'stable',
              [key for key in before if before[key] == after[key]], flush=True)
        assert draft and 'SHELL-PRIMARY' not in screen['text']
        assert before == after and before['codex'] and any(re.search(r'(?:3[0-7]|38|9[0-7])', style) for style in sgr)
        print('result.read keys', sorted(screen), 'revision', screen['revision'], 'truncated', screen['truncated'])
        rpc('tab.focus', tab_id=tab)
        until(lambda: 'UNSENT-PREVIEW-DRAFT' in capture(), 'draft after returning')
        content_before = content_revision(target)
        baseline = facts(target)
        assert int(baseline['pgrp']) in baseline['codex'] and not baseline['focus']
        begin = time.monotonic()
        rpc('pane.send_text', pane_id=target, text='\x1a')
        until(lambda: facts(target)['pgrp'] != baseline['pgrp'], 'shell foreground after suspending Codex')
        other = "printf '\\033[2J\\033[H\\033[35m%s-%s\\033[0m\\n' OTHER FOREGROUND; read answer"
        emit(target, shlex.join(['/bin/sh', '-c', other]) + '; fg')
        until(lambda: 'OTHER-FOREGROUND' in capture(), 'other foreground screen')
        middle_process = facts(target)['pgrp']
        assert middle_process != baseline['pgrp']
        middle = read(target)
        assert 'OTHER-FOREGROUND' in middle['text'] and 'UNSENT-PREVIEW-DRAFT' not in middle['text']
        rpc('pane.send_keys', pane_id=target, keys=['Enter'])
        until(lambda: facts(target)['pgrp'] == baseline['pgrp'], 'original foreground process')
        end = until(lambda: (state if (state := facts(target)) == baseline else None), 'identical A observations after returning')
        print('same-pane A/B/A', 'middle marker OTHER-FOREGROUND', 'read revision', middle['revision'],
              'elapsed ms', round((time.monotonic() - begin) * 1000),
              'changed fields', [key for key in baseline if baseline[key] != end[key]], flush=True)
        content_after = content_revision(target)
        print('copy_motion content_revision', content_before, '->', content_after, flush=True)
        until(lambda: 'UNSENT-PREVIEW-DRAFT' in capture(), 'original Codex composer after fg')
        emit(primary, '''awk 'BEGIN {for (row=1;row<=180;row++) printf "ROW-%03d\\n",row}' ''')
        until(lambda: 'ROW-180' in capture(), 'numbered scrollback')
        rpc('pane.scroll', pane_id=primary, offset_from_bottom=50)
        def viewport():
            return re.findall(r'ROW-\d{3}', capture())
        visible = until(lambda: (rows if (rows := viewport()) and 'ROW-180' not in rows else None), 'scrolled viewport')
        prior_scroll = rpc('pane.get', pane_id=primary)['pane']['scroll']
        sampled = re.findall(r'ROW-\d{3}', read(primary)['text'])
        post_scroll = rpc('pane.get', pane_id=primary)['pane']['scroll']
        print('scrolled shell', visible[0], visible[-1], 'captured', sampled[0], sampled[-1],
              'offset before/after', prior_scroll['offset_from_bottom'], post_scroll['offset_from_bottom'])
        assert prior_scroll['offset_from_bottom'] > 0 and prior_scroll == post_scroll and visible == sampled == viewport()
        rpc('pane.scroll', pane_id=primary, offset_from_bottom=0)
        emit(primary, "printf '\\033[2J\\033[H'; sleep 1")
        strip_styles = re.compile(r'\x1b\[[0-9;:]*m')
        until(lambda: not strip_styles.sub('', read(primary)['text']).strip(), 'empty viewport')
        time.sleep(1.1)
        sample = project / 'alternate.txt'
        sample.write_text('FULL-SCREEN-FIXTURE\n')
        emit(primary, shlex.join(['vi', '-Nu', 'NONE', '-n', '-i', 'NONE', '-R',
                                  '-c', 'syntax match Fixture /FULL-SCREEN-FIXTURE/',
                                  '-c', 'highlight Fixture ctermfg=Red', str(sample)]) + "; printf '\\033[2J\\033[H%s-%s\\n' BACK SHELL")
        until(lambda: re.search(r'\x1b\[[\d;]*mFULL-SCREEN-FIXTURE', read(primary)['text']), 'styled vi viewport')
        rpc('pane.send_text', pane_id=primary, text='\x1b:q!\r')
        until(lambda: 'BACK-SHELL' in capture(), 'return from vi')
        paint = root / 'paint.py'
        paint.write_text('import os, time\nwidth, height = os.get_terminal_size()\n'
                         'for row in range(height):\n'
                         '    print(f"\\033[{row + 1};1H" + "".join(f"\\033[38;2;{col};{row};123;48;2;{row};{col};234mX" for col in range(width)), end="")\n'
                         'print("\\033[0m", end="", flush=True)\ntime.sleep(2)\n')
        emit(primary, shlex.join([sys.executable, str(paint)]))
        large = until(lambda: (data if len((data := read(primary))['text']) > 65536 else None), 'oversize viewport')
        print('oversize ANSI bytes', len(large['text'].encode()), 'truncated', large['truncated'])
        rpc('pane.close', pane_id=primary)
        try:
            read(primary)
        except ValueError as error:
            assert error.args == ('pane.read', 'pane_not_found')
        else:
            raise AssertionError('closed pane still readable')
        print('PASS: visible ANSI reads show the selected pane, including its current foreground screen.')
        return 0
    finally:
        tm('kill-server', check=False)
        if api.exists():
            run(herdr, 'server', 'stop', check=False)
        if server.poll() is None:
            server.terminate()
        server.wait(timeout=10)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--herdr', type=Path, required=True)
    parser.add_argument('--codex', type=Path, required=True)
    args = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix='ah-read-', dir='/tmp') as temporary, socket.socket() as reserved:
        reserved.bind(('127.0.0.1', 0))
        raise SystemExit(exercise(Path(temporary).resolve(), args.herdr.resolve(), args.codex.resolve(),
                                  shutil.which('tmux'), reserved.getsockname()[1]))
