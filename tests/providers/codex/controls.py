#!/usr/bin/env python3
"""Compile production handler copies; only named assertion failures count as catches."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
root=Path(__file__).resolve().parents[3]
source_path='fixtures/codex/handler.lms'
cases=[
 ('initialize state','x.set("phase","initialized");','x.set("phase","ready");','TestCodexProviderUsageLifecycle'),
 ('fork identity','thread_reply(x,fork);','thread_reply(x,sid());','TestCodexProviderResumeFork'),
 ('usage event','x.notify(method,params.to_json());','if(method!="thread/tokenUsage/updated"){x.notify(method,params.to_json());}','TestCodexProviderUsageLifecycle'),
 ('interrupt status',r'\"status\":\"interrupted\"',r'\"status\":\"completed\"','TestCodexProviderInterrupt'),
 ('RPC error code','x.error(code,c.get("error").as_string());','x.error(-32600,c.get("error").as_string());','TestCodexProviderAdverseFidelity/rpc-error'),
]
pkg=Path(os.environ['LMCTL_PACKAGE_ROOT']);node=shutil.which('node');assert node
with tempfile.TemporaryDirectory(prefix='codex-controls-') as temp:
 work=Path(temp)/'repo';shutil.copytree(root,work,ignore=shutil.ignore_patterns('.git','build','node_modules','__pycache__'))
 source=(work/source_path).read_text();code=(work/'internal/rpcstdio/presets/codex.lmcode').read_bytes()
 def run(pattern):
  r=subprocess.run(['go','test','./internal/rpcstdio','-count=1','-run',pattern],cwd=work,capture_output=True,text=True)
  return r,r.stdout+r.stderr
 r,out=run('^TestCodexProvider');assert r.returncode==0,out;print('BASELINE Codex handlers PASS',flush=True)
 for name,before,after,test in cases:
  assert source.count(before)==1,('stale mutation anchor',name,source.count(before))
  (work/source_path).write_text(source.replace(before,after));(work/'internal/rpcstdio/presets/codex.lmcode').write_bytes(code)
  r=subprocess.run([node,'scripts/capture-fixture.mjs',source_path,'internal/rpcstdio/presets/codex.lmcode'],cwd=work,capture_output=True,text=True);assert r.returncode==0,(name,r.stdout,r.stderr)
  r,out=run('^'+test+'$');assert r.returncode!=0 and '--- FAIL:' in out and '[build failed]' not in out and 'panic:' not in out,(name,out)
  print('CAUGHT '+name+' at '+test,flush=True)
  if name=='usage event':
   suffix='.exe' if os.name=='nt' else '';binary=work/('codex-fixture'+suffix)
   subprocess.run(['go','build','-o',str(binary),'./tests/providers/codex/fakecli'],cwd=work,check=True,capture_output=True)
   env={**os.environ,'CODEX_FIXTURE_BIN':str(binary)}
   r=subprocess.run([node,str(root/'tests/providers/codex/case.mjs'),'success'],cwd=root,env=env,capture_output=True,text=True)
   assert r.returncode==1 and 'Codex exact input/cache/output/reasoning facts' in r.stderr,(r.stdout,r.stderr)
   print('CAUGHT actual native usage mutant at published-client accounting facts',flush=True)
 print('PASS5 Codex handler-family mutations and real-client usage control',flush=True)
