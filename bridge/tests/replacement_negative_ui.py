"""Replacement fault acceptance through the real popup; no legacy notify/locks."""
import copy,json,subprocess,os,signal

def exercise(root,binary,managed,run,popup,key,text,screen,capture,records,wait,saved):
    original=copy.deepcopy(records())
    reviewed_records={}
    expected_running=0
    before={p:p.read_bytes() for p in (root/'codex/sessions').rglob('*.jsonl')}
    def refusal(name,reason):
        popup();key('/');text(name);key('Enter');key('Enter');screen('Review query-resume');key('a');key('Enter');key('i');screen(reason)
        assert {r['name']:(r['codex_session_id'],r.get('instance_id'),r.get('magpie_model')) for r in records() if r['name'] in saved}==saved
        assert {p:p.read_bytes() for p in (root/'codex/sessions').rglob('*.jsonl')}==before,'refusal changed conversation files'
        assert sum(r['running'] for r in json.loads(run([binary,'sessions','--json']).stdout))==expected_running,'refusal launched client'
        key('Escape');key('Escape');key('q')
        print('PASS replacement negative UI:',reason,flush=True)
    run([binary,'magpie','stop']);refusal('UI-A','gateway offline');run([binary,'magpie','start'])
    if next(r for r in original if r['name']=='UI-B').get('codex_connection')!='caller':
        run([managed,'provider','rm','relay-b']);refusal('UI-B','saved Magpie model or routing group is unavailable')
        (root/'harbor/magpie/bin/data/providers.json').write_bytes((root/'provider-a-restore.json').read_bytes())
        wait(lambda:'relay-b/test-model' in run([binary,'models','--instance','managed','--json']).stdout,'restored catalog')
    else:
        # Joined recovery delegates model/effort to Codex's saved thread. The
        # initial Harbor choice is not a current-model assertion; see
        # TestJoinedRecoveryUsesCodexSelectionWithoutHarborProfile.
        # Actual resumed route/history is checked by app_acceptance afterward.
        print('Joined recovery model remains client-owned; initial-model removal refusal applies only to legacy connections',flush=True)
    (root/'project').rename(root/'project-missing')
    try:refusal('UI-A','working directory is unavailable')
    finally:(root/'project-missing').rename(root/'project')
    # Fixture-owned persistence is the external input under fault injection;
    # outcomes and absence of execution are observed only through public APIs.
    record_file=root/'harbor/sessions.json'
    for status in ['missing','conflict']:
        altered=copy.deepcopy(original)
        next(r for r in altered if r['name']=='UI-A')['conversation']['status']=status
        record_file.write_text(json.dumps(altered))
        # A restart may require explicit reviewed-compatibility reassociation.
        # Both provenance paths must refuse a subsequently missing/conflicting
        # observation; assert the precise public explanation for that input.
        provenance=next(r for r in altered if r['name']=='UI-A')['conversation']['provenance']
        reason=('conversation observation needs review' if provenance=='herdr-pane-observation'
                else 'query recovery requires an upstream-observed reference')
        try:refusal('UI-A',reason)
        finally:record_file.write_text(json.dumps(original))
    # A complete saved reference with a missing final observation can be
    # explicitly reviewed in the UI; approval must not start a client or
    # claim the reference was freshly observed by Herdr.
    for name in ['UI-A','UI-Native']:
        altered=copy.deepcopy(original)
        missing=next(r for r in altered if r['name']==name)
        reference=copy.deepcopy(missing['conversation']['reference'])
        audit_count=len(missing.get('reassociations',[]))
        missing['conversation']['status']='missing'
        record_file.write_text(json.dumps(altered))
        try:
            popup();key('/');text(name);key('Tab');key('v');screen('Review review-reference')
            import time
            time.sleep(.25);key('Tab');key('Enter');screen('Inspect evidence')
            assert next(r for r in records() if r['name']==name)['conversation']['status']=='missing', 'unapproved reference was changed'
            key('a');key('Enter')
            wait(lambda:next(r for r in records() if r['name']==name)['conversation']['status']=='reviewed','saved reference review')
            reviewed=next(r for r in records() if r['name']==name)
            assert reviewed['conversation']['reference']==reference and reviewed['conversation']['provenance']=='reviewed-compatibility'
            assert len(reviewed.get('reassociations',[]))==audit_count+1 and reviewed['reassociations'][-1]['accepted']
            assert not next(r for r in json.loads(run([binary,'sessions','--json']).stdout) if r['name']==name)['running'], 'reference review started a client'
            reviewed_records[name]=copy.deepcopy(reviewed)
            if 'HARBOR' in capture():key('q')
            print('PASS saved identity review for',name,'requires acceptance and preserves conversation',flush=True)
        finally:record_file.write_text(json.dumps(original))
    for config_dir,reason in [('', 'Magpie instance unknown'),(str(root/'different-config'),'Magpie instance changed')]:
        altered=copy.deepcopy(original)
        next(r for r in altered if r['name']=='UI-A')['instance_snapshot']['config_dir']=config_dir
        record_file.write_text(json.dumps(altered))
        try:refusal('UI-A',reason)
        finally:record_file.write_text(json.dumps(original))
    sleeper=subprocess.Popen(['/bin/sleep','120'],start_new_session=True,env={'PATH':'/usr/bin:/bin','HOME':str(root/'home')})
    try:
        duplicate=copy.deepcopy(next(r for r in original if r['name']=='UI-A'))
        duplicate.update(id='replacement-known-duplicate',name='Known duplicate fixture',process_group=sleeper.pid)
        # This process is a safety fixture, not an observed Herdr agent.
        record_file.write_text(json.dumps(original+[duplicate]));expected_running=0
        refusal('UI-A','still running in another session')
    finally:
        record_file.write_text(json.dumps(original));os.killpg(sleeper.pid,signal.SIGTERM);sleeper.wait();expected_running=0
    assert not list((root/'harbor').glob('notify-*.json')),'legacy notification dependency'
    # Carry the UI-approved records into the following real recovery checks.
    # This proves that explicit review is usable, rather than only persistent.
    for index,record in enumerate(original):
        if record['name'] in reviewed_records:
            original[index]=reviewed_records[record['name']]
    record_file.write_text(json.dumps(original))
