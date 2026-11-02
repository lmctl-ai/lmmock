// Portable delivery of the same SIGINT event handled by the unmodified published runner.
import path from 'node:path';
import {pathToFileURL} from 'node:url';
import childProcess from 'node:child_process';
import {syncBuiltinESMExports} from 'node:module';
import {observeTurnStart} from './received-turn.mjs';
// Observe delivery without replacing the native process or altering its bytes.
// The immediate runs after the client's data/line callbacks and promise jobs,
// unlike observing a server write before Windows has delivered the client pipe.
const nativeSpawn=childProcess.spawn;
let observed=false;
childProcess.spawn=function(...args){
 const child=nativeSpawn.apply(this,args);
 observeTurnStart(child.stdout,()=>{
  if(!observed&&process.connected){observed=true;process.send({observed:'turn/started'});}
 });
 return child;
};
syncBuiltinESMExports();
const entry=path.join(process.argv[3],'bin/lmscript');
process.argv=[process.execPath,entry,process.argv[2]];
process.on('message',message=>{if(message?.signal==='SIGINT')process.emit('SIGINT');});
try{await import(pathToFileURL(entry).href);}finally{if(process.connected)process.disconnect();}
