"""Fault injection into fixture-owned resources; observations through real Harbor UI."""
import fcntl,json,pathlib

def exercise(root,binary,managed,run,popup,key,text,screen,capture,records,wait,saved):
    original={r['name']:r for r in records()}
    counts=lambda:{r['name']:(r['codex_session_id'],r.get('instance_id'),r.get('magpie_model')) for r in records()}
    rollouts=lambda:set(p.name for p in (root/'codex/sessions').rglob('*.jsonl'))
    before=rollouts()
    def refusal(name,reason):
        popup();key('/');text(name);key('Enter');screen('Resume conversation');screen('🚢 '+name);key('Enter');screen(reason)
        (root/('negative-'+reason.split(';')[0].replace(' ','-')[:35]+'.txt')).write_text(capture())
        assert counts()==saved,'refusal changed connection or conversation'
        assert rollouts()==before,'refusal created a replacement conversation'
        assert not any(r['running'] for r in json.loads(run([binary,'sessions','--json']).stdout)), 'refusal launched a writer'
        key('Escape');key('q')
        print('PASS negative UI:',reason,flush=True)
    run([binary,'magpie','stop'])
    refusal('UI-A','gateway offline')
    run([binary,'magpie','start'])
    run([managed,'provider','rm','relay-b'])
    refusal('UI-B','saved Magpie model or routing group is unavailable')
    # Use the existing fixture provider address; retain its exact configuration.
    providers=root/'provider-a-restore.json'
    # Caller snapshots the portable provider file before entering this module.
    config=root/'harbor/magpie/bin/data/providers.json'
    config.write_bytes(providers.read_bytes())
    wait(lambda:'relay-a/test-model' in run([binary,'models','--instance','managed','--json']).stdout,'restored model')
    run([managed,'group','set','ui-group','models=relay-b/test-model'])
    popup();key('/');text('UI-A');key('Enter');screen('Resume conversation');screen('🚢 UI-A')
    screen('Groups may change members');screen('Review in Magpie')
    assert counts()==saved and rollouts()==before
    key('Escape');key('q')
    assert not any(r['running'] for r in json.loads(run([binary,'sessions','--json']).stdout)), 'changed group started without confirmation'
    run([managed,'group','set','ui-group','models=relay-a/test-model'])
    print('PASS changed group retains selection, requires review, cancellation launches nothing',flush=True)
    (root/'project').rename(root/'project-missing')
    try:refusal('UI-A','working directory is unavailable')
    finally:(root/'project-missing').rename(root/'project')
    # The lock is an independently held OS resource. Its semantics on real
    # interactive Codex are also exercised by sessions_real.py.
    lock=root/'codex/thread-writer-locks'/ (original['UI-A']['codex_session_id']+'.lock')
    with lock.open('a+') as f:
        fcntl.flock(f,fcntl.LOCK_EX|fcntl.LOCK_NB)
        refusal('UI-A','conversation is open elsewhere')
    # Simulate a validated second main-thread notification using another real
    # rollout from these sessions. Never manufacture a conversation file.
    rec=original['UI-A']
    plan=next(p for p in (root/'harbor').glob('notify-*.json') if json.loads(p.read_text()).get('session_id')==rec['id'] and json.loads(p.read_text()).get('token')==rec['run_token'])
    run([binary,'_notify',plan,json.dumps({'type':'agent-turn-complete','thread-id':original['UI-B']['codex_session_id'],'cwd':str(root/'project')})])
    refusal('UI-A','conversation ID ambiguous')
    run([binary,'verify','--directory',root/'project',rec['id'],rec['codex_session_id']],input='yes\n')
    native=root/'bin/codex';native.chmod(0o600)
    try:refusal('UI-Native','Codex was not found on PATH')
    finally:native.chmod(0o700)
    assert counts()==saved and rollouts()==before
