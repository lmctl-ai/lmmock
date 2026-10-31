// Portable delivery of the same SIGINT event handled by the unmodified published runner.
import path from 'node:path';
import {pathToFileURL} from 'node:url';
const entry=path.join(process.argv[3],'bin/lmscript');
process.argv=[process.execPath,entry,'--no-preflight',process.argv[2]];
process.on('message',message=>{if(message?.signal==='SIGINT')process.emit('SIGINT');});
try{await import(pathToFileURL(entry).href);}finally{if(process.connected)process.disconnect();}
