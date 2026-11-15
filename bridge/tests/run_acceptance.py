#!/usr/bin/env python3
"""Run the maintained package/runtime gates with fresh environments and retained evidence."""
import argparse,json,os,pathlib,shutil,subprocess,sys,tarfile,tempfile,time
sys.dont_write_bytecode=True

def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--package',required=True)
    p.add_argument('--codex',required=True,help='native Codex executable, never codex.js')
    p.add_argument('--mcp',required=True,help='built pinned herdr-mcp dist/index.js')
    p.add_argument('--onboarding',choices=('managed','external'),required=True)
    a=p.parse_args()
    archive=pathlib.Path(a.package).resolve();native=pathlib.Path(a.codex).resolve();mcp=pathlib.Path(a.mcp).resolve()
    assert native.read_bytes()[:4] in (b'\x7fELF',b'\xcf\xfa\xed\xfe'), 'native Codex required'
    root=pathlib.Path(tempfile.mkdtemp(prefix='harbor-gates-',dir='/tmp')).resolve();root.chmod(0o700)
    stage=root/'package';stage.mkdir()
    with tarfile.open(archive) as t:
        assert all(n in ('agent-harbor','herdr','magpie','README.md','install.sh','licenses') or n.startswith('licenses/') for n in t.getnames())
        t.extractall(stage,filter='data')
    copied=root/'codex-native';shutil.copy2(native,copied);copied.chmod(0o700)
    scripts=pathlib.Path(__file__).resolve().parent
    results=[]
    print('gate diagnostics',root,flush=True)
    def invoke(label,script,args):
        work=root/label;work.mkdir()
        env={'PATH':os.environ.get('PATH','/usr/bin:/bin'),'PYTHONDONTWRITEBYTECODE':'1','TERM':'xterm-256color','LANG':'C.UTF-8'}
        for key in ('HOME','XDG_CONFIG_HOME','XDG_DATA_HOME','XDG_STATE_HOME','XDG_CACHE_HOME','XDG_RUNTIME_DIR','TMPDIR','CODEX_HOME','CODEX_SQLITE_HOME'):
            dest=work/key;dest.mkdir(mode=0o700);env[key]=str(dest)
        (work/'CODEX_HOME/config.toml').write_text('check_for_update_on_startup = false\n')
        command=[sys.executable,str(scripts/script),*map(str,args)]
        start=time.monotonic()
        with (work/'output.log').open('w') as log:
            result=subprocess.run(command,env=env,stdout=log,stderr=subprocess.STDOUT)
        results.append({'gate':label,'command':command,'exit':result.returncode,'seconds':round(time.monotonic()-start,2)})
        (root/'results.json').write_text(json.dumps(results,indent=2))
        print(label,'PASS' if result.returncode==0 else 'FAIL',results[-1]['seconds'],flush=True)
        if result.returncode:raise RuntimeError(f'{label} failed; inspect {work}/output.log')
    b,h,m=stage/'agent-harbor',stage/'herdr',stage/'magpie'
    invoke('install','package_install.py',[archive])
    invoke('identity','magpie_instances.py',['--binary',b,'--magpie',m,'--keep'])
    invoke('group','e2e_magpie_group.py',['--magpie',m])
    invoke('capabilities','codex_capabilities.py',['--codex',copied])
    invoke('sessions','query_recovery_real.py',['--binary',b,'--herdr',h,'--codex',copied,'--work-root',root/'sessions'/'TMPDIR'])
    invoke('manual','manual_fixture_smoke.py',['--binary',b,'--herdr',h,'--magpie',m,'--codex',copied])
    invoke('smoke','app_smoke.py',['--binary',b,'--herdr',h,'--codex',copied])
    invoke('ui','app_acceptance.py',['--full-state','--replacement','--renamed-codex','--binary',b,'--herdr',h,'--magpie',m,'--codex',copied,'--mcp',mcp,'--onboarding',a.onboarding])
    print('PASS all maintained runtime gates',root,flush=True)

if __name__=='__main__':main()
