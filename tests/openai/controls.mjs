// Compile production lmcode variants; compiler failure is not a caught mutant.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import os from 'node:os';
import {spawnSync} from 'node:child_process';
const dir=fs.mkdtempSync(path.join(os.tmpdir(),'lmmock-openai-controls-'));
const binary=path.resolve(process.argv[2]||'build/lmmock');
const source=fs.readFileSync('fixtures/openai/handler.lms','utf8');
const schema=JSON.stringify(path.resolve('examples/schema.json').replaceAll('\\','/'));
try {
 for(const dialect of ['baseline','chat','responses']) {
  let code=source.replace('"../../examples/schema.json"',schema);
  if(dialect!=='baseline') {
   const anchor='handle(x, "'+dialect+'");';assert.equal(code.split(anchor).length,2);
   code=code.replace(anchor,'handle(x, "'+(dialect==='chat'?'responses':'chat')+'");');
  }
  const script=path.join(dir,dialect+'.lms'),program=path.join(dir,dialect+'.bin');fs.writeFileSync(script,code);
  // Invoke compiler through bash so npm Windows .cmd launchers are honored.
  const compiled=spawnSync('bash',['-c','"$LMSCRIPT_BIN" --no-preflight "$1" "$2"','compile',script,program],{encoding:'utf8',env:{...process.env,LMSCRIPT_BIN:process.env.LMSCRIPT_BIN||'lmscript'}});
  assert.equal(compiled.status,0,compiled.stdout+compiled.stderr);
  const r=spawnSync(binary,['exec','--lmcode',program,'--config','fixtures/openai/wire.json','--',process.execPath,'tests/openai/contract.mjs'],{encoding:'utf8'});
  if(dialect==='baseline')assert.equal(r.status,0,r.stdout+r.stderr);
  else {assert.notEqual(r.status,0);assert.match(r.stderr,/authenticated success/);}
  console.log('PASS '+dialect+' compiled handler control');
 }
}finally{fs.rmSync(dir,{recursive:true,force:true});}
