#!/usr/bin/env python3
"""Exercise the one-command manual fixture, its marker journal and owned cleanup."""
import argparse,json,os,pathlib,subprocess,sys,urllib.request
sys.dont_write_bytecode=True
p=argparse.ArgumentParser()
for name in ('binary','herdr','magpie','codex'):p.add_argument('--'+name,required=True)
a=p.parse_args();script=pathlib.Path(__file__).with_name('manual_acceptance.py')
env=dict(os.environ,PATH='/usr/bin:/bin:/usr/sbin:/sbin')
root=None
try:
    result=subprocess.run([sys.executable,str(script),'start',*sum((['--'+n,getattr(a,n)] for n in ('binary','herdr','magpie','codex')),[])],env=env,text=True,capture_output=True,check=True)
    print(result.stdout,flush=True)
    root=pathlib.Path(next(line.removeprefix('Fixture: ') for line in result.stdout.splitlines() if line.startswith('Fixture: ')))
    ports=json.loads((root/'ports.json').read_text())
    for label,marker in [('A','AH_LOCAL_FIRST'),('B','AH_RELAY_B'),('C','AH_LOCAL_C')]:
        if label=='C':
            url=f'http://127.0.0.1:{ports[label]}/v1/responses'
            body={'model':'test-model','input':[{'role':'user','content':marker}],'stream':True}
        else:
            url='http://'+ports['magpie']+'/v1/chat/completions'
            body={'model':'relay-'+label.lower()+'/test-model','messages':[{'role':'user','content':marker}],'stream':True}
        request=urllib.request.Request(url,json.dumps(body).encode(),{'Content-Type':'application/json','Authorization':'Bearer fixture-only'})
        with urllib.request.urlopen(request,timeout=30) as response:response.read()
        journal=[json.loads(line) for line in (root/'requests.log').read_text().splitlines()]
        assert any(e['destination']==label and e['kind']=='main' and marker in e['latest_user_markers'] for e in journal)
        assert not any(e['destination']!=label and marker in e['latest_user_markers'] for e in journal)
    for action in ('status','restart','status'):
        subprocess.run([sys.executable,str(script),action,str(root)],env=env,check=True)
    print('PASS manual fixture: prebuilt without Node/Go, real gateway A/B, native C, classified markers and restart',root,flush=True)
finally:
    if root:subprocess.run([sys.executable,str(script),'stop',str(root)],env=env,check=True)
