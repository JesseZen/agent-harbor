#!/usr/bin/env python3
"""Real Herdr 0.9.5 mouse regression; owns all state and synthetic records."""
import argparse, http.server, os, pathlib, shlex, shutil, subprocess, tempfile, threading, time

p=argparse.ArgumentParser();p.add_argument('--binary',required=True);p.add_argument('--herdr',required=True);p.add_argument('--codex',required=True);p.add_argument('--no-color',action='store_true');a=p.parse_args()
# Exclusively own the discovery address; never probe an existing service.
class DiscoveryHandler(http.server.BaseHTTPRequestHandler):
 def do_GET(self): self.send_error(404)
 def log_message(self,*args): pass
try:
 discovery=http.server.ThreadingHTTPServer(('127.0.0.1',3425),DiscoveryHandler)
except OSError:
 print('SKIP: standard discovery port is in use; no existing service contacted')
 raise SystemExit(77)
threading.Thread(target=discovery.serve_forever,daemon=True).start()
root=pathlib.Path(tempfile.mkdtemp(prefix='harbor09-smoke-',dir='/tmp')).resolve()
env={'PATH':'/usr/bin:/bin:/usr/sbin:/sbin','TERM':'xterm-256color','SHELL':'/bin/sh','LANG':'en_US.UTF-8','COLORTERM':'truecolor'}
if a.no_color:env['NO_COLOR']='1'
for key,leaf in [('HOME','home'),('XDG_CONFIG_HOME','config'),('XDG_DATA_HOME','data'),('XDG_STATE_HOME','state'),('XDG_CACHE_HOME','cache'),('XDG_RUNTIME_DIR','runtime'),('TMPDIR','tmp'),('CODEX_HOME','codex'),('CODEX_SQLITE_HOME','sqlite')]:
 (root/leaf).mkdir(mode=0o700);env[key]=str(root/leaf)
(root/'codex/config.toml').write_text('check_for_update_on_startup = false\n')
(root/'bin').mkdir();shutil.copy2(a.binary,root/'bin/agent-harbor');shutil.copy2(a.herdr,root/'bin/herdr')
native=pathlib.Path(a.codex)
assert native.read_bytes()[:4] in (b'\x7fELF',b'\xcf\xfa\xed\xfe',b'\xca\xfe\xba\xbe'), 'use a native Codex binary, never the npm wrapper'
shutil.copy2(native,root/'bin/codex');(root/'bin/codex').chmod(0o700)
(root/'bin/herdr').chmod(0o700)
(root/'bin/agent-harbor').chmod(0o700)
env['PATH']=str(root/'bin')+':'+env['PATH'];env['AGENT_HARBOR_HERDR_BIN']=str(root/'bin/herdr');env['AGENT_HARBOR_BRIDGE_HOME']=str(root/'harbor')
tmux=shutil.which('tmux');sock=str(root/'tmux.sock')
def run(args,check=True):return subprocess.run(list(map(str,args)),env=env,text=True,capture_output=True,check=check)
def tm(*args):return run([tmux,'-S',sock,*args]).stdout
def capture():return tm('capture-pane','-p','-t','app:0.0')
def wait(text):
 end=time.time()+20
 while time.time()<end:
  out=capture()
  if text in out:return out
  time.sleep(.2)
 raise RuntimeError('missing '+text+'\n'+capture())
try:
 launcher=root/'launch.sh'
 launcher.write_text('#!/bin/sh\n'+''.join('export '+key+'='+shlex.quote(value)+'\n' for key,value in env.items())+'agent-harbor\nprintf \"exit %s\\n\" \"$?\"\nexec /bin/sh\n')
 launcher.chmod(0o700)
 tm('new-session','-d','-s','app','-x','160','-y','45',str(launcher))
 wait('First run')
 wait(run([root/'bin/codex','--version']).stdout.strip())
 out=wait('herdr 0.9.5');(root/'onboarding.txt').write_text(out)
 tm('send-keys','-t','app:0.0','Enter');wait('Review: local')
 tm('send-keys','-t','app:0.0','Enter')
 out=wait('Harbor ^B a')

 def keys(*items):tm('send-keys','-t','app:0.0',*items);time.sleep(.5)
 import json,unicodedata
 records=[]
 for i,name in enumerate(['Harbor 日本語 🚢','Docs review','UI cleanup']):
  project=root/('project-'+str(i));project.mkdir()
  records.append(dict(version=2,id=('%032x'%(i+1)),name=name,working_directory=str(project),cli='codex',mode='local',recovery='ready',started=False,created_at='2026-11-16T00:00:00Z'))
 (root/'harbor/sessions.json').write_text(json.dumps(records))
 def click(label,button=0):
  lines=capture().splitlines();y=max(i for i,line in enumerate(lines) if label in line)
  prefix=lines[y][:lines[y].index(label)];x=sum(2 if unicodedata.east_asian_width(c) in ('W','F') else 0 if unicodedata.combining(c) else 1 for c in prefix)+1
  tm('send-keys','-t','app:0.0','-l',f'\x1b[<{button};{x};{y+1}M'+(f'\x1b[<0;{x};{y+1}m' if button==0 else ''));time.sleep(.4)
 def save(name):
  (root/(name+'.ansi')).write_text(tm('capture-pane','-e','-p','-t','app:0.0'))
 keys('C-b');keys('a');wait('HARBOR');wait('Harbor 日本語')
 for cols,rows in [(160,45),(100,30),(60,20)]:
  tm('resize-window','-t','app:0','-x',str(cols),'-y',str(rows));time.sleep(.5)
  keys('i');wait('Session details');save('secondary-detail-'+str(cols));click('Back');wait('Search sessions')
  keys('?');wait('Keyboard help');save('secondary-help-'+str(cols));keys('Down');click('Back');wait('Search sessions')
  keys('e');wait('Rename session');keys('C-u');keys('q');keys('?');save('secondary-rename-'+str(cols));assert 'Rename session' in capture();click('Cancel');wait('Search sessions')
 # Failed validation retains the edit; a retry persists exactly the selected ID.
 tm('resize-window','-t','app:0','-x','100','-y','30');time.sleep(.5)
 keys('e');wait('Rename session');keys('C-u');click('Apply');wait('non-empty session name')
 keys('-l','Renamed 日本語 🚢');click('Apply');wait('Search sessions');wait('Renamed 日本語')
 saved=json.loads((root/'harbor/sessions.json').read_text())
 assert saved[0]['id']==records[0]['id'] and saved[0]['name']=='Renamed 日本語 🚢', saved
 assert [r['name'] for r in saved[1:]]==[r['name'] for r in records[1:]]
 save('secondary-renamed-100')
 # A stale saved record causes a real service refusal. Restore only fixture data,
 # retain the typed edit, and retry against the original reviewed target.
 keys('e');wait('Rename session');keys('C-u');keys('-l','Retry retained')
 changed=[dict(r) for r in saved];changed[0]['name']='Changed elsewhere'
 (root/'harbor/sessions.json').write_text(json.dumps(changed))
 click('Apply');wait('Rename session');wait('changed');assert 'Retry retained' in capture()
 save('secondary-error-100')
 (root/'harbor/sessions.json').write_text(json.dumps(saved));click('Apply');wait('Search sessions');wait('Retry retained')
 assert json.loads((root/'harbor/sessions.json').read_text())[0]['name']=='Retry retained'
 # Close and reopen the popup to read persisted state through the real list path.
 keys('q');wait('Harbor ^B a');keys('C-b');keys('a');wait('Retry retained')
 save('secondary-reentry-100')
 print('PASS secondary popup detail/help/back/input/cancel/validation/service refusal/retry/persistence at three sizes',root)
finally:
 run([root/'bin/agent-harbor','stop'],check=False)
 run([tmux,'-S',sock,'kill-server'],check=False)
 discovery.shutdown();discovery.server_close()
