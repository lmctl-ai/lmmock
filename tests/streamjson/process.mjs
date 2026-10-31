import assert from 'node:assert/strict';
import fs from 'node:fs';import os from 'node:os';import path from 'node:path';import {spawnSync} from 'node:child_process';
const binary=path.resolve(process.argv[2]||'build/lmmock'), code=path.resolve(process.argv[3]||'tests/streamjson/handler.dslb');
const dir=fs.mkdtempSync(path.join(os.tmpdir(),'lmmock-jsonlines-'));
try{
 const fixture=path.join(dir,'config.json'),audit=path.join(dir,'audit.jsonl');fs.writeFileSync(audit,'');
 fs.writeFileSync(fixture,JSON.stringify({audit,init:{events:[{type:'system',subtype:'init',session_id:'fixture'}]},turns:[{expect:'ping',events:[{type:'result',result:'pong'}],close:true}]}));
 const p=spawnSync(binary,['-p','','--input-format','stream-json'],{env:{...process.env,LMMOCK_JSONLINES_PROGRAM:code,LMMOCK_JSONLINES_SCENARIO:fixture},input:'{"type":"user","message":{"role":"user","content":"ping"}}\n',encoding:'utf8'});
 assert.equal(p.status,0,p.stdout+p.stderr);assert.deepEqual(p.stdout.trim().split('\n').map(JSON.parse),[{type:'system',subtype:'init',session_id:'fixture'},{type:'result',result:'pong'}]);
 assert.equal(fs.readFileSync(audit,'utf8').trim().split('\n').length,2);console.log('PASS generic JSONL annotated start/message dispatch');
}finally{fs.rmSync(dir,{recursive:true,force:true});}
