import assert from 'node:assert/strict';
import fs from 'node:fs';
import {createRequire} from 'node:module';
const require=createRequire(new URL('../vercel/package.json',import.meta.url));
const {createOpenAI}=require('@ai-sdk/openai');
const {generateText,streamText}=require('ai');
const config=process.env.MOCK_CONFIG;
const original=JSON.parse(fs.readFileSync(config));
const set=selected=>fs.writeFileSync(config,JSON.stringify({...original,selected}));
for(const dialect of ['chat','responses']) {
 const provider=createOpenAI({baseURL:process.env.LMMOCK_URL+'/v1',apiKey:original.keys[dialect]});
 const model=dialect==='chat'?provider.chat('fixture-gpt'):provider.responses('fixture-gpt');
 set('success');
 const good=await generateText({model,prompt:'ping',maxRetries:0});
 assert.equal(good.text,'pong');assert.equal(good.usage.inputTokens,10);assert.equal(good.usage.outputTokens,2);
 const s=streamText({model,prompt:'ping',maxRetries:0});
 await s.consumeStream();assert.equal(await s.text,'pong');assert.equal((await s.usage).totalTokens,12);
 set('tools');
 // Low-level SDK parser preserves tool arguments without executing a tool.
 const tool=await model.doGenerate({prompt:[{role:'user',content:[{type:'text',text:'weather'}]}]});
 const call=tool.content.find(v=>v.type==='tool-call');assert.equal(call.toolName,'weather');assert.equal(call.input,'{"city":"Paris"}');
 const streamed=await model.doStream({prompt:[{role:'user',content:[{type:'text',text:'weather'}]}]});
 const parts=[];for await(const part of streamed.stream)parts.push(part);
 assert.ok(parts.some(v=>v.type==='tool-call'&&v.toolName==='weather'),dialect+' streamed tool');
 for(const adverse of ['bad-usage','auth-error','rate-limit','server-error']) {
  set(adverse);
  let error;try{await generateText({model,prompt:'ping',maxRetries:0});}catch(e){error=e;}
  assert.ok(error,dialect+' '+adverse+' must reject');
  assert.equal(error.name,'AI_APICallError',dialect+' '+adverse+' SDK error class');
  if(adverse==='rate-limit'){assert.equal(error.statusCode,429);assert.equal(error.responseHeaders['x-ratelimit-remaining-requests'],'0');assert.equal(error.responseHeaders['retry-after'],'0');}
 }
 set('bad-terminal');
 const errors=[];const bad=streamText({model,prompt:'ping',maxRetries:0,onError:({error})=>errors.push(error)});
 await bad.consumeStream();assert.ok(errors.length,dialect+' malformed terminal must signal parser error');
 assert.equal(errors[0].name,'AI_TypeValidationError',dialect+' terminal error class');
 console.log('PASS '+dialect+' JSON/SSE, tools, usage, HTTP failures and malformed terminal: '+errors.map(e=>e.name).join(','));
}
set('success');
