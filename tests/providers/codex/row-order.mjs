// A covering attempt-key index must not reorder sequential turn assertions.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {pathToFileURL} from 'node:url';
import {DatabaseSync} from 'node:sqlite';
import {prepareCase,root} from './client.mjs';
async function ordered(make){
 const c=make('success');
 const db=new DatabaseSync(path.join(c.dir,'state.db'));
 db.exec('CREATE TABLE team_chat_log(id INTEGER PRIMARY KEY,attempt_key TEXT UNIQUE); INSERT INTO team_chat_log VALUES(1,\'z-last-key\'),(2,\'a-first-key\');');db.close();
 const evidence=c.collect({status:0,signal:null,stdout:'{}',stderr:''});
 try{assert.deepEqual(evidence.rows.map(r=>r.id),[1,2],'chronological row order must not follow random attempt keys');}finally{evidence.cleanup();}
}
await ordered(prepareCase);
const temp=fs.mkdtempSync(path.join(os.tmpdir(),'codex-row-order-'));
try{
 const source=fs.readFileSync(path.join(root,'tests/providers/codex/client.mjs'),'utf8');assert.ok(source.includes('ORDER BY id'));
 const file=path.join(temp,'client.mjs');fs.writeFileSync(file,source.replace(' ORDER BY id',' ORDER BY attempt_key').replace("export const root=path.resolve(import.meta.dirname,'../../..');",'export const root='+JSON.stringify(root)+';'));
 const mutant=await import(pathToFileURL(file).href);
 await assert.rejects(ordered(mutant.prepareCase),/chronological row order/);
 console.log('PASS ordered transcript rows; CAUGHT attempt-key ordering with reversed keys');
}finally{fs.rmSync(temp,{recursive:true,force:true});}
