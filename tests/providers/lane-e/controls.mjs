// Compile actual lmcode handler mutations, then exercise the published adapter.
import assert from 'node:assert/strict';import fs from 'node:fs';import path from 'node:path';import os from 'node:os';import {spawnSync} from 'node:child_process';
const repo=path.resolve(import.meta.dirname,'../../..'),binary=path.resolve(process.argv[2]);
const compiler=process.env.LMSCRIPT_BIN||'lmscript';
const mutations=[
 {file:'print',provider:'agy',variant:'nonzero',from:'x.exit(response.get("exit").as_int());',to:'x.exit(0);',oracle:/native exit contract|successful empty reply/},
 {file:'dsh',provider:'dsh',variant:'success',from:'quote(text)',to:'quote("mutated reply")',oracle:/handler reply mismatch/},
 {file:'muse',provider:'muse',variant:'success',from:'let terminal="completed"',to:'let terminal="unknown"',oracle:/Muse returned an unknown turn terminal/},
 {file:'sidecar-boot',provider:'dsh',variant:'success',from:'let url=input.get("url").as_string();',to:'let url="https://127.0.0.1:1";',oracle:/invalid dsh boot URL scheme/},
];
for(const m of mutations){
 const invoke=env=>spawnSync(process.execPath,[path.join(repo,'tests/providers/lane-e/case.mjs'),m.provider,m.variant],{cwd:repo,env:{...process.env,LMMOCK_TEST_BINARY:binary,...env},encoding:'utf8'});
 const baseline=invoke({});assert.equal(baseline.status,m.variant==='nonzero'?1:0,baseline.stdout+baseline.stderr);assert.doesNotMatch(baseline.stderr,/FAIL lane-E/);
 const temp=fs.mkdtempSync(path.join(repo,'build','lane-e-handler-control-'));
 try{
  const sourceFile=path.join(temp,'handler.lms'),code=path.join(temp,'handler.lmcode');let source=fs.readFileSync(path.join(repo,'fixtures/providers/lane-e',m.file+'.lms'),'utf8');assert.ok(source.includes(m.from),'missing mutation anchor');
  const schema=path.join(repo,m.file==='dsh'?'examples/schema.json':'internal/rpcstdio/presets/schema.json').replaceAll('\\','/');source=source.replace(/schema="[^"]+"/,`schema=${JSON.stringify(schema)}`).replace(m.from,m.to);fs.writeFileSync(sourceFile,source);
  const command=process.execPath;const args=[path.join(repo,'scripts/capture-fixture.mjs'),sourceFile,code];
  const compile=spawnSync(command,args,{cwd:repo,env:{...process.env,LMSCRIPT_BIN:compiler},encoding:'utf8'});assert.equal(compile.status,0,compile.stdout+compile.stderr);
  const mutated=invoke({[m.file==='sidecar-boot'?'LANE_E_BOOT_OVERRIDE':'LANE_E_HANDLER_OVERRIDE']:code});assert.equal(mutated.status,1,'mutant survived');assert.match(mutated.stderr,/FAIL lane-E/);assert.match(mutated.stderr,m.oracle,'wrong mutation assertion');console.log('CAUGHT lane-E handler family',m.file);
 }finally{fs.rmSync(temp,{recursive:true,force:true});}
}
