// Strict wire qualification for the canned text streams; this is not an SDK parser.
// SDK qualification is independent and must not inherit this oracle's result.
import assert from 'node:assert/strict';
export function decodeProviderStream(text, dialect, contentType) {
 assert.ok(['anthropic','gemini','ollama','miniplayer'].includes(dialect),'known stream dialect');
 assert.ok(contentType?.split(';')[0] === (dialect==='ollama'?'application/x-ndjson':'text/event-stream'),'stream content-type');
 const records=dialect==='ollama'?text.trim().split(/\r?\n/):text.trim().split(/\r?\n\r?\n/).map(record=>{
  const data=record.split(/\r?\n/).filter(l=>l.startsWith('data:')).map(l=>l.slice(5).trimStart()).join('\n');
  assert.ok(data,'SSE data required');return data;
 });
 let output='',input,completion,terminal=false,finish=false,started=false,block=false,delta=false;
 for(const raw of records) {
  assert.equal(terminal,false,'event after stream terminal');
  if(dialect==='miniplayer'&&raw==='[DONE]') {assert.ok(finish,'terminal before finish');terminal=true;continue;}
  const r=JSON.parse(raw);assert.ok(r&&typeof r==='object','stream JSON object');
  if(dialect==='anthropic') {
   switch(r.type) {
    case 'message_start': assert.equal(started,false,'duplicate message start');started=true;input=r.message.usage.input_tokens;break;
    case 'content_block_start': assert.ok(started&&!block&&!finish,'block start ordering');block=true;break;
    case 'content_block_delta': assert.ok(block&&!finish,'delta ordering');assert.equal(typeof r.delta.text,'string');output+=r.delta.text;break;
    case 'content_block_stop': assert.ok(block&&!finish,'block stop ordering');block=false;break;
    case 'message_delta': assert.ok(started&&!block&&!finish,'message finish ordering');completion=r.usage.output_tokens;finish=true;break;
    case 'message_stop': assert.ok(finish,'terminal before finish');terminal=true;break;
    default: throw Error('unexpected Anthropic text event');
   }
  } else if(dialect==='gemini') {
   assert.equal(finish,false,'event after finish');
   for(const candidate of r.candidates??[]) {
    for(const part of candidate.content?.parts??[]) {assert.equal(typeof part.text,'string');output+=part.text;delta=true;}
    if(candidate.finishReason) {assert.ok(delta,'finish before content');finish=true;terminal=true;}
   }
   if(r.usageMetadata) {input=r.usageMetadata.promptTokenCount;completion=r.usageMetadata.candidatesTokenCount;}
  } else if(dialect==='ollama') {
   assert.equal(typeof r.done,'boolean','done must be boolean');
   const value=r.response??r.message?.content;assert.equal(typeof value,'string');output+=value;
   if(r.done) {assert.ok(delta,'terminal before content');terminal=true;input=r.prompt_eval_count;completion=r.eval_count;}else delta=true;
  } else {
   for(const choice of r.choices??[]) {
    if(choice.delta?.content) {assert.equal(finish,false,'content after finish');output+=choice.delta.content;delta=true;}
    if(choice.finish_reason) {assert.ok(delta&&!finish,'finish ordering');finish=true;}
   }
   if(r.usage) {assert.ok(finish,'usage before finish');input=r.usage.prompt_tokens;completion=r.usage.completion_tokens;}
  }
 }
 assert.ok(terminal,'missing stream terminal');
 for(const count of [input,completion]) assert.ok(Number.isSafeInteger(count)&&count>=0,'invalid stream usage');
 return {text:output,input_tokens:input,output_tokens:completion};
}
export function checkProviderStream(text, expected, contentType) {
 const actual=decodeProviderStream(text,expected.dialect,contentType);
 for(const key of ['text','input_tokens','output_tokens']) assert.equal(actual[key],expected[key],`stream ${key}`);
 return actual;
}
