// Baselines and adverse cases pass through the same strict wire decoder.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import {checkProviderStream} from '../provider-fixtures/stream-contract.mjs';
const root=process.env.LMMOCK_URL,dialect=process.env.DIALECT,config=process.env.MOCK_CONFIG;
assert.ok(root&&config);assert.ok(['anthropic','gemini','ollama','miniplayer'].includes(dialect));
const original=JSON.parse(fs.readFileSync(config));
const set=(selected,patch={})=>fs.writeFileSync(config,JSON.stringify({...original,selected,...patch}));
function request(stream=true,key) {
 if(dialect==='anthropic')return ['/v1/messages',{'x-api-key':key??'fixture-anthropic-key','anthropic-version':'2023-06-01'},{model:'fixture-claude',messages:[{role:'user',content:'ping'}],stream}];
 if(dialect==='gemini')return [`/v1beta/models/fixture-gemini:${stream?'streamGenerateContent':'generateContent'}`,{'x-goog-api-key':key??'fixture-gemini-key'},{contents:[{parts:[{text:'ping'}]}]}];
 if(dialect==='ollama')return ['/api/chat',key?{authorization:'Bearer '+key}:{},{model:'fixture-ollama',messages:[{role:'user',content:'ping'}],stream}];
 return ['/v1/chat/completions',{authorization:'Bearer '+(key??'fixture-miniplayer-key'),'x-mock-provider':'deepseek'},{model:'deepseek-chat',messages:[{role:'user',content:'ping'}],stream}];
}
async function receive(stream=true,key) {
 const [path,headers,body]=request(stream,key);
 const r=await fetch(root+path,{method:'POST',headers:{'content-type':'application/json',...headers},body:JSON.stringify(body)});
 return {status:r.status,text:await r.text(),type:r.headers.get('content-type')};
}
const expected={dialect,text:'pong',input_tokens:10,output_tokens:2};
try {
 set('success');let good=await receive();assert.equal(good.status,200);checkProviderStream(good.text,expected,good.type);
 for(const fault of ['bad-usage','bad-event-ordering','wrong-content-type','missing-terminal']) {
  set(fault);const r=await receive(fault!=='wrong-content-type');assert.equal(r.status,200);
  assert.throws(()=>checkProviderStream(r.text,expected,r.type),undefined,`${dialect} ${fault} must fail the success decoder`);
  console.log('CAUGHT wire contract',dialect,fault);
 }
 set('success',dialect==='ollama'?{auth_mode:'bearer'}:{});
 const secret='synthetic-wrong-auth-value';const auth=await receive(false,secret);
 assert.equal(auth.status,dialect==='gemini'?403:401);assert.ok(!auth.text.includes(secret),'credential leakage');JSON.parse(auth.text);
 if(dialect==='miniplayer') {
  for(const provider of ['moonshot','openrouter','qwen','ollama']) {
   set('auth-error',{provider});const r=await fetch(root+'/v1/chat/completions',{method:'POST',headers:{'content-type':'application/json',authorization:'Bearer '+secret,'x-mock-provider':provider},body:JSON.stringify({model:'fixture-miniplayer',stream:false})});
   assert.equal(r.status,401);assert.ok(!(await r.text()).includes(secret));
  }
 }
 console.log('PASS',dialect,'baseline stream, adverse decoder controls and auth/redaction');
} finally { fs.writeFileSync(config,JSON.stringify(original)); }
