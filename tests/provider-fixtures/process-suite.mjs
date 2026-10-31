// Process manifests own their stdio peers; never start an HTTP listener for them.
import path from 'node:path';
import fs from 'node:fs';
import {executeProcess,checkExpectation} from './run.mjs';
export async function runProcessSuite(loaded,tempDir){
 const {manifest,root}=loaded;const vars={repo_root:root,tmpdir:tempDir,base_url:'',lmmock_url:'',server_url_env:''};let passed=0;const failures=[];
 for(const testCase of manifest.cases){
  if(testCase.request.kind!=='process')throw Error('stdio manifest requires process cases');
  try{
   if(testCase.request.executable!=='node')throw Error('stdio process case must use a repo-owned Node harness; refusing a bare provider command');
   const entry=fs.realpathSync(path.resolve(root,testCase.request.argv[0]));const tests=fs.realpathSync(path.join(root,'tests'));
   if(!entry.startsWith(tests+path.sep))throw Error('process harness escapes fixture tests directory');
   const request={...testCase.request,executable:process.execPath,argv:[entry,...testCase.request.argv.slice(1)]};
   const result=await executeProcess(request,root,vars);await checkExpectation(testCase,result,vars);passed++;console.log('PASS '+manifest.id+'/'+testCase.id);}catch(e){failures.push(String(e));}
 }
 if(failures.length)throw Error(failures.join('\n'));return {passed,failed:0};
}
