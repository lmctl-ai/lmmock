import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {spawnSync} from 'node:child_process';
const root=process.cwd(),tmp=fs.mkdtempSync(path.join(os.tmpdir(),'acp-profile-controls-'));
const source=fs.readFileSync('fixtures/acp/handler.lms','utf8');
function compiler(){for(const dir of process.env.PATH.split(path.delimiter)){const p=path.resolve(dir,'../@lmctl-ai/lmctl/bin/lmscript');if(fs.existsSync(p))return p;}throw Error('private npm compiler required');}
const cases=[
 ['wrong-auth', 'text(cfg(),"fault")=="auth"','false',['copilot-auth','kimi-auth','opencode-auth','hermes-auth','miniplayer-auth','gemini-auth']],
 ['reverse-chunks','update(x,events.at(i))','update(x,events.at(events.len()-1-i))',['miniplayer-success']],
 ['premature-terminal','let ticket=x.defer_reply();x.close();','x.reply("{\\"stopReason\\":\\"end_turn\\"}");',['miniplayer-missing']],
 ['lost-cancel-reply','x.reply_to(x.get("pending"),"{\\"stopReason\\":\\"cancelled\\"}");','x.reply_to(x.get("pending"),"{\\"stopReason\\":\\"end_turn\\"}");',['miniplayer-cancel']],
];
try{
 for(const [label,before,after,ids]of cases){
  assert.ok(source.includes(before),'stale mutation anchor '+label);
  const fixture=path.join(tmp,label+'.lms'),binary=path.join(tmp,label+'.lmcode');
  fs.writeFileSync(fixture,source.replace('schema="../../internal/rpcstdio/presets/schema.json"','schema='+JSON.stringify(path.join(root,'internal/rpcstdio/presets/schema.json'))).replaceAll(before,after));
  let r=spawnSync(process.execPath,[path.resolve('scripts/capture-fixture.mjs'),fixture,binary],{encoding:'utf8'});assert.equal(r.status,0,r.stdout+r.stderr);
  for(const id of ids){
   r=spawnSync(process.execPath,['tests/acp-profiles/run-case.mjs',id],{encoding:'utf8'});assert.equal(r.status,0,'baseline '+id+' '+r.stdout+r.stderr);
   r=spawnSync(process.execPath,['tests/acp-profiles/run-case.mjs',id],{env:{...process.env,LMMOCK_ACP_BYTECODE:binary},encoding:'utf8'});
   assert.notEqual(r.status,0,'SURVIVED '+label+' '+id);assert.match(r.stderr,/AssertionError/,'must fail an assertion, not a loader/compiler error: '+r.stderr);
   console.log('CAUGHT '+label+' '+id);
  }
 }
 const module=await import('./run-case.mjs');await assert.rejects(module.runCase('copilot-auth',{quiet:true,expectedOverride:{error_class:'quota_exhausted'}}),/error_class/);console.log('CAUGHT mutated expected error_class');
}finally{fs.rmSync(tmp,{recursive:true,force:true});}
