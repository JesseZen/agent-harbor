"""Isolated real Herdr/tmux acceptance for Sessions Pin, Archive, and Group Folding."""

import argparse
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import socket
import subprocess
import sys
import tempfile
import time

from pane_read_probe import until

ANSI_RE = re.compile(r'\x1b\[[0-9;:?]*[a-zA-Z]')


def plain_lines(text):
    return [ANSI_RE.sub('', line) for line in text.splitlines()]


def check_column_alignment(frame_text, label, expanded):
    lines = plain_lines(frame_text)
    normal_col = None
    toggle_col = None
    expanded_col = None
    for line in lines:
        idx = line.find('Stopped 001')
        if idx >= 0 and normal_col is None:
            normal_col = idx
        if not expanded:
            t_idx = line.find('▸ Show ')
            if t_idx >= 0 and toggle_col is None:
                toggle_col = t_idx + len('▸ ')
        else:
            t_idx = line.find('▾ Show less')
            if t_idx >= 0 and toggle_col is None:
                toggle_col = t_idx + len('▾ ')
            e_idx = line.find('Stopped 006')
            if e_idx >= 0 and expanded_col is None:
                expanded_col = e_idx
    assert normal_col is not None, f'{label}: missing Stopped 001:\n{frame_text}'
    assert toggle_col is not None, f'{label}: missing toggle row:\n{frame_text}'
    assert toggle_col == normal_col, f'{label}: toggle col {toggle_col} != normal col {normal_col}\n{frame_text}'
    if expanded and expanded_col is not None:
        assert expanded_col == normal_col, f'{label}: expanded col {expanded_col} != normal col {normal_col}\n{frame_text}'


def exercise(args, root):
    root = root.resolve()
    tmux_path = shutil.which('tmux') or '/opt/homebrew/bin/tmux'
    env = {
        'PATH': os.pathsep.join(dict.fromkeys([str(Path(tmux_path).parent), '/usr/bin', '/bin', '/usr/sbin', '/sbin'])),
        'SHELL': '/bin/sh',
        'TERM': 'xterm-256color',
        'COLORTERM': 'truecolor',
        'LANG': 'en_US.UTF-8',
        'PS1': 'fixture$ ',
        'GIT_ALLOW_PROTOCOL': 'file',
        'GIT_TERMINAL_PROMPT': '0',
    }
    for key in ('HOME', 'XDG_CONFIG_HOME', 'XDG_DATA_HOME', 'XDG_STATE_HOME',
                'XDG_CACHE_HOME', 'XDG_RUNTIME_DIR', 'TMPDIR', 'CODEX_HOME', 'CODEX_SQLITE_HOME'):
        directory = root / key.lower()
        directory.mkdir(mode=0o700)
        env[key] = str(directory)
    harbor = root / 'h'
    harbor.mkdir(mode=0o700)
    ownership = harbor / '.agent-harbor-bridge'
    ownership.write_text('agent-harbor-bridge-state-v1\n')
    ownership.chmod(0o600)
    (harbor / 'herdr').mkdir(mode=0o700)
    herdr_socket = harbor / 'herdr/herdr.sock'
    env.update(
        AGENT_HARBOR_BRIDGE_HOME=str(harbor),
        HERDR_SOCKET_PATH=str(herdr_socket),
        HERDR_CONFIG_PATH=str(root / 'herdr.toml'),
    )
    project = root / 'organize-project'
    project.mkdir()

    # Synthetic client history file that Remove MUST NOT touch.
    history_file = Path(env['CODEX_HOME']) / 'sessions' / 'rollout-keep.jsonl'
    history_file.parent.mkdir(parents=True, mode=0o700)
    history_payload = '{"type":"message","content":"client-history-untouched"}\n'
    history_file.write_text(history_payload)

    binary = root / 'agent-harbor'
    shutil.copy2(args.binary, binary)
    command = shlex.join(['env', 'HERDR_SOCKET_PATH=' + str(herdr_socket), str(binary), '_app'])
    Path(env['HERDR_CONFIG_PATH']).write_text(
        'onboarding = false\n[update]\nversion_check = false\nmanifest_check = false\n'
        '[[keys.command]]\nkey = "prefix+a"\ntype = "popup"\n'
        f'command = {json.dumps(command)}\ndescription = "Organize fixture"\nwidth = "80%"\nheight = "80%"\n'
    )

    def run(*argv, check=True):
        result = subprocess.run(argv, env=env, cwd=project, capture_output=True, text=True, timeout=15)
        if check and result.returncode:
            raise RuntimeError(f'{Path(argv[0]).name} failed: {result.stderr or result.stdout}')
        return result.stdout

    def tm(*values, check=True):
        return run(tmux_path, '-f', '/dev/null', '-S', root / 'tmux.sock', *values, check=check)

    def capture(color=False):
        return tm('capture-pane', '-pe' if color else '-p', '-t', 'org:0.0')

    def key(*keys):
        tm('send-keys', '-t', 'org:0.0', *keys)

    def press(k, count=1):
        for _ in range(count):
            key(k)
            time.sleep(0.05)

    def step_until(k, predicate, label, max_steps=40):
        for _ in range(max_steps):
            before = capture()
            if predicate():
                return
            key(k)
            deadline = time.monotonic() + 1.5
            while time.monotonic() < deadline:
                if capture() != before:
                    break
                time.sleep(0.03)
        raise RuntimeError(f'failed stepping {k} for {label}\n--- SCREEN ---\n{capture()}')

    def wait_for(predicate, label, timeout=10):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            if predicate():
                return
            time.sleep(0.05)
        raise RuntimeError(f'timed out waiting for {label}\n--- SCREEN ---\n{capture()}')

    def rpc(method, **params):
        with socket.socket(socket.AF_UNIX) as client:
            client.settimeout(3)
            client.connect(str(herdr_socket))
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

    def report(pane, activity):
        rpc('pane.report_agent', pane_id=pane, source='org-fixture', agent='codex', state=activity)

    def save(label, columns):
        if not args.output:
            return
        public = capture(True)
        public = public.replace(str(root), '/fixture/isolated'.ljust(len(str(root)), 'x'))
        for private in (socket.gethostname().split('.')[0], os.environ.get('USER', '')):
            if private:
                public = public.replace(private, 'x' * len(private))
        (args.output / f'{label}-{columns}.ansi').write_text(public)

    server = subprocess.Popen([args.herdr, 'server'], env=env, cwd=project,
                              stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
        until(herdr_socket.exists, 'isolated Herdr API')
        tm('new-session', '-d', '-s', 'org', '-x', '160', '-y', '45', str(args.herdr))
        prior = until(lambda: rpc('pane.list')['panes'], 'initial pane')[0]['pane_id']
        send(prior, "printf '\\033[2J\\033[H'; exec /bin/sleep 240")

        # Create Harbor's real Default Profile so records have valid Profile bindings.
        prof = json.loads(run(binary, 'profiles', 'new', 'Default').strip())
        codex_client = next(c for c in prof['clients'] if c['client'] == 'codex')
        real_binding = dict(
            workspace_id=prof['workspace_id'],
            profile_id=prof['id'],
            client='codex',
            environment_id=codex_client['id'],
        )
        profile_codex_home = codex_client['home']
        profile_sqlite_home = codex_client.get('sqlite_home', '')

        inode = herdr_socket.stat()
        sock_id = f'{inode.st_dev}:{inode.st_ino}'
        records = []
        active_panes = {}
        active_pgids = {}

        # Create 9 live Active tabs: 1 Working + 8 Idle (so 3 Idle fold behind "Show 3 more").
        active_specs = [('Active Work', 'working', False)] + [
            (f'Active Idle {i:02d}', 'idle', False) for i in range(1, 9)
        ]
        for idx, (name, activity, pinned) in enumerate(active_specs, 1):
            pane = tab()
            token = f'org-active-{idx}'
            env_vars = (
                f'AH_SESSION_RUN_TOKEN={token} '
                f'AH_PROFILE_WORKSPACE={shlex.quote(real_binding["workspace_id"])} '
                f'AH_PROFILE_ID={shlex.quote(real_binding["profile_id"])} '
                f'AH_PROFILE_ENVIRONMENT={shlex.quote(real_binding["environment_id"])} '
                f'CODEX_HOME={shlex.quote(profile_codex_home)}'
            )
            if profile_sqlite_home:
                env_vars += f' CODEX_SQLITE_HOME={shlex.quote(profile_sqlite_home)}'
            cmd_str = (
                f"printf '\\033[2J\\033[H  {name}\\n'; "
                f"exec /usr/bin/env {env_vars} {shlex.quote(sys.executable)} -c 'import time; time.sleep(240)'"
            )
            send(pane, cmd_str)
            pgid = until(
                lambda p=pane: rpc('pane.process_info', pane_id=p)['process_info'].get('foreground_process_group_id'),
                f'pgid for {name}',
            )
            # Wait until python process with token is foreground (pgid != shell).
            time.sleep(0.15)
            pgid = rpc('pane.process_info', pane_id=pane)['process_info'].get('foreground_process_group_id')
            report(pane, activity)
            rec_id = f'{idx:032x}'
            conv_uuid = f'00000000-0000-7000-8000-{idx:012x}'
            active_panes[name] = pane
            active_pgids[name] = pgid
            records.append(dict(
                version=3,
                id=rec_id,
                name=name,
                working_directory=str(project),
                cli='codex',
                mode='local',
                profile=real_binding,
                recovery='ready',
                started=True,
                pinned=pinned,
                created_at=f'2026-11-24T12:{60 - idx:02d}:00Z',
                herdr_pane=pane,
                herdr_tab=info(pane)['tab_id'],
                herdr_socket=str(herdr_socket),
                herdr_socket_identity=sock_id,
                run_token=token,
                codex_home=profile_codex_home,
                process_group=pgid,
                codex_session_id=conv_uuid,
                conversation=dict(
                    reference=dict(source='herdr:codex', agent='codex', kind='id',
                                   value=conv_uuid),
                    status='observed',
                    provenance='herdr-pane-observation',
                    observed_at='2026-11-24T12:00:00Z',
                    pane=pane,
                    socket_identity=sock_id,
                    run_token=token,
                ),
            ))

        # Add 100 Stopped sessions:
        # #0: Pinned Magpie session with Profile & Conversation (for Archive/Unarchive preservation check)
        # #1..#99: Ordinary historical_exit sessions (94 folded behind "Show 94 more")
        target_id = 'a' * 32
        target_conv = dict(
            reference=dict(source='herdr:codex', agent='codex', kind='id',
                           value='12345678-1234-7123-8123-123456789abc'),
            status='observed',
            provenance='herdr-pane-observation',
            observed_at='2026-11-24T00:00:00Z',
            pane='p-target',
            socket_identity=sock_id,
            run_token='tok-target',
        )
        target_profile = real_binding
        records.append(dict(
            version=3,
            id=target_id,
            name='Keep Magpie Pin',
            working_directory=str(project),
            cli='codex',
            mode='magpie',
            instance_id='lab',
            magpie_url='http://127.0.0.1:4321',
            magpie_model='relay/gpt-5',
            profile=target_profile,
            conversation=target_conv,
            codex_session_id='12345678-1234-7123-8123-123456789abc',
            recovery='ready',
            started=True,
            pinned=True,
            created_at='2026-11-23T23:59:00Z',
            last_exit_at='2026-11-24T00:00:00Z',
        ))
        for i in range(1, 100):
            conv_uuid = f'11111111-1111-7111-8111-{i:012x}'
            records.append(dict(
                version=3,
                id=f'b{i:031x}',
                name=f'Stopped {i:03d}',
                working_directory=str(project),
                cli='codex',
                mode='local',
                profile=real_binding,
                recovery='ready',
                started=True,
                created_at=f'2026-11-20T{23 - (i // 60):02d}:{59 - (i % 60):02d}:00Z',
                last_exit_at='2026-11-21T00:00:00Z',
                codex_session_id=conv_uuid,
                conversation=dict(
                    reference=dict(source='herdr:codex', agent='codex', kind='id',
                                   value=conv_uuid),
                    status='observed',
                    provenance='herdr-pane-observation',
                    observed_at='2026-11-24T12:00:00Z',
                    pane=pane,
                    socket_identity=sock_id,
                    run_token=token,
                ),
            ))

        session_file = harbor / 'sessions.json'
        session_file.write_text(json.dumps(records, indent=2))
        session_file.chmod(0o600)

        # Wait until Harbor verifies all 9 live runs via Herdr.
        def all_live_verified():
            views = json.loads(run(binary, 'sessions', '--json').strip())
            live = {v['name'] for v in views if v['runtime'].get('presence') == 'observed' and not v.get('external')}
            return len(live) == 9
        until(all_live_verified, '9 verified live runs')

        # Create 1 External agent tab (unmanaged).
        ext_pane = tab()
        send(ext_pane, "exec /bin/sleep 240")
        time.sleep(0.1)
        report(ext_pane, 'idle')

        # Open Harbor popup at 160x45 (wide split).
        key('C-b', 'a')
        wait_for(lambda: 'Show 3 more' in capture() and 'Show 94 more' in capture(), 'initial folded popup (110+ records)')
        save('01-wide-folded', 160)
        check_column_alignment(capture(), '160x45 collapsed', expanded=False)

        # 1. Pure keyboard crossing Active -> Show 3 more -> Stopped -> Show 94 more -> External.
        step_until('j', lambda: '> ▸ Show 3 more' in capture(), 'cursor on Active toggle', max_steps=10)

        # 2. Live resurfacing: folded Active Idle 08 starts Working -> surfaces immediately above Show 2 more!
        assert 'Active Idle 08' not in capture(), 'Active Idle 08 should be folded initially'
        report(active_panes['Active Idle 08'], 'working')
        wait_for(lambda: 'Active Idle 08' in capture() and 'Show 2 more' in capture(), 'folded Idle 08 resurfaced on Working')
        save('02-wide-resurfaced', 160)
        # Verify its real Herdr process group was untouched.
        assert rpc('pane.process_info', pane_id=active_panes['Active Idle 08'])['process_info']['foreground_process_group_id'] == active_pgids['Active Idle 08']

        # 3. Cross into Stopped toggle and expand in place -> "▾ Show less", check alignment!
        step_until('j', lambda: '> ▸ Show 94 more' in capture(), 'cursor on Stopped toggle', max_steps=12)
        key('Enter')
        wait_for(lambda: '> ▾ Show less' in capture() and 'Stopped 006' in capture(), 'Stopped expanded in place')
        save('03-wide-expanded', 160)
        check_column_alignment(capture(), '160x45 expanded', expanded=True)

        # 4. Browse deep (30+ rows down) into expanded Stopped, verify footer Collapse shortcut, press 'z' -> returns to action row!
        step_until('j', lambda: bool(re.search(r'> ■ Stopped 0(3[5-9]|[4-9]\d)', capture())) and 'Collapse' in capture(), 'deep in expanded Stopped', max_steps=45)
        save('04-wide-deep', 160)
        key('z')
        wait_for(lambda: '> ▸ Show 94 more' in capture() and 'Stopped 035' not in capture(), 'z collapsed Stopped back to action row')

        # 5. Search finds folded item Stopped 088.
        key('/')
        time.sleep(0.08)
        tm('send-keys', '-t', 'org:0.0', '-l', 'Stopped 088')
        wait_for(lambda: 'Stopped 088' in capture(), 'search found folded Stopped 088')
        save('05-wide-search', 160)
        key('Enter')
        time.sleep(0.08)
        key('Escape')
        wait_for(lambda: 'Show 93 more' in capture() and 'Stopped 088' in capture(), 'cleared search keeps selected Stopped 088 visible')

        # 6. Pin Active Idle 01 (verify 📌 moves to top of Active & persists without touching Herdr).
        step_until('k', lambda: '> ● Active Work' in capture(), 'back at top of Active', max_steps=25)
        step_until('j', lambda: '> ● Active Idle 01' in capture(), 'cursor on Active Idle 01', max_steps=5)
        key('P')
        wait_for(lambda: '📌 Active Idle 01' in capture(), 'Active Idle 01 pinned at top')
        persisted = json.loads(session_file.read_text())
        assert any(r['name'] == 'Active Idle 01' and r.get('pinned') for r in persisted), 'Pin did not persist'
        assert rpc('pane.process_info', pane_id=active_panes['Active Idle 01'])['process_info']['foreground_process_group_id'] == active_pgids['Active Idle 01']

        # 7. Archive refusal on Idle session, then Archive & Unarchive on Keep Magpie Pin.
        key('A')
        wait_for(lambda: 'cannot archive' in capture().lower(), 'Idle session refused Archive')
        # Move to Keep Magpie Pin (top of Stopped).
        step_until('j', lambda: '> ■ 📌 Keep Magpie Pin' in capture(), 'cursor on Keep Magpie Pin', max_steps=15)
        key('A')
        wait_for(lambda: 'Archived (1)' in capture() and 'Keep Magpie Pin' not in capture(), 'Keep Magpie Pin archived')
        # Verify Conversation, Profile, Magpie connection, and Pin are completely intact on disk!
        persisted = {r['id']: r for r in json.loads(session_file.read_text())}
        rec = persisted[target_id]
        assert rec['archived'] is True and rec['pinned'] is True
        assert rec['conversation']['reference'] == target_conv['reference']
        assert rec['profile'] == target_profile
        assert rec['mode'] == 'magpie' and rec['instance_id'] == 'lab' and rec['magpie_model'] == 'relay/gpt-5'

        # Open Archived view ('g'), verify Enter opens Details (never launches), then Unarchive ('u').
        key('g')
        wait_for(lambda: 'Archived' in capture() and 'Keep Magpie Pin' in capture() and '📌' in capture(), 'Archived view rendered')
        save('06-wide-archived', 160)
        key('Enter')
        wait_for(lambda: 'Unarchive this session before starting or resuming' in capture(), 'Enter opened Archived Details')
        key('Escape')
        wait_for(lambda: 'Keep Magpie Pin' in capture() and 'u Unarchive' in capture(), 'Esc returned to Archived view')
        key('u')
        wait_for(lambda: 'No archived sessions' in capture(), 'Unarchived Keep Magpie Pin')
        persisted = {r['id']: r for r in json.loads(session_file.read_text())}
        rec = persisted[target_id]
        assert not rec.get('archived') and rec['pinned'] is True
        assert rec['conversation']['reference'] == target_conv['reference']
        assert rec['profile'] == target_profile
        assert rec['mode'] == 'magpie' and rec['instance_id'] == 'lab' and rec['magpie_model'] == 'relay/gpt-5'
        key('Escape')
        wait_for(lambda: 'Harbor › Archived' not in capture() and 'Keep Magpie Pin' in capture(), 'Esc returned to normal Sessions with Keep Magpie Pin')

        # Archive Stopped 001, open Archived view, Remove record ('x' -> Enter), verify client history untouched!
        step_until('j', lambda: '> ■ Stopped 001' in capture(), 'cursor on Stopped 001', max_steps=8)
        key('A')
        wait_for(lambda: 'Archived (1)' in capture(), 'Stopped 001 archived')
        key('g')
        wait_for(lambda: 'Harbor › Archived' in capture() and 'Stopped 001' in capture(), 'Stopped 001 in Archived view')
        key('x')
        wait_for(lambda: 'Remove record' in capture() and 'Codex history is kept' in capture(), 'Remove confirm in Archived')
        time.sleep(0.25)  # past 200ms review click/enter guard
        key('Enter')
        wait_for(lambda: 'No archived sessions' in capture(), 'Stopped 001 removed from Archived view')
        persisted_names = {r['name'] for r in json.loads(session_file.read_text())}
        assert 'Stopped 001' not in persisted_names, 'Stopped 001 still in sessions.json'
        assert history_file.read_text() == history_payload, 'client history file modified by Remove!'
        key('Escape')
        wait_for(lambda: 'Harbor › Archived' not in capture() and ('Keep Magpie Pin' in capture() or 'Stopped 002' in capture()), 'back in normal Sessions')

        # 8. Check alignment in narrow & medium outer Herdr popup sizes (100x30, 80x24, 60x24) before & after expand!
        for cols, rows in ((100, 30), (80, 24), (60, 24)):
            tm('resize-window', '-t', 'org:0', '-x', str(cols), '-y', str(rows))
            time.sleep(0.35)
            step_until('j', lambda: '> ▸ Show 93 more' in capture(), f'{cols}x{rows} cursor on Stopped toggle', max_steps=15)
            frame_before = capture()
            save(f'07-collapsed-{cols}x{rows}', cols)
            lines_before = plain_lines(frame_before)
            col_item = next((l.find('Stopped 006') for l in lines_before if 'Stopped 006' in l), None)
            col_toggle = next((l.find('▸ Show ') + len('▸ ') for l in lines_before if '▸ Show ' in l), None)
            assert col_item is not None and col_toggle is not None and col_item == col_toggle, (
                f'{cols}x{rows} collapsed alignment mismatch: item={col_item} toggle={col_toggle}\n{frame_before}'
            )
            key('Enter')
            wait_for(lambda: '> ▾ Show less' in capture(), f'{cols}x{rows} expanded in place')
            # Step once into expanded rows so compact viewports scroll Stopped 007 into view alongside Show less.
            key('j')
            wait_for(lambda: '▾ Show less' in capture() and 'Stopped 007' in capture(), f'{cols}x{rows} expanded row visible')
            frame_after = capture()
            save(f'08-expanded-{cols}x{rows}', cols)
            lines_after = plain_lines(frame_after)
            col_less = next((l.find('▾ Show less') + len('▾ ') for l in lines_after if '▾ Show less' in l), None)
            col_exp = next((l.find('Stopped 007') for l in lines_after if 'Stopped 007' in l), None)
            assert col_less == col_item and col_exp == col_item, (
                f'{cols}x{rows} expanded alignment mismatch: item={col_item} less={col_less} exp={col_exp}\n{frame_after}'
            )
            key('z')
            wait_for(lambda: '> ▸ Show 93 more' in capture(), f'{cols}x{rows} collapsed back')
            step_until('k', lambda: 'Stopped 006' in capture(), f'{cols}x{rows} step back', max_steps=3)

        print('PASS')
    finally:
        tm('kill-server', check=False)
        server.terminate()
        try:
            server.wait(timeout=5)
        except subprocess.TimeoutExpired:
            server.kill()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--binary', required=True)
    parser.add_argument('--herdr', required=True)
    parser.add_argument('--output', type=Path)
    args = parser.parse_args()
    if args.output:
        args.output.mkdir(parents=True, exist_ok=True)
    base_tmp = '/private/tmp' if Path('/private/tmp').is_dir() else None
    with tempfile.TemporaryDirectory(prefix='ho-', dir=base_tmp) as tmp:
        exercise(args, Path(tmp))


if __name__ == '__main__':
    main()
