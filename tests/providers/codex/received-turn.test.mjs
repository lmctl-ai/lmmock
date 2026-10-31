import assert from 'node:assert/strict';
import {PassThrough} from 'node:stream';
import {observeTurnStart} from './received-turn.mjs';
const stdout=new PassThrough();let parsed=false,calls=0;
let complete;
const done=new Promise(resolve=>{complete=resolve;});
// The observer is attached by spawn before the published parser attaches itself.
observeTurnStart(stdout,()=>{assert.equal(parsed,true,'signal must follow client callbacks and promise jobs');calls++;complete();});
stdout.on('data',()=>queueMicrotask(()=>{parsed=true;}));
stdout.write('{"method":"turn/sta');
assert.equal(calls,0,'partial record cannot trigger cancellation');
stdout.write('rted"}\n{"method":"turn/started"}\n');
await done;
assert.equal(calls,1,'one notification despite duplicate records');
stdout.end();
console.log('PASS received-record rendezvous follows parser promise jobs exactly once');
