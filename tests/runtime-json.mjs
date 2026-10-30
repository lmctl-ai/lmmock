import assert from 'node:assert/strict';
import {mkdtempSync,writeFileSync,rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join,resolve} from 'node:path';
import {spawnSync} from 'node:child_process';
const dir=mkdtempSync(join(tmpdir(),'lmmock-json-'));
const binary=resolve(process.argv[2]);
const greeting='runtime JSON: "hello"\nUnicode 🦊 and <metadata> {{literal}} $'+'{literal}';
const input='request JSON: "quoted"\\path\n<request.text> $'+'{literal}';
try {
 const suite=join(dir,'suite.json');
 writeFileSync(suite,JSON.stringify({steps:[
  {path:'/message',json:{message:greeting,'metadata.enabled':true,items:[]}},
  {path:'/message?text='+encodeURIComponent(input),json:{message:input,'items.0':input}},
  {path:'/message',json:{message:greeting,items:[]}},
 ]}));
 const result=spawnSync(binary,['exec','--script','examples/json-response.lms','--',binary,'check','--suite',suite],
 {env:{...process.env,LMMOCK_GREETING:greeting},encoding:'utf8',timeout:30000});
 assert.equal(result.status,0,result.stdout+result.stderr);
 console.log('PASS runtime lmscript JSON, typed updates, escaping and immutable fixtures');
} finally {rmSync(dir,{recursive:true,force:true});}
