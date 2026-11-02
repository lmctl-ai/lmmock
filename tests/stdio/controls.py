#!/usr/bin/env python3
"""Production copies only; a build failure, panic or missing test is not a catch."""
import os,pathlib,shutil,subprocess,tempfile
root=pathlib.Path(__file__).resolve().parents[2]
cases=[
 ('wrong reply id','internal/rpcstdio/engine.go','"id": x.id','"id": 99','TestACPStreamingAndID',''),
 ('missing flush','internal/rpcstdio/framing.go','return f.Flush()','_ = f; return nil','TestWireFlushFailure',''),
 ('wrong content length','internal/rpcstdio/framing.go','len(b)))','len(b)+1))','TestFraming',''),
 ('lost persistent turn','internal/rpcstdio/engine.go','if e.opts.StateDir == "" {','if true {','TestAgentCellsPersistAndIsolate',''),
 ('shared alias counter','internal/rpcstdio/engine.go','e.opts.Alias+"\\x00"+key','"shared" + "\\x00" + key','TestAgentCellsPersistAndIsolate',''),
 ('lost client correlation','internal/rpcstdio/engine.go','!e.pending[string(id)]','false','TestMCPClientCorrelation',''),
 ('wrong ACP stop reason','internal/rpcstdio/presets/acp.lms','let reason="end_turn"','let reason="cancelled"','TestACPStreamingAndID','acp'),
 ('reversed ACP chunks','internal/rpcstdio/presets/acp.lms','chunks.at(i)','chunks.at(chunks.len() - 1 - i)','TestACPStreamingAndID','acp'),
 ('silent unscripted agent','internal/rpcstdio/presets/claude.lms','x.abort("unscripted or duplicate cell for alias " + x.alias() + " turn " + str(turn));','found = json_parse("{\\"text\\":\\"silent fallback\\"}");','TestAgentCellsPersistAndIsolate/claude','claude'),
 ('guard escape','cmd/lmmock/stdio.go','return "", fmt.Errorf("refusing peer command outside fake-bin")','return p, nil','TestStdioFailClosedPeerGuard',''),
]
with tempfile.TemporaryDirectory(prefix='stdio-controls-') as tmp:
 work=pathlib.Path(tmp)/'repo';shutil.copytree(root,work,ignore=shutil.ignore_patterns('.git','build','node_modules','__pycache__'))
 originals={name:(work/name).read_bytes() for _,name,_,_,_,_ in cases}
 bins={name:(work/'internal/rpcstdio/presets'/name).read_bytes() for name in ['acp.lmcode','claude.lmcode']}
 def run(pattern,package):
  p=subprocess.run(['go','test',package,'-count=1','-run',pattern],cwd=work,capture_output=True,text=True)
  return p.returncode,p.stdout+p.stderr
 code,out=run('Test','./internal/rpcstdio');assert code==0,out
 code,out=run('TestStdio','./cmd/lmmock');assert code==0,out
 print('BASELINE PASS',flush=True)
 for label,name,before,after,pattern,preset in cases:
  for file,data in originals.items():(work/file).write_bytes(data)
  for file,data in bins.items():(work/'internal/rpcstdio/presets'/file).write_bytes(data)
  source=originals[name].decode();assert before in source,('stale anchor',label)
  (work/name).write_text(source.replace(before,after))
  if preset:
   compiler=os.environ.get('LMSCRIPT_BIN','lmscript')
   command=[compiler]
   if os.name=='nt':
    resolved=pathlib.Path(shutil.which(compiler) or compiler)
    if resolved.suffix.lower()=='.cmd':command=['node',str(resolved.parent.parent/'@lmctl-ai/lmctl/bin/lmscript')]
   p=subprocess.run(['node','scripts/capture-fixture.mjs','internal/rpcstdio/presets/'+preset+'.lms','internal/rpcstdio/presets/'+preset+'.lmcode'],cwd=work,capture_output=True,text=True)
   assert p.returncode==0,(label,p.stdout,p.stderr)
  code,out=run(pattern,'./cmd/lmmock' if name.startswith('cmd/') else './internal/rpcstdio')
  assert code!=0 and '--- FAIL:' in out and '[build failed]' not in out and 'panic:' not in out,(label,out)
  print('CAUGHT '+label,flush=True)
 print('PASS',len(cases),'stdio production-copy controls')
