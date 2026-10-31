import assert from 'node:assert/strict';
const root=process.env.LMMOCK_URL;
const post=async(path,body,headers={})=>fetch(root+path,{method:'POST',headers:{'content-type':'application/json',...headers},body:JSON.stringify(body)});
if(process.env.DIALECT==='gemini') {
 for(const op of ['generateContent','streamGenerateContent']) {
  const r=await post('/v1beta/models/unknown:'+op,{contents:[]},{'x-goog-api-key':'fixture-gemini-key'});
  assert.equal(r.status,404,'unknown Gemini model must be rejected');assert.equal((await r.json()).error.status,'NOT_FOUND');
 }
 const r=await post('/v1beta/models/gemini-2.5-flash:generateContent',{contents:[]},{'x-goog-api-key':'fixture-gemini-key'});assert.equal(r.status,200);
} else {
 const r=await post('/api/chat',{model:'bad"model\\name',stream:false,messages:[]});assert.equal(r.status,404);assert.equal((await r.json()).error,'model \'bad"model\\name\' not found');
 for(const stream of [false,true]) {
  const response=await post('/api/generate',{model:'fixture-ollama',prompt:'ping',stream});assert.equal(response.status,200);
  const rows=(await response.text()).trim().split('\n').map(JSON.parse);assert.equal(rows.map(r=>r.response).join(''),'pong');
  for(const row of rows) assert.equal(row.message,undefined,'generate must not emit chat messages');assert.equal(rows.at(-1).done,true);
 }
}
console.log('PASS native dialect regressions',process.env.DIALECT);
