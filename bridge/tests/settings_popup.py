#!/usr/bin/env python3
"""Real Herdr 0.9.5 mouse regression; owns all state and synthetic records."""
import argparse, http.server, os, pathlib, shlex, shutil, subprocess, tempfile, threading, time

p=argparse.ArgumentParser();p.add_argument('--binary',required=True);p.add_argument('--herdr',required=True);p.add_argument('--codex',required=True);a=p.parse_args()
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
 for cols,rows in [(60,20),(100,30),(160,45)]:
  tm('resize-window','-t','app:0','-x',str(cols),'-y',str(rows));time.sleep(.4)
  keys('C-b');keys(',');wait('Harbor · Settings')
  (root/('settings-'+str(cols)+'.ansi')).write_text(tm('capture-pane','-e','-p','-t','app:0.0'))
  keys('Escape')
 keys('C-b');keys('a');wait('HARBOR');keys(',');wait('Harbor · Settings')
 def click(label):
  lines=capture().splitlines();y=next(i for i,line in enumerate(lines) if label in line)
  prefix=lines[y][:lines[y].index(label)]
  import unicodedata
  x=sum(2 if unicodedata.east_asian_width(c) in ('W','F') else 0 if unicodedata.combining(c) else 1 for c in prefix)+1
  tm('send-keys','-t','app:0.0','-l',f'\x1b[<0;{x};{y+1}M\x1b[<0;{x};{y+1}m');time.sleep(.5)
 # Verify motion through Herdr separately from click support. Some host
 # terminals forward clicks only; that is recorded without inventing hover.
 lines=capture().splitlines();y=next(i for i,l in enumerate(lines) if 'Connections' in l);x=lines[y].index('Connections')+1
 tm('send-keys','-t','app:0.0','-l',f'\x1b[<35;{x};{y+1}M');time.sleep(.4)
 motion=tm('capture-pane','-e','-p','-t','app:0.0');(root/'hover-160.ansi').write_text(motion)
 (root/'motion.txt').write_text('underline observed through Herdr' if '\x1b[4m' in motion else 'motion underline not observed; click and keyboard supported')
 click('Search settings');keys('-l','Magpie');assert 'prefix+m' in capture() and 'prefix+a' not in capture()
 keys('Escape');keys('Escape')
 click('prefix+a');keys('C-u');keys('-l','prefix+t');click('Accept field')
 click('prefix+,');keys('C-u');keys('-l','ctrl+g');click('Accept field')
 click('prefix+m');keys('C-u');keys('-l','prefix+f');click('Accept field')
 click('Save changes');out=wait('Saved for this workspace');(root/'settings-saved-160.ansi').write_text(tm('capture-pane','-e','-p','-t','app:0.0'))
 (root/'saved.txt').write_text(out)
 import json
 assert json.loads((root/'harbor/shortcuts.json').read_text())=={'harbor':'prefix+t','settings':'ctrl+g','magpie':'prefix+f'}
 # A separate keyboard-only edit/save path reuses the persisted values.
 keys('Escape');keys(',');wait('Harbor · Settings')
 for value in ['prefix+t','ctrl+g','prefix+f']:
  keys('Enter');keys('C-u');keys('-l',value);keys('Enter');keys('Down')
 keys('Down');keys('Enter');wait('Saved for this workspace')
 assert json.loads((root/'harbor/shortcuts.json').read_text())=={'harbor':'prefix+t','settings':'ctrl+g','magpie':'prefix+f'}
 keys('Escape');keys('q');time.sleep(.3)
 keys('C-b');keys('t');wait('HARBOR');keys('q')
 keys('C-b');keys('f');wait('No recorded instances.');keys('q')
 keys('C-g');wait('Harbor · Settings');keys('Escape')
 # Detach and re-enter exact generated configuration, then stop and reopen.
 keys('C-b');keys('q');wait('exit 0')
 tm('send-keys','-t','app:0.0','C-c');tm('send-keys','-t','app:0.0','-l',str(root/'bin/agent-harbor'));keys('Enter');wait('Harbor ^B t')
 keys('C-g');wait('Harbor · Settings');keys('Escape');keys('C-b');keys('q')
 run([root/'bin/agent-harbor','stop'])
 tm('send-keys','-t','app:0.0','C-c');tm('send-keys','-t','app:0.0','-l',str(root/'bin/agent-harbor'));keys('Enter');wait('Harbor ^B t')
 keys('C-g');wait('Harbor · Settings')
 (root/'saved.ansi').write_text(tm('capture-pane','-e','-p','-t','app:0.0'))
 print('PASS actual check/reload/new shortcut/settings popup three sizes/reentry/restart',root)
finally:
 run([root/'bin/agent-harbor','stop'],check=False)
 run([tmux,'-S',sock,'kill-server'],check=False)
 discovery.shutdown();discovery.server_close()
