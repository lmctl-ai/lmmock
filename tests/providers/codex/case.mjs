import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import {root,runCase} from './client.mjs';
const manifest=JSON.parse(fs.readFileSync(path.join(root,'fixtures/providers/codex-app-server.json')));
const id=process.argv.at(-1);const test=manifest.cases.find(c=>c.id===id);assert.ok(test,'declared Codex case required');
const vars=test.request.env;const e=runCase(vars.LMMOCK_CODEX_CASE,{model:vars.LMMOCK_CODEX_MODEL,resume:vars.LMMOCK_CODEX_MODE==='resume',batch:vars.LMMOCK_CODEX_MODE==='batch'});
try{
 assert.equal(e.exit,0,'published lmscript must finish its result probe: '+e.stderr);
 assert.ok(e.result?.result,'published provider result map is required');
 assert.equal(e.stdout.trim().split('\n').length,1,'exactly one published probe result line');
 if(vars.LMMOCK_CODEX_MODE==='batch')assert.equal(e.stderr,'');
 else assert.match(e.stderr,/^agent \d{2}:\d{2}:\d{2}: prompting Worker\r?\n\s*$/,'unexpected provider stderr');
 assert.equal(e.requests[0].method,'initialize');assert.equal(e.requests[0].params.clientInfo.name,'lmctl');
 assert.equal(JSON.parse(fs.readFileSync(path.join(process.env.LMCTL_PACKAGE_ROOT,'package.json'))).version,'0.2.448','pinned published package identity');
 assert.equal(e.requests[0].params.clientInfo.version,'0.0.0','published bundle handshake fallback is pinned separately from package identity');
 assert.deepEqual(e.argv,['app-server','--listen','stdio://','-c','model='+JSON.stringify(vars.LMMOCK_CODEX_MODEL)],'real app-server argv and model selection');
 assert.equal(e.requests[1].method,'initialized');
 const open=e.requests[2];assert.equal(open.method,vars.LMMOCK_CODEX_MODE==='agent'?'thread/start':'thread/resume');
 if(open.method==='thread/resume')assert.equal(open.params.threadId,'11111111-1111-4111-8111-111111111111');
 const turns=e.requests.filter(r=>r.method==='turn/start');
 assert.equal(turns.length,vars.LMMOCK_CODEX_MODE==='batch'?2:1);
 if(turns.length===2){assert.equal(turns[1].params.input[0].text,'pong','turn2 depends on real turn1 reply');assert.equal(e.result.second.reply,'dependent answer');}
 assert.equal(e.rows.length,turns.length,'one accounting/transcript row per accepted turn');
 for(const row of e.rows){assert.ok(row.attempt_key);assert.equal(row.provider,'codex');assert.equal(row.native_session_id,'11111111-1111-4111-8111-111111111111','recorded native session identity');assert.equal(row.model_requested,vars.LMMOCK_CODEX_MODEL);}
 assert.equal(e.rows[0].message,'ping');
 const row=e.rows[0];const provider=e.result.result;
 if(provider.status==='ok')assert.equal(e.result.member.sessionid,'11111111-1111-4111-8111-111111111111');
 if(process.argv.includes('--require-error-classes')){
  assert.equal(provider.status,'error',id+': malformed usage must not be accepted as a successful turn');
  assert.match(e.result.failure?.class??'',/protocol|usage/i,id+': specialized protocol/usage error_class is required');
 }
 if(id==='malformed-usage'){
  assert.equal(row.tok_input_uncached,null,'malformed input cannot become a numeric count');assert.equal(row.cost_reason,'usage_unavailable');
 }else if(provider.status==='ok'){
  for(const r of e.rows)assert.deepEqual([r.tok_input_uncached,r.tok_cache_read,r.tok_output,r.tok_reasoning],[70,30,12,4],'Codex exact input/cache/output/reasoning facts');
 }
 if(id==='foreign-event')assert.equal(provider.reply,'pong','foreign turn cannot contaminate output');
 if(id==='wrong-turn-id'||id==='missing-terminal'){assert.equal(provider.status,'error');assert.match(provider.error,/before completing the turn/);}
 if(id==='app-error'){assert.equal(provider.status,'error');assert.match(provider.error,/Turn errored: fixture app-server failure/);}
 if(id==='rpc-error'){assert.equal(provider.status,'error');assert.match(provider.error,/codex app-server error: fixture app-server RPC failure/);}
 const usage={input_tokens:row.tok_input_uncached===null?null:row.tok_input_uncached+row.tok_cache_read,cached_input_tokens:row.tok_cache_read,output_tokens:row.tok_output,reasoning_output_tokens:row.tok_reasoning};
 process.stdout.write(JSON.stringify({provider,row_count:e.rows.length,model:row.model_reported,session_id:e.result.member.sessionid??null,usage,usage_reason:row.usage_reason,cost_reason:row.cost_reason,error_class:provider.status==='ok'?null:e.result.failure?.class??null})+'\n');
}finally{e.cleanup();}
