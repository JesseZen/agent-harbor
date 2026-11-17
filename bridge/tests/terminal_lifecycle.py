#!/usr/bin/env python3
"""Isolated Linux PTY gate for the production Harbor model and terminal options.
Build its opt-in probe with go test -c. No real service or account is accessed.
"""
import argparse, fcntl, json, os, pathlib, pty, re, select, shlex, signal, struct, tempfile, termios, time

p=argparse.ArgumentParser();p.add_argument('--test-binary',required=True);a=p.parse_args()
binary=str(pathlib.Path(a.test_binary).resolve())
root=pathlib.Path(tempfile.mkdtemp(prefix='harbor-terminal-',dir='/tmp'));root.chmod(0o700)
for mode in ('quit','interrupt','panic','suspend'):
 home=root/mode;home.mkdir(mode=0o700)
 env={'HOME':str(home),'PATH':'/usr/bin:/bin','TERM':'xterm-256color','LANG':'C.UTF-8','PS1':'FIXTURE> ','HARBOR_TERMINAL_PROBE':mode,'AGENT_HARBOR_BRIDGE_HOME':str(home/'harbor')}
 for name in ('XDG_CONFIG_HOME','XDG_STATE_HOME','XDG_CACHE_HOME','XDG_DATA_HOME','CODEX_HOME','CODEX_SQLITE_HOME','TMPDIR'):
  path=home/name.lower();path.mkdir(mode=0o700);env[name]=str(path)
 pid,fd=pty.fork()
 if pid==0:os.execve('/bin/bash',['bash','--noprofile','--norc','-i'],env)
 fcntl.ioctl(fd,termios.TIOCSWINSZ,struct.pack('HHHH',30,100,0,0))
 output=bytearray()
 def read_until(value,timeout=15):
  start=len(output);end=time.monotonic()+timeout
  while time.monotonic()<end:
   if select.select([fd],[],[],.1)[0]:
    try:chunk=os.read(fd,65536)
    except OSError:break
    output.extend(chunk)
   if value in re.sub(rb'\x1b\[[0-?]*[ -/]*[@-~]',b'',output[start:]):return
  raise AssertionError(f'{mode}: missing {value!r}: '+output[-2500:].decode(errors='replace'))
 def send(value):os.write(fd,value)
 try:
  read_until(b'FIXTURE> ')
  command=f'stty -g > {shlex.quote(str(home/"before"))}; {shlex.quote(binary)} -test.run=^TestHarborTerminalLifecycleProbe$ -test.v; stty -g > {shlex.quote(str(home/"after"))}; echo PROBE_RETURNED\n'
  send(command.encode());read_until(b'Search sessions')
  if mode=='suspend':
   # Suspend from an actual text field; the draft must survive job control.
   send(b'/');time.sleep(.15);send(b'draft-kept');read_until(b'draft-kept');send(b'\x1a');read_until(b'FIXTURE> ')
   assert (home/'before').read_text()==(home/'after').read_text(), 'suspend failed to restore termios'
   send(b'fg\n');read_until(b'draft-kept');send(b'\x03');read_until(b'PASS')
   send(f'stty -g > {shlex.quote(str(home/"resumed"))}; echo RESUME_CHECKED\n'.encode());read_until(b'RESUME_CHECKED\r\n')
   deadline=time.monotonic()+3
   while not (home/'resumed').exists() and time.monotonic()<deadline:time.sleep(.05)
   assert (home/'before').read_text()==(home/'resumed').read_text(), 'resume exit failed to restore termios'
  else:
   send({'quit':b'q','interrupt':b'\x03','panic':b'!'}[mode]);read_until(b'PROBE_RETURNED\r\n')
   assert (home/'before').read_text()==(home/'after').read_text(), 'exit failed to restore termios'
   assert b'PASS' in output and b'FAIL' not in output, 'probe failed'
  for sequence in (b'\x1b[?1049l',b'\x1b[?25h',b'\x1b[?1003l',b'\x1b[?1006l',b'\x1b[?2004l'):
   assert sequence in output, f'missing terminal cleanup {sequence!r}'
  (home/'terminal.ansi').write_bytes(output)
  print('PASS',mode,'termios, alternate screen, cursor, mouse and paste cleanup',flush=True)
 finally:
  (home/'terminal.ansi').write_bytes(output)
  try:os.killpg(pid,signal.SIGTERM)
  except ProcessLookupError:pass
  os.close(fd);os.waitpid(pid,0)
(root/'result.json').write_text(json.dumps({'passed':True,'modes':['quit','interrupt','panic','suspend'],'service':'synthetic production-model probe'}))
print('diagnostics',root)
