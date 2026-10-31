import assert from 'node:assert/strict';import fs from 'node:fs';import os from 'node:os';import path from 'node:path';import {spawnSync} from 'node:child_process';
import {clientPackage,runProvider} from './run.mjs';
const root=process.cwd(),binary=path.resolve(process.argv[2]||'build/lmmock'),dir=fs.mkdtempSync(path.join(os.tmpdir(),'lmmock-streamjson-controls-'));
try{
 const pkg=clientPackage(),source=fs.readFileSync('fixtures/streamjson/handler.lms','utf8');
 const anchor='x.emit(events.at(i).to_json());';assert.equal(source.split(anchor).length-1,1,'terminal mutation anchor');
 const changed=source.replace(anchor,'if(events.at(i).get("type").as_string()!="result"){x.emit(events.at(i).to_json());}').replace('schema="../../internal/rpcstdio/presets/schema.json"','schema='+JSON.stringify(path.join(root,'internal/rpcstdio/presets/schema.json').replaceAll('\\','/')));
 const script=path.join(dir,'drop-terminal.lms'),code=path.join(dir,'drop-terminal.dslb');fs.writeFileSync(script,changed);
 const compiled=spawnSync(process.execPath,[path.join(pkg,'bin/lmscript'),'--no-preflight',script,code],{encoding:'utf8'});assert.ifError(compiled.error);assert.equal(compiled.status,0,'mutant must compile: '+compiled.stderr);
 for(const provider of ['claude','qwen']){
  const manifestFile=`fixtures/providers/${provider}-stream-json.json`;
  await runProvider({manifestFile,binary,only:'complete'});
  await runProvider({manifestFile,binary,only:'missing-terminal'});
  await assert.rejects(()=>runProvider({manifestFile,binary,only:'complete',code}),/json status expected "ok", got "error"/);
  console.log(`CAUGHT ${provider} handler mutation: dropped terminal => actual adapter unknown error`);
  await assert.rejects(()=>runProvider({manifestFile,binary,only:'missing-terminal',mutateExpected:true}),/error_class expected model_not_offered, got "unknown"/);
  console.log(`CAUGHT ${provider} expected-error-class mutation`);
 }
}finally{fs.rmSync(dir,{recursive:true,force:true});}
