import test from 'node:test';import assert from 'node:assert/strict';import path from 'node:path';import {fileURLToPath} from 'node:url';
import {runProcessSuite} from './process-suite.mjs';
const root=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'../..');
function loaded(executable,entry){return {root,manifest:{id:'owned-process-control',server:{kind:'stdio'},cases:[{id:'probe',request:{kind:'process',executable,argv:[entry]},expect:{}}]}};}
test('owned Node placeholders resolve before containment and process checks',async()=>{
 let calls=0;const result=await runProcessSuite(loaded('{{node_binary}}','{{repo_root}}/tests/streamjson/process.mjs'),root,{executeProcess:async(request,base,vars)=>{calls++;assert.equal(request.executable,process.execPath);assert.equal(vars.node_binary,process.execPath);assert.equal(request.argv[0],path.join(root,'tests/streamjson/process.mjs'));return {exitCode:0,stdout:'{}',stderr:''};},checkExpectation:async()=>{}});
 assert.equal(calls,1);assert.deepEqual(result,{passed:1,failed:0});
});
test('process ownership refuses a bare provider without spawning',async()=>{
 await assert.rejects(()=>runProcessSuite(loaded('codex','tests/streamjson/process.mjs'),root,{executeProcess:async()=>{assert.fail('must not spawn');},checkExpectation:async()=>{}}),/refusing a bare provider command/);
});
test('Node ownership still refuses an entrypoint outside tests',async()=>{
 await assert.rejects(()=>runProcessSuite(loaded('node','{{repo_root}}/README.md'),root,{executeProcess:async()=>{assert.fail('must not spawn');},checkExpectation:async()=>{}}),/escapes fixture tests directory/);
});
