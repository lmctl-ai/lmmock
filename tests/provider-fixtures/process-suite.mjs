// Process manifests own their stdio peers; never start an HTTP listener for them.
import path from 'node:path';
import fs from 'node:fs';
export async function runProcessSuite(loaded,tempDir,{executeProcess,checkExpectation}){
 const {manifest,root}=loaded;if(manifest.server.kind!=='stdio')throw Error('owned process suite requires stdio manifest');const vars={repo_root:root,tmpdir:tempDir,base_url:'',lmmock_url:'',server_url_env:'',node_binary:process.execPath};let passed=0;const failures=[];
 for(const testCase of manifest.cases){
  if(testCase.request.kind!=='process')throw Error('stdio manifest requires process cases');
  try{
   if(!['node','{{node_binary}}'].includes(testCase.request.executable))throw Error('stdio process case must use a repo-owned Node harness; refusing a bare provider command');
   const entry=fs.realpathSync(path.resolve(root,testCase.request.argv[0].replaceAll('{{repo_root}}',root)));const tests=fs.realpathSync(path.join(root,'tests'));
   if(!entry.startsWith(tests+path.sep))throw Error('process harness escapes fixture tests directory');
   const request={...testCase.request,executable:process.execPath,argv:[entry,...testCase.request.argv.slice(1)]};
   const result=await executeProcess(request,root,vars);await checkExpectation(testCase,result,vars);passed++;console.log('PASS '+manifest.id+'/'+testCase.id);}catch(e){failures.push(String(e));}
 }
 if(failures.length)throw Error(failures.join('\n'));return {passed,failed:0};
}
