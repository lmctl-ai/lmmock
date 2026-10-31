// Strict commands remain executable and fail semantically on the pinned client.
// These gaps are reported as unmet acceptance, never as specialized-class coverage.
import assert from 'node:assert/strict';
import path from 'node:path';
import fs from 'node:fs/promises';
import os from 'node:os';
import {spawnSync} from 'node:child_process';
import {root} from './client.mjs';
import {loadFixture,checkExpectation,executeProcess} from '../../provider-fixtures/run.mjs';
const {manifest}=await loadFixture('fixtures/providers/codex-app-server.json',root);
const {runProcessSuite}=await import('../../provider-fixtures/process-suite.mjs');
const tmp=await fs.mkdtemp(path.join(os.tmpdir(),'codex-manifest-'));
try{await runProcessSuite({manifest,root},tmp,{executeProcess:async(...args)=>{const result=await executeProcess(...args);if(result.exitCode!==0)process.stderr.write(result.stderr);return result;},checkExpectation});}
finally{await fs.rm(tmp,{recursive:true,force:true});}
const auth=manifest.cases.find(c=>c.id==='auth-error');
const actual=spawnSync(process.execPath,[path.join(root,'tests/providers/codex/case.mjs'),'auth-error'],{cwd:root,env:process.env,encoding:'utf8'});
assert.equal(actual.status,0,actual.stdout+actual.stderr);
await checkExpectation(auth,{exitCode:actual.status,stdout:actual.stdout,stderr:actual.stderr},{tmpdir:root});
await assert.rejects(()=>checkExpectation({...auth,expect:{...auth.expect,error_class:'quota_exhausted'}},{exitCode:actual.status,stdout:actual.stdout,stderr:actual.stderr},{tmpdir:root}),/error_class expected/);
console.log('CAUGHT wrong error_class expectation on real published auth failure');
for(const id of ['malformed-usage','wrong-turn-id']){
 const r=spawnSync(process.execPath,[path.join(root,'tests/providers/codex/case.mjs'),'--require-error-classes',id],{cwd:root,env:process.env,encoding:'utf8'});
 assert.equal(r.status,1,'strict client contract must expose the pinned gap: '+r.stdout+r.stderr);
 assert.match(r.stderr,/AssertionError/);
 assert.match(r.stderr,id==='malformed-usage'?/malformed usage must not be accepted/:/specialized protocol\/usage error_class is required/);
 console.log('KNOWN CLIENT GAP '+id+': strict named assertion fires; specialized error_class NOT qualified');
}
let r=spawnSync(process.execPath,[path.join(root,'tests/providers/codex/interrupt.mjs'),'--require-finished-attempt'],{cwd:root,env:process.env,encoding:'utf8'});
if(r.status===1){assert.match(r.stderr,/published cancellation must finish its accounting attempt/);console.log('KNOWN CLIENT GAP cancellation: strict finished-attempt assertion fires');}
else {assert.equal(r.status,0,r.stdout+r.stderr);console.log('PASS finished cancellation on this platform; cross-platform settlement not inferred');}
r=spawnSync(process.execPath,[path.join(root,'tests/providers/codex/interrupt.mjs')],{cwd:root,env:process.env,encoding:'utf8'});
assert.equal(r.status,0,r.stdout+r.stderr);process.stdout.write(r.stdout);
