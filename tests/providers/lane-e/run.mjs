import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {spawnSync} from 'node:child_process';
import {loadFixture,executeProcess,checkExpectation} from '../../provider-fixtures/run.mjs';
const repo=path.resolve(import.meta.dirname,'../../..');
const binary=path.resolve(process.argv[2]||'build/lmmock'+(process.platform==='win32'?'.exe':''));
process.env.LMMOCK_TEST_BINARY=binary;
const temp=fs.mkdtempSync(path.join(os.tmpdir(),'lane-e-contract-'));
let count=0;
try{
 for(const provider of ['agy','pi','lmplayer','dsh','muse']){
  const {manifest}=await loadFixture(path.join(repo,'fixtures/providers/lane-e/manifests',provider+'.json'),repo);
  const vars={repo_root:repo,tmpdir:temp,base_url:'http://127.0.0.1:1',lmmock_url:'http://127.0.0.1:1',server_url_env:''};
  for(const testCase of manifest.cases){
   const result=await executeProcess(testCase.request,repo,vars);
   await checkExpectation(testCase,result,vars);
   if(testCase.expect.error_class){const wrong=structuredClone(testCase);wrong.expect.error_class='binary_missing';await assert.rejects(()=>checkExpectation(wrong,result,vars),/error_class expected/);}
   console.log('PASS real published adapter',provider,testCase.id);count++;
  }
 }
 // Diagnostic only: the strict successful-reply oracle MUST reject this actual
 // immutable-client behavior. It is not counted as a qualified provider case.
 const gaps=[];
 for(const provider of ['agy','pi','lmplayer']){
  const r=spawnSync(process.execPath,[path.join(repo,'tests/providers/lane-e/case.mjs'),provider,'empty-gap'],{env:process.env,encoding:'utf8'});assert.equal(r.status,0,r.stderr);const gap=JSON.parse(r.stdout);assert.equal(gap.qualified,false);
  assert.throws(()=>assert.ok(gap.observed.status!=='ok'||gap.observed.replyText.length>0,'successful empty reply'),/successful empty reply/);
  gaps.push(gap);console.log('KNOWN CLIENT DEFECT (not qualified):',provider,'0.2.448 zero-exit empty output is accepted');
 }
 if(process.env.LANE_E_EVIDENCE)fs.writeFileSync(process.env.LANE_E_EVIDENCE,JSON.stringify({qualified:count,clientGaps:gaps},null,2)+'\n');
 console.log('PASS',count,'qualified lane-E cases; client defects reported separately');
}finally{fs.rmSync(temp,{recursive:true,force:true});}
