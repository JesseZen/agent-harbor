#!/usr/bin/env python3
"""Native multi-machine UI, real SSH, native Codex and disconnect acceptance.

Run on a Linux test client against a separate Linux test server. Both roots are
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
    for key in ('host', 'binary', 'herdr', 'codex', 'known-hosts'):
        parser.add_argument('--' + key, required=True)
    parser.add_argument('--remote-root', required=True, help='new exclusive absolute directory')
    args = parser.parse_args()
    assert pathlib.Path(args.codex).open('rb').read(4) == b'\x7fELF', 'native Linux Codex executable required'
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
        with pathlib.Path(source).open('rb') as stream:
            command = 'umask 077; cat > ' + shlex.quote(str(target)) + '; chmod 700 ' + shlex.quote(str(target))
            subprocess.run(ssh_base + [command], stdin=stream, check=True, timeout=180,
                           stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)

    here = pathlib.Path(__file__).resolve().parent
    for source, leaf in [(args.binary, 'bin/agent-harbor'), (args.herdr, 'bin/herdr'),
                         (args.codex, 'bin/codex'), (here / 'e2e_local.py', 'e2e_local.py'),
                         (here / 'fixture_process.py', 'fixture_process.py'),
                         (here / 'remote_server_fixture.py', 'remote_server_fixture.py')]:
        upload(source, remote / leaf)

    def server(mode):
        try:
            result = ssh('python3', remote / 'remote_server_fixture.py', mode, text=True)
        except subprocess.CalledProcessError as error:
            raise RuntimeError('server ' + mode + ': ' + error.stderr) from error
        return json.loads(result.stdout) if mode in ('setup', 'snapshot') else result.stdout

    env = {'PATH': '/usr/bin:/bin:/usr/sbin:/sbin', 'TERM': 'xterm-256color',
           'SHELL': '/bin/sh', 'LANG': 'C.UTF-8', 'NO_COLOR': '1'}
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
        tm('send-keys', '-t', 'native:0.0', *value)
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
        key('n')
        contains('New session')
        key('Enter')
        wait(lambda: 'Ask Codex' in screen() or 'Hooks need review' in screen(), 'remote Codex startup')
        if 'Hooks need review' in screen():
            key('Down', 'Enter')
        contains('Ask Codex')
        records = wait(lambda: server('snapshot')['records'], 'remote session record')
        record = records[0]
        assert record['working_directory'] == str(remote / 'project'), record
        pid = record['process_group']
        assert pid > 1
        local_records = root / 'harbor/sessions.json'
        assert not local_records.exists() or json.loads(local_records.read_text()) == []
        text('AH_REMOTE_OVERNIGHT')
        key('Enter')
        wait(lambda: server('snapshot')['request_started'], 'remote request start')
        tm('kill-session', '-t', 'native')
        wait(lambda: server('snapshot')['request_completed'], 'request completed without client')
        after = server('snapshot')['records'][0]
        assert after['process_group'] == pid, (record, after)
        assert after.get('codex_session_id')
        reference = after.get('conversation', {}).get('reference')
        open_client()
        choose_remote()
        output = run([wrapper, '--machine', profile_id, 'agent', 'read', after['herdr_pane']]).stdout
        (root / 'remote-after-reconnect.json').write_text(output)
        assert 'HELLO_FROM_ISOLATED_MOCK' in output, output
        again = server('snapshot')['records'][0]
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
