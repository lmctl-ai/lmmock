import {StringDecoder} from 'node:string_decoder';
// The signal rendezvous belongs after client data/line callbacks and their
// promise jobs. It observes bytes without consuming or replacing the stream.
export function observeTurnStart(stdout, notify) {
 if(!stdout)return;
 const decoder=new StringDecoder('utf8');let pending='',observed=false;
 stdout.on('data',chunk=>{
  pending+=typeof chunk==='string'?chunk:decoder.write(chunk);let end;
  while((end=pending.indexOf('\n'))!==-1){
   const line=pending.slice(0,end);pending=pending.slice(end+1);
   let message;try{message=JSON.parse(line);}catch{continue;}
   if(!observed&&message.method==='turn/started'){
    observed=true;setImmediate(notify);
   }
  }
 });
}
