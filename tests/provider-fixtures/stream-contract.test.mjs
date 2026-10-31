import assert from 'node:assert/strict';
import fs from 'node:fs';
import test from 'node:test';
import {checkExpectation} from './run.mjs';
for(const dialect of ['anthropic','gemini','ollama','miniplayer']) {
 const good=JSON.parse(fs.readFileSync(new URL(`../../fixtures/${dialect}/wire.json`,import.meta.url))).cases.success.events;
 const c={id:dialect,request:{kind:'http'},expect:{stream:{dialect,text:'pong',input_tokens:10,output_tokens:2}}};
 const result=text=>({text,status:200,headers:new Headers({'content-type':dialect==='ollama'?'application/x-ndjson':'text/event-stream'})});
 test(dialect+' stream contract catches corrupted success, usage, order and terminal',async()=>{
  await checkExpectation(c,result(good.join('')),{});
  for(const rows of [good.map(e=>e.replaceAll('{','{INVALID ')),good.map(e=>e.replaceAll('pong','broken')),good.slice(0,-1),[...good].reverse(),good.map(e=>e.replaceAll(':10',':999'))]) {
   await assert.rejects(()=>checkExpectation(c,result(rows.join('')),{}),undefined,'corrupt stream must fail '+dialect);
  }
 });
}
