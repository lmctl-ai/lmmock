// Independent pinned SDK parsing, separate from the strict wire-contract oracle.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import {createRequire} from 'node:module';
const require=createRequire(new URL('../vercel/package.json',import.meta.url));
const {streamText}=require('ai');
const {createAnthropic}=require('@ai-sdk/anthropic');
const {createGoogleGenerativeAI}=require('@ai-sdk/google');
const {createOpenAICompatible}=require('@ai-sdk/openai-compatible');
const dialect=process.env.DIALECT,root=process.env.LMMOCK_URL,config=process.env.MOCK_CONFIG;
const original=fs.readFileSync(config);const wire=JSON.parse(original);
const model=dialect==='anthropic'?createAnthropic({baseURL:root+'/v1',apiKey:wire.key})('fixture-claude'):
 dialect==='gemini'?createGoogleGenerativeAI({baseURL:root+'/v1beta',apiKey:wire.key})('fixture-gemini'):
 createOpenAICompatible({name:'fixture',baseURL:root+'/v1',apiKey:wire.key,includeUsage:true})('deepseek-chat');
async function parse(selected,invalid=false) {
 const data={...wire,selected};if(invalid)data.cases.success.events=data.cases.success.events.map(e=>e.replaceAll('{','{INVALID '));
 fs.writeFileSync(config,JSON.stringify(data));const errors=[];
 const r=streamText({model,prompt:'ping',maxRetries:0,onError:({error})=>errors.push(error)});
 let text='';for await(const chunk of r.textStream)text+=chunk;
 if(errors.length)throw errors[0];return {text,usage:await r.usage};
}
try {
 const good=await parse('success');assert.equal(good.text,'pong');assert.equal(good.usage.inputTokens,10);assert.equal(good.usage.outputTokens,2);
 await assert.rejects(()=>parse('bad-usage'),undefined,'SDK must reject malformed usage');
 await assert.rejects(()=>parse('success',true),undefined,'SDK must reject invalid JSON');
 console.log('PASS independent SDK stream text/usage and malformed usage/JSON',dialect);
}finally{fs.writeFileSync(config,original);}
