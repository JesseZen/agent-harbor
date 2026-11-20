#!/usr/bin/env python3
"""Real Herdr shortcut editor, live reload, popup routing and visual captures."""
import argparse, http.server, json, os, pathlib, shlex, shutil, subprocess, tempfile, threading, time

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
 workspace = root / 'harbor'
 prefs = workspace / 'shortcuts.json'
 legacy = {'harbor': 'prefix+a', 'settings': 'prefix+,', 'magpie': 'prefix+m'}
 tm('new-session','-d','-s','app','-x','160','-y','45',str(launcher))
 wait('Harbor ^B a')
 prefs.write_text(json.dumps(legacy))
 prefs.chmod(0o600)

 def keys(*items):
  if items[0] == '-l':
   tm('send-keys', '-t', 'app:0.0', *items)
   time.sleep(.15)
   return
  for key in items:
   tm('send-keys', '-t', 'app:0.0', key)
   time.sleep(.2 if key == 'Escape' else .1)

 def snapshot(name, width):
  (root / f'{name}-{width}.ansi').write_text(tm('capture-pane', '-e', '-p', '-t', 'app:0.0'))

 def edit(value):
  keys('C-u')
  keys('-l', value)
  keys('Enter')

 def click(label):
  import unicodedata
  lines = capture().splitlines()
  y = next(i for i, line in enumerate(lines) if label in line)
  prefix = lines[y][:lines[y].index(label)]
  x = sum(2 if unicodedata.east_asian_width(c) in ('W', 'F') else 0 if unicodedata.combining(c) else 1 for c in prefix) + 1
  keys('-l', f'\x1b[<0;{x};{y+1}M\x1b[<0;{x};{y+1}m')

 keys('C-b', ',')
 wait('Harbor · Settings')
 assert json.loads(prefs.read_text()) == legacy
 # Keep the existing prefix binding and add a direct alias via the mouse.
 click('prefix+a')
 wait('Add shortcut')
 click('Add shortcut')
 click('Add shortcut')
 wait('Apply to draft')
 edit('ctrl+g')
 wait('ctrl+g')
 keys('Enter')
 edit('prefix+m')
 wait('Magpie already uses prefix+m')
 snapshot('shortcut-conflict', 160)
 keys('Escape', 'Escape')
 wait('Search settings')
 keys('Up', 'Enter', 'Down', 'Enter')
 wait('Apply to draft')
 edit('ctrl+a')
 keys('Escape', 'Down', 'Down', 'Enter', 'Down', 'Enter')
 wait('Apply to draft')
 edit('ctrl+t')
 keys('Escape', 'Down', 'Enter', 'Enter')
 wait('Apply to draft')
 edit('ctrl+n')
 keys('Escape', 'C-s')
 wait('Saved for this workspace')
 saved = {**legacy, 'harbor': 'prefix+a ctrl+g', 'magpie': 'prefix+m ctrl+t',
          'prefix': 'ctrl+b ctrl+a', 'new_session': 'ctrl+n'}
 assert json.loads(prefs.read_text()) == saved
 keys('Escape')

 for cols, rows in [(160,45),(100,30),(80,24),(60,24)]:
  tm('resize-window', '-t', 'app:0', '-x', str(cols), '-y', str(rows))
  time.sleep(.35)
  keys('C-a', ',')
  wait('Harbor · Settings')
  snapshot('shortcuts', cols)
  keys('Enter')
  wait('Add shortcut')
  snapshot('shortcut-bindings', cols)
  keys('Down', 'Down', 'Enter')
  wait('Apply to draft')
  snapshot('shortcut-add', cols)
  keys('Escape', 'Escape', 'Escape')
  keys('C-g')
  wait('Search sessions')
  snapshot('sessions', cols)
  keys('q')
  keys('C-t')
  wait('No recorded instances')
  snapshot('magpie', cols)
  keys('q')
  keys('C-n')
  wait('New session')
  wait('Project')
  snapshot('new-session', cols)
  keys('Escape')
  wait('Search sessions')
  keys('q')

 # Verify both prefixes, both Harbor aliases and both Magpie aliases live.
 for prefix in ('C-b', 'C-a'):
  keys(prefix, 'a')
  wait('Search sessions')
  keys('q')
 keys('C-a', 'm')
 wait('No recorded instances')
 keys('q')

 tm('resize-window', '-t', 'app:0', '-x', '160', '-y', '45')
 keys('C-a', ',')
 wait('Search settings')
 # Remove the default prefix and check the new prefix via a live reload.
 keys('Up', 'Enter', 'Delete', 'Escape', 'C-s')
 wait('Saved for this workspace')
 assert json.loads(prefs.read_text()) == {**saved, 'prefix': 'ctrl+a'}
 keys('Escape', 'C-b', 'a')
 assert 'Search sessions' not in capture()
 keys('C-a', 'a')
 wait('Search sessions')
 keys('q', 'C-a', 'q')
 wait('exit 0')
 run([root / 'bin/agent-harbor', 'stop'])
 tm('send-keys', '-t', 'app:0.0', '-l', str(root / 'bin/agent-harbor'))
 keys('Enter')
 wait('Harbor ^A a')
 keys('C-n')
 wait('Project')
 keys('Escape', 'q')
 assert run([root / 'bin/agent-harbor', 'sessions', '--json']).stdout.strip() in ('[]', 'null')
 keys('C-a', ',')
 wait('Search settings')
 keys('C-r')
 wait('Defaults restored in draft')
 assert json.loads(prefs.read_text()) == {**saved, 'prefix': 'ctrl+a'}
 keys('C-s')
 wait('Saved for this workspace')
 assert json.loads(prefs.read_text()) == legacy
 keys('Escape', 'C-b', 'a')
 wait('Search sessions')
 keys(',')
 wait('Search settings')
 keys('/')
 keys('-l', 'Magpie')
 filtered = wait('prefix+m')
 assert 'prefix+a' not in filtered
 keys('Escape', 'Escape', 'Escape')
 wait('Search sessions')
 keys('n')
 wait('Project')
 keys('Escape')
 wait('Search sessions')
 print('PASS shortcuts, prefix aliases and replacement, live reload, restart and popup captures', root)
finally:
 run([root/'bin/agent-harbor','stop'],check=False)
 run([tmux,'-S',sock,'kill-server'],check=False)
 discovery.shutdown();discovery.server_close()
