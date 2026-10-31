// Wire oracle independent of the handler: changing auth or usage must fail.
import assert from 'node:assert/strict';
const root=process.env.LMMOCK_URL;
assert.ok(root,'run inside lmmock exec');
for(const [route,key] of [['chat/completions','fixture-chat-key'],['responses','fixture-responses-key']]) {
 const response=await fetch(root+'/v1/'+route,{method:'POST',headers:{authorization:'Bearer '+key,'content-type':'application/json'},body:JSON.stringify({model:'fixture-gpt',messages:[],input:'ping'})});
 assert.equal(response.status,200,route+' authenticated success');
 const body=await response.json();
 assert.equal(body.usage.total_tokens,12,route+' total token contract');
 const bad=await fetch(root+'/v1/'+route,{method:'POST',headers:{authorization:'Bearer wrong','content-type':'application/json'},body:'{}'});
 assert.equal(bad.status,401,route+' wrong auth refusal');
 const other=route==='responses'?'fixture-chat-key':'fixture-responses-key';
 const swapped=await fetch(root+'/v1/'+route,{method:'POST',headers:{authorization:'Bearer '+other},body:'{}'});
 assert.equal(swapped.status,401,route+' must not accept the other route key');
}
console.log('PASS OpenAI route-specific auth and token contract');
