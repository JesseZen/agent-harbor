#!/usr/bin/env python3
"""Run all Harbor-owned popup gates on one copied binary/dependency set."""
import argparse,json,pathlib,subprocess
p=argparse.ArgumentParser()
for key in ('binary','herdr','codex','magpie','output'):p.add_argument('--'+key,required=True)
p.add_argument('--no-color',action='store_true');a=p.parse_args()
out=pathlib.Path(a.output);out.mkdir(parents=True,exist_ok=True)
results=[]
for name in ('settings','sessions','creation','directory','selector','connection','accounts','secondary','review'):
 script=pathlib.Path(__file__).with_name(name+'_popup.py')
 if not script.exists():raise RuntimeError('missing maintained fixture: '+str(script))
 command=['python3',str(script),'--binary',a.binary,'--herdr',a.herdr,'--codex',a.codex]
 if name in ('selector','connection'):command+=['--magpie',a.magpie]
 if a.no_color:command+=['--no-color']
 log=out/(name+'.log')
 with log.open('w') as stream:r=subprocess.run(command,stdout=stream,stderr=subprocess.STDOUT)
 result={'fixture':name,'exit_code':r.returncode,'log':str(log),'mode':'NO_COLOR' if a.no_color else 'color'}
 results.append(result);(out/'results.json').write_text(json.dumps(results,indent=2))
 print(('PASS' if r.returncode==0 else 'FAIL')+' '+name,flush=True)
 if r.returncode:raise SystemExit(r.returncode)
