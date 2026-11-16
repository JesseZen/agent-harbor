#!/usr/bin/env python3
"""Native multi-machine UI, real SSH, native Codex and disconnect acceptance.

Run on a Linux or macOS client against a separate Linux server. Both roots are
exclusive fixture resources. No real credentials, conversations or gateways are used.
"""
import argparse
import json
import os
import pathlib
import shlex
import shutil
import subprocess
import tempfile
import time
import unicodedata


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--color', action='store_true', help='keep theme colors on both hosts for visual acceptance')
    for key in ('host', 'binary', 'herdr', 'codex', 'known-hosts'):
        parser.add_argument('--' + key, required=True)
    for key in ('binary', 'herdr', 'codex'):
        parser.add_argument('--remote-' + key, help='Linux server executable; defaults to client executable')
    parser.add_argument('--remote-root', required=True, help='new exclusive absolute directory')
    args = parser.parse_args()
    assert pathlib.Path(args.codex).open('rb').read(4) in (b'\x7fELF', b'\xcf\xfa\xed\xfe'), 'native client Codex required'
    remote_codex = args.remote_codex or args.codex
    assert pathlib.Path(remote_codex).open('rb').read(4) == b'\x7fELF', 'native Linux server Codex required'
    remote = pathlib.PurePosixPath(args.remote_root)
    assert remote.is_absolute() and remote.name.startswith('harbor-remote-accept-')
    root = pathlib.Path(tempfile.mkdtemp(prefix='harbor-remote-client-', dir='/tmp')).resolve()
    root.chmod(0o700)
    print('client diagnostics', root, flush=True)
    print('server diagnostics', remote, flush=True)
    ssh_base = ['/usr/bin/ssh', '-T', '-o', 'BatchMode=yes', '-o', 'StrictHostKeyChecking=yes',
                '-o', 'UserKnownHostsFile=' + str(pathlib.Path(args.known_hosts).resolve()),
                '-o', 'ConnectTimeout=10', '--', args.host]

    def ssh(*command, **kwargs):
        return subprocess.run(ssh_base + [' '.join(map(shlex.quote, map(str, command)))],
                              capture_output=True, check=True, timeout=90, **kwargs)

    ssh('python3', '-c', 'import os,sys; os.mkdir(sys.argv[1],0o700); os.mkdir(sys.argv[1]+"/bin",0o700)', remote)

    def upload(source, target):
        # Native clients can exceed 250 MB. Stream compression avoids a second
        # artifact on the client and keeps cloud SSH gateways within the timeout.
        command = 'umask 077; gzip -dc > ' + shlex.quote(str(target)) + ' && chmod 700 ' + shlex.quote(str(target))
        compressor = subprocess.Popen(['gzip', '-c', str(source)], stdout=subprocess.PIPE)
        try:
            subprocess.run(ssh_base + [command], stdin=compressor.stdout, check=True, timeout=600,
                           stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
            assert compressor.wait(timeout=10) == 0, 'artifact compression failed'
        finally:
            compressor.stdout.close()
            if compressor.poll() is None:
                compressor.kill()
                compressor.wait()

    here = pathlib.Path(__file__).resolve().parent
    for source, leaf in [(args.remote_binary or args.binary, 'bin/agent-harbor'),
                         (args.remote_herdr or args.herdr, 'bin/herdr'),
                         (remote_codex, 'bin/codex'), (here / 'e2e_local.py', 'e2e_local.py'),
                         (here / 'fixture_process.py', 'fixture_process.py'),
                         (here / 'remote_server_fixture.py', 'remote_server_fixture.py')]:
        upload(source, remote / leaf)

    def server(mode):
        try:
            color = ['--color'] if args.color and mode == 'setup' else []
            result = ssh('python3', remote / 'remote_server_fixture.py', mode, *color, text=True)
        except subprocess.CalledProcessError as error:
            raise RuntimeError('server ' + mode + ': ' + error.stderr) from error
        return json.loads(result.stdout) if mode in ('setup', 'snapshot') else result.stdout

    env = {'PATH': '/usr/bin:/bin:/usr/sbin:/sbin', 'TERM': 'xterm-256color',
           'SHELL': '/bin/sh', 'LANG': 'en_US.UTF-8', 'NO_COLOR': '1'}
    if args.color:
        env.pop('NO_COLOR')
    for key, leaf in [('HOME', 'home'), ('XDG_CONFIG_HOME', 'config'), ('XDG_STATE_HOME', 'state'),
                      ('XDG_DATA_HOME', 'data'), ('XDG_CACHE_HOME', 'cache'),
                      ('XDG_RUNTIME_DIR', 'runtime'), ('TMPDIR', 'tmp'),
                      ('CODEX_HOME', 'codex'), ('CODEX_SQLITE_HOME', 'sqlite')]:
        (root / leaf).mkdir(mode=0o700)
        env[key] = str(root / leaf)
    (root / 'bin').mkdir(mode=0o700)
    for source, name in [(args.binary, 'agent-harbor'), (args.herdr, 'herdr'), (args.codex, 'codex')]:
        shutil.copy2(source, root / 'bin' / name)
    env['PATH'] = str(root / 'bin') + ':' + env['PATH']
    env['AGENT_HARBOR_BRIDGE_HOME'] = str(root / 'harbor')
    (root / 'harbor').mkdir(mode=0o700)
    (root / 'codex/config.toml').write_text('check_for_update_on_startup = false\n')
    # Only public host keys are copied. Authentication remains native OpenSSH.
    keys = root / 'known_hosts'
    shutil.copy2(args.known_hosts, keys)
    ssh_shim = root / 'bin/ssh'
    ssh_shim.write_text('#!/bin/sh\nexec /usr/bin/ssh -o ' +
                        shlex.quote('UserKnownHostsFile=' + str(keys)) + ' "$@"\n')
    ssh_shim.chmod(0o700)
    tmux = shutil.which('tmux')
    sock = str(root / 'tmux.sock')
    binary = root / 'bin/agent-harbor'
    wrapper = root / 'harbor/herdr/bin/herdr'

    def run(command, check=True):
        result = subprocess.run(list(map(str, command)), env=env, capture_output=True,
                                text=True, timeout=90)
        if check and result.returncode:
            (root / 'last-command-error.txt').write_text(result.stdout + result.stderr)
            raise RuntimeError(result.stdout + result.stderr)
        return result

    def tm(*command):
        return run([tmux, '-S', sock, *command]).stdout

    def screen():
        return tm('capture-pane', '-p', '-t', 'native:0.0')

    def wait(predicate, label, timeout=50):
        end = time.monotonic() + timeout
        while time.monotonic() < end:
            value = predicate()
            if value:
                return value
            time.sleep(.2)
        (root / 'failure-screen.txt').write_text(screen())
        raise RuntimeError(label + '\n' + screen())

    def text(value):
        tm('send-keys', '-t', 'native:0.0', '-l', value)

    def key(*value):
        for item in value:
            tm('send-keys', '-t', 'native:0.0', item)
            time.sleep(.3)

    def contains(value):
        return wait(lambda: value in screen(), value)

    def open_client():
        tm('new-session', '-d', '-s', 'native', '-x', '160', '-y', '45', str(binary))
        contains('RemoteBuild')

    def choose_remote():
        output = screen()
        line_number, line = next((i, row) for i, row in enumerate(output.splitlines()) if 'RemoteBuild' in row)
        prefix = line[:line.index('RemoteBuild')]
        col = sum(0 if unicodedata.combining(c) else 2 if unicodedata.east_asian_width(c) in ('W', 'F') else 1 for c in prefix) + 3
        text(f'\x1b[<0;{col};{line_number+1}M\x1b[<0;{col};{line_number+1}m')
        time.sleep(1)

    try:
        run([binary, 'sessions', '--json'], check=False)
        (root / 'harbor/onboarding.done').write_text('v1\n')
        descriptor = server('setup')
        result = run([binary, 'remote', 'add', 'RemoteBuild', '--ssh', args.host,
                      '--binary', remote / 'remote-harbor', '--state', descriptor['state'], '--native'])
        (root / 'machine-add.txt').write_text(result.stdout)
        machines = json.loads(run([binary, 'remote', 'list', '--json']).stdout)
        profile = next(p for p in machines if p['label'] == 'RemoteBuild')
        assert profile['remote_executable'] == descriptor['executable']
        profile_id = profile['id']
        open_client()
        choose_remote()
        key('C-b', 'a')
        contains('HARBOR')
        (root / 'remote-popup.txt').write_text(screen())
        (root / 'remote-popup-160.ansi').write_text(tm('capture-pane', '-e', '-p', '-t', 'native:0.0'))
        key('n')
        contains('New session')
        key('Enter')
        wait(lambda: 'Ask Codex' in screen() or 'Hooks need review' in screen(), 'remote Codex startup')
        # Codex can briefly render its prompt before showing first-run hook trust.
        time.sleep(2)
        if 'Hooks need review' in screen():
            key('2', 'Enter')
        contains('Ask Codex')
        time.sleep(1)  # The first prompt can render while Codex is finishing startup.
        if 'hook needs' in screen():
            key('t', 'Escape')
        records = wait(lambda: server('snapshot')['records'], 'remote session record')
        record = records[0]
        assert record['working_directory'] == str(remote / 'project'), record
        pid = record['process_group']
        assert pid > 1
        original_process = server('snapshot')['processes'][str(pid)]
        assert original_process['group'] == pid
        local_records = root / 'harbor/sessions.json'
        assert not local_records.exists() or json.loads(local_records.read_text()) == []
        text('AH_REMOTE_OVERNIGHT')
        contains('AH_REMOTE_OVERNIGHT')
        time.sleep(.5)  # Separate pasted text from the submit key across SSH.
        key('Enter')
        time.sleep(1)
        if 't trust all' in screen():
            key('t', 'Escape')
            text('AH_REMOTE_OVERNIGHT')
            time.sleep(.5)
            key('Enter')
        wait(lambda: server('snapshot')['request_started'], 'remote request start')
        tm('kill-session', '-t', 'native')
        wait(lambda: server('snapshot')['request_completed'], 'request completed without client')
        completed = server('snapshot')
        after = completed['records'][0]
        assert completed['processes'][str(pid)] == original_process
        assert after['process_group'] == pid, (record, after)
        after = wait(lambda: next((row for row in server('snapshot')['records']
                                  if row.get('codex_session_id') and row.get('conversation', {}).get('reference')), None),
                     'official remote conversation reference')
        reference = after.get('conversation', {}).get('reference')
        assert reference, after
        open_client()
        choose_remote()
        contains('HELLO_FROM_ISOLATED_MOCK')
        (root / 'remote-after-reconnect.txt').write_text(screen())
        reconnected = server('snapshot')
        again = reconnected['records'][0]
        assert reconnected['processes'][str(pid)] == original_process
        assert again['process_group'] == pid and again.get('conversation', {}).get('reference') == reference
        # Keep the old saved path and prove that a changed workspace cannot be targeted.
        server('replace-identity')
        failed = run([binary, 'remote', 'status', profile_id, '--json'], check=False)
        assert failed.returncode != 0 and 'reachable' not in failed.stdout
        (root / 'replacement-rejected.txt').write_text(failed.stdout + failed.stderr)
        server('restore-identity')
        summary = {'native_remote_popup': True, 'real_native_codex': True,
                   'same_process_after_disconnect': True, 'same_conversation_after_reconnect': True,
                   'workspace_replacement_rejected': True, 'local_sessions_untouched': True,
                   'physical_mac_sleep_tested': False}
        (root / 'results.json').write_text(json.dumps(summary, indent=2))
        print('PASS', json.dumps(summary), flush=True)
    finally:
        run([tmux, '-S', sock, 'kill-server'], check=False)
        run([binary, 'stop'], check=False)
        server('stop')


if __name__ == '__main__':
    main()
