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
root=pathlib.Path(tempfile.mkdtemp(prefix='harbor03-smoke-',dir='/tmp')).resolve()
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
  lines=capture().splitlines();y=next(i for i,line in enumerate(lines) if label in line)
  prefix=lines[y][:lines[y].index(label)];x=sum(2 if unicodedata.east_asian_width(c) in ('W','F') else 0 if unicodedata.combining(c) else 1 for c in prefix)+1
  tm('send-keys','-t','app:0.0','-l',f'\x1b[<{button};{x};{y+1}M'+(f'\x1b[<0;{x};{y+1}m' if button==0 else ''));time.sleep(.4)
 def save(name):
  (root/(name+'.ansi')).write_text(tm('capture-pane','-e','-p','-t','app:0.0'))
 keys('C-b');keys('a');wait('Search sessions');wait('Harbor 日本語')
 for cols,rows in [(160,45),(100,30),(60,20)]:
  tm('resize-window','-t','app:0','-x',str(cols),'-y',str(rows));time.sleep(.5)
  save('sessions-'+str(cols))
  if 'Docs review' not in capture():click('UI cleanup',64);wait('Docs review')
  click('Docs review');assert 'Search sessions' in capture() and 'Start session' not in capture()
  click('Details');wait('Session details');assert 'Docs review' in capture();keys('Escape')
  click('Search sessions');keys('-l','UI cleanup');assert 'Docs review' not in capture();keys('Escape')
  click('More');wait('Actions for UI cleanup');click('Rename');keys('Escape');wait('Search sessions')
  keys('i');wait('Session details');assert 'UI cleanup' in capture();keys('Escape')
 # Navigation is independent of selection and uses actual controls.
 tm('resize-window','-t','app:0','-x','160','-y','45');time.sleep(.5)
 click('Settings');wait('Harbor · Settings');click('Sessions');wait('Search sessions')
 click('Connections');wait('No recorded instances.');keys('Escape');wait('Search sessions')
 click('New session');wait('New session');keys('Escape');wait('Search sessions')
 (root/'harbor/sessions.json').write_text('[]');click('Refresh');wait('No matching sessions');save('sessions-empty-160')
 print('PASS sessions selection/search/details/navigation/new/resize mouse and keyboard',root)
finally:
 run([root/'bin/agent-harbor','stop'],check=False)
 run([tmux,'-S',sock,'kill-server'],check=False)
 discovery.shutdown();discovery.server_close()
