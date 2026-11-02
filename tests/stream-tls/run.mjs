import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
const binary=path.resolve(process.argv[2]);
const scratch=fs.mkdtempSync(path.join(os.tmpdir(),'lmmock stream TLS '));
try{
 const result=path.join(scratch,'client.json');
 const call=spawnSync(binary,['exec','--tls','--tls-host','fixed.mock.test','--lmcode','build/streaming.lmcode','--',process.execPath,'tests/stream-tls/client.mjs'],{env:{...process.env,LMMOCK_TEST_RESULT:result,TMPDIR:scratch,TMP:scratch,TEMP:scratch},encoding:'utf8'});
 assert.equal(call.status,0,call.stderr+call.stdout);
 const info=JSON.parse(fs.readFileSync(result,'utf8'));
 assert.equal(fs.existsSync(info.ca),false,'per-run CA survived exec child close');
 console.log(call.stdout.trim());
 for(const args of [['--tls-host','fixed.mock.test'],['--tls','--tls-host','https://bad']]){
  const refused=spawnSync(binary,['exec',...args,'--lmcode','build/streaming.lmcode','--',process.execPath,'-e','process.exit(0)'],{encoding:'utf8'});
  assert.notEqual(refused.status,0,'invalid TLS configuration was accepted');
 }
 console.log('PASS TLS lifecycle, spaces in CA path, invalid configuration controls');
}finally{fs.rmSync(scratch,{recursive:true,force:true});}
