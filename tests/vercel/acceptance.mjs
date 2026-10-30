import assert from 'node:assert/strict';
import {writeFileSync} from 'node:fs';
import {generateText,streamText,embed,embedMany,tool,stepCountIs,Output} from 'ai';
import {createOpenAI} from '@ai-sdk/openai';
import {createOpenAICompatible} from '@ai-sdk/openai-compatible';
import {z} from 'zod';
const baseURL=process.env.LMMOCK_URL+'/v1';
assert.ok(process.env.LMMOCK_URL,'Run via tests/vercel/run.mjs');
const set=patch=>writeFileSync(process.env.MOCK_CONFIG,JSON.stringify({apiKey:'fixture-key',...patch}));
const openai=createOpenAI({baseURL,apiKey:'fixture-key'});
const compatible=createOpenAICompatible({name:'lmmock',baseURL,apiKey:'fixture-key',includeUsage:true,supportsStructuredOutputs:true});
const schema=z.object({city:z.string()});
const tools={weather:tool({description:'Weather in a city',inputSchema:schema,execute:async({city})=>({city,temperature:21})})};
let passed=0;
async function check(name,fn){set({});await fn();passed++;console.log('PASS '+name);}
for(const [name,model] of [['compatible',compatible.chatModel('mock-agent')],['openai-chat',openai.chat('mock-agent')],['openai-responses',openai('mock-agent')]]){
 await check(name+' generateText',async()=>{
  const r=await generateText({model,prompt:'ping',maxRetries:0});
  assert.equal(r.text,'pong'); assert.equal(r.finishReason,'stop');
  assert.equal(r.usage.inputTokens,2);
 });
 await check(name+' streamText',async()=>{
  const r=streamText({model,prompt:'ping',maxRetries:0});
  let text='';for await(const delta of r.textStream)text+=delta;
  assert.equal(text,'pong');assert.equal(await r.finishReason,'stop');
  assert.equal((await r.usage).inputTokens,2);
 });
 await check(name+' tool round trip',async()=>{
  const r=await generateText({model,prompt:'Weather in Paris?',tools,stopWhen:stepCountIs(3),maxRetries:0});
  assert.equal(r.steps.length,2);assert.equal(r.steps[0].toolCalls[0].toolName,'weather');
  assert.deepEqual(r.steps[0].toolCalls[0].input,{city:'Paris'});
  assert.match(r.text,/Tool result:.*Paris/);
 });
 await check(name+' streamed tool round trip',async()=>{
  const r=streamText({model,prompt:'Weather?',tools,stopWhen:stepCountIs(3),maxRetries:0});
  await r.consumeStream();
  assert.equal((await r.steps).length,2);assert.match(await r.text,/Tool result:.*Paris/);
 });
 await check(name+' tool choice none',async()=>{
  const r=await generateText({model,prompt:'ping',tools,toolChoice:'none',maxRetries:0});
  assert.equal(r.text,'pong');assert.equal(r.toolCalls.length,0);
 });
 await check(name+' structured output',async()=>{
  const r=await generateText({model,prompt:'City?',output:Output.object({schema}),maxRetries:0});
  assert.deepEqual(r.output,{city:'Paris'});
 });
}
for(const [name,model] of [['compatible',compatible.embeddingModel('mock-embedding')],['openai',openai.embedding('mock-embedding')]]){
 await check(name+' embeddings',async()=>{
  const single=await embed({model,value:'one',maxRetries:0});
  assert.deepEqual(single.embedding,[0.1,0.2,0.3]);
  const batch=await embedMany({model,values:['one','two'],maxRetries:0});
  assert.deepEqual(batch.embeddings,[[0.1,0.2,0.3],[0.1,0.2,0.3]]);
 });
}
await check('model discovery',async()=>{
 const r=await fetch(baseURL+'/models',{headers:{Authorization:'Bearer fixture-key'}});
 assert.equal(r.status,200);assert.equal((await r.json()).data[0].id,'mock-agent');
});
for(const [mode,status] of [['unauthorized',401],['rate-limit',429],['server-error',503]]){
 await check('SDK error '+mode,async()=>{
  set({mode});
  await assert.rejects(generateText({model:compatible('mock-agent'),prompt:'ping',maxRetries:0}),e=>e.statusCode===status);
 });
}
await check('invalid JSON is rejected',async()=>{
 set({mode:'bad-json'});
 await assert.rejects(generateText({model:compatible('mock-agent'),prompt:'ping',maxRetries:0}));
});
await check('custom scripted text',async()=>{
 set({text:'custom reply'});
 assert.equal((await generateText({model:openai('mock-agent'),prompt:'ping',maxRetries:0})).text,'custom reply');
});

await check('retry recovers',async()=>{
 set({mode:'retry'});
 const r=await generateText({model:compatible('mock-agent'),prompt:'ping',maxRetries:1});
 assert.equal(r.text,'pong');
});
await check('disconnect is rejected',async()=>{
 set({mode:'disconnect'});
 await assert.rejects(generateText({model:compatible('mock-agent'),prompt:'ping',maxRetries:0}));
});
await check('client deadline aborts slow handler',async()=>{
 set({mode:'timeout'});
 await assert.rejects(generateText({model:compatible('mock-agent'),prompt:'ping',maxRetries:0,abortSignal:AbortSignal.timeout(50)}));
});
await check('real bad credential',async()=>{
 const wrong=createOpenAICompatible({name:'bad',baseURL,apiKey:'wrong'});
 await assert.rejects(generateText({model:wrong('mock-agent'),prompt:'ping',maxRetries:0}),e=>e.statusCode===401 && !e.message.includes('wrong'));
});
console.log('PASS Vercel AI SDK acceptance: '+passed+' scenarios');
